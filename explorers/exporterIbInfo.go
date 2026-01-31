package exporter

type ExporterIbInfo struct {
	ExporterCheckSheduleJob
}

func (exp *ExporterIbInfo) GetName() string {
	return "ibinfo"
}
