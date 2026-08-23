package hook

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
)

// ask 는 한 세션 안에서 프롬프트를 던진다. transcript 경로가 세션을 가른다.
func ask(t *testing.T, c *config.Config, sd, transcript, prompt string) string {
	t.Helper()
	return runHook(t, c, sd, EventUserPromptSubmit, Input{
		Cwd: "/tmp/proj/alpha", Prompt: prompt, TranscriptPath: transcript,
	}).out
}

const probe = "저장 엔진을 무엇으로 할지 다시 보자"

// ★ **이미 컨텍스트에 있는 것을 다시 밀어 넣지 않는다.**
//
// 실측(2026-08-23, 이 저장소의 실제 세션 28프롬프트): 주입 38,316자 중 16,126자
// (42%)가 **같은 세션에서 이미 주입한 노트의 재주입**이었다. 모델은 그것을 이미
// 컨텍스트에 갖고 있으므로 두 번째부터는 아무것도 더해 주지 않는다.
//
// 그래도 **자리는 남긴다** — "지금 이게 관련 있다" 는 신호는 매번 필요하고,
// 경로가 있어야 에이전트가 열어 볼 수 있다. 요약 본문만 뺀다.
func TestSecondInjectionDropsTheSummary(t *testing.T) {
	c := cfg(t)
	sd := pendingStore(t)
	tr := filepath.Join(t.TempDir(), "session.jsonl")

	first := ask(t, c, sd, tr, probe)
	if !strings.Contains(first, "[과거 결정 참조]") {
		t.Fatalf("첫 주입이 아예 없다:\n%s", first)
	}
	second := ask(t, c, sd, tr, probe)

	if len(second) >= len(first) {
		t.Errorf("두 번째 주입이 안 줄었다 (%d → %d자):\n%s", len(first), len(second), second)
	}
	// 경로는 남아야 한다 — 열어 볼 수 없으면 신호가 무의미하다.
	if !strings.Contains(second, ".md") {
		t.Errorf("두 번째 주입에 경로가 없다:\n%s", second)
	}
}

// 첫 주입은 한 글자도 안 바뀐다. 이 최적화가 정상 경로를 건드리면 안 된다.
func TestFirstInjectionUnchanged(t *testing.T) {
	c := cfg(t)
	tr := filepath.Join(t.TempDir(), "a.jsonl")

	withKey := ask(t, c, pendingStore(t), tr, probe)
	// 세션 키가 없으면(둘 다 빈 값) 최적화가 꺼진다 — 그때와 같아야 한다.
	noKey := runHook(t, c, pendingStore(t), EventUserPromptSubmit,
		Input{Cwd: "/tmp/proj/alpha", Prompt: probe}).out

	if withKey != noKey {
		t.Errorf("첫 주입이 달라졌다:\n--- 세션키 있음 ---\n%s\n--- 없음 ---\n%s", withKey, noKey)
	}
}

// ★ 세션 키를 모르면 **끄고 전문을 낸다.** 조용히 덜 주는 것보다 낫다.
func TestWithoutSessionKeyAlwaysFull(t *testing.T) {
	c := cfg(t)
	sd := pendingStore(t)
	in := Input{Cwd: "/tmp/proj/alpha", Prompt: probe}

	a := runHook(t, c, sd, EventUserPromptSubmit, in).out
	b := runHook(t, c, sd, EventUserPromptSubmit, in).out
	if a != b {
		t.Errorf("세션을 못 가리는데 두 번째가 줄었다:\n%s", b)
	}
}

// 세션이 다르면 서로 영향을 주지 않는다.
func TestSeenIsPerSession(t *testing.T) {
	c := cfg(t)
	sd := pendingStore(t)
	dir := t.TempDir()

	first := ask(t, c, sd, filepath.Join(dir, "s1.jsonl"), probe)
	other := ask(t, c, sd, filepath.Join(dir, "s2.jsonl"), probe)

	if other != first {
		t.Errorf("다른 세션인데 줄었다:\n--- s1 ---\n%s\n--- s2 ---\n%s", first, other)
	}
}

// ★★ **압축 뒤에는 전문을 다시 낸다.**
//
// 이게 없으면 최적화가 조용한 품질 저하가 된다 — 압축으로 앞부분이 날아가면
// 요약은 컨텍스트에 없는데 포인터만 계속 나오고, 에이전트는 근거를 잃는다.
func TestCompactResetsSeen(t *testing.T) {
	c := cfg(t)
	sd := pendingStore(t)
	tr := filepath.Join(t.TempDir(), "session.jsonl")

	first := ask(t, c, sd, tr, probe)
	if second := ask(t, c, sd, tr, probe); second == first {
		t.Fatal("두 번째가 안 줄었다 — 이 테스트의 전제가 깨졌다")
	}

	runHook(t, c, sd, EventPreCompact, Input{Cwd: "/tmp/proj/alpha", TranscriptPath: tr})

	if after := ask(t, c, sd, tr, probe); after != first {
		t.Errorf("압축 뒤에도 요약이 안 돌아왔다:\n--- 처음 ---\n%s\n--- 압축 뒤 ---\n%s", first, after)
	}
}

// 세션을 새로 열면(clear) 컨텍스트가 비었으므로 다시 전문이다.
func TestSessionClearResetsSeen(t *testing.T) {
	c := cfg(t)
	sd := pendingStore(t)
	tr := filepath.Join(t.TempDir(), "session.jsonl")

	first := ask(t, c, sd, tr, probe)
	_ = ask(t, c, sd, tr, probe)

	runHook(t, c, sd, EventSessionStart,
		Input{Cwd: "/tmp/proj/alpha", TranscriptPath: tr, Source: "clear"})

	if after := ask(t, c, sd, tr, probe); after != first {
		t.Errorf("clear 뒤에도 요약이 안 돌아왔다:\n%s", after)
	}
}

// ★ **한 세션에 같은 결과를 두 번 묻지 않는다.**
//
// retro.Ask 는 주입된 1위 노트의 결과를 묻는데, 세션 스로틀이 없어서 같은 노트가
// 계속 1위면 **매 프롬프트마다 같은 질문이 요약까지 붙어 반복된다.** 답을 모르면
// 매번 넘어가게 되고, 그러면 그 물음은 배경이 되어 죽는다.
func TestOutcomeIsAskedOncePerSession(t *testing.T) {
	c := cfg(t)
	sd := pendingStore(t)
	tr := filepath.Join(t.TempDir(), "session.jsonl")

	first := ask(t, c, sd, tr, probe)
	if !strings.Contains(first, "결과가 안 적혀 있다") {
		t.Fatalf("첫 물음이 없다 — 이 테스트의 전제가 깨졌다:\n%s", first)
	}
	second := ask(t, c, sd, tr, probe)
	if strings.Contains(second, "결과가 안 적혀 있다") {
		t.Errorf("같은 세션에서 같은 것을 또 묻는다:\n%s", second)
	}
}
