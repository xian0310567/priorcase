package hook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// runHookOn 은 runHook 과 같되 호스트를 고른다.
//
// runHook 을 안 고치고 새로 두는 이유: 기존 훅 테스트 전부가 Claude Code 경로를
// 지키는 회귀 그물이다. 그쪽 시그니처를 건드리면 **이 변경이 그 그물을 통과했는지**를
// 알 수 없게 된다.
func runHookOn(t *testing.T, h Host, c *config.Config, stateDir string, ev Event, in Input) run {
	t.Helper()
	var out, errb strings.Builder
	e := Run(context.Background(), Options{
		Event: ev, Input: in, Config: c, Layout: store.NewLayout(c),
		StateDir: stateDir, Host: h, Out: &out, Err: &errb,
	})
	return run{out: out.String(), err: errb.String(), e: e}
}

// codexContext 는 Codex stdout 을 풀어 additionalContext 를 준다.
// 감싸는 형태가 틀리면 여기서 t.Fatal 이 난다 — 그게 이 테스트의 요점이다.
func codexContext(t *testing.T, stdout string) string {
	t.Helper()
	var env struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("Codex stdout 이 JSON 이 아니다: %v\n%s", err, stdout)
	}
	if env.HookSpecificOutput.HookEventName == "" {
		t.Fatalf("hookEventName 이 비어 있다:\n%s", stdout)
	}
	return env.HookSpecificOutput.AdditionalContext
}

// **Codex 는 평문 stdout 을 컨텍스트로 먹지 않는다.** 봉투에 담아야 한다.
// 담지 않으면 주입이 통째로 사라지는데, 훅은 exit 0 이라 아무 표시도 안 난다.
func TestCodexWrapsContextInJSON(t *testing.T) {
	sd := pendingStore(t)
	got := runHookOn(t, HostCodex, cfg(t), sd, EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})

	ctx := codexContext(t, got.out)
	if !strings.Contains(ctx, "priorcase") {
		t.Errorf("세션 진입 컨텍스트가 봉투 안에 없다:\n%s", ctx)
	}
}

func TestCodexUsesHostEventName(t *testing.T) {
	sd := pendingStore(t)
	got := runHookOn(t, HostCodex, cfg(t), sd, EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})

	var env map[string]map[string]string
	if err := json.Unmarshal([]byte(got.out), &env); err != nil {
		t.Fatalf("JSON 이 아니다: %v\n%s", err, got.out)
	}
	// `session-start` 가 아니라 `SessionStart` 다 — 호스트가 아는 이름이어야 한다.
	if n := env["hookSpecificOutput"]["hookEventName"]; n != "SessionStart" {
		t.Errorf("hookEventName = %q, 원하는 값 %q", n, "SessionStart")
	}
}

// **Claude Code 경로는 한 바이트도 안 바뀐다.** 도는 시스템을 고치는 변경이므로
// 이쪽이 회귀하면 얻는 것보다 잃는 것이 크다.
func TestClaudeCodeOutputStaysPlain(t *testing.T) {
	sd := pendingStore(t)
	plain := runHook(t, cfg(t), sd, EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})
	viaHost := runHookOn(t, HostClaudeCode, cfg(t), sd, EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})

	if plain.out != viaHost.out {
		t.Errorf("호스트를 명시했더니 출력이 달라졌다:\n--- 전 ---\n%s\n--- 후 ---\n%s", plain.out, viaHost.out)
	}
	if strings.HasPrefix(strings.TrimSpace(plain.out), "{") {
		t.Errorf("Claude Code 출력이 JSON 으로 바뀌었다:\n%s", plain.out)
	}
}

// **낼 것이 없으면 아무것도 안 낸다.** 빈 봉투(`{}`)를 내면 Codex 전사에 빈
// developer 메시지가 매 턴 쌓인다 — 늘 뜨는 것은 곧 배경이 되고, 배경이 되면
// 진짜 주입도 같이 안 읽힌다.
func TestCodexEmitsNothingWithoutContext(t *testing.T) {
	sd := pendingStore(t)
	got := runHookOn(t, HostCodex, cfg(t), sd, EventStop, Input{Cwd: "/tmp/proj/alpha"})

	if strings.TrimSpace(got.out) != "" {
		t.Errorf("낼 컨텍스트가 없는데 stdout 에 무언가 있다:\n%q", got.out)
	}
}

// 줄바꿈이 살아야 한다. 회수 주입은 여러 줄짜리 블록이고, 한 줄로 뭉개지면
// 모델이 읽는 모양이 달라진다.
func TestCodexPreservesNewlines(t *testing.T) {
	sd := pendingStore(t)
	got := runHookOn(t, HostCodex, cfg(t), sd, EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})

	if ctx := codexContext(t, got.out); !strings.Contains(ctx, "\n") {
		t.Errorf("줄바꿈이 사라졌다:\n%q", ctx)
	}
}
