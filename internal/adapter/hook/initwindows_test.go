package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withGOOS 는 배선을 만드는 머신의 OS 를 바꿔 준다. 윈도우 배선을 맥에서 검증한다.
func withGOOS(t *testing.T, os string) {
	t.Helper()
	prev := goos
	goos = os
	t.Cleanup(func() { goos = prev })
}

func planFor(t *testing.T, o InitOptions) map[string][]hookGroup {
	t.Helper()
	p, err := BuildPlan(o)
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Hooks map[string][]hookGroup `json:"hooks"`
	}
	if err := json.Unmarshal(p.after, &root); err != nil {
		t.Fatalf("계획 결과가 JSON 이 아니다: %v\n%s", err, p.after)
	}
	return root.Hooks
}

// ★ 윈도우 배선은 **셸을 안 탄다.**
//
// Claude Code 는 윈도우에서 Git Bash 가 있으면 bash, 없으면 PowerShell 을 쓴다 —
// 같은 조직 안에서도 머신마다 갈린다. `PRIORCASE_HOOK=1 "<경로>" …` 은 둘 다에서
// 안 돌고, 안 돌면 **조용히** 안 돈다. exec form 은 셸을 아예 안 낀다.
func TestWindowsWiringUsesExecForm(t *testing.T) {
	withGOOS(t, "windows")
	hooks := planFor(t, InitOptions{Binary: `C:\Tools\prior.exe`, Host: HostClaudeCode})
	if len(hooks) == 0 {
		t.Fatal("배선이 하나도 안 나왔다")
	}
	for ev, groups := range hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if h.Command != `C:\Tools\prior.exe` {
					t.Errorf("%s: command 가 %q — 실행파일 경로만 있어야 한다", ev, h.Command)
				}
				if len(h.Args) == 0 || h.Args[0] != "hook" {
					t.Errorf("%s: args 가 %v — [hook …] 이어야 한다", ev, h.Args)
				}
				// 셸 문법이 한 조각도 섞이면 안 된다.
				for _, bad := range []string{hookMarker, "/usr/bin/env", "&&", ";", "$env:"} {
					if strings.Contains(h.Command, bad) {
						t.Errorf("%s: command 에 셸 문법 %q 가 섞였다: %s", ev, bad, h.Command)
					}
				}
			}
		}
	}
}

// POSIX 배선은 **안 바뀐다.** 지금 돌고 있는 것이고 바꿀 이유가 없다.
func TestPosixWiringUnchanged(t *testing.T) {
	withGOOS(t, "darwin")
	hooks := planFor(t, InitOptions{Binary: "/usr/local/bin/prior", Host: HostClaudeCode})
	for ev, groups := range hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if !strings.HasPrefix(h.Command, hookMarker+" ") {
					t.Errorf("%s: POSIX 명령이 표시로 시작하지 않는다: %s", ev, h.Command)
				}
				if len(h.Args) != 0 {
					t.Errorf("%s: POSIX 인데 args 가 붙었다: %v", ev, h.Args)
				}
			}
		}
	}
}

// exec form 도 우리 것으로 알아봐야 한다 — 못 알아보면 두 번 심어 훅이 두 벌이 된다.
func TestIsOursRecognisesBothForms(t *testing.T) {
	for _, tc := range []struct {
		name string
		h    hookEntry
		want bool
	}{
		{"POSIX 셸형", hookEntry{Command: hookMarker + ` "/usr/local/bin/prior" hook stop`}, true},
		{"윈도우 exec 형", hookEntry{Command: `C:\Tools\prior.exe`, Args: []string{"hook", "stop"}}, true},
		{"확장자 없는 exec 형", hookEntry{Command: "/usr/local/bin/prior", Args: []string{"hook", "stop"}}, true},
		{"남의 훅", hookEntry{Command: "/usr/bin/mytool", Args: []string{"hook", "stop"}}, false},
		{"우리 바이너리인데 hook 이 아니다", hookEntry{Command: `C:\Tools\prior.exe`, Args: []string{"doctor"}}, false},
		{"인자 없는 남의 명령", hookEntry{Command: "echo hi"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isOurs(tc.h); got != tc.want {
				t.Errorf("isOurs() = %v, want %v", got, tc.want)
			}
		})
	}
}

// 재배선이 멱등이어야 한다 — 윈도우에서도 두 번 돌려 훅이 두 벌이 되면 안 된다.
//
// **이것이 exec form 의 진짜 위험이다.** 셸형에는 표시 문자열이 있어 알아보기
// 쉬웠는데, exec form 은 `command` 가 우리 바이너리라는 사실로만 판정한다(isOurs).
// 그 판정이 헐거우면 두 번째 배선이 첫 배선을 남의 훅으로 보고 남긴다.
func TestWindowsRewireIsIdempotent(t *testing.T) {
	withGOOS(t, "windows")
	path := filepath.Join(t.TempDir(), "settings.json")
	o := InitOptions{SettingsPath: path, Binary: `C:\Tools\prior.exe`, Host: HostClaudeCode}

	first, err := BuildPlan(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, first.after, 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := BuildPlan(o)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.after) != string(second.after) {
		t.Errorf("두 번 돌리니 배선이 달라졌다:\n첫판:\n%s\n둘째판:\n%s", first.after, second.after)
	}
	if second.Keep != 0 {
		t.Errorf("첫 배선을 남의 훅으로 오인해 %d개를 남겼다 — 그러면 훅이 두 벌이 된다", second.Keep)
	}
	if len(second.Remove) != len(first.Add) {
		t.Errorf("걷어낸 것 %d개 · 처음 심은 것 %d개 — 자기가 심은 것을 다 알아봐야 한다",
			len(second.Remove), len(first.Add))
	}
}
