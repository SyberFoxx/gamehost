package home

import (
	"path/filepath"
	"testing"
)

// covers every resolution branch of Dir
func TestDir(t *testing.T) {
	tests := []struct {
		name     string
		root     string
		xdg      string
		userHome string
		want     string
	}{
		{
			name:     "override wins",
			root:     "/srv/gamehost",
			xdg:      "/xdg",
			userHome: "/home/test",
			want:     "/srv/gamehost",
		},
		{
			name:     "xdg data home",
			xdg:      "/xdg",
			userHome: "/home/test",
			want:     "/xdg/gamehost",
		},
		{
			name:     "relative xdg is ignored",
			xdg:      "relative",
			userHome: "/home/test",
			want:     "/home/test/.local/share/gamehost",
		},
		{
			name:     "fallback to user home",
			userHome: "/home/test",
			want:     "/home/test/.local/share/gamehost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.Setenv forbids t.Parallel, so these cases run sequentially.
			t.Setenv(EnvRoot, tt.root)
			t.Setenv("XDG_DATA_HOME", tt.xdg)
			t.Setenv("HOME", tt.userHome)

			got, err := Dir()
			if err != nil {
				t.Fatalf("Dir() error = %v", err)
			}
			if want := filepath.Clean(tt.want); got != want {
				t.Errorf("Dir() = %q, want %q", got, want)
			}
		})
	}
}
