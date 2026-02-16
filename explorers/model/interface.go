package model

import (
	"github.com/LazarenkoA/prometheus_1C_exporter/settings"
	"github.com/prometheus/client_golang/prometheus"
)

//go:generate mockgen -source=./interface.go -destination=../mock/BaseExporterMock.go
type IExporter interface {
	prometheus.Collector

	Pause(expName string)
	Continue(expName string)
	Stop()
	GetType() MetricType
	Construct(s *settings.Settings, metricName string) IExporter
	GetName() string
}

type MetricType byte

const (
	Undefined MetricType = iota
	TypeRAC
	TypeOS
)
