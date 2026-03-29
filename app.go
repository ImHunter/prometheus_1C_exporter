package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
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
	reloadMutex sync.Mutex
}

// ==================== Жизненный цикл ====================

func (a *app) Init(_ svc.Environment) (err error) {
	expl.InitMeterFunctions()
	a.metric, err = new(expl.Metrics).FillMetrics(a.settings)
	if err != nil {
		return err
	}

	km, err := keymanager.NewKeyManager(a.settings.RAC.Host, a.settings.RAC.Port, logger.DefaultLogger)
	a.keyManager = km
	if err != nil {
		logger.Errorf("Failed to init keymanager: %v", err)
	}

	// Загружаем секреты с диска, если включен внутренний режим
	if a.settings.IsInternalSecrets() && a.keyManager != nil {
		decrypted, err := a.keyManager.LoadLastSecrets()
		if err == nil {
			var secrets settings.IBCredentials
			if json.Unmarshal(decrypted, &secrets) == nil {
				a.settings.UpdateSecrets(&secrets)
				logger.DefaultLogger.Info("Loaded last secrets from disk")
			}
		} else if !os.IsNotExist(err) {
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

	if a.settings.IsExternalSecrets() {
		go a.settings.GetDBCredentials(a.ctx, expl.CForce)
	}
	go a.reloadWatcher()

	a.register()

	// Если внутренний режим и GitLab настроен, запускаем периодический триггер для получения секретов
	if a.settings.IsInternalSecrets() && a.settings.GitlabConfigured() && a.keyManager != nil {
		go func() {
			time.Sleep(5 * time.Second)
			if err := a.triggerPipeline(); err != nil {
				logger.DefaultLogger.Errorf("Failed to trigger pipeline on start: %v", err)
			}
			ticker := time.NewTicker(time.Hour * time.Duration(rand.Intn(4)+2))
			defer ticker.Stop()
			for range ticker.C {
				if err := a.triggerPipeline(); err != nil {
					logger.DefaultLogger.Errorf("Failed to trigger pipeline (periodic): %v", err)
				}
			}
		}()
	}

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
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)
	<-c
	a.renewSettings()
}

func (a *app) renewSettings() {
	a.reloadMutex.Lock()
	defer a.reloadMutex.Unlock()

	news, err := settings.LoadSettings(a.settings.SettingsPath)
	if err != nil {
		logger.Error(err)
		os.Exit(1)
	}
	a.settings.AssignFrom(news)
	logger.InitLogger(a.settings.LogDir, a.settings.LogLevel)

	// Пересоздаем keyManager (после обновления настроек)
	km, err := keymanager.NewKeyManager(a.settings.RAC_Host(), a.settings.RAC_Port(), logger.DefaultLogger)
	if err == nil {
		a.keyManager = km
		logger.DefaultLogger.Info("KeyManager reinitialized")
	} else {
		logger.DefaultLogger.Errorf("keymanager init failed: %v", err)
		if a.settings.IsInternalSecrets() {
			logger.Error("Cannot run in internal secrets mode without keymanager, exiting")
			os.Exit(1)
		}
		a.keyManager = nil
	}

	// Загружаем секреты с диска, если режим внутренний и keyManager есть
	if a.keyManager != nil && a.settings.IsInternalSecrets() {
		decrypted, err := a.keyManager.LoadLastSecrets()
		if err == nil {
			var secrets settings.IBCredentials
			if json.Unmarshal(decrypted, &secrets) == nil {
				a.settings.UpdateSecrets(&secrets)
				logger.DefaultLogger.Info("Loaded last secrets from disk")
			}
		} else if !os.IsNotExist(err) {
			logger.DefaultLogger.Warnf("Failed to load secrets from disk: %v", err)
		}
	}

	// Останавливаем старые экспортеры и удаляем их из реестров
	for _, ex := range a.metric.Exporters {
		ex.Stop()
		a.osRegistry.Unregister(ex)
		a.racRegistry.Unregister(ex)
	}

	// Создаем новые метрики и регистрируем
	newMetrics := &expl.Metrics{}
	newMetrics.FillMetrics(a.settings)

	for _, ex := range newMetrics.Exporters {
		if newMetrics.Contains(ex.GetName()) {
			var targetRegistry *prometheus.Registry
			switch ex.GetType() {
			case model.TypeOS:
				targetRegistry = a.osRegistry
			case model.TypeRAC:
				targetRegistry = a.racRegistry
			default:
				logger.DefaultLogger.Warnf("Unknown metric type %v for metric %s – skipping registration", ex.GetType(), ex.GetName())
				continue
			}
			if err := targetRegistry.Register(ex); err != nil {
				logger.DefaultLogger.Errorf("Failed to register metric %s: %v", ex.GetName(), err)
			}
		} else {
			ex.Stop()
		}
	}
	a.metric = newMetrics

	// Запуск пайплайна, если включен внутренний режим и GitLab настроен
	if a.settings.IsInternalSecrets() && a.settings.GitlabConfigured() && a.keyManager != nil {
		go func() {
			time.Sleep(time.Hour * time.Duration(rand.Intn(6)+2))
			if err := a.triggerPipeline(); err != nil {
				logger.DefaultLogger.Errorf("Failed to trigger pipeline after reload: %v", err)
			}
		}()
	}
}

// ==================== Метрики ====================

func (a *app) register() {
	for _, ex := range a.metric.Exporters {
		if a.metric.Contains(ex.GetName()) {
			var targetRegistry *prometheus.Registry
			switch ex.GetType() {
			case model.TypeOS:
				targetRegistry = a.osRegistry
			case model.TypeRAC:
				targetRegistry = a.racRegistry
			default:
				logger.DefaultLogger.Warnf("Unknown metric type %v for metric %s – skipping registration", ex.GetType(), ex.GetName())
				continue
			}
			if err := targetRegistry.Register(ex); err != nil {
				logger.DefaultLogger.Errorf("Failed to register metric %s in %s registry: %v", ex.GetName(), ex.GetType(), err)
			} else {
				logger.DefaultLogger.Infof("Registered metric %s in %s registry", ex.GetName(), ex.GetType())
			}
		} else {
			ex.Stop()
			a.osRegistry.Unregister(ex)
			a.racRegistry.Unregister(ex)
			logger.DefaultLogger.Debugf("Метрика %q пропущена – удалена из реестров", ex.GetName())
		}
	}
}

// ==================== HTTP-обработчики ====================

func (a *app) initHTTP() {
	siteMux := http.NewServeMux()

	// Метрики
	// siteMux.Handle("/metrics", promhttp.Handler())
	// siteMux.Handle("/metrics_os", promhttp.HandlerFor(a.osRegistry, promhttp.HandlerOpts{}))
	// siteMux.Handle("/metrics_rac", promhttp.HandlerFor(a.racRegistry, promhttp.HandlerOpts{}))
	siteMux.Handle("/metrics", a.metricsHandler(a.osRegistry, a.racRegistry))
	siteMux.Handle("/metrics_os", a.metricsHandler(a.osRegistry))
	siteMux.Handle("/metrics_rac", a.metricsHandler(a.racRegistry))
	siteMux.Handle("/metrics_internal", a.metricsHandler(prometheus.DefaultGatherer))

	siteMux.Handle("/Continue", expl.Continue(a.metric))
	siteMux.Handle("/Pause", expl.Pause(a.metric))

	// Профилирование
	siteMux.HandleFunc("/debug/pprof/", pprof.Index)
	siteMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	siteMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	siteMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	siteMux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// Конфигурация
	siteMux.HandleFunc("POST /config/set", a.setConfigHandler)
	siteMux.HandleFunc("GET /config/get", a.getConfigHandler)

	// Управление
	siteMux.HandleFunc("POST /shutdown_emulate", a.crash)

	// Информация
	siteMux.HandleFunc("/", a.homePage)
	siteMux.HandleFunc("/log", a.getLog)

	// Секреты
	siteMux.HandleFunc("GET /secrets", a.secretsInfoHandler)
	siteMux.HandleFunc("POST /secrets/set", a.setSecretsHandler)
	siteMux.HandleFunc("POST /secrets/encrypt", a.encryptSecretsHandler)
	siteMux.HandleFunc("GET /secrets/pub_key", a.secretsPubKeyHandler)

	a.httpSrv = &http.Server{
		Handler: siteMux,
		Addr:    ":" + a.port,
	}
}

func (a *app) metricsHandler(gatherers ...prometheus.Gatherer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		opts := promhttp.HandlerOpts{
			DisableCompression: a.settings.GetDisableMetricsCompression(),
		}
		promhttp.HandlerFor(prometheus.Gatherers(gatherers), opts).ServeHTTP(w, r)
	})
}

// ----- Конфигурация -----

func (a *app) setConfigHandler(w http.ResponseWriter, r *http.Request) {
	logger.DefaultLogger.Info("Начинаем обработку метода /set_config")

	if r.Method != "POST" {
		w.WriteHeader(405)
		logger.DefaultLogger.Error("Требуется использование метода POST")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(400)
		logger.DefaultLogger.Error("Ошибка получения файла: ", err)
		return
	}
	defer file.Close()

	tempPath := a.settings.SettingsPath + ".tmp"
	out, err := os.Create(tempPath)
	if err != nil {
		w.WriteHeader(500)
		logger.DefaultLogger.Error("Ошибка создания временного файла: ", err)
		return
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		w.WriteHeader(500)
		logger.DefaultLogger.Error("Ошибка копирования файла: ", err)
		return
	}
	out.Close()

	if err := os.Rename(tempPath, a.settings.SettingsPath); err != nil {
		w.WriteHeader(500)
		logger.DefaultLogger.Error("Ошибка замены конфигурационного файла: ", err)
		return
	}

	w.WriteHeader(201)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	a.renewSettings()
}

func (a *app) getConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data, err := os.ReadFile(a.settings.SettingsPath)
	if err != nil {
		http.Error(w, "failed to read config file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=settings.yml")
	w.Write(data)
}

// ----- Управление -----

func (a *app) crash(w http.ResponseWriter, r *http.Request) {
	exitCodeStr := r.URL.Query().Get("exit_code")
	exitCode := 1
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

// ----- Информация -----

func (a *app) homePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	info := map[string]interface{}{
		"service":     "Prometheus Exporter для кластера 1С",
		"description": "Экспортер метрик Prometheus",
		"version":     version,
		"status":      "running",
		"endpoints": []map[string]string{
			{"path": "/", "method": "GET", "description": "Информационная страница"},
			{"path": "/metrics", "method": "GET", "description": "Основные метрики Prometheus"},
			{"path": "/metrics_os", "method": "GET", "description": "Метрики операционной системы"},
			{"path": "/metrics_rac", "method": "GET", "description": "Метрики RAC"},
			{"path": "/metrics_internal", "method": "GET", "description": "Метрики работы экспортера"},
			{"path": "/Continue", "method": "GET", "description": "Возобновить сбор метрик"},
			{"path": "/Pause", "method": "GET", "description": "Приостановить сбор метрик"},
			{"path": "/debug/pprof/", "method": "GET", "description": "Профилирование Go"},
			{"path": "/config/set", "method": "POST", "description": "Загружает новый конфигурационный файл (multipart/form-data) и применяет его"},
			{"path": "/config/get", "method": "GET", "description": "Возвращает текущий конфигурационный файл settings.yml"},
			{"path": "/shutdown_emulate", "method": "POST", "description": "Аварийное завершение"},
			{"path": "/log", "method": "GET", "description": "Читает содержимое лога: с начала, с конца или с произвольного места"},
			{"path": "/secrets", "method": "GET", "description": "Информация о загруженных секретах (метаданные, список баз, время обновления)"},
			{"path": "/secrets/set", "method": "POST", "description": "Принимает зашифрованные секреты (бинарные данные)"},
			{"path": "/secrets/encrypt", "method": "POST", "description": "Шифрует открытые секреты (JSON) и возвращает зашифрованный JSON"},
			{"path": "/secrets/pub_key", "method": "GET", "description": "Возвращает публичный ключ RSA в формате PEM"},
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

// ----- Секреты -----

// secretsInfoHandler – GET /secrets
func (a *app) secretsInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hasSecrets, hasDefault, hasRas, bases, lastUpdated := a.settings.GetSecretsInfo()
	resp := map[string]interface{}{
		"has_secrets": hasSecrets,
		"has_default": hasDefault,
		"has_ras":     hasRas,
		"bases":       bases,
	}
	if !lastUpdated.IsZero() {
		resp["last_updated"] = lastUpdated.Format(time.RFC3339)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// setSecretsHandler – POST /secrets/set (принимает бинарные данные)
func (a *app) setSecretsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.keyManager == nil {
		http.Error(w, "key manager not initialized", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	plaintext, err := a.keyManager.Decrypt(body)
	if err != nil {
		http.Error(w, "decryption failed", http.StatusBadRequest)
		return
	}

	var secrets settings.IBCredentials
	if err := json.Unmarshal(plaintext, &secrets); err != nil {
		http.Error(w, "invalid secrets format", http.StatusBadRequest)
		return
	}

	a.settings.UpdateSecrets(&secrets)
	if err := a.keyManager.SaveLastSecrets(plaintext); err != nil {
		logger.DefaultLogger.Errorf("Failed to save secrets to disk: %v", err)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "secrets updated"})
}

// encryptSecretsHandler – POST /secrets/encrypt
func (a *app) encryptSecretsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.keyManager == nil {
		http.Error(w, "key manager not initialized", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var dummy interface{}
	if err := json.Unmarshal(body, &dummy); err != nil {
		http.Error(w, "invalid JSON format", http.StatusBadRequest)
		return
	}

	encrypted, err := a.keyManager.Encrypt(body)
	if err != nil {
		http.Error(w, "encryption failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(encrypted)
}

// secretsPubKeyHandler – GET /secrets/pub_key
func (a *app) secretsPubKeyHandler(w http.ResponseWriter, r *http.Request) {
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

// ==================== Вспомогательные методы ====================

func (a *app) triggerPipeline() error {
	if !a.settings.IsInternalSecrets() || !a.settings.GitlabConfigured() || a.keyManager == nil {
		return nil
	}
	gl := a.settings.GitLab

	// Используем явные поля: GitLabHome и ProjectID
	if gl.GitLabHome == "" || gl.ProjectID == 0 {
		return fmt.Errorf("GitLabHome or ProjectID not configured")
	}

	apiURL := fmt.Sprintf("%s/api/v4/projects/%d/trigger/pipeline", gl.GitLabHome, gl.ProjectID)

	data := url.Values{}
	data.Set("ref", gl.Branch)
	if gl.SecretsFile != "" {
		data.Set("variables[SECRETS_FILE]", gl.SecretsFile)
	}

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", gl.AccessToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
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
