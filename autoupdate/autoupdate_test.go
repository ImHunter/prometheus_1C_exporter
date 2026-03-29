package autoupdate

import (
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/LazarenkoA/prometheus_1C_exporter/settings"
)

func Test_getProjectSlug(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *settings.GitLabSettings
		want    string
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			want:    "",
			wantErr: true,
		},
		{
			name:    "empty ProjectURL",
			cfg:     &settings.GitLabSettings{ProjectURL: ""},
			want:    "",
			wantErr: true,
		},
		{
			name:    "valid URL",
			cfg:     &settings.GitLabSettings{ProjectURL: "https://gitlab.example.com/namespace/project"},
			want:    "namespace/project",
			wantErr: false,
		},
		{
			name:    "URL with trailing slash",
			cfg:     &settings.GitLabSettings{ProjectURL: "https://gitlab.example.com/namespace/project/"},
			want:    "namespace/project/",
			wantErr: false,
		},
		{
			name:    "URL with path only",
			cfg:     &settings.GitLabSettings{ProjectURL: "https://gitlab.example.com/namespace/project"},
			want:    "namespace/project",
			wantErr: false,
		},
		{
			name:    "invalid URL",
			cfg:     &settings.GitLabSettings{ProjectURL: "://invalid"},
			want:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getProjectSlug(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("getProjectSlug() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getProjectSlug() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_splitSlug(t *testing.T) {
	tests := []struct {
		name      string
		slug      string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "valid slug",
			slug:      "namespace/project",
			wantOwner: "namespace",
			wantRepo:  "project",
			wantErr:   false,
		},
		{
			name:      "slug with multiple slashes",
			slug:      "namespace/sub/project",
			wantOwner: "namespace",
			wantRepo:  "sub/project",
			wantErr:   false,
		},
		{
			name:      "empty slug",
			slug:      "",
			wantOwner: "",
			wantRepo:  "",
			wantErr:   true,
		},
		{
			name:      "no slash",
			slug:      "project",
			wantOwner: "",
			wantRepo:  "",
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := splitSlug(tt.slug)
			if (err != nil) != tt.wantErr {
				t.Errorf("splitSlug() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("splitSlug() owner = %v, want %v", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("splitSlug() repo = %v, want %v", repo, tt.wantRepo)
			}
		})
	}
}

func Test_compareVersions(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
		wantErr bool
	}{
		{
			name:    "newer version",
			current: "1.0.0",
			latest:  "1.0.1",
			want:    true,
			wantErr: false,
		},
		{
			name:    "same version",
			current: "1.0.0",
			latest:  "1.0.0",
			want:    false,
			wantErr: false,
		},
		{
			name:    "older version",
			current: "1.0.1",
			latest:  "1.0.0",
			want:    false,
			wantErr: false,
		},
		{
			name:    "invalid current",
			current: "invalid",
			latest:  "1.0.0",
			want:    false,
			wantErr: true,
		},
		{
			name:    "invalid latest",
			current: "1.0.0",
			latest:  "invalid",
			want:    false,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := compareVersions(tt.current, tt.latest)
			if (err != nil) != tt.wantErr {
				t.Errorf("compareVersions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("compareVersions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_buildAssetName(t *testing.T) {
	asset := buildAssetName()
	if asset == "" {
		t.Error("buildAssetName() returned empty string")
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(asset, ".exe") {
		t.Errorf("buildAssetName() for windows should end with .exe, got %s", asset)
	}
	if runtime.GOOS != "windows" && strings.HasSuffix(asset, ".exe") {
		t.Errorf("buildAssetName() for non-windows should not have .exe, got %s", asset)
	}
}

func Test_createHTTPClient(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "with token",
			token: "glpat-123",
		},
		{
			name:  "without token",
			token: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createHTTPClient(tt.token)
			if client == nil {
				t.Error("createHTTPClient() returned nil")
			}
			if tt.token != "" {
				if _, ok := client.Transport.(*tokenTransport); !ok {
					t.Error("createHTTPClient() with token should set tokenTransport")
				}
			} else {
				if client != http.DefaultClient {
					t.Error("createHTTPClient() without token should return http.DefaultClient")
				}
			}
		})
	}
}
