package exporter

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/LazarenkoA/prometheus_1C_exporter/explorers/model"
	"github.com/LazarenkoA/prometheus_1C_exporter/settings"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/pkg/errors"

	"github.com/prometheus/client_golang/prometheus"
)

type ExporterSessions struct {
	ExporterInfobaseInfo

	mx                 sync.RWMutex
	cache              *expirable.LRU[string, []map[string]string]
	getMetricKindsFunc func(s *settings.Settings) []settings.TypeMetricKind
}

type labelValuesMap map[string]int

func (exp *ExporterSessions) Construct(s *settings.Settings, metricName string) model.IExporter {
	exp.BaseExporter = newBase(metricName)
	exp.logger.Info("Создание объекта")
	exp.getMetricKindsFunc = func(s *settings.Settings) []settings.TypeMetricKind {
		return s.MetricKinds.Session
	}

	labelName := s.GetNamePrefix() + exp.GetName()

	if exp.usedSummary(s) {
		exp.summary = prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Name:        labelName,
				Help:        "Сессии 1С",
				Objectives:  map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
				ConstLabels: prometheus.Labels{"host": exp.host, "ras_host": s.GetRASHostPort()},
			},
			[]string{"base"},
		)
	}

	if exp.usedGauge(s) {
		exp.gauge = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name:        labelName + "_gauge",
				Help:        "Сессии 1С (Gauge)",
				ConstLabels: prometheus.Labels{"host": exp.host, "ras_host": s.GetRASHostPort()},
			},
			[]string{"base", "appid"},
		)
	}

	exp.settings = s
	exp.ExporterInfobaseInfo.settings = s
	exp.cache = expirable.NewLRU[string, []map[string]string](5, nil, time.Second*5)
	exp.buff = newLabeledValuesCollection("infobase", "appid")

	exp.initAllMeterParams()

	go exp.fillBaseList()

	return exp
}

func (exp *ExporterSessions) initAllMeterParams() {

	params := &(exp.meterParams)
	params.add("count", "count", ApplyMethodInc).funcValueReader = createSessReader()
}

func (exp *ExporterSessions) getValue() {

	if !exp.isAllowedReading() {
		return
	}

	exp.logger.Info("получение данных экспортера")
	ses, err := exp.getSessions()
	exp.setAllowedReading(err == nil)
	if err != nil {
		exp.logger.Error(errors.Wrap(err, "getSessions error"))
		return
	}

	for _, item := range ses {

		lv := newLabeledValues()
		lv.labelsData["infobase"] = item["infobase"]
		lv.labelsData["appid"] = item["app-id"]
		lv.readMeterValues(&item, exp.meterParams)
		lv.applyToCollection(&exp.buff, exp.meterParams)

		lv = newLabeledValues()
		lv.labelsData["infobase"] = item["infobase"]
		lv.labelsData["appid"] = "*"
		lv.readMeterValues(&item, exp.meterParams)
		lv.applyToCollection(&exp.buff, exp.meterParams)

	}

	for _, lv := range exp.buff.data() {
		lv.labelsData["base"] = exp.findBaseName(lv.labelsData["infobase"])
	}

	if exp.usedSummary(exp.settings) {
		exp.summary.Reset()
		for _, lv := range exp.buff.data() {
			if lv.labelsData["appid"] == "*" && lv.metersData["count"] != nil {
				exp.summary.With(lv.GetWith("base")).Observe(float64(*lv.metersData["count"]))
			}
		}
	}

	if exp.usedGauge(exp.settings) {
		exp.gauge.Reset()
		for _, lv := range exp.buff.data() {
			if lv.labelsData["appid"] != "*" && lv.metersData["count"] != nil {
				exp.gauge.With(lv.GetWith("base", "appid")).Set(float64(*lv.metersData["count"]))
			}
		}
	}
	exp.buff.clear()
}

func (exp *ExporterSessions) getSessions() (sesData []map[string]string, err error) {
	exp.mx.Lock()
	defer exp.mx.Unlock()

	if v, ok := exp.cache.Get("result"); ok {
		exp.logger.Debug("данные получены из кеша")
		return v, nil
	}

	var param []string
	if exp.settings.RAC_Host() != "" {
		param = append(param, strings.Join(appendParam([]string{exp.settings.RAC_Host()}, exp.settings.RAC_Port()), ":"))
	}

	param = append(param, "session", "list")
	param = exp.appendLogPass(param)

	param = append(param, fmt.Sprintf("--cluster=%v", exp.GetClusterID()))

	cmdCommand := exec.CommandContext(exp.ctx, exp.settings.RAC_Path(), param...)
	if result, err := exp.run(cmdCommand); err != nil {
		exp.logger.Error(err)
		return []map[string]string{}, err
	} else {
		exp.formatMultiResult(result, &sesData)
	}

	exp.cache.Add("result", sesData)
	return sesData, nil
}

func (exp *ExporterSessions) Collect(ch chan<- prometheus.Metric) {
	if exp.isLocked.Load() {
		return
	}

	exp.getValue()
	if exp.usedSummary(exp.settings) {
		exp.summary.Collect(ch)
	}
	if exp.usedGauge(exp.settings) {
		exp.gauge.Collect(ch)
	}
}

// func (exp *ExporterSessions) GetName() string {
// 	return "session"
// }

func (exp *ExporterSessions) GetType() model.MetricType {
	return model.TypeRAC
}

func (exp *ExporterSessions) usedSummary(s *settings.Settings) bool {
	if exp.getMetricKindsFunc == nil {
		return false
	}
	return slices.Contains(exp.getMetricKindsFunc(s), settings.KindSummary)
}

func (exp *ExporterSessions) usedGauge(s *settings.Settings) bool {
	if exp.getMetricKindsFunc == nil {
		return false
	}
	return slices.Contains(exp.getMetricKindsFunc(s), settings.KindGauge)
}

func (exp *ExporterSessions) usedHistogram(s *settings.Settings) bool {
	if exp.getMetricKindsFunc == nil {
		return false
	}
	return slices.Contains(exp.getMetricKindsFunc(s), settings.KindNativeHistogram)
}

// Возвращает настройки видов метрик экспортера. Для других видов экспортеров требуется переопределить,
// чтобы методы usedSummary, usedGauge, usedHistogram корректно работали.
func (exp *ExporterSessions) getMetricKinds(s *settings.Settings) []settings.TypeMetricKind {
	return s.MetricKinds.Session
}

func createSessReader() valueReader {
	return func(item *map[string]string, mp *MeterParams) *int64 {
		val := new(int64)
		*val = 1
		return val
	}
}
