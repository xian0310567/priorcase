package judge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// **앞자리가 실패해도 폴백이 돌 시간이 남아야 한다.**
//
// 사용자가 A안을 고를 때 정확히 이것을 물었다 — "폴백이 붙어도 모델은 썼는데
// 기록에는 실패하는 케이스는 별로 없겠지?" 답은 예산에 달려 있다. 앞자리가 상한을
// 크게 잡으면 그것이 예산을 다 먹고 폴백이 이름만 남는다.
//
// promoteBudget(90초)은 hook 패키지의 상수라 여기서 직접 못 본다. 그래서 관계만
// 못박는다: CodexTimeout 두 번이 승격 예산 안에 들어가야 한다 = 앞자리가 통째로
// 실패해도 절반 이상이 남는다.
func TestCodexTimeoutLeavesRoomForFallback(t *testing.T) {
	const promoteBudget = 90 * time.Second // hook.promoteBudget 과 같아야 한다

	if CodexTimeout >= DefaultTimeout {
		t.Errorf("CodexTimeout(%v) 이 DefaultTimeout(%v) 보다 짧아야 한다 — 앞자리는 빨리 포기해야 한다",
			CodexTimeout, DefaultTimeout)
	}
	if CodexTimeout*2 > promoteBudget {
		t.Errorf("CodexTimeout(%v) 이 예산(%v)의 절반을 넘는다 — 앞자리가 실패하면 폴백이 못 돈다",
			CodexTimeout, promoteBudget)
	}
	// **폴백에 남는 시간이 claude 의 실측 최대의 두 배는 돼야 한다.**
	// scan.go 의 maxExcerpt 주석이 24000B 에서 claude 최대 22.7초를 쟀다.
	const claudeWorst = 23 * time.Second
	if rest := promoteBudget - CodexTimeout; rest < claudeWorst*2 {
		t.Errorf("앞자리 실패 뒤 남는 시간이 %v 다 — claude 실측 최대 %v 의 두 배는 남아야 한다",
			rest, claudeWorst)
	}
	// 앞자리에도 여유가 있어야 한다. 자주 시간을 넘기면 두 벤더 시간을 다 쓴다.
	const codexWorst = 25 * time.Second // 24000B 실측 최대 24.9초
	if CodexTimeout < codexWorst*3/2 {
		t.Errorf("CodexTimeout(%v) 이 실측 최대 %v 의 1.5배 미만이다 — 앞자리가 자주 넘긴다",
			CodexTimeout, codexWorst)
	}
}

type fakeJudge struct {
	name string
	err  error
	v    Verdict
	seen *[]string
}

func (f fakeJudge) Decide(ctx context.Context, req Request) (Verdict, error) {
	*f.seen = append(*f.seen, f.name)
	if f.err != nil {
		return Verdict{}, f.err
	}
	return f.v, nil
}

// 앞이 성공하면 뒤는 부르지 않는다 — 부르면 두 벤더에 같은 발췌를 두 번 보낸다.
func TestChainStopsAtFirstSuccess(t *testing.T) {
	var seen []string
	ch := Chain{
		fakeJudge{name: "codex", v: Verdict{Tier: TierWorklog}, seen: &seen},
		fakeJudge{name: "claude", err: errors.New("불려선 안 된다"), seen: &seen},
	}
	if _, err := ch.Decide(context.Background(), Request{}); err != nil {
		t.Fatalf("앞이 성공했는데 에러가 났다: %v", err)
	}
	if len(seen) != 1 || seen[0] != "codex" {
		t.Errorf("호출 순서 = %v, want [codex] 하나만", seen)
	}
}

// 앞이 실패하면 뒤가 받는다. 그게 이 사슬의 존재 이유다.
func TestChainFallsBackOnFailure(t *testing.T) {
	var seen []string
	ch := Chain{
		fakeJudge{name: "codex", err: errors.New("로그아웃"), seen: &seen},
		fakeJudge{name: "claude", v: Verdict{Tier: TierDecision, Slug: "s", Summary: "x"}, seen: &seen},
	}
	v, err := ch.Decide(context.Background(), Request{})
	if err != nil {
		t.Fatalf("폴백이 받지 못했다: %v", err)
	}
	if v.Tier != TierDecision {
		t.Errorf("판정이 안 왔다: %+v", v)
	}
	if len(seen) != 2 {
		t.Errorf("폴백을 안 불렀다: %v", seen)
	}
}

// 다 실패하면 **전부를 묶어** 낸다. 하나만 내면 사람이 다른 쪽도 죽은 줄 모른다.
func TestChainReportsEveryFailure(t *testing.T) {
	var seen []string
	ch := Chain{
		fakeJudge{name: "codex", err: errors.New("코덱스 로그아웃"), seen: &seen},
		fakeJudge{name: "claude", err: errors.New("클로드 쿼터 소진"), seen: &seen},
	}
	_, err := ch.Decide(context.Background(), Request{})
	if err == nil {
		t.Fatal("다 실패했는데 에러가 없다")
	}
	for _, want := range []string{"코덱스 로그아웃", "클로드 쿼터 소진"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q 가 보고에 없다: %v", want, err)
		}
	}
}

// 예산이 이미 끝났으면 **부르지 않는다.** 부르면 즉시 실패하고 그 실패가 로그를
// 채워 진짜 원인을 가린다 (원장 62건 중 52건이 그 인공물이었던 전례가 있다).
func TestChainSkipsWhenBudgetGone(t *testing.T) {
	var seen []string
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := Chain{fakeJudge{name: "codex", seen: &seen}, fakeJudge{name: "claude", seen: &seen}}
	if _, err := ch.Decide(ctx, Request{}); err == nil {
		t.Error("취소된 컨텍스트에서 성공을 냈다")
	}
	if len(seen) != 0 {
		t.Errorf("취소됐는데 판별기를 불렀다: %v", seen)
	}
}

func TestFlavorArgs(t *testing.T) {
	codex := strings.Join(FlavorCodex.args("m"), " ")
	for _, want := range []string{"exec", "--ignore-user-config", "--ephemeral", "-m m"} {
		if !strings.Contains(codex, want) {
			t.Errorf("codex 인자에 %q 가 없다: %s", want, codex)
		}
	}
	// **stdin 표시가 마지막이어야 한다.** 실측에서 프롬프트를 인자로 주면 불안정했다.
	if a := FlavorCodex.args("m"); a[len(a)-1] != "-" {
		t.Errorf("codex 인자가 stdin(-) 으로 끝나지 않는다: %v", a)
	}
	claude := strings.Join(FlavorClaude.args("m"), " ")
	for _, want := range []string{"--print", "--model m", "--max-turns 1"} {
		if !strings.Contains(claude, want) {
			t.Errorf("claude 인자에 %q 가 없다: %s", want, claude)
		}
	}
}

func TestFlavorOf(t *testing.T) {
	for path, want := range map[string]Flavor{
		"/usr/local/bin/codex":          FlavorCodex,
		"/opt/homebrew/bin/claude":      FlavorClaude,
		"/Users/x/codex-tools/claude":   FlavorClaude, // 디렉토리 이름에 속지 않는다
		"/Users/x/.local/bin/codex-cli": FlavorCodex,
		"":                              FlavorClaude, // 모르면 원래 동작
	} {
		if got := FlavorOf(path); got != want {
			t.Errorf("FlavorOf(%q) = %q, want %q", path, got, want)
		}
	}
}

// 설정의 judge_model 은 **claude 값**이다. codex 에 넘기면 없는 모델이라 실패한다.
func TestConfiguredModelDoesNotLeakToCodex(t *testing.T) {
	c := newFlavorCLI(FlavorCodex, "/bin/codex", "")
	if c.Model != DefaultCodexModel {
		t.Errorf("codex 기본 모델 = %q, want %q", c.Model, DefaultCodexModel)
	}
	if got := FindFlavorAt("/bin/sh", "claude-haiku-4-5"); got != nil && got.Flavor == FlavorClaude && got.Model != "claude-haiku-4-5" {
		t.Errorf("claude 에는 설정 모델이 가야 한다: %q", got.Model)
	}
}

// 제로값 CLI 는 claude 로 돈다 — 옛 호출부가 전부 그랬으므로 기본이 바뀌면 안 된다.
func TestZeroCLIIsClaude(t *testing.T) {
	var c CLI
	if got := c.Flavor.defaultTimeout(); got != DefaultTimeout {
		t.Errorf("제로값 상한 = %v, want %v", got, DefaultTimeout)
	}
	if !strings.Contains(strings.Join(c.Flavor.args("m"), " "), "--print") {
		t.Error("제로값이 claude 인자를 안 쓴다")
	}
}
