package hook

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/sync"
)

// unpushed 는 아직 리모트로 안 간 것이 있는지다 — 커밋 안 된 변경과 안 밀린 커밋 둘 다.
func unpushed(t *testing.T, vault string) bool {
	t.Helper()
	st := exec.Command("git", "status", "--porcelain")
	st.Dir = vault
	out, _ := st.Output()
	if strings.TrimSpace(string(out)) != "" {
		return true
	}
	rl := exec.Command("git", "rev-list", "--count", "@{upstream}..HEAD")
	rl.Dir = vault
	out, _ = rl.Output()
	return strings.TrimSpace(string(out)) != "0"
}

// dirtyNote 는 볼트에 결정 노트를 하나 떨어뜨린다 — 밀 것이 생긴 상태.
func dirtyNote(t *testing.T, vault, name string) {
	t.Helper()
	p := filepath.Join(vault, "alpha", "decisions", name)
	if err := os.WriteFile(p, []byte("---\ntype: decision\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ★ **Codex 에는 SessionEnd 가 없다** (host_test.go 의 실측). push 가 걸릴 자리가
// Stop 밖에 없으므로 거기서 밀어야 한다 — 안 그러면 개인 맥에서 내린 결정이
// 회사에서 영영 안 보인다. 이 동기화 기능을 만든 이유가 정확히 그 고장이었다.
func TestCodexStopPushesVault(t *testing.T) {
	c := cfg(t)
	v := gitVault(t, c)
	dirtyNote(t, v, "alpha-결정-코덱스에서-2026-08-23.md")

	runHookOn(t, HostCodex, c, pendingStore(t), EventStop, Input{Cwd: "/tmp/proj/alpha"})

	if unpushed(t, v) {
		t.Error("Codex 의 Stop 이 볼트를 안 밀었다 — 이 결정은 다른 머신에서 안 보인다")
	}
}

// ★ **Stop 은 턴마다 온다.** 매번 밀면 대화 한 번에 네트워크를 수십 번 탄다.
func TestCodexStopDebounces(t *testing.T) {
	c := cfg(t)
	v := gitVault(t, c)
	sd := pendingStore(t)
	// 방금 동기화한 것으로 해 둔다 — 세션 진입의 pull 이 남기는 도장과 같은 모양이다.
	if err := sync.WriteStamp(sd, sync.Stamp{At: time.Now(), OK: true}); err != nil {
		t.Fatal(err)
	}
	dirtyNote(t, v, "alpha-결정-너무빠르다-2026-08-23.md")

	runHookOn(t, HostCodex, c, sd, EventStop, Input{Cwd: "/tmp/proj/alpha"})

	if !unpushed(t, v) {
		t.Error("디바운스 창 안인데 밀었다 — 턴마다 네트워크를 탄다")
	}
}

// ★ **Claude Code 는 SessionEnd 가 있으므로 Stop 에서 밀지 않는다.**
// 이 테스트가 없으면 이번 변경이 돌고 있는 시스템에 턴마다 push 를 심을 수 있다.
func TestClaudeCodeStopDoesNotPush(t *testing.T) {
	c := cfg(t)
	v := gitVault(t, c)
	dirtyNote(t, v, "alpha-결정-클로드-2026-08-23.md")

	runHook(t, c, pendingStore(t), EventStop, Input{Cwd: "/tmp/proj/alpha"})

	if !unpushed(t, v) {
		t.Error("Claude Code 의 Stop 이 밀었다 — 턴마다 네트워크를 탄다")
	}
}

// **밀 것이 없으면 시계도 안 본다.** Status 는 로컬만 보므로 공짜고,
// 그래서 평소 Stop 에는 아무 비용이 없다.
func TestCodexStopSkipsCleanVault(t *testing.T) {
	c := cfg(t)
	gitVault(t, c)
	sd := pendingStore(t)

	runHookOn(t, HostCodex, c, sd, EventStop, Input{Cwd: "/tmp/proj/alpha"})

	if _, ok := sync.ReadStamp(sd); ok {
		t.Error("밀 것이 없는데 동기화를 시도했다 (도장이 찍혔다)")
	}
}

// 디바운스 판정은 순수 함수로 둔다 — 시계를 넣어 창 경계를 정확히 시험한다.
func TestDueForStopPush(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		st   sync.Stamp
		have bool
		want bool
	}{
		{"도장이 없으면 민다", sync.Stamp{}, false, true},
		{"창 안이면 안 민다", sync.Stamp{At: now.Add(-time.Minute), OK: true}, true, false},
		{"창을 넘으면 민다", sync.Stamp{At: now.Add(-stopPushInterval - time.Second), OK: true}, true, true},
		// 실패했어도 창은 지킨다. 막힌 망에서 턴마다 재시도하면 그 지연을 사람이 겪는다.
		{"실패 직후에도 창을 지킨다", sync.Stamp{At: now.Add(-time.Minute), OK: false}, true, false},
	}
	for _, tt := range tests {
		if got := dueForStopPush(tt.st, tt.have, now); got != tt.want {
			t.Errorf("%s: dueForStopPush = %v, 원하는 값 %v", tt.name, got, tt.want)
		}
	}
}
