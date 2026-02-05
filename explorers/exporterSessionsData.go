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

func (exp *ExporterSessionsData) Construct(s *settings.Settings) *ExporterSessionsData {

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

	exp.buff = newLabeledValuesCollection("id")
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
			exemplarChecker = newExemplarChecker(&exp.buff, "base")
			usedExemplars = true
		}
		for _, h := range exp.histograms {
			h.Reset()
		}
	}

	for k, lv := range exp.buff.dataMap {

		if exp.usedSummary(exp.settings) {
			with = lv.GetWithAll()
			with["id"] = k[0]
			for n, m := range lv.metersData {
				if m == nil {
					continue
				}
				with["datatype"] = n
				exp.summary.With(with).Observe(float64(*m))
			}
		}

		if exp.usedHistogram(exp.settings) {
			withLabel := lv.GetWith("base", "appid")
			withExemplar := lv.GetWith("id", "user")
			for n, m := range lv.metersData {
				if m == nil {
					continue
				}
				hist := exp.histograms[n]
				if usedExemplars && exemplarChecker.isExemplar(*lv, n) {
					hist.With(withLabel).(prometheus.ExemplarObserver).ObserveWithExemplar(float64(*m), withExemplar)
				} else {
					hist.With(withLabel).Observe(float64(*m))
				}
			}
		}

		exp.buff.deleteByKey(k)

		// clear(exemplarChecker.keys)
		// clear(exemplarChecker.values)

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

	lv.writeToBuf(exp.buff, exp.meterParams)

}

func (exp *ExporterSessionsData) getMetricKinds(s *settings.Settings) []settings.TypeMetricKind {
	return s.MetricKinds.SessionsData
}

func (exp *ExporterSessionsData) usedExemplars() bool {
	return exp.settings.Other.UseExemplars
}

func (exp *ExporterSessionsData) initAllMeterParams() {

	params := &(exp.meterParams)

	params.add("memory-total", "Память (всего)", ApplyMethodSet)
	params.add("memory-current", "Память (текущая)", ApplyMethodMax)
	params.add("read-current", "Чтение (текущее)", ApplyMethodMax)
	params.add("read-total", "Чтение (всего)", ApplyMethodSet)
	params.add("write-current", "Запись (текущая)", ApplyMethodMax)
	params.add("write-total", "Запись (всего)", ApplyMethodSet)
	params.add("duration-current", "Время вызова (текущее)", ApplyMethodMax)
	// Устанавливается дополнительное поле в OtherSourceFields для совместимости со старой платформой.
	// Ссылка на багборд https://bugboard.v8.1c.ru/error/000150161
	params.add("duration-current-dbms", "Длительность текущего вызова СУБД", ApplyMethodMax).setOtherSourceFields([]string{"duration current-dbms"})
	params.add("duration-all", "Общее время работы сессии", ApplyMethodMax)
	params.add("duration-all-service", "Время работы сервисов кластера с начала сеанса или соединения", ApplyMethodMax)
	params.add("duration-all-dbms", "Общее время выполнения операций в СУБД", ApplyMethodMax)
	params.add("cpu-time-current", "Процессорное время (текущее)", ApplyMethodMax)
	params.add("cpu-time-total", "Процессорное время (всего)", ApplyMethodSet)
	params.add("dbms-bytes-all", "Объем данных, переданных из/в СУБД", ApplyMethodSet)
	params.add("calls-all", "Количество вызовов (запросов) за все время", ApplyMethodMax)
	params.add("blocked-by-ls", "Количество блокировок локального сервиса", ApplyMethodMax)
	params.add("blocked-by-dbms", "Количество блокировок СУБД", ApplyMethodMax)
	params.add("db-proc-took", "Время соединения СУБД", ApplyMethodMax)
	params.add("db-proc-took-at", "Продолжительность соединения СУБД ", ApplyMethodMax).setName("dbproctookatduration").setDataType(MeterDataDuration)
	params.add("started-at", "Длительность сеанса", ApplyMethodMax).setName("startedatduration").setDataType(MeterDataDuration)
	params.add("started-at", "Начало сеанса", ApplyMethodMax).setDataType(MeterDataUnixDate)
	params.add("last-active-at", "Прошло времени с последней активности сессии", ApplyMethodMax).setName("lastactiveatduration").setDataType(MeterDataDuration)
	params.add("last-active-at", "Время последней активности сессии", ApplyMethodMax).setDataType(MeterDataUnixDate)
	params.add("passive-session-hibernate-time", "Время в секундах бездействия до перевода сессии в спящий режим", ApplyMethodMax)
	params.add("hibernate-session-terminate-time", "Время, через которое сессия завершается после перехода в спящий режим", ApplyMethodMax)

}
