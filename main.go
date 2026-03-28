package main

////go:generate go run install/release.go
// //go:generate git commit -am "bump $PROM_VERSION"
// //go:generate git tag -af $PROM_VERSION -m "$PROM_VERSION"

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/LazarenkoA/prometheus_1C_exporter/logger"
	"github.com/LazarenkoA/prometheus_1C_exporter/settings"
	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
	"github.com/judwhite/go-svc"
)

var (
	version   = "dev"
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

	logger.InitLogger(s.LogDir, s.LogLevel)
	logger.Infof("Версия: %q, gitCommit: %q", version, gitCommit)

	if id, ok := s.GetReleasesProjectID(); ok {
		updated, err := checkAndUpdate(id, version)
		if err != nil {
			logger.Errorf("Auto-update failed: %v", err)
		} else if updated {
			logger.Warn("Restarting for auto-update")
			os.Exit(1)
		} else {
			logger.Info("Self-updated")
		}
	}

	if err := svc.Run(&app{settings: s, port: port}); err != nil {
		logger.DefaultLogger.Error(err)
		os.Exit(1)
	}
}

func checkAndUpdate(projectID int, currentVersion string) (bool, error) {
	if currentVersion == "dev" {
		logger.DefaultLogger.Warnln("Development version, skipping auto-update")
		return false, nil
	}

	repo := selfupdate.NewRepositoryID(projectID)

	assetName := fmt.Sprintf("1C_exporter_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		assetName += ".exe"
	}

	latest, found, err := selfupdate.DetectLatest(context.Background(), repo)
	if err != nil {
		return false, fmt.Errorf("detect version: %w", err)
	}
	if !found {
		logger.DefaultLogger.Infoln("No releases found")
		return false, nil
	}

	// Парсим текущую версию и версию из релиза
	cur, err := semver.NewVersion(currentVersion)
	if err != nil {
		return false, fmt.Errorf("parse current version: %w", err)
	}
	latestVer, err := semver.NewVersion(latest.Version())
	if err != nil {
		return false, fmt.Errorf("parse latest version: %w", err)
	}

	if !latestVer.GreaterThan(cur) {
		logger.Infof("Current version %s is up to date", currentVersion)
		return false, nil
	}

	logger.Infof("New version %s found, updating from %s", latest.Version(), currentVersion)

	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return false, fmt.Errorf("get executable path: %w", err)
	}

	err = selfupdate.UpdateTo(context.Background(), latest.AssetURL, latest.AssetName, exe)
	if err != nil {
		return false, fmt.Errorf("update: %w", err)
	}

	logger.DefaultLogger.Infoln("Update successful, exiting for restart")
	return true, nil
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
