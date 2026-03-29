package autoupdate

import (
	"net/http"
	"runtime"
	"strings"
	"testing"
)

func Test_extractProjectSlug(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{"empty URL", "", "", true},
		{"simple", "https://gitlab.example.com/namespace/project", "namespace/project", false},
		{"trailing slash", "https://gitlab.example.com/namespace/project/", "namespace/project", false},
		{"with .git", "https://gitlab.example.com/namespace/project.git", "namespace/project", false},
		{".git and slash", "https://gitlab.example.com/namespace/project.git/", "namespace/project", false},
		{"extra path", "https://gitlab.example.com/namespace/project/-/tree/main", "namespace/project/-/tree/main", false},
		{"invalid URL", "://invalid", "", true},
		{"no path", "https://gitlab.example.com/", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractProjectSlug(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractProjectSlug() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractProjectSlug() = %v, want %v", got, tt.want)
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
		{"valid", "namespace/project", "namespace", "project", false},
		{"multi", "namespace/sub/project", "namespace", "sub/project", false},
		{"empty", "", "", "", true},
		{"no slash", "project", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := splitSlug(tt.slug)
			if (err != nil) != tt.wantErr {
				t.Errorf("splitSlug() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %v, want %v", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %v, want %v", repo, tt.wantRepo)
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
		{"newer", "1.0.0", "1.0.1", true, false},
		{"same", "1.0.0", "1.0.0", false, false},
		{"older", "1.0.1", "1.0.0", false, false},
		{"invalid current", "invalid", "1.0.0", false, true},
		{"invalid latest", "1.0.0", "invalid", false, true},
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
		t.Error("buildAssetName() returned empty")
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(asset, ".exe") {
		t.Errorf("on windows expected .exe, got %s", asset)
	}
	if runtime.GOOS != "windows" && strings.HasSuffix(asset, ".exe") {
		t.Errorf("on non-windows .exe suffix, got %s", asset)
	}
}

type mockTransport struct {
	RoundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.RoundTripFunc != nil {
		return m.RoundTripFunc(req)
	}
	return nil, nil
}

func Test_createHTTPClient(t *testing.T) {
	base := &mockTransport{}
	tests := []struct {
		name      string
		token     string
		wantToken bool
	}{
		{"with token", "glpat-123", true},
		{"without token", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createHTTPClient(tt.token, base)
			if client == nil {
				t.Error("client is nil")
				return
			}
			if tt.wantToken {
				tr, ok := client.Transport.(*tokenTransport)
				if !ok {
					t.Error("expected tokenTransport")
				} else {
					if tr.token != tt.token {
						t.Errorf("token = %v, want %v", tr.token, tt.token)
					}
					if tr.base != base {
						t.Error("base transport not set")
					}
				}
			} else {
				if client.Transport != base {
					t.Error("expected base transport")
				}
			}
		})
	}
}
