package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

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
}

func (a *app) Init(_ svc.Environment) (err error) {
	expl.InitMeterFunctions()
	a.metric, err = new(expl.Metrics).FillMetrics(a.settings)
	if err != nil {
		return err
	}
	a.ctx, a.cancel = context.WithCancel(context.Background())

	a.osRegistry = prometheus.NewRegistry()
	a.racRegistry = prometheus.NewRegistry()

	// lic := new(expl.ExporterClientLic).Construct(a.settings)             // Клиентские лицензии
	// perf := new(expl.ExporterAvailablePerformance).Construct(a.settings) // Доступная производительность
	// sJob := new(expl.ExporterCheckSheduleJob).Construct(a.settings)      // Проверка галки "блокировка регламентных заданий"
	// iin := new(expl.ExporterInfobaseInfo).Construct(a.settings)          // Информация о запретах в информационной базе
	// ses := new(expl.ExporterSessions).Construct(a.settings)              // Сеансы
	// conn := new(expl.ExporterConnects).Construct(a.settings)             // Соединения
	// currentMem := new(expl.ExporterSessionsData).Construct(a.settings)   // Текущая память сеанса
	// cpu := new(expl.CPU).Construct(a.settings)                           // CPU
	// proc := new(expl.Processes).Construct(a.settings)                    // Данные CPU/память в разрезе процессов
	// disk := new(expl.ExporterDisk).Construct(a.settings)                 // Диск

	// a.metric.AppendExporter(proc, cpu, disk, currentMem, lic, perf, sJob, ses, conn, iin)
	a.initHTTP()

	return nil
}

func (a *app) Start() error {
	logger.DefaultLogger.Info("Запущен сбор метрик: ", strings.Join(a.metric.Metrics, ","))
	fmt.Println("port :", a.port)

	// if a.metric.Contains("shedule_job") && (a.settings.DBCredentials == nil || a.settings.DBCredentials.URL == "") {
	// 	return errors.New("для метрики \"shedule_job\" обязательно должен быть заполнен параметр DBCredentials")
	// }

	go a.settings.GetDBCredentials(a.ctx, expl.CForce)
	go a.reloadWatcher()

	a.register()
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
	signal.Notify(c, syscall.SIGHUP) // SIGHUP получаем при отпавки reload

	<-c

	a.renewSettings()
}

func (a *app) renewSettings() {

	news, err := settings.LoadSettings(a.settings.SettingsPath)
	if err != nil {
		logger.DefaultLogger.Error(err)
		os.Exit(1)
	}
	*a.settings = *news

	logger.InitLogger(a.settings.LogDir, a.settings.LogLevel)

	a.unregisterAll()
	a.metric.FillMetrics(a.settings)
	a.register()

	logger.DefaultLogger.Info("Обновлены настройки")
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

	a.httpSrv = &http.Server{
		Handler: siteMux,
		Addr:    ":" + a.port,
	}

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
		} else {
			exitCode = 1
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
		"version":     "1.5.1.23",
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

	// Читаем нужный диапазон лога
	lines, start, end, total, err := a.readLogRange(mode, n, from)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Выводим результат
	fmt.Fprintf(w, "=== Lines %d to %d of %d ===\n\n", start, end, total)
	for i, line := range lines {
		fmt.Fprintf(w, "%6d: %s\n", start+i, line)
	}
}

// parseLogParams парсит параметры запроса
func (a *app) parseLogParams(r *http.Request) (mode string, n, from int) {
	mode = r.URL.Query().Get("mode")
	n, _ = strconv.Atoi(r.URL.Query().Get("n"))
	from, _ = strconv.Atoi(r.URL.Query().Get("from"))

	if n <= 0 || n > 10000 {
		n = 100
	}
	if from < 1 {
		from = 1
	}

	return mode, n, from
}

func (a *app) readLogRange(mode string, n, from int) ([]string, int, int, int, error) {
	// Формируем путь к файлу
	logPath := filepath.Join("logs", "log.txt")
	if a.settings.LogDir != "" {
		logPath = filepath.Join(a.settings.LogDir, "log.txt")
	}

	// Открываем файл
	file, err := os.Open(logPath)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	defer file.Close()

	// Для режима last (по умолчанию) используем чтение с конца файла
	if mode == "" {
		mode = "last"
	}

	switch mode {
	case "last":
		return a.readLastLines(file, n)

	case "first", "range":
		scanner := bufio.NewScanner(file)
		var result []string
		currentLine := 1
		total := 0

		for scanner.Scan() {
			total++

			if mode == "first" && currentLine <= n {
				result = append(result, scanner.Text())
			} else if mode == "range" && currentLine >= from && currentLine < from+n {
				result = append(result, scanner.Text())
			}

			currentLine++
		}

		if err := scanner.Err(); err != nil {
			return nil, 0, 0, 0, err
		}

		if total == 0 {
			return nil, 0, 0, 0, fmt.Errorf("log file is empty")
		}

		if mode == "first" {
			return result, 1, len(result), total, nil
		} else { // range
			if from > total {
				return nil, 0, 0, total, fmt.Errorf("start line %d exceeds total lines %d", from, total)
			}
			return result, from, from + len(result) - 1, total, nil
		}

	default:
		return nil, 0, 0, 0, fmt.Errorf("invalid mode: %s", mode)
	}
}

// readLastLines читает последние N строк
func (a *app) readLastLines(file *os.File, n int) ([]string, int, int, int, error) {
	// Получаем размер файла
	stat, err := file.Stat()
	if err != nil {
		return nil, 0, 0, 0, err
	}

	if stat.Size() == 0 {
		return nil, 0, 0, 0, fmt.Errorf("log file is empty")
	}

	// Читаем с конца файла блоками
	blockSize := int64(8192) // 8KB
	fileSize := stat.Size()
	offset := fileSize

	var data []byte
	var lines []string

	// Читаем блоки с конца, пока не наберем нужное количество строк
	for offset > 0 && len(lines) <= n {
		readSize := blockSize
		if offset < readSize {
			readSize = offset
		}

		offset -= readSize
		buffer := make([]byte, readSize)

		_, err := file.ReadAt(buffer, offset)
		if err != nil && err != io.EOF {
			return nil, 0, 0, 0, err
		}

		data = append(buffer, data...)
		lines = strings.Split(string(data), "\n")

		// Если прочитали не с начала файла, отбрасываем первую частичную строку
		if offset > 0 {
			lines = lines[1:]
		}
	}

	// Очищаем от пустых строк
	clean := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			clean = append(clean, line)
		}
	}
	lines = clean

	// Берем последние n строк
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	// Подсчитываем общее количество строк
	total := a.countLines(file)

	startLine := total - len(lines) + 1
	if startLine < 1 {
		startLine = 1
	}

	return lines, startLine, startLine + len(lines) - 1, total, nil
}

// countLines подсчитывает общее количество строк в файле
func (a *app) countLines(file *os.File) int {
	// Сохраняем текущую позицию
	currentPos, _ := file.Seek(0, io.SeekCurrent)

	// Возвращаемся в начало
	file.Seek(0, io.SeekStart)

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
	}

	// Восстанавливаем позицию
	file.Seek(currentPos, io.SeekStart)

	return count
}
