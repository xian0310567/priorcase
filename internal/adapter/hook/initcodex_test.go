package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 이 맥에 실제로 깔려 있는 ~/.codex/hooks.json 의 모양이다. 남의 훅(oh-my-codex)과
// 최상위 `state`(신뢰 해시)가 같이 산다 — **둘 다 살아남아야 한다.**
const realCodexHooks = `{
  "state": {
    "/Users/x/.codex/hooks.json:session_start:0:0": {
      "trusted_hash": "sha256:cc7f36"
    }
  },
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume|clear",
        "hooks": [{"type": "command", "command": "node /omx/codex-native-hook.js"}]
      }
    ],
    "PostToolUse": [
      {"hooks": [{"type": "command", "command": "node /omx/codex-native-hook.js"}]}
    ]
  }
}`

// codexPlan 은 계획을 세우고 적용한 뒤, 쓰인 파일을 파싱해 준다.
func codexPlan(t *testing.T, seed string) (path string, hooks map[string][]hookGroup, root map[string]any) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "hooks.json")
	if seed != "" {
		if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := BuildPlan(InitOptions{SettingsPath: path, Host: HostCodex, Binary: "/usr/local/bin/prior"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(ReadSettings(path)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	root = map[string]any{}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("쓴 것이 JSON 이 아니다: %v\n%s", err, raw)
	}
	b, _ := json.Marshal(root["hooks"])
	if err := json.Unmarshal(b, &hooks); err != nil {
		t.Fatal(err)
	}
	return path, hooks, root
}

// ours 는 우리가 심은 훅 명령만 준다.
func ours(hooks map[string][]hookGroup, event string) []string {
	var out []string
	for _, g := range hooks[event] {
		for _, h := range g.Hooks {
			if strings.Contains(h.Command, hookMarker) {
				out = append(out, h.Command)
			}
		}
	}
	return out
}

// ★ **Codex 에는 SessionEnd 가 없다.** 없는 이벤트에 심어 봐야 영영 안 불리는데,
// 설정 파일에는 다섯 줄이 보이므로 배선된 줄 알게 된다.
func TestCodexPlanWiresOnlyRealEvents(t *testing.T) {
	_, hooks, _ := codexPlan(t, "")

	for _, want := range []string{"SessionStart", "UserPromptSubmit", "Stop", "PreCompact"} {
		if len(ours(hooks, want)) != 1 {
			t.Errorf("%s 에 우리 훅이 %d개 (1개여야 한다)", want, len(ours(hooks, want)))
		}
	}
	if _, ok := hooks["SessionEnd"]; ok {
		t.Error("Codex 에 없는 SessionEnd 를 배선했다")
	}
}

// ★ 플래그가 빠지면 Codex 에서 주입이 통째로 사라진다 — 그리고 조용하다.
func TestCodexPlanPassesHostFlag(t *testing.T) {
	_, hooks, _ := codexPlan(t, "")

	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "Stop", "PreCompact"} {
		for _, cmd := range ours(hooks, ev) {
			if !strings.Contains(cmd, "--host codex") {
				t.Errorf("%s 훅에 --host codex 가 없다: %s", ev, cmd)
			}
		}
	}
}

// ★ **남의 것을 지우지 않는다.** 이 파일은 oh-my-codex 와 공유한다.
func TestCodexPlanKeepsForeignHooksAndState(t *testing.T) {
	_, hooks, root := codexPlan(t, realCodexHooks)

	foreign := 0
	for _, groups := range hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if strings.Contains(h.Command, "codex-native-hook.js") {
					foreign++
				}
			}
		}
	}
	if foreign != 2 {
		t.Errorf("남의 훅이 %d개 남았다 (2개여야 한다)", foreign)
	}
	// Codex 는 훅을 해시로 신뢰한다. 이걸 날리면 사용자가 전부 다시 승인해야 한다.
	if _, ok := root["state"]; !ok {
		t.Error("최상위 state(신뢰 해시)가 사라졌다")
	}
	// 남의 SessionStart matcher 도 그대로여야 한다.
	kept := false
	for _, g := range hooks["SessionStart"] {
		if g.Matcher == "startup|resume|clear" {
			kept = true
		}
	}
	if !kept {
		t.Error("남의 훅 matcher 가 사라졌다")
	}
}

// ★ 두 번 돌려도 훅이 두 벌이 되지 않는다.
func TestCodexPlanIsIdempotent(t *testing.T) {
	path, _, _ := codexPlan(t, realCodexHooks)

	p, err := BuildPlan(InitOptions{SettingsPath: path, Host: HostCodex, Binary: "/usr/local/bin/prior"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(ReadSettings(path)); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	var hooks map[string][]hookGroup
	b, _ := json.Marshal(root["hooks"])
	if err := json.Unmarshal(b, &hooks); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "Stop", "PreCompact"} {
		if n := len(ours(hooks, ev)); n != 1 {
			t.Errorf("%s 에 우리 훅이 %d개 — 두 번 돌려서 두 벌이 됐다", ev, n)
		}
	}
}

// **Claude Code 배선은 안 바뀐다.** --host 를 안 붙이는 것까지 포함해서다 —
// 붙이면 지금 돌고 있는 훅 명령이 전부 달라진다.
func TestClaudeCodePlanUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	p, err := BuildPlan(InitOptions{SettingsPath: path, Binary: "/usr/local/bin/prior"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "--host") {
		t.Errorf("Claude Code 배선에 --host 가 붙었다:\n%s", raw)
	}
	if !strings.Contains(string(raw), "SessionEnd") {
		t.Error("Claude Code 의 SessionEnd 배선이 사라졌다")
	}
}
