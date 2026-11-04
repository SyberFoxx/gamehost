package doctor

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers the platform gate
func TestPlatform(t *testing.T) {
	tests := []struct {
		name       string
		goos       string
		goarch     string
		wantStatus Status
	}{
		{"linux amd64", "linux", "amd64", OK},
		{"linux arm64", "linux", "arm64", OK},
		{"darwin", "darwin", "arm64", Fail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := platform(tt.goos, tt.goarch)
			if got.Status != tt.wantStatus {
				t.Errorf("platform() status = %v, want %v", got.Status, tt.wantStatus)
			}
		})
	}
}

// covers the SteamCMD lookup and its failure message
func TestSteamcmd(t *testing.T) {
	tests := []struct {
		name       string
		locate     func() (string, error)
		wantStatus Status
		wantDetail string
	}{
		{
			name:       "found",
			locate:     func() (string, error) { return "/usr/bin/steamcmd", nil },
			wantStatus: OK,
			wantDetail: "/usr/bin/steamcmd",
		},
		{
			name:       "missing",
			locate:     func() (string, error) { return "", errors.New("not found") },
			wantStatus: Fail,
			wantDetail: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := checkSteamcmd(tt.locate)
			if got.Status != tt.wantStatus {
				t.Errorf("steamcmd() status = %v, want %v", got.Status, tt.wantStatus)
			}
			if !strings.Contains(got.Detail, tt.wantDetail) {
				t.Errorf("steamcmd() detail = %q, want it to contain %q", got.Detail, tt.wantDetail)
			}
		})
	}
}

// covers the display probe
func TestDisplay(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		wantStatus Status
	}{
		{"set", ":0", OK},
		{"unset", "", Warn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			getenv := func(string) string { return tt.value }
			if got := display(getenv); got.Status != tt.wantStatus {
				t.Errorf("display() status = %v, want %v", got.Status, tt.wantStatus)
			}
		})
	}
}

// covers writable, missing and blocked data roots
func TestRootWritable(t *testing.T) {
	t.Run("writable", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		if got := rootWritable(dir); got.Status != OK {
			t.Fatalf("rootWritable() = %v (%s), want OK", got.Status, got.Detail)
		}
	})

	t.Run("nested path under a file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		file := filepath.Join(dir, "file")
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := rootWritable(filepath.Join(file, "sub")); got.Status != Fail {
			t.Errorf("rootWritable() = %v, want Fail", got.Status)
		}
	})
}

// covers the report summary and alignment
func TestFormat(t *testing.T) {
	t.Parallel()

	report := Report{
		Root: "/data/gamehost",
		Checks: []Check{
			{Name: "platform", Status: OK, Detail: "linux/amd64", Required: true},
			{Name: "steamcmd", Status: Fail, Detail: "not found", Required: true},
			{Name: "display", Status: Warn, Detail: "unset"},
		},
	}

	var buf bytes.Buffer
	if err := Format(&buf, report); err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	got := buf.String()
	for _, want := range []string{"root: /data/gamehost", "ok", "platform", "linux/amd64", "fail", "not found", "environment is not ready"} {
		if !strings.Contains(got, want) {
			t.Errorf("Format() output missing %q:\n%s", want, got)
		}
	}
}

// covers the healthy shortcut
func TestHealthy(t *testing.T) {
	tests := []struct {
		name   string
		checks []Check
		want   bool
	}{
		{"all good", []Check{{Status: OK, Required: true}}, true},
		{"optional failure", []Check{{Status: Fail}}, true},
		{"required failure", []Check{{Status: Fail, Required: true}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := (Report{Checks: tt.checks}).Healthy(); got != tt.want {
				t.Errorf("Healthy() = %v, want %v", got, tt.want)
			}
		})
	}
}
