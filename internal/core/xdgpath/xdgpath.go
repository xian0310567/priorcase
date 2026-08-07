// Package xdgpath 는 XDG Base Directory 를 명세 문자 그대로 해석한다.
// os.UserConfigDir 은 macOS 에서 ~/Library/Application Support 를 주므로 쓰지 않는다.
package xdgpath

import (
	"os"
	"path/filepath"
)

func ConfigHome() (string, error) { return resolve("XDG_CONFIG_HOME", ".config") }
func StateHome() (string, error)  { return resolve("XDG_STATE_HOME", ".local", "state") }

// resolve 는 환경변수가 절대경로일 때만 채택한다 (XDG 명세).
func resolve(env string, fallback ...string) (string, error) {
	if v := os.Getenv(env); filepath.IsAbs(v) {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{home}, fallback...)...), nil
}
