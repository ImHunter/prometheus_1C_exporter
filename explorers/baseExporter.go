package exporter

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LazarenkoA/prometheus_1C_exporter/explorers/model"
	"github.com/LazarenkoA/prometheus_1C_exporter/settings"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/softlandia/cpd"
	"go.uber.org/zap"
	"golang.org/x/text/encoding/charmap"

	"github.com/LazarenkoA/prometheus_1C_exporter/logger"
)

type ExporterFactory func() model.IExporter

var (
	// Канал для передачи флага принудительного обновления данных из REST
	CForce chan struct{}
	// Типы экспортеров
	ExporterFactories = map[string]ExporterFactory{
		"available_performance": func() model.IExporter { return new(ExporterAvailablePerformance) },
		"shedule_job":           func() model.IExporter { return new(ExporterCheckSheduleJob) },
		"client_lic":            func() model.IExporter { return new(ExporterClientLic) },
		"connect":               func() model.IExporter { return new(ExporterConnects) },
		"cpu":                   func() model.IExporter { return new(CPU) },
		"disk":                  func() model.IExporter { return new(ExporterDisk) },
		"ibinfo":                func() model.IExporter { return new(ExporterInfobaseInfo) },
		"processes":             func() model.IExporter { return new(Processes) },
		"session":               func() model.IExporter { return new(ExporterSessions) },
		"sessions_data":         func() model.IExporter { return new(ExporterSessionsData) },
	}
)

func init() {
	CForce = make(chan struct{}, 1)
}

//go:generate mockgen -source=$GOFILE -package=mock_models -destination=./mock/mockRunner.go
type IRunner interface {
	Run(cmd *exec.Cmd) (string, error)
}

type cmdRunner struct {
}

// базовый класс для всех метрик
type BaseExporter struct {
	mx         sync.RWMutex
	summary    IPrometheusMetric //*prometheus.SummaryVec
	gauge      *prometheus.GaugeVec
	settings   *settings.Settings
	ctx        context.Context
	cancel     context.CancelFunc
	isLocked   atomic.Bool
	logger     *zap.SugaredLogger
	host       string
	runner     IRunner
	metricName string
}

// базовый класс для всех метрик собираемых через RAC
type BaseRACExporter struct {
	BaseExporter

	clusterID string
}

type Metrics struct {
	Exporters []model.IExporter
	Metrics   []string // метрики
}

func newBase(name string) BaseExporter {
	host, _ := os.Hostname()
	ctx, cancel := context.WithCancel(context.Background())

	return BaseExporter{
		host:       host,
		logger:     logger.DefaultLogger.Named(name),
		ctx:        ctx,
		cancel:     cancel,
		runner:     new(cmdRunner),
		metricName: name,
	}
}

func (r *cmdRunner) Run(cmd *exec.Cmd) (string, error) {
	timeout := time.Second * 15
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)
	errch := make(chan error, 1)

	err := cmd.Start()
	if err != nil {
		return "", errors.Wrap(err, "running error")
	}

	// запускаем в горутине т.к. наблюдалось что при выполнении RAC может происходить завсание, поэтому нам нужен таймаут
	go func() {
		errch <- cmd.Wait()
	}()

	select {
	case <-time.After(timeout): // timeout
		// завершаем процесс
		cmd.Process.Kill()
		return "", errors.New("command execution was interrupted by timeout")
	case err := <-errch:
		if err != nil {
			stderr := cmd.Stderr.(*bytes.Buffer)
			texterr := string(normalizeEncodingBytes(stderr.Bytes()))
			errText := fmt.Sprintf("Произошла ошибка запуска:\n\terr:%v\n\t", err.Error())
			if texterr != "" {
				errText += fmt.Sprintf("StdErr:%v\n", texterr)
			}

			return "", errors.New(errText)
		} else {
			// return cmd.Stdout.(*bytes.Buffer).String(), nil
			stdout := cmd.Stdout.(*bytes.Buffer)
			return string(normalizeEncodingBytes(stdout.Bytes())), nil
		}
	}
}

func (exp *BaseExporter) Construct(sett *settings.Settings, metricName string) model.IExporter {
	// Пока ничего не делаем. Для полноты интерфейса. По идее, сюда нужно перетащить содержимое newBase. А в newBase сделать вызов Construct.
	return exp
}

func (exp *BaseExporter) run(cmd *exec.Cmd) (string, error) {
	exp.logger.With("исполняемый файл", cmd.Path).
		With("параметры", cmd.Args).
		Debug("выполнение команды")

	return exp.runner.Run(cmd)
}

func (exp *BaseExporter) Stop() {
	exp.logger.Info("метрика остановлена")
	exp.cancel()
}

func (exp *BaseExporter) Pause(expName string) {
	l := exp.logger.With("name", expName)

	if exp.isLocked.CompareAndSwap(false, true) {
		l.Info("Pause. Блокировка установлена")

		if exp.summary != nil {
			exp.summary.Reset()
		}
		if exp.gauge != nil {
			exp.gauge.Reset()
		}
	} else {
		l.Debug("Pause. Уже заблокировано")
	}
}

func (exp *BaseExporter) Continue(expName string) {
	l := exp.logger.With("name", expName)

	if exp.isLocked.CompareAndSwap(true, false) {
		l.Debug("Continue. Блокировка снята")
	} else {
		l.Debug("Continue. Блокировка не была установлена")
	}
}

func (exp *BaseExporter) Describe(ch chan<- *prometheus.Desc) {
	if exp.summary != nil {
		exp.summary.Describe(ch)
	}
	if exp.gauge != nil {
		exp.gauge.Describe(ch)
	}
}

func (exp *BaseRACExporter) formatMultiResult(strIn string, outData *[]map[string]string) {
	exp.logger.Debug("Парс многострочного результата")

	strIn = normalizeEncoding(strIn)
	strIn = strings.Replace(strIn, "\r", "", -1)
	*outData = []map[string]string{} // очистка

	reg := regexp.MustCompile(`(?m)^$`)
	for _, part := range reg.Split(strIn, -1) {
		data := exp.formatResult(part)

		if len(data) == 0 {
			continue
		}

		*outData = append(*outData, data)
	}
}

func (exp *BaseRACExporter) formatResult(strIn string) map[string]string {
	strIn = normalizeEncoding(strIn)
	result := make(map[string]string)
	for _, line := range strings.Split(strIn, "\n") {
		parts := strings.Split(line, ":")
		// могут быть параметры с временем started-at : 2021-08-17T11:12:09
		if len(parts) >= 2 {
			result[strings.Trim(parts[0], " \r\t")] = strings.Trim(strings.Join(parts[1:], ":"), " \r\t")
		}
	}

	exp.logger.With("strIn", strIn).With("out", result).Debug("Парс результата")
	return result
}

func (exp *BaseRACExporter) appendLogPass(param []string) []string {
	if login := exp.settings.RAC_Login(); login != "" {
		param = append(param, fmt.Sprintf("--cluster-user=%v", login))
		if pwd := exp.settings.RAC_Pass(); pwd != "" {
			param = append(param, fmt.Sprintf("--cluster-pwd=%v", pwd))
		}
	}
	return param
}

func (exp *BaseExporter) GetName() string {
	return exp.metricName
}

func (exp *BaseExporter) GetType() model.MetricType {
	return model.Undefined
}
func (exp *BaseExporter) Collect(chan<- prometheus.Metric) {
	// Ничего не делаем. Пока для полноты интерфейса.
}

func normalizeEncoding(str string) string {

	// encoding := cpd.CodepageAutoDetect([]byte(str))

	// switch encoding {
	// case cpd.CP866:
	// 	encoder := charmap.CodePage866.NewDecoder()
	// 	if msg, err := encoder.String(str); err == nil {
	// 		return msg
	// 	}
	// }
	// return str
	b := normalizeEncodingBytes([]byte(str))
	return string(b)
}

func normalizeEncodingBytes(b []byte) []byte {
	encoding := cpd.CodepageAutoDetect(b)

	switch encoding {
	case cpd.CP866:
		encoder := charmap.CodePage866.NewDecoder()
		if be, err := encoder.Bytes(b); err == nil {
			return be
		}
	}
	return b
}

func (exp *BaseRACExporter) GetClusterID() string {
	update := func() {
		defer exp.logger.Debug("получен идентификатор кластера ", exp.clusterID)

		var param []string
		if exp.settings.RAC_Host() != "" {
			param = append(param, strings.Join(appendParam([]string{exp.settings.RAC_Host()}, exp.settings.RAC_Port()), ":"))
		}

		param = append(param, "cluster")
		param = append(param, "list")

		cmdCommand := exec.CommandContext(exp.ctx, exp.settings.RAC_Path(), param...)
		cluster := make(map[string]string)
		if result, err := exp.runner.Run(cmdCommand); err != nil {
			exp.logger.Error(fmt.Errorf("произошла ошибка выполнения при попытке получить идентификатор кластера: \n\t%v", err.Error())) // Если идентификатор кластера не получен, то нет смысла продолжать работу приложения
		} else {
			cluster = exp.formatResult(result)
		}

		if id, ok := cluster["cluster"]; !ok {
			exp.logger.Error(errors.New("Не удалось получить идентификатор кластера"))
		} else {
			exp.clusterID = id
		}
	}

	exp.mx.Lock()

	if exp.clusterID == "" {
		update() // обновляем
	}

	exp.mx.Unlock()

	return exp.clusterID
}

func (exp *Metrics) AppendExporter(ex ...model.IExporter) {
	exp.Exporters = append(exp.Exporters, ex...)
}

func (exp *Metrics) FillMetrics(set *settings.Settings) (*Metrics, error) {
	var err error
	exp.Metrics = []string{}
	for expName := range set.GetExporters() {
		factory, exists := ExporterFactories[expName]
		if exists {
			var newExporter model.IExporter = factory()
			newExporter.Construct(set, expName)
			exp.Exporters = append(exp.Exporters, newExporter)
			exp.Metrics = append(exp.Metrics, expName)
		} else {
			err = errors.New(fmt.Sprintf("Не описан тип экспортера %s", expName))
		}
	}

	return exp, err
}

func (exp *Metrics) Contains(name string) bool {
	if len(exp.Metrics) == 0 {
		return true // Если не задали метрики через параметр, то используем все метрики
	}

	for _, item := range exp.Metrics {
		if strings.Trim(item, " ") == strings.Trim(name, " ") {
			return true
		}
	}

	return false
}

func (exp *Metrics) findExporter(names ...string) (result []model.IExporter) {
	for _, name := range names {
		for i, _ := range exp.Exporters {
			if strings.EqualFold(exp.Exporters[i].GetName(), strings.Trim(name, " ")) || name == "all" {
				result = append(result, exp.Exporters[i])
			}
		}
	}

	return result
}

func Pause(metrics *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, fmt.Sprintf("Метод %q не поддерживается", r.Method), http.StatusInternalServerError)
			return
		}
		logger.DefaultLogger.With("URL", r.URL.RequestURI()).Debug("Пауза")

		metricNames := r.URL.Query().Get("metricNames")
		offsetMinStr := r.URL.Query().Get("offsetMin")

		var offsetMin int
		if offsetMinStr != "" {
			if v, err := strconv.ParseInt(offsetMinStr, 0, 0); err == nil {
				offsetMin = int(v)
				logger.DefaultLogger.Infof("Сбор метрик включится автоматически через %d минут, в %v", offsetMin, time.Now().Add(time.Minute*time.Duration(offsetMin)))
			} else {
				logger.DefaultLogger.With("offsetMin", offsetMinStr).Error(errors.Wrap(err, "Ошибка конвертации offsetMin"))
			}
		}

		logger.DefaultLogger.Infof("Приостановить сбор метрик %q", metricNames)
		exps := metrics.findExporter(strings.Split(metricNames, ",")...)
		for i, _ := range exps {
			exps[i].Pause(exps[i].GetName())

			// автовключение паузы
			if offsetMin > 0 {
				go func(i int) {
					<-time.After(time.Minute * time.Duration(offsetMin))
					exps[i].Continue(exps[i].GetName())
				}(i)
			}
		}
	})
}

func Continue(metrics *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, fmt.Sprintf("Метод %q не поддерживается", r.Method), http.StatusInternalServerError)
			return
		}
		logger.DefaultLogger.With("URL", r.URL.RequestURI()).Debug("Продолжить")

		metricNames := r.URL.Query().Get("metricNames")
		logger.DefaultLogger.Info("Продолжить сбор метрик ", metricNames)

		exps := metrics.findExporter(strings.Split(metricNames, ",")...)
		for _, exp := range exps {
			exp.Continue(exp.GetName())
		}
	})
}

func GetVal[T any](ival interface{}) T {
	var result T

	if v, ok := ival.(T); ok {
		return v
	}

	return result
}

func appendParam(in []string, value string) []string {
	if value != "" {
		in = append(in, value)
	}
	return in
}

// func bytesBuffToString(bb *bytes.Buffer) string {
// 	txt := normalizeEncoding(bb.String())
// 	return txt
// }

// func bytesBuffToString(bb *bytes.Buffer) string {
// 	// Определяем кодировку
// 	enc, name, certain := charset.DetermineEncoding(bb.Bytes(), "text/plain")
// 	// fmt.Printf("Определена кодировка: %s (уверенность: %v)\n", name, certain)

// 	// Преобразуем в UTF-8
// 	decoder := enc.NewDecoder()
// 	reader := transform.NewReader(bytes.NewReader(bb.Bytes()), decoder)
// 	var txt string
// 	if decoded, err := io.ReadAll(reader); err == nil {
// 		txt = string(decoded)
// 	} else {
// 		txt = bb.String()
// 	}
// 	return txt
// }
