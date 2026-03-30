package autoupdate

import (
	"testing"
)

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
