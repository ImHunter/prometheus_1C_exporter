package autoupdate

import (
	"context"
	"fmt"

	"github.com/LazarenkoA/prometheus_1C_exporter/logger"
	"github.com/LazarenkoA/prometheus_1C_exporter/settings"
	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
)

// CheckAndUpdate проверяет наличие новой версии в GitLab и обновляет бинарник.
func CheckAndUpdate(cfg *settings.GitLabSettings, currentVersion string) (bool, error) {
	if currentVersion == "dev" {
		logger.DefaultLogger.Warnln("Development version, skipping auto-update")
		return false, nil
	}
	if cfg == nil {
		return false, fmt.Errorf("GitLab settings missing")
	}
	if cfg.GitLabHome == "" || cfg.ProjectID == 0 {
		return false, fmt.Errorf("GitLabHome or ProjectID not set")
	}

	// Создаём GitLabSource с базовым URL
	source, err := selfupdate.NewGitLabSource(selfupdate.GitLabConfig{
		BaseURL:  cfg.GitLabHome,
		APIToken: cfg.AccessToken,
	})
	if err != nil {
		return false, fmt.Errorf("create GitLab source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:     source,
		Prerelease: true,
	})
	if err != nil {
		return false, fmt.Errorf("create updater: %w", err)
	}

	repository := selfupdate.NewRepositoryID(cfg.ProjectID)
	logger.Infof("Checking for updates in GitLab project ID %d", cfg.ProjectID)

	release, found, err := updater.DetectLatest(context.Background(), repository)
	if err != nil {
		return false, fmt.Errorf("detect latest release: %w", err)
	}
	if !found {
		logger.DefaultLogger.Infoln("No releases found")
		return false, nil
	}

	needUpdate, err := compareVersions(currentVersion, release.Version())
	if err != nil {
		return false, err
	}
	if !needUpdate {
		logger.Infof("Current version %s is up to date", currentVersion)
		return false, nil
	}

	logger.Infof("New version %s found, updating from %s", release.Version(), currentVersion)

	exePath, err := selfupdate.ExecutablePath()
	if err != nil {
		return false, fmt.Errorf("get executable path: %w", err)
	}

	if err := updater.UpdateTo(context.Background(), release, exePath); err != nil {
		return false, fmt.Errorf("update binary: %w", err)
	}

	logger.DefaultLogger.Infoln("Update successful, exiting for restart")
	return true, nil
}

// compareVersions сравнивает две версии, возвращает true, если latest > current.
func compareVersions(current, latest string) (bool, error) {
	cur, err := semver.NewVersion(current)
	if err != nil {
		return false, fmt.Errorf("parse current version: %w", err)
	}
	lat, err := semver.NewVersion(latest)
	if err != nil {
		return false, fmt.Errorf("parse latest version: %w", err)
	}
	return lat.GreaterThan(cur), nil
}
