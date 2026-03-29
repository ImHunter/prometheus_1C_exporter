package autoupdate

import (
	"net/http"
	"runtime"
	"strings"
	"testing"
)

// ----------------------------------------------------------------------------
// extractProjectSlug
// ----------------------------------------------------------------------------
func Test_extractProjectSlug(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{
			name:    "empty URL",
			url:     "",
			want:    "",
			wantErr: true,
		},
		{
			name:    "simple URL",
			url:     "https://gitlab.example.com/namespace/project",
			want:    "namespace/project",
			wantErr: false,
		},
		{
			name:    "URL with trailing slash",
			url:     "https://gitlab.example.com/namespace/project/",
			want:    "namespace/project",
			wantErr: false,
		},
		{
			name:    "URL with .git",
			url:     "https://gitlab.example.com/namespace/project.git",
			want:    "namespace/project",
			wantErr: false,
		},
		{
			name:    "URL with .git and trailing slash",
			url:     "https://gitlab.example.com/namespace/project.git/",
			want:    "namespace/project",
			wantErr: false,
		},
		{
			name:    "nested path",
			url:     "https://gitlab.example.com/devops/exporters/1c_exporter_config",
			want:    "devops/exporters/1c_exporter_config",
			wantErr: false,
		},
		{
			name:    "extra path segments (/-/tree/main)",
			url:     "https://gitlab.example.com/namespace/project/-/tree/main",
			want:    "namespace/project/-/tree/main",
			wantErr: false,
		},
		{
			name:    "invalid URL",
			url:     "://invalid",
			want:    "",
			wantErr: true,
		},
		{
			name:    "no project path",
			url:     "https://gitlab.example.com/",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractProjectSlug(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractProjectSlug() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractProjectSlug() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// splitSlug
// ----------------------------------------------------------------------------
func Test_splitSlug(t *testing.T) {
	tests := []struct {
		name      string
		slug      string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "valid",
			slug:      "namespace/project",
			wantOwner: "namespace",
			wantRepo:  "project",
			wantErr:   false,
		},
		{
			name:      "nested",
			slug:      "devops/exporters/1c_exporter_config",
			wantOwner: "devops",
			wantRepo:  "exporters/1c_exporter_config",
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
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// extractBaseURL
// ----------------------------------------------------------------------------
func Test_extractBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{
			name:    "empty URL",
			url:     "",
			want:    "",
			wantErr: true,
		},
		{
			name:    "simple HTTPS",
			url:     "https://gitlab.example.com/namespace/project",
			want:    "https://gitlab.example.com",
			wantErr: false,
		},
		{
			name:    "with .git",
			url:     "https://gitlab.example.com/namespace/project.git",
			want:    "https://gitlab.example.com",
			wantErr: false,
		},
		{
			name:    "trailing slash",
			url:     "https://gitlab.example.com/namespace/project/",
			want:    "https://gitlab.example.com",
			wantErr: false,
		},
		{
			name:    "HTTP with port",
			url:     "http://localhost:8080/group/project",
			want:    "http://localhost:8080",
			wantErr: false,
		},
		{
			name:    "invalid URL",
			url:     "://invalid",
			want:    "",
			wantErr: true,
		},
		{
			name:    "missing scheme",
			url:     "gitlab.example.com/namespace/project",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractBaseURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractBaseURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// extractOwnerRepo (integration)
// ----------------------------------------------------------------------------
func Test_extractOwnerRepo(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "simple",
			url:       "https://gitlab.example.com/namespace/project",
			wantOwner: "namespace",
			wantRepo:  "project",
			wantErr:   false,
		},
		{
			name:      "nested",
			url:       "https://gitlab.example.com/devops/exporters/1c_exporter_config",
			wantOwner: "devops",
			wantRepo:  "exporters/1c_exporter_config",
			wantErr:   false,
		},
		{
			name:      "with .git",
			url:       "https://gitlab.example.com/group/subgroup/repo.git",
			wantOwner: "group",
			wantRepo:  "subgroup/repo",
			wantErr:   false,
		},
		{
			name:      "trailing slash",
			url:       "https://gitlab.example.com/namespace/project/",
			wantOwner: "namespace",
			wantRepo:  "project",
			wantErr:   false,
		},
		{
			name:      "invalid URL",
			url:       "://invalid",
			wantOwner: "",
			wantRepo:  "",
			wantErr:   true,
		},
		{
			name:      "no path",
			url:       "https://gitlab.example.com/",
			wantOwner: "",
			wantRepo:  "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := extractOwnerRepo(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractOwnerRepo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// compareVersions
// ----------------------------------------------------------------------------
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
		{"current invalid", "invalid", "1.0.0", false, true},
		{"latest invalid", "1.0.0", "invalid", false, true},
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

// ----------------------------------------------------------------------------
// buildAssetName
// ----------------------------------------------------------------------------
func Test_buildAssetName(t *testing.T) {
	asset := buildAssetName()
	if asset == "" {
		t.Error("buildAssetName() returned empty string")
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(asset, ".exe") {
		t.Errorf("on Windows expected .exe suffix, got %q", asset)
	}
	if runtime.GOOS != "windows" && strings.HasSuffix(asset, ".exe") {
		t.Errorf("on non-Windows unexpected .exe suffix, got %q", asset)
	}
}

// ----------------------------------------------------------------------------
// tokenTransport
// ----------------------------------------------------------------------------
type mockTransport struct {
	RoundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.RoundTripFunc != nil {
		return m.RoundTripFunc(req)
	}
	return nil, nil
}

func Test_tokenTransport_RoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		token     string
		wantError bool
	}{
		{"with token", "test-token-123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headerChecked := false
			mockBase := &mockTransport{
				RoundTripFunc: func(req *http.Request) (*http.Response, error) {
					if req.Header.Get("PRIVATE-TOKEN") != tt.token {
						t.Errorf("expected PRIVATE-TOKEN = %q, got %q", tt.token, req.Header.Get("PRIVATE-TOKEN"))
					}
					headerChecked = true
					return nil, nil
				},
			}
			tr := &tokenTransport{token: tt.token, base: mockBase}
			req, _ := http.NewRequest("GET", "http://example.com", nil)
			_, err := tr.RoundTrip(req)
			if (err != nil) != tt.wantError {
				t.Errorf("RoundTrip() error = %v, wantError %v", err, tt.wantError)
			}
			if !headerChecked {
				t.Error("RoundTrip() did not call base transport")
			}
		})
	}
}
