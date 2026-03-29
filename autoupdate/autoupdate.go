package autoupdate

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"

	"github.com/LazarenkoA/prometheus_1C_exporter/logger"
	"github.com/LazarenkoA/prometheus_1C_exporter/settings"
	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
)

// CheckAndUpdate выполняет проверку и обновление экспортера.
func CheckAndUpdate(cfg *settings.GitLabSettings, currentVersion string) (bool, error) {
	if currentVersion == "dev" {
		logger.DefaultLogger.Warnln("Development version, skipping auto-update")
		return false, nil
	}

	slug, err := getProjectSlug(cfg)
	if err != nil {
		return false, fmt.Errorf("get project slug: %w", err)
	}

	owner, repo, err := splitSlug(slug)
	if err != nil {
		return false, err
	}

	client := createHTTPClient(cfg.AccessToken)

	var origTransport http.RoundTripper
	if cfg.AccessToken != "" {
		origTransport = http.DefaultTransport
		http.DefaultTransport = client.Transport
		defer func() { http.DefaultTransport = origTransport }()
	}

	release, err := detectLatestRelease(context.Background(), owner, repo)
	if err != nil {
		return false, fmt.Errorf("detect latest release: %w", err)
	}
	if release == nil {
		logger.DefaultLogger.Infoln("No releases found")
		return false, nil
	}

	expectedAssetName := buildAssetName()
	if release.AssetName != expectedAssetName {
		logger.Infof("Expected asset %q, found %q. Skipping update.", expectedAssetName, release.AssetName)
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

	if err := updateBinary(context.Background(), release, exePath); err != nil {
		return false, fmt.Errorf("update: %w", err)
	}

	logger.DefaultLogger.Infoln("Update successful, exiting for restart")
	return true, nil
}

// getProjectSlug извлекает namespace/project из URL.
func getProjectSlug(cfg *settings.GitLabSettings) (string, error) {
	if cfg == nil || cfg.ProjectURL == "" {
		return "", fmt.Errorf("GitLab ProjectURL not set")
	}
	u, err := url.Parse(cfg.ProjectURL)
	if err != nil {
		return "", fmt.Errorf("invalid ProjectURL: %w", err)
	}
	path := strings.TrimPrefix(u.Path, "/")
	if path == "" {
		return "", fmt.Errorf("cannot extract namespace/project from URL")
	}
	return path, nil
}

// splitSlug разделяет "namespace/project" на две части.
func splitSlug(slug string) (owner, repo string, err error) {
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid slug format: %s", slug)
	}
	return parts[0], parts[1], nil
}

// createHTTPClient создаёт HTTP клиент с добавлением заголовка PRIVATE-TOKEN.
func createHTTPClient(token string) *http.Client {
	if token == "" {
		return http.DefaultClient
	}
	return &http.Client{
		Transport: &tokenTransport{token: token},
	}
}

// detectLatestRelease получает последний релиз через selfupdate.
func detectLatestRelease(ctx context.Context, owner, repo string) (*selfupdate.Release, error) {
	repository := selfupdate.NewRepositorySlug(owner, repo)
	release, found, err := selfupdate.DetectLatest(ctx, repository)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return release, nil
}

// buildAssetName формирует имя бинарника для текущей платформы.
func buildAssetName() string {
	asset := fmt.Sprintf("1C_exporter_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}
	return asset
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

// updateBinary заменяет текущий бинарник на новый.
func updateBinary(ctx context.Context, release *selfupdate.Release, cmdPath string) error {
	return selfupdate.UpdateTo(ctx, release.AssetURL, release.AssetName, cmdPath)
}

// tokenTransport добавляет заголовок авторизации к запросам.
type tokenTransport struct {
	token string
	base  http.RoundTripper
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.base == nil {
		t.base = http.DefaultTransport
	}
	req.Header.Set("PRIVATE-TOKEN", t.token)
	return t.base.RoundTrip(req)
}
