package exporter

import (
	"time"

	"github.com/LazarenkoA/prometheus_1C_exporter/explorers/model"
	"github.com/LazarenkoA/prometheus_1C_exporter/settings"
	"github.com/hashicorp/golang-lru/v2/expirable"

	"github.com/prometheus/client_golang/prometheus"
)

type ExporterSessionsData struct {
	ExporterSessions

	histograms map[string]*prometheus.HistogramVec
}

var localTimeLocation *time.Location

func (exp *ExporterSessionsData) Construct(s *settings.Settings) *ExporterSessionsData {

	localTimeLocation, _ = time.LoadLocation("Local")

	exp.BaseExporter = newBase(exp.GetName())
	exp.logger.Info("Создание объекта")

	exp.meterParams = make(MeterParamsCollection, 0, 10)
	exp.initAllMeterParams()

	labelName := s.GetMetricNamePrefix() + exp.GetName()

	if exp.usedSummary(s) {
		exp.summary = prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Name:        labelName,
				Help:        "Показатели сессий из кластера 1С",
				Objectives:  map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
				ConstLabels: prometheus.Labels{"ras_host": s.GetRASHostPort(), "host": exp.host},
			},
			[]string{"base", "user", "id", "datatype", "appid"},
		)
	}

	if exp.usedHistogram(s) {

		exp.histograms = map[string]*prometheus.HistogramVec{}
		for _, mp := range exp.meterParams {
			exp.histograms[mp.Name] = prometheus.NewHistogramVec(
				prometheus.HistogramOpts{
					Name:                            labelName + "_" + mp.Name,
					Help:                            "Гистограммы показателя сессий кластера 1С: " + mp.Description,
					ConstLabels:                     prometheus.Labels{"ras_host": s.GetRASHostPort(), "host": exp.host},
					NativeHistogramBucketFactor:     1.1,
					NativeHistogramMaxBucketNumber:  20,
					NativeHistogramMinResetDuration: 1 * time.Hour,
				},
				[]string{"base", "appid"},
			)
		}
	}

	exp.buff = rasDataCollection{}
	exp.settings = s
	exp.ExporterInfobaseInfo.settings = s
	exp.cache = expirable.NewLRU[string, []map[string]string](5, nil, time.Second*5)
	go exp.fillBaseList() // в данном экспортере нужен список баз

	// эта метрика содержит показатели memory-current, write-current и прочие current
	// прометей может приходить за данными довольно редко, раз в 15 секунд, или раз в минуту, как правило серверный вызов 1С проходит быстрее и такие показатели не будут прочитаны
	// показатели нужно собирать довольно часто, чаще чем приходит прометей за данными, их просто накапливаем в буфер, потом отдаем прометею когда он придет
	go exp.collectMetrics(time.Second * 5)

	return exp
}

func (exp *ExporterSessionsData) collectMetrics(delay time.Duration) {
	for {
		ses, _ := exp.getSessions()
		for _, item := range ses {
			exp.loadRasRow(&item)
		}

		select {
		case <-time.After(delay):
		case <-exp.ctx.Done():
			return
		}
	}
}

func (exp *ExporterSessionsData) getValue() {
	exp.logger.Info("получение данных экспортера")

	var with prometheus.Labels
	var exemplarChecker ExemplarChecker
	var usedExemplars bool

	exp.mx.Lock()
	defer exp.mx.Unlock()

	if exp.usedSummary(exp.settings) {
		exp.summary.Reset()
	}

	if exp.usedHistogram(exp.settings) {
		if exp.usedExemplars() {
			exemplarChecker = findExemplars(&exp.buff)
			usedExemplars = true
		}
		for _, h := range exp.histograms {
			h.Reset()
		}
	}

	for k, v := range exp.buff {

		if exp.usedSummary(exp.settings) {
			with = v.GetWithAll()
			with["id"] = k
			for n, m := range v.metersData {
				if m == nil {
					continue
				}
				with["datatype"] = n
				exp.summary.With(with).Observe(float64(*m))
			}
		}

		if exp.usedHistogram(exp.settings) {
			withLabel := v.GetWith("base", "appid")
			withExemplar := v.GetWith("id", "user")
			for n, m := range v.metersData {
				if m == nil {
					continue
				}
				hist := exp.histograms[n]
				if usedExemplars && exemplarChecker.isExemplar(k, v.labelsData["base"], v.labelsData["appid"], n) {
					hist.With(withLabel).(prometheus.ExemplarObserver).ObserveWithExemplar(float64(*m), withExemplar)
				} else {
					hist.With(withLabel).Observe(float64(*m))
				}
			}
		}

		toDel := exp.buff[k]
		clear(toDel.labelsData)
		clear(toDel.metersData)
		exp.buff[k] = nil
		delete(exp.buff, k)

		clear(exemplarChecker.keys)
		clear(exemplarChecker.values)

	}
}

func (exp *ExporterSessionsData) Collect(ch chan<- prometheus.Metric) {

	if exp.isLocked.Load() {
		return
	}

	exp.getValue()

	if exp.usedSummary(exp.settings) {
		exp.summary.Collect(ch)
	}

	if exp.usedHistogram(exp.settings) {
		for _, h := range exp.histograms {
			h.Collect(ch)
		}
	}

}

func (exp *ExporterSessionsData) GetName() string {
	return "sessions_data"
}

func (exp *ExporterSessionsData) GetType() model.MetricType {
	return model.TypeRAC
}

func (exp *ExporterSessionsData) loadRasRow(rasRowItem *map[string]string) {

	lv := newLabeledValues()

	lv.labelsData["appid"] = (*rasRowItem)["app-id"]
	lv.labelsData["user"] = (*rasRowItem)["user-name"]
	lv.labelsData["id"] = (*rasRowItem)["session-id"]
	lv.labelsData["base"] = exp.findBaseName((*rasRowItem)["infobase"])

	lv.readMeterValues(rasRowItem, exp.meterParams)

	exp.mx.Lock()
	defer exp.mx.Unlock()

	lv.writeToBuf(exp.buff, exp.meterParams, "id")

}

func (exp *ExporterSessionsData) getMetricKinds(s *settings.Settings) []settings.TypeMetricKind {
	return s.MetricKinds.SessionsData
}

func (exp *ExporterSessionsData) usedExemplars() bool {
	return exp.settings.Other.UseExemplars
}

func (exp *ExporterSessionsData) initAllMeterParams() {

	params := &(exp.meterParams)

	params.add("memory-total", "Память (всего)", false)
	params.add("memory-current", "Память (текущая)", true)
	params.add("read-current", "Чтение (текущее)", true)
	params.add("read-total", "Чтение (всего)", false)
	params.add("write-current", "Запись (текущая)", true)
	params.add("write-total", "Запись (всего)", false)
	params.add("duration-current", "Время вызова (текущее)", true)
	// Устанавливается дополнительное поле в OtherSourceFields для совместимости со старой платформой.
	// Ссылка на багборд https://bugboard.v8.1c.ru/error/000150161
	params.add("duration-current-dbms", "Длительность текущего вызова СУБД", true).setOtherSourceFields([]string{"duration current-dbms"})
	params.add("duration-all", "Общее время работы сессии", true)
	params.add("duration-all-service", "Время работы сервисов кластера с начала сеанса или соединения", true)
	params.add("duration-all-dbms", "Общее время выполнения операций в СУБД", true)
	params.add("cpu-time-current", "Процессорное время (текущее)", true)
	params.add("cpu-time-total", "Процессорное время (всего)", false)
	params.add("dbms-bytes-all", "Объем данных, переданных из/в СУБД", true)
	params.add("calls-all", "Количество вызовов (запросов) за все время", true)
	params.add("blocked-by-ls", "Количество блокировок локального сервиса", true)
	params.add("blocked-by-dbms", "Количество блокировок СУБД", true)
	params.add("db-proc-took", "Время соединения СУБД", true)
	params.add("db-proc-took-at", "Продолжительность соединения СУБД ", true).setName("dbproctookatduration").setDataType(MeterDataDuration)
	params.add("started-at", "Длительность сеанса", true).setName("startedatduration").setDataType(MeterDataDuration)
	params.add("started-at", "Начало сеанса", true).setDataType(MeterDataUnixDate)
	params.add("last-active-at", "Прошло времени с последней активности сессии", true).setName("lastactiveatduration").setDataType(MeterDataDuration)
	params.add("last-active-at", "Время последней активности сессии", true).setDataType(MeterDataUnixDate)
	params.add("passive-session-hibernate-time", "Время в секундах бездействия до перевода сессии в спящий режим", true)
	params.add("hibernate-session-terminate-time", "Время, через которое сессия завершается после перехода в спящий режим", true)

}

func findExemplars(d *rasDataCollection) ExemplarChecker {

	// Пока решено, что экземплярами по счетчикам будут сессии, где обнаружено максимальное значение

	var maxVal int64
	var base string

	finder := ExemplarChecker{
		data:   d,
		keys:   make(map[string]map[string]string),
		values: make(map[string]map[string]int64),
	}

	for sessId, sessData := range *finder.data {
		base = sessData.labelsData["base"]
		for paramId, paramVal := range sessData.metersData {

			if finder.values[base] == nil {
				finder.values[base] = make(map[string]int64)
				finder.keys[base] = make(map[string]string)
			}

			maxVal = finder.values[base][paramId]
			if *paramVal > maxVal {
				finder.values[base][paramId] = *paramVal
				finder.keys[base][paramId] = sessId
			}
		}
	}

	return finder
}

type ExemplarChecker struct {
	keys   map[string]map[string]string
	values map[string]map[string]int64
	data   *rasDataCollection
}

func (finder *ExemplarChecker) isExemplar(sess string, base string, appid string, param string) bool {
	targetSess := finder.keys[base][param]
	return sess == targetSess
}
