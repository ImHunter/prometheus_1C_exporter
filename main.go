package main

////go:generate go run install/release.go
// //go:generate git commit -am "bump $PROM_VERSION"
// //go:generate git tag -af $PROM_VERSION -m "$PROM_VERSION"

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/LazarenkoA/prometheus_1C_exporter/autoupdate"
	"github.com/LazarenkoA/prometheus_1C_exporter/logger"
	"github.com/LazarenkoA/prometheus_1C_exporter/settings"
	"github.com/judwhite/go-svc"
)

var (
	version = "dev"
	// version   = "1.5.1"
	gitCommit = "undefined"
)

func main() {

	var settingsPath, port string
	var help, v bool

	flag.StringVar(&settingsPath, "settings", "", "Путь к файлу настроек")
	flag.StringVar(&port, "port", "9091", "Порт для прослушивания")
	flag.BoolVar(&help, "help", false, "Помощь")
	flag.BoolVar(&v, "version", false, "Версия")
	flag.Parse()

	if help {
		flag.Usage()
		return
	}

	if v {
		fmt.Printf("Версия: %s\n", version)
		return
	}

	if settingsPath == "" {
		fmt.Println("не заполнен параметр \"settings\"")
		os.Exit(1)
	}

	s, err := settings.LoadSettings(settingsPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	logger.InitLogger(logDir(s), s.LogLevel)
	logger.Infof("Версия: %q, gitCommit: %q", version, gitCommit)

	if s.GitLab != nil && s.GitLab.AccessToken == "" {
		if token, err := loadTokenFromFile(); err == nil {
			s.GitLab.AccessToken = token
			logger.Info("GitLab token loaded from saved file")
		}
	}

	if s.GitlabConfigured() {
		logger.Info("Auto-update: checking for updates")
		updated, err := autoupdate.CheckAndUpdate(s.GitLab, version)
		if err != nil {
			logger.Errorf("Auto-update failed: %v", err)
		} else if updated {
			logger.Info("Auto-update: Successfully, exiting for restart")
			os.Exit(1)
		} else {
			logger.Info("Auto-update: already up to date")
		}
	} else {
		logger.Info("Auto-update: not configured (GitLab.ProjectID or GitLab.Home missing)")
	}

	if err := svc.Run(&app{settings: s, port: port}); err != nil {
		logger.DefaultLogger.Error(err)
		os.Exit(1)
	}
}

func logDir(sett *settings.Settings) string {
	if sett != nil && sett.LogDir != "" {
		return sett.LogDir
	}
	execPath, _ := autoupdate.ExecutablePath()
	execDir := filepath.Dir(execPath)
	return filepath.Join(execDir, logger.DefaultLogDir)
}

// add info
// go build -o "1c_exporter" -ldflags "-s -w" - билд чутка меньше размером
// ansible app_servers -m shell -a  "systemctl stop 1c_exporter.service && yes | cp /mnt/share/GO/prometheus_1C_exporter/1c_exporter /usr/local/bin/1c_exporter &&  systemctl start 1c_exporter.service"
//
// pprof
// https://www.jajaldoang.com/post/profiling-go-app-with-pprof/
// go tool pprof -svg heap > out.svg (визуальный граф)
// go tool pprof -http=:8082 .\heap (просмотр в браузере)
//
//  go vet -vettool="C:\GOPATH\go\bin\fieldalignment.exe" ./...
//
// go test -fuzz=Fuzz_formatMultiResult .\explorers\ -fuzztime=30s
