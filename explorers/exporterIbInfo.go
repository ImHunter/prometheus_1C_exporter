package exporter

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LazarenkoA/prometheus_1C_exporter/explorers/model"
	"github.com/LazarenkoA/prometheus_1C_exporter/settings"

	"github.com/pkg/errors"

	"github.com/prometheus/client_golang/prometheus"
)

type ExporterInfobaseInfo struct {
	BaseRACExporter

	mx             sync.RWMutex
	nextScrapeTime *time.Time
	currentBackoff time.Duration
	meterParams    MeterParamsCollection
	buff           *labeledValuesCollection
}

var (
	baseList        []map[string]string
	fillBaseListRun sync.Mutex
)

const (
	initialBackoff = 30 * time.Second
	backoffFactor  = 1.2
	maxBackoff     = time.Hour
)

func (exp *ExporterInfobaseInfo) Construct(s *settings.Settings, metricName string) model.IExporter {
	exp.BaseExporter = newBase(metricName)
	exp.logger.Info("Создание объекта")

	labelName := s.GetNamePrefix() + exp.GetName()
	exp.gauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        labelName,
			ConstLabels: prometheus.Labels{"ras_host": s.GetRASHostPort()},
			Help:        "Установленные запреты в информационной базе",
		},
		[]string{"base", "datatype"},
	)

	exp.settings = s
	exp.buff = newLabeledValuesCollection("guid")
	exp.initAllMeterParams()

	// Получаем список баз в кластере
	go exp.fillBaseList()

	return exp
}

func (exp *ExporterInfobaseInfo) initAllMeterParams() {
	params := &(exp.meterParams)
	params.add("scheduled-jobs-deny", "Запрет регламентных заданий", ApplyMethodSet).setDataType(MeterDataOnOff)
	params.add("sessions-deny", "Запрет начала сеансов", ApplyMethodSet).setDataType(MeterDataOnOff)
}

func (exp *ExporterInfobaseInfo) getValue() {
	exp.logger.Info("получение данных экспортера")

	exp.gauge.Reset()
	var iVal *int64
	var gVal float64

	if err := exp.getData(); err == nil {
		for _, lv := range exp.buff.data() {
			for _, mp := range exp.meterParams {
				iVal = lv.metersData[mp.Name]
				if iVal == nil {
					continue
				} else {
					gVal = float64(*iVal)
				}
				with := lv.GetWith("base")
				with["datatype"] = mp.Name
				exp.gauge.With(with).Set(gVal)
			}
		}
	} else {
		exp.logger.Error(err)
	}
	exp.buff.clear()
}

func (exp *ExporterInfobaseInfo) getData() (err error) {
	exp.logger.Debug("Получение данных")

	type dbinfo struct {
		guid, name string
		lv         labeledValues
	}

	if !exp.isAllowedReading() {
		exp.logger.Warn("Чтение %s приостановлено", exp.GetName())
		return nil
	}

	exp.mx.RLock()
	currentBases := make([]map[string]string, len(baseList))
	copy(currentBases, baseList)
	exp.mx.RUnlock()

	if len(currentBases) == 0 {
		return nil
	}

	chanIn := make(chan *dbinfo, len(currentBases))
	chanOut := make(chan *dbinfo, len(currentBases))

	var hasError int32
	var hasSuccess int32
	var wg sync.WaitGroup

	// Воркеры
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					atomic.StoreInt32(&hasError, 1)
					exp.logger.Errorw("Паника в воркере", "recover", r)
				}
			}()

			for db := range chanIn {
				type infoResult struct {
					baseinfo map[string]string
					err      error
				}
				resultCh := make(chan infoResult, 1)

				go func() {
					defer func() {
						if r := recover(); r != nil {
							resultCh <- infoResult{err: fmt.Errorf("паника в getInfoBase: %v", r)}
						}
					}()
					bi, e := exp.getInfoBase(db.guid, db.name) // старый вызов без контекста
					resultCh <- infoResult{bi, e}
				}()

				var baseinfo map[string]string
				var err error

				select {
				case res := <-resultCh:
					baseinfo, err = res.baseinfo, res.err
				case <-time.After(10 * time.Second):
					err = fmt.Errorf("превышено время ожидания (10 с) для базы %s", db.name)
				}

				lv := newLabeledValues()
				if err == nil {
					atomic.StoreInt32(&hasSuccess, 1)
					exp.logger.With("dbguid", db.guid, "dbname", db.name).Debug("Успешно получены данные")
					lv.labelsData["base"] = db.name
					lv.labelsData["guid"] = db.guid
					lv.readMeterValues(&baseinfo, exp.meterParams)
					lv.applyToCollection(exp.buff, exp.meterParams)
				} else {
					undefinedVal := int64(-1)
					atomic.StoreInt32(&hasError, 1)
					exp.logger.With("dbguid", db.guid, "dbname", db.name).Debug("Ошибка при получении данных")
					lv.labelsData["base"] = db.name
					lv.labelsData["guid"] = db.guid
					lv.metersData["scheduledjobsdeny"] = &undefinedVal
					lv.metersData["sessionsdeny"] = &undefinedVal
					lv.applyToCollection(exp.buff, exp.meterParams)
				}

				db.lv = *lv
				chanOut <- db
			}
		}()
	}

	// Отправка заданий
	go func() {
		for _, item := range currentBases {
			exp.logger.Debugf("Запрашиваем информацию для базы %s", item["name"])
			chanIn <- &dbinfo{
				name: item["name"],
				guid: item["infobase"],
			}
		}
		close(chanIn)
	}()

	// Закрытие выходного канала
	go func() {
		wg.Wait()
		close(chanOut)
	}()

	// Чтение результатов
	for range chanOut {
		// просто читаем
	}

	// Единое применение задержки
	if atomic.LoadInt32(&hasError) == 1 && atomic.LoadInt32(&hasSuccess) == 0 {
		exp.setAllowedReading(false)
	} else {
		exp.setAllowedReading(true)
	}

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
	if login != "" {
		param = append(param, fmt.Sprintf("--infobase-user=%v", login))
	}
	if pass != "" {
		param = append(param, fmt.Sprintf("--infobase-pwd=%v", pass))
	}

	exp.logger.With("param", param).Debugf("Получаем информацию для базы %q", basename)
	if result, err := exp.run(exec.CommandContext(exp.ctx, exp.settings.RAC_Path(), param...)); err != nil {
		exp.logger.With("infobase-user", login).Error(err)
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

	defer func() {
		if r := recover(); r != nil {
			exp.logger.Errorw("Паника в fillBaseList", "recover", r)
			// Перезапускаем горутину
			time.Sleep(5 * time.Second) // небольшая задержка перед перезапуском
			go exp.fillBaseList()
		}
	}()

	// fillBaseList вызывается из нескольких мест, но нам достаточно одной горутины
	fillBaseListRun.Lock()
	defer fillBaseListRun.Unlock()

	// редко, но все же список баз может быть изменен, поэтому делаем обновление периодическим
	t := time.NewTicker(time.Hour)
	defer t.Stop()

	for {
		exp.logger.Info("получаем список баз")
		if err := exp.getListInfobase(); err != nil {
			exp.logger.Error(errors.Wrap(err, "ошибка получения списка баз"))
			t.Reset(time.Minute) // если была ошибка пробуем через минуту
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

func (exp *ExporterInfobaseInfo) setAllowedReading(isOk bool) {
	exp.mx.Lock()
	defer exp.mx.Unlock()

	if isOk {
		if exp.nextScrapeTime != nil {
			exp.logger.Info("Отключено ограничение чтения")
			exp.nextScrapeTime = nil
			exp.currentBackoff = 0
		}
	} else {
		now := time.Now()

		if exp.nextScrapeTime == nil {
			exp.currentBackoff = initialBackoff
			next := now.Add(exp.currentBackoff)
			exp.nextScrapeTime = &next
			exp.logger.Info("Включено ограничение чтения")
		} else {
			exp.currentBackoff = time.Duration(float64(exp.currentBackoff) * backoffFactor)
			if exp.currentBackoff > maxBackoff {
				exp.currentBackoff = maxBackoff
			}

			next := now.Add(exp.currentBackoff)
			exp.nextScrapeTime = &next
			exp.logger.Debugf("Увеличена задержка до %v", exp.currentBackoff)
		}
	}
}

func (exp *ExporterInfobaseInfo) isAllowedReading() bool {
	exp.mx.RLock()
	defer exp.mx.RUnlock()

	if exp.nextScrapeTime == nil {
		return true
	}
	return time.Now().After(*exp.nextScrapeTime)
}
