package exporter

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/LazarenkoA/prometheus_1C_exporter/explorers/model"
	"github.com/LazarenkoA/prometheus_1C_exporter/settings"

	"github.com/pkg/errors"

	"github.com/prometheus/client_golang/prometheus"
)

type ExporterInfobaseInfo struct {
	BaseRACExporter

	mx          sync.RWMutex
	meterParams MeterParamsCollection
	buff        rasDataCollection
}

var (
	baseList        []map[string]string
	fillBaseListRun sync.Mutex
)

func (exp *ExporterInfobaseInfo) Construct(s *settings.Settings) *ExporterInfobaseInfo {
	exp.BaseExporter = newBase(exp.GetName())
	exp.logger.Info("Создание объекта")

	labelName := s.GetMetricNamePrefix() + exp.GetName()
	exp.gauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        labelName,
			ConstLabels: prometheus.Labels{"ras_host": s.GetRASHostPort()},
			Help:        "Установленные запреты в информационной базе",
		},
		[]string{"base", "datatype"},
	)

	exp.settings = s
	exp.buff = rasDataCollection{}
	exp.meterParams = make(MeterParamsCollection, 0, 10)
	exp.initAllMeterParams()

	// Получаем список баз в кластере
	go exp.fillBaseList()

	return exp
}

func (exp *ExporterInfobaseInfo) initAllMeterParams() {
	params := &(exp.meterParams)
	params.add("scheduled-jobs-deny", "Запрет регламентных заданий", false).setDataType(MeterDataOnOff)
	params.add("sessions-deny", "Запрет начала сеансов", false).setDataType(MeterDataOnOff)
}

func (exp *ExporterInfobaseInfo) getValue() {
	exp.logger.Info("получение данных экспортера")

	if err := exp.getData(); err == nil {
		for _, lv := range exp.buff {
			for _, mp := range exp.meterParams {
				with := lv.GetWith("base")
				with["datatype"] = mp.Name
				exp.gauge.With(with).Set(float64(*lv.metersData[mp.Name]))
			}
		}
		for k := range exp.buff {
			delete(exp.buff, k)
		}

	} else {
		exp.gauge.Reset()
		exp.logger.Error(err)
	}
}

func (exp *ExporterInfobaseInfo) getData() (err error) {
	exp.logger.Debug("Получение данных")

	// проверяем блокировку рег. заданий по каждой базе
	// информация по базе получается довольно долго, особенно если в кластере много баз (например тестовый контур), поэтому делаем через пул воркеров
	type dbinfo struct {
		guid, name string
		lv         labeledValues
	}

	chanIn := make(chan *dbinfo, 5)
	chanOut := make(chan *dbinfo)
	wg := new(sync.WaitGroup)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for db := range chanIn {
				if baseinfo, err := exp.getInfoBase(db.guid, db.name); err == nil {
					lv := newLabeledValues()
					lv.labelsData["base"] = db.name
					lv.labelsData["guid"] = db.guid
					lv.readMeterValues(&baseinfo, exp.meterParams)
					lv.writeToBuf(exp.buff, exp.meterParams, "guid")
					db.lv = *lv
					chanOut <- db
				} else {
					exp.logger.Error(err)
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(chanOut)
	}()

	go func() {
		exp.mx.RLock()
		defer exp.mx.RUnlock()

		for _, item := range baseList {
			exp.logger.Debugf("Запрашиваем информацию для базы %s", item["name"])
			chanIn <- &dbinfo{name: item["name"], guid: item["infobase"]}
		}
		close(chanIn)
	}()

	return nil
}

func (exp *ExporterInfobaseInfo) getInfoBase(baseGuid, basename string) (map[string]string, error) {
	login, pass := exp.settings.GetLogPass(basename)

	var param []string
	if exp.settings.RAC_Host() != "" {
		param = append(param, strings.Join(appendParam([]string{exp.settings.RAC_Host()}, exp.settings.RAC_Port()), ":"))
	}

	param = append(param, "infobase")
	param = append(param, "info")
	param = exp.appendLogPass(param)

	param = append(param, fmt.Sprintf("--cluster=%v", exp.GetClusterID()))
	param = append(param, fmt.Sprintf("--infobase=%v", baseGuid))
	param = append(param, fmt.Sprintf("--infobase-user=%v", login))
	param = append(param, fmt.Sprintf("--infobase-pwd=%v", pass))

	exp.logger.With("param", param).Debugf("Получаем информацию для базы %q", basename)
	if result, err := exp.run(exec.CommandContext(exp.ctx, exp.settings.RAC_Path(), param...)); err != nil {
		exp.logger.Error(err)
		return map[string]string{}, err
	} else {
		var baseInfo []map[string]string
		exp.formatMultiResult(result, &baseInfo)
		if len(baseInfo) > 0 {
			return baseInfo[0], nil
		} else {
			return nil, errors.New(fmt.Sprintf("Не удалось получить информацию по базе %q", basename))
		}
	}
}

func (exp *ExporterInfobaseInfo) findBaseName(ref string) string {
	exp.mx.RLock()
	defer exp.mx.RUnlock()

	for _, b := range baseList {
		if b["infobase"] == ref {
			return b["name"]
		}
	}

	return ""
}

func (exp *ExporterInfobaseInfo) fillBaseList() {
	// fillBaseList вызывается из нескольких мест, но нам достаточно одной горутины, остальные пусть встают в очередь
	// если завершится текущий экспортер, то стартанет следующий
	fillBaseListRun.Lock()
	defer fillBaseListRun.Unlock()

	// редко, но все же список баз может быть изменен, поэтому делаем обновление периодическим, чтобы не приходилось перезапускать экспортер
	t := time.NewTicker(time.Hour)
	defer t.Stop()

	for {
		exp.logger.Info("получаем список баз")
		if err := exp.getListInfobase(); err != nil {
			exp.logger.Error(errors.Wrap(err, "ошибка получения списка баз"))
			t.Reset(time.Minute) // если была ошибка пробуем через минуту, если ошибка пропала, то вернем часовой интервал
		} else {
			t.Reset(time.Hour)
		}

		select {
		case <-t.C:
		case <-exp.ctx.Done():
			exp.logger.Debug("context is done")
			return
		}
	}

}

func (exp *ExporterInfobaseInfo) getListInfobase() error {
	exp.mx.Lock()
	defer exp.mx.Unlock()

	var param []string
	if exp.settings.RAC_Host() != "" {
		param = append(param, strings.Join(appendParam([]string{exp.settings.RAC_Host()}, exp.settings.RAC_Port()), ":"))
	}

	param = append(param, "infobase")
	param = append(param, "summary")
	param = append(param, "list")
	param = exp.appendLogPass(param)
	param = append(param, fmt.Sprintf("--cluster=%v", exp.GetClusterID()))

	if result, err := exp.run(exec.CommandContext(exp.ctx, exp.settings.RAC_Path(), param...)); err != nil {
		return err
	} else {
		exp.formatMultiResult(result, &baseList)
	}

	return nil
}

func (exp *ExporterInfobaseInfo) Collect(ch chan<- prometheus.Metric) {
	if exp.isLocked.Load() {
		return
	}

	exp.getValue()
	exp.gauge.Collect(ch)
}

func (exp *ExporterInfobaseInfo) GetType() model.MetricType {
	return model.TypeRAC
}

func (exp *ExporterInfobaseInfo) GetName() string {
	return "ibinfo"
}
