package exporter

import (
	"github.com/LazarenkoA/prometheus_1C_exporter/explorers/model"
	"github.com/LazarenkoA/prometheus_1C_exporter/settings"

	"github.com/prometheus/client_golang/prometheus"
)

type ExporterCheckSheduleJob struct {
	ExporterInfobaseInfo
}

func (exp *ExporterCheckSheduleJob) Construct(s *settings.Settings, metricName string) model.IExporter {
	exp.BaseExporter = newBase(metricName)
	exp.logger.Info("Создание объекта")

	labelName := s.GetNamePrefix() + exp.GetName()
	exp.gauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        labelName,
			ConstLabels: prometheus.Labels{"ras_host": s.GetRASHostPort()},
			Help:        "Состояние галки \"блокировка регламентных заданий\": если галка установлена - значение будет 1; иначе 0; или метрика будет отсутствовать",
		},
		[]string{"base"},
	)

	exp.settings = s
	exp.buff = newLabeledValuesCollection("guid")
	exp.initAllMeterParams()

	// Получаем список баз в кластере
	go exp.fillBaseList()

	return exp
}

func (exp *ExporterCheckSheduleJob) getValue() {
	exp.logger.Info("получение данных экспортера")

	exp.gauge.Reset()
	if err := exp.getData(); err == nil {
		for _, lv := range exp.buff.dataMap {
			exp.gauge.With(lv.GetWith("base")).Set(float64(*lv.metersData["scheduledjobsdeny"]))
		}
	} else {
		exp.logger.Error(err)
	}
	exp.buff.clear()
}

func (exp *ExporterCheckSheduleJob) Collect(ch chan<- prometheus.Metric) {
	if exp.isLocked.Load() {
		return
	}

	exp.getValue()
	exp.gauge.Collect(ch)
}

// func (exp *ExporterCheckSheduleJob) GetName() string {
// 	return "shedule_job"
// }
