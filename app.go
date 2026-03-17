package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"crypto-core/keymanager"

	"github.com/LazarenkoA/prometheus_1C_exporter/explorers/model"
	"github.com/prometheus/client_golang/prometheus"

	expl "github.com/LazarenkoA/prometheus_1C_exporter/explorers"
	"github.com/LazarenkoA/prometheus_1C_exporter/logger"
	"github.com/LazarenkoA/prometheus_1C_exporter/settings"
	"github.com/judwhite/go-svc"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type app struct {
	settings    *settings.Settings
	metric      *expl.Metrics
	httpSrv     *http.Server
	port        string
	ctx         context.Context
	cancel      context.CancelFunc
	osRegistry  *prometheus.Registry
	racRegistry *prometheus.Registry
	keyManager  *keymanager.KeyManager
}

func (a *app) Init(_ svc.Environment) (err error) {

	expl.InitMeterFunctions()
	a.metric, err = new(expl.Metrics).FillMetrics(a.settings)
	if err != nil {
		return err
	}

	km, err := keymanager.NewKeyManager(a.settings.RAC.Host, a.settings.RAC.Port)
	a.keyManager = km
	if err != nil {
		logger.Errorf("Failed to init keymanager: %v", err)
	}

	// Попытка загрузить последние секреты с диска
	if a.keyManager != nil {
		decrypted, err := a.keyManager.LoadLastSecrets()
		if err == nil {
			var secrets settings.IBCredentials
			if json.Unmarshal(decrypted, &secrets) == nil {
				a.settings.UpdateSecrets(&secrets)
				logger.DefaultLogger.Info("Loaded last secrets from disk")
			}
		} else if !os.IsNotExist(err) {
			// Файл отсутствует – нормально при первом запуске
			logger.DefaultLogger.Warnf("Failed to load secrets from disk: %v", err)
		}
	}

	a.ctx, a.cancel = context.WithCancel(context.Background())

	a.osRegistry = prometheus.NewRegistry()
	a.racRegistry = prometheus.NewRegistry()

	a.initHTTP()

	return nil
}

func (a *app) Start() error {
	logger.DefaultLogger.Info("Запущен сбор метрик: ", strings.Join(a.metric.Metrics, ","))
	fmt.Println("port :", a.port)

	go a.settings.GetDBCredentials(a.ctx, expl.CForce)
	go a.reloadWatcher()

	a.register()

	// Если включён режим gitlab, запускаем пайплайн для получения секретов
	if a.gitlabSecretsAvailable() {
		go func() {
			// Небольшая задержка, чтобы сервер успел запуститься
			time.Sleep(5 * time.Second)
			if err := a.triggerPipeline(); err != nil {
				logger.DefaultLogger.Errorf("Failed to trigger pipeline on start: %v", err)
			}
		}()
	}

	// Запуск HTTP-сервера
	go func() {
		if err := a.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.DefaultLogger.Error(err)
		}
	}()

	return nil

}

func (a *app) Stop() error {
	logger.DefaultLogger.Info("Остановка приложения")

	defer a.cancel()

	ctx, _ := context.WithTimeout(a.ctx, time.Second*10)
	return a.httpSrv.Shutdown(ctx)
}

func (a *app) reloadWatcher() {
	// Обработка сигала от ОС
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP) // SIGHUP получаем при отправке reload

	<-c

	a.renewSettings()
}

func (a *app) renewSettings() {
	news, err := settings.LoadSettings(a.settings.SettingsPath)
	if err != nil {
		logger.Error(err)
		os.Exit(1)
	}

	a.settings.AssignFrom(news)
	logger.InitLogger(a.settings.LogDir, a.settings.LogLevel)

	a.unregisterAll()
	a.metric.FillMetrics(a.settings)
	a.register()

	km, err := keymanager.NewKeyManager(a.settings.RAC_Host(), a.settings.RAC_Port())
	if err == nil {
		a.keyManager = km
		logger.DefaultLogger.Info("KeyManager reinitialized")
	} else {
		logger.DefaultLogger.Errorf("keymanager init failed: %v", err)
		if a.settings.GitlabSecretsEnabled() {
			logger.Error("Cannot run in gitlab mode without keymanager, exiting")
			os.Exit(1)
		}
		a.keyManager = nil
	}

	// Если GitLab-секреты доступны, запускаем пайплайн
	if a.gitlabSecretsAvailable() {
		go func() {
			time.Sleep(2 * time.Second)
			if err := a.triggerPipeline(); err != nil {
				logger.DefaultLogger.Errorf("Failed to trigger pipeline after reload: %v", err)
			}
		}()
	}
}

func (a *app) initHTTP() {
	siteMux := http.NewServeMux()

	siteMux.Handle("/metrics", promhttp.Handler())
	siteMux.Handle("/metrics_os", promhttp.HandlerFor(a.osRegistry, promhttp.HandlerOpts{}))
	siteMux.Handle("/metrics_rac", promhttp.HandlerFor(a.racRegistry, promhttp.HandlerOpts{}))
	siteMux.Handle("/Continue", expl.Continue(a.metric))
	siteMux.Handle("/Pause", expl.Pause(a.metric))

	siteMux.HandleFunc("/debug/pprof/", pprof.Index)
	siteMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	siteMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	siteMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	siteMux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	siteMux.HandleFunc("POST /set_config", a.setConfig)
	siteMux.HandleFunc("POST /set_binarypath", a.setBinaryPath)
	siteMux.HandleFunc("POST /shutdown_emulate", a.crash)

	siteMux.HandleFunc("/", a.homePage)
	siteMux.HandleFunc("/log", a.getLog)

	siteMux.HandleFunc("POST /set_secrets", a.setSecretsHandler)
	siteMux.HandleFunc("/public-key", a.publicKeyHandler)

	a.httpSrv = &http.Server{
		Handler: siteMux,
		Addr:    ":" + a.port,
	}
}

func (a *app) unregisterAll() {
	for _, ex := range a.metric.Exporters {
		prometheus.Unregister(ex)
	}
}

func (a *app) register() {
	for _, ex := range a.metric.Exporters {
		if a.metric.Contains(ex.GetName()) {
			prometheus.MustRegister(ex)

			switch ex.GetType() {
			case model.TypeOS:
				a.osRegistry.Register(ex)
			case model.TypeRAC:
				a.racRegistry.Register(ex)
			}

		} else {
			ex.Stop()
			prometheus.Unregister(ex)
			logger.DefaultLogger.Debugf("Метрика %q пропущена", ex.GetName())
		}
	}
}

func (a *app) setConfig(w http.ResponseWriter, r *http.Request) {

	logger.DefaultLogger.Info("Начинаем обработку метода /set_config")

	if r.Method != "POST" {
		w.WriteHeader(405)
		logger.DefaultLogger.Error("Требуется использование метода POST")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(400)
		logger.DefaultLogger.Error(err)
		return
	}
	defer file.Close()

	out, err := os.Create(a.settings.SettingsPath)
	if err != nil {
		w.WriteHeader(500)
		logger.DefaultLogger.Error(err)
		return
	}
	defer out.Close()

	io.Copy(out, file)
	w.WriteHeader(201)

	a.renewSettings()

}

func (a *app) setBinaryPath(w http.ResponseWriter, r *http.Request) {

	logger.DefaultLogger.Info("Начинаем обработку метода /set_binarypath")

	if r.Method != "POST" {
		w.WriteHeader(405)
		logger.DefaultLogger.Error("Требуется использование метода POST")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(400)
		logger.DefaultLogger.Error("Ошибка чтения тела запроса: " + err.Error())
		return
	}
	defer r.Body.Close()

	binaryPath := string(bytes.TrimSpace(body))

	if binaryPath == "" {
		w.WriteHeader(400)
		logger.DefaultLogger.Error("Пустое значение binaryPath")
		return
	}

	err = a.settings.SetBinaryPath(binaryPath)
	if err != nil {
		w.WriteHeader(400)
		logger.DefaultLogger.Error(err)
		return
	}

	w.WriteHeader(201)
	logger.DefaultLogger.Infof("BinaryPath успешно установлен: %s", binaryPath)

}

func (a *app) crash(w http.ResponseWriter, r *http.Request) {

	exitCodeStr := r.URL.Query().Get("exit_code")
	exitCode := 1 // значение по умолчанию

	if exitCodeStr != "" {
		if code, err := strconv.Atoi(exitCodeStr); err == nil {
			exitCode = code
		}
	}

	logger.DefaultLogger.Infof("Crash with exit code %d", exitCode)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "crash_emulated",
		"exit_code": exitCode,
		"message":   "Application will exit with the specified code",
	})

	time.Sleep(100 * time.Millisecond)
	os.Exit(exitCode)

}

func (a *app) homePage(w http.ResponseWriter, r *http.Request) {

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	info := map[string]interface{}{
		"service":     "Prometheus Exporter для кластера 1С",
		"description": "Экспортер метрик Prometheus",
		"version":     "1.5.1.25",
		"status":      "running",
		"endpoints": []map[string]string{
			{"path": "/", "method": "GET", "description": "Информационная страница"},
			{"path": "/metrics", "method": "GET", "description": "Основные метрики Prometheus"},
			{"path": "/metrics_os", "method": "GET", "description": "Метрики операционной системы"},
			{"path": "/metrics_rac", "method": "GET", "description": "Метрики RAC"},
			{"path": "/Continue", "method": "GET", "description": "Возобновить сбор метрик"},
			{"path": "/Pause", "method": "GET", "description": "Приостановить сбор метрик"},
			{"path": "/debug/pprof/", "method": "GET", "description": "Профилирование Go"},
			{"path": "/set_config", "method": "POST", "description": "Установка конфигурации"},
			{"path": "/shutdown_emulate", "method": "POST", "description": "Аварийное завершение"},
			{"path": "/set_binarypath", "method": "POST", "description": "Установка источника скачивания бинарного файла, при использовании WinSW"},
			{"path": "/log", "method": "GET", "description": "Читает содержимое лога: с начала, с конца или с произвольного места"},
		},
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.Encode(info)
}

func (a *app) getLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mode, n, from := a.parseLogParams(r)

	var cfg *logger.ReadConfig

	switch mode {
	case "first":
		cfg = &logger.ReadConfig{Count: n}
	case "last":
		cfg = &logger.ReadConfig{Count: -n}
	case "range":
		cfg = &logger.ReadConfig{
			StartLine: int64(from),
			EndLine:   int64(from + n - 1),
		}
	default:
		http.Error(w, fmt.Sprintf("invalid mode: %s", mode), http.StatusBadRequest)
		return
	}

	result, err := logger.ReadLogs(cfg)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read logs: %v", err), http.StatusInternalServerError)
		return
	}

	if len(result.Entries) == 0 {
		fmt.Fprintf(w, "=== No log entries found ===\n")
		return
	}

	fmt.Fprintf(w, "=== Lines %d to %d of %d ===\n\n",
		result.FromLine, result.ToLine, result.TotalLines)

	for _, entry := range result.Entries {
		fmt.Fprintf(w, "%6d: %s\n", entry.LineNumber, entry.Content)
	}

}

// parseLogParams парсит параметры запроса
func (a *app) parseLogParams(r *http.Request) (mode string, n, from int) {
	mode = r.URL.Query().Get("mode")
	n, _ = strconv.Atoi(r.URL.Query().Get("n"))
	from, _ = strconv.Atoi(r.URL.Query().Get("from"))

	if mode == "" {
		mode = "last"
	}

	if n <= 0 || n > 1000 {
		n = logger.DefaultPageSize
	}

	if from < 1 {
		from = 1
	}

	return mode, n, from
}

// setSecretsHandler принимает зашифрованные данные, расшифровывает и сохраняет.
func (a *app) setSecretsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !a.gitlabSecretsAvailable() {
		http.Error(w, "GitLab secrets not configured", http.StatusServiceUnavailable)
		return
	}

	if a.keyManager == nil {
		http.Error(w, "key manager not initialized", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		EncryptedData []byte `json:"encrypted_data"` // base64-encoded зашифрованный пакет
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Расшифровываем
	plaintext, err := a.keyManager.Decrypt(req.EncryptedData)
	if err != nil {
		http.Error(w, "decryption failed", http.StatusBadRequest)
		return
	}

	// Парсим JSON с секретами
	var secrets settings.IBCredentials
	if err := json.Unmarshal(plaintext, &secrets); err != nil {
		http.Error(w, "invalid secrets format", http.StatusBadRequest)
		return
	}

	// Сохраняем в настройках
	a.settings.UpdateSecrets(&secrets)

	// Сохраняем последние секреты на диск (для холодного старта)
	if err := a.keyManager.SaveLastSecrets(plaintext); err != nil {
		// Ошибка сохранения не критична, но логируем
		logger.DefaultLogger.Errorf("Failed to save secrets to disk: %v", err)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "secrets updated")
}

// publicKeyHandler отдаёт публичный ключ в формате PEM.
func (a *app) publicKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if a.keyManager == nil {
		http.Error(w, "key manager not initialized", http.StatusServiceUnavailable)
		return
	}
	pem, err := a.keyManager.PublicKeyPEM()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Write([]byte(pem))
}

func (a *app) triggerPipeline() error {
	if !a.gitlabSecretsAvailable() {
		logger.DefaultLogger.Debug("triggerPipeline: gitlab secrets not available, skipping")
		return nil
	}
	gl := a.settings.DBCredentials.GitLab
	// Дополнительная валидация полей (можно оставить, но они уже должны быть)
	if gl.RepoURL == "" || gl.Branch == "" || gl.TriggerToken == "" {
		return fmt.Errorf("incomplete GitLab settings: RepoURL, Branch, TriggerToken required")
	}

	// Извлекаем путь проекта из RepoURL
	// Пример: "https://gitlab.example.com/namespace/project.git" -> "namespace/project"
	parsed, err := url.Parse(gl.RepoURL)
	if err != nil {
		return fmt.Errorf("invalid RepoURL: %w", err)
	}
	projectPath := strings.TrimPrefix(parsed.Path, "/")
	projectPath = strings.TrimSuffix(projectPath, ".git")
	if projectPath == "" {
		return fmt.Errorf("could not extract project path from RepoURL")
	}
	// Кодируем путь для использования в URL (заменяет / на %2F)
	encodedPath := url.PathEscape(projectPath)

	// Формируем URL для trigger API
	baseURL := strings.TrimSuffix(gl.RepoURL, ".git")
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/trigger/pipeline", baseURL, encodedPath)

	data := url.Values{}
	data.Set("token", gl.TriggerToken)
	data.Set("ref", gl.Branch)
	if gl.SecretsFile != "" {
		data.Set("variables[SECRETS_FILE]", gl.SecretsFile)
	}

	resp, err := http.PostForm(apiURL, data)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gitlab trigger failed with status %s: %s", resp.Status, body)
	}
	logger.DefaultLogger.Info("Pipeline triggered successfully")
	return nil
}

func (a *app) gitlabSecretsAvailable() bool {
	return a.settings.GitlabSecretsEnabled() && a.keyManager != nil
}
