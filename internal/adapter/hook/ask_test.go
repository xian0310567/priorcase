package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
)

// ★★★ **훅이 실제로 그 질문을 내는가.**
//
// retro.Ask 를 직접 부르는 시험은 "규칙이 옳은가" 만 본다. 이 프로젝트에서 다섯
// 번 난 사고가 전부 **함수는 옳은데 조립부가 안 부른다** 였다. 여기서는 진짜 훅을
// 돌려 stdout 에 무엇이 실리는지 본다 — 그 stdout 이 곧 에이전트의 컨텍스트다.

// askFixture 는 볼트에 결정 노트 하나를 심고 그 설정을 준다.
func askFixture(t *testing.T, slug, date, status, outcome, summary string) *config.Config {
	t.Helper()
	c := cfg(t)
	dir := filepath.Join(c.Vaults[0].Path, "alpha", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(
		"---\ntype: decision\ndate: %s\ndomain: [alpha]\nsummary: %q\nstatus: %s\noutcome: %s\n---\n\n## 결정\n본문\n",
		date, summary, status, outcome)
	name := fmt.Sprintf("alpha-결정-%s-%s.md", slug, date)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return c
}

func daysAgo(n int) string { return time.Now().AddDate(0, 0, -n).Format("2006-01-02") }

// ★★★ 오래된 결정이 회수되면 **그 자리에서 결과를 묻는다.**
//
// 실측(2026-08-14): 결정 157건 중 결과가 적힌 것이 2건(1.3%)이다. 회고 큐에는
// 52건이 쌓여 있는데 아무도 안 본다 — 목록을 보러 가는 일은 따로 시간을 내는
// 일이고, 따로 시간을 내는 일은 안 일어난다.
func TestOldRecalledDecisionIsAskedInline(t *testing.T) {
	c := askFixture(t, "큐브샛통신", daysAgo(20), "active", "pending", "큐브샛 통신을 UHF 대신 S밴드로 간다")
	r := runHook(t, c, t.TempDir(), EventUserPromptSubmit,
		Input{Prompt: "큐브샛 통신 S밴드 결정을 다시 보자", Cwd: "/tmp/proj/alpha"})
	if r.e != nil {
		t.Fatalf("훅이 실패했다: %v (stderr=%s)", r.e, r.err)
	}
	if !strings.Contains(r.out, "결과를 아직 모르는 결정") {
		t.Fatalf("결과를 안 물었다:\n%s", r.out)
	}
	// **무엇을 치면 되는지 그 자리에 있어야 한다.** 명령을 찾아 가야 하면 안 한다.
	// **내가 심은 노트를 물어야 한다.** 볼트 픽스처에는 다른 결정도 있어서,
	// 이 확인이 없으면 엉뚱한 노트를 물어도 통과한다 — 실제로 그렇게 한 번 통과했다.
	if !strings.Contains(r.out, "alpha-결정-큐브샛통신") {
		t.Errorf("엉뚱한 노트를 물었다:\n%s", r.out)
	}
	if !strings.Contains(r.out, "prior review") || !strings.Contains(r.out, "--outcome") {
		t.Errorf("답하는 방법을 안 알려 준다:\n%s", r.out)
	}
	if !strings.Contains(r.out, "큐브샛 통신을 UHF") {
		t.Errorf("무슨 결정인지 안 보여 준다:\n%s", r.out)
	}
}

// ★★★ **최근 결정에는 안 묻는다.** 답이 "아직 모른다" 뿐인 질문은 소음이고,
// 소음은 무시하는 법을 가르친다.
func TestFreshDecisionIsNotAskedInline(t *testing.T) {
	c := askFixture(t, "큐브샛통신", daysAgo(1), "active", "pending", "큐브샛 통신을 UHF 대신 S밴드로 간다")
	r := runHook(t, c, t.TempDir(), EventUserPromptSubmit,
		Input{Prompt: "큐브샛 통신 S밴드 결정을 다시 보자", Cwd: "/tmp/proj/alpha"})
	if strings.Contains(r.out, "결과를 아직 모르는 결정") {
		t.Errorf("어제 결정에 결과를 물었다:\n%s", r.out)
	}
	// 회수 자체는 되어야 한다 — 안 되면 이 시험이 아무것도 안 지킨다.
	if !strings.Contains(r.out, "과거 결정 참조") {
		t.Fatalf("회수가 아예 안 됐다 — 시험이 헛돈다:\n%s", r.out)
	}
}

// ★★★ **뒤집힌 결정은 나이를 안 본다.** 뒤집혔다는 것 자체가 결과가 났다는 뜻이다.
func TestSupersededIsAskedInlineEvenIfFresh(t *testing.T) {
	c := askFixture(t, "큐브샛통신", daysAgo(1), "superseded", "pending", "큐브샛 통신을 UHF 대신 S밴드로 간다")
	r := runHook(t, c, t.TempDir(), EventUserPromptSubmit,
		Input{Prompt: "큐브샛 통신 S밴드 결정을 다시 보자", Cwd: "/tmp/proj/alpha"})
	if !strings.Contains(r.out, "결과를 아직 모르는 결정") {
		t.Fatalf("뒤집힌 결정에 결과를 안 물었다:\n%s", r.out)
	}
	if !strings.Contains(r.out, "alpha-결정-큐브샛통신") {
		t.Errorf("엉뚱한 노트를 물었다:\n%s", r.out)
	}
	if !strings.Contains(r.out, "뒤집힌") {
		t.Errorf("왜 묻는지 안 말한다:\n%s", r.out)
	}
}

// ★★★ **이미 답한 것은 다시 안 묻는다.** 같은 질문이 반복되면 그것이 곧
// 무시하는 법을 가르치는 신호가 된다.
func TestAnsweredDecisionIsNotAskedInline(t *testing.T) {
	c := askFixture(t, "큐브샛통신", daysAgo(20), "active", "good", "큐브샛 통신을 UHF 대신 S밴드로 간다")
	r := runHook(t, c, t.TempDir(), EventUserPromptSubmit,
		Input{Prompt: "큐브샛 통신 S밴드 결정을 다시 보자", Cwd: "/tmp/proj/alpha"})
	if strings.Contains(r.out, "결과를 아직 모르는 결정") {
		t.Errorf("이미 답한 것을 또 물었다:\n%s", r.out)
	}
}

// ★★★ **회수가 비면 아무것도 안 묻는다.** 주입이 없는데 질문만 뜨면 무엇에
// 대한 질문인지 알 수 없다.
func TestNoRecallNoQuestion(t *testing.T) {
	c := askFixture(t, "큐브샛통신", daysAgo(20), "active", "pending", "큐브샛 통신을 UHF 대신 S밴드로 간다")
	r := runHook(t, c, t.TempDir(), EventUserPromptSubmit,
		Input{Prompt: "전혀 상관없는 주제 zzzqqq 에 대해 물어본다", Cwd: "/tmp/proj/alpha"})
	if strings.Contains(r.out, "결과를 아직 모르는 결정") {
		t.Errorf("회수가 없는데 물었다:\n%s", r.out)
	}
}
