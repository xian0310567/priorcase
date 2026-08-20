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
// StateDir 는 priorcase 의 상태 디렉토리다.
//
// **머신마다 다른 것이 사는 곳이다** — 미확인 구간이 그 머신의 transcript 절대
// 경로를 키로 쓰고, 동기화 도장이 "이 머신이 마지막으로 언제 밀었나" 를 담는다.
// 그래서 이 디렉토리는 볼트와 달리 **동기화 대상이 아니다.**
//
// 경로 조립이 여기 하나여야 한다. daemon 과 core 가 각자 이어 붙이면 한쪽을
// 고칠 때 다른 쪽이 조용히 다른 자리를 보게 된다.
func StateDir() (string, error) {
	state, err := StateHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "priorcase"), nil
}

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
