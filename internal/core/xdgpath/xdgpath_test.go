package xdgpath

import (
	"path/filepath"
	"testing"
)

func TestConfigHome(t *testing.T) {
	home := "/home/tester"
	tests := []struct {
		name string
		env  string
		want string
	}{
		{"미설정이면 ~/.config", "", filepath.Join(home, ".config")},
		{"절대경로면 그대로", "/custom/cfg", "/custom/cfg"},
		{"상대경로는 무시하고 기본값", "relative/cfg", filepath.Join(home, ".config")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", tt.env)
			t.Setenv("HOME", home)
			got, err := ConfigHome()
			if err != nil {
				t.Fatalf("ConfigHome() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ConfigHome() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStateHome(t *testing.T) {
	home := "/home/tester"
	tests := []struct {
		name string
		env  string
		want string
	}{
		{"미설정이면 ~/.local/state", "", filepath.Join(home, ".local", "state")},
		{"절대경로면 그대로", "/custom/state", "/custom/state"},
		{"상대경로는 무시하고 기본값", "relative/state", filepath.Join(home, ".local", "state")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", tt.env)
			t.Setenv("HOME", home)
			got, err := StateHome()
			if err != nil {
				t.Fatalf("StateHome() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("StateHome() = %q, want %q", got, tt.want)
			}
		})
	}
}
