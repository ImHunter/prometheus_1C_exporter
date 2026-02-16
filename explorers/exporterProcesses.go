package exporter

import (
	"strconv"

	"github.com/LazarenkoA/prometheus_1C_exporter/explorers/model"
	"github.com/LazarenkoA/prometheus_1C_exporter/settings"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/process"
)

//go:generate mockgen -source=$GOFILE -package=mock_models -destination=./mock/mockProcesses.go
type IProcessesInfo interface {
	Processes() ([]*process.Process, error)
}

type Processes struct {
	BaseExporter

	hInfo IProcessesInfo
}

func (exp *Processes) Construct(s *settings.Settings, metricName string) model.IExporter {
	exp.BaseExporter = newBase(metricName)
	exp.logger.Info("Создание объекта")

	labelName := s.GetNamePrefix() + exp.GetName()
	exp.summary = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       labelName,
			Help:       "Метрики CPU/памяти в разрезе процессов",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"host", "pid", "procName", "metrics"},
	)

	exp.hInfo = new(hardwareInfo)
	exp.settings = s

	return exp
}

func (exp *Processes) getValue() {
	exp.logger.Info("получение данных экспортера")

	processes, err := exp.hInfo.Processes()
	if err != nil {
		exp.logger.Error(errors.Wrap(err, "get processes error"))
		return
	}

	exp.summary.Reset()
	for _, p := range processes {
		var memInfo process.MemoryInfoStat

		cpuPercent, _ := p.CPUPercent()
		memPercent, _ := p.MemoryPercent()
		if mInfo, err := p.MemoryInfo(); err == nil {
			memInfo = *mInfo
		}

		if procName, err := p.Name(); err == nil {
			exp.summary.WithLabelValues(exp.host, strconv.Itoa(int(p.Pid)), procName, "cpu").Observe(cpuPercent)
			exp.summary.WithLabelValues(exp.host, strconv.Itoa(int(p.Pid)), procName, "memoryPercent").Observe(float64(memPercent))
			exp.summary.WithLabelValues(exp.host, strconv.Itoa(int(p.Pid)), procName, "memoryRSS").Observe(float64(memInfo.RSS))
			exp.summary.WithLabelValues(exp.host, strconv.Itoa(int(p.Pid)), procName, "memoryVMS").Observe(float64(memInfo.VMS))
		}
	}
}

func (exp *Processes) Collect(ch chan<- prometheus.Metric) {
	if exp.isLocked.Load() {
		return
	}

	exp.getValue()
	exp.summary.Collect(ch)
}

// func (cpu *Processes) GetName() string {
// 	return "processes"
// }

func (exp *Processes) GetType() model.MetricType {
	return model.TypeOS
}

// sum(topk(5, processes{quantile="0.99", metrics="memoryRSS"})) by (procName)
