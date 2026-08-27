package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// ★★★ **규칙은 이 시스템을 만든 이유에 가장 가까운 계층이다.**
//
// 실측(2026-08-27): 교차 프로젝트 주입은 이미 57.3%였는데 어시스턴트가 그 노트를
// 실제로 언급한 것은 4.4%뿐이었다. 넘어오는데 못 쓴 것이다 — 결정 요약의 76%가
// 사건 서술이라("GP-1561 실동작 검증 완료") 다른 프로젝트에서는 쓸 것이 없었다.
// 도메인 쌍 어휘 Jaccard 평균이 0.046 이라 낱말도 안 겹친다.
//
// 규칙은 도메인이 없어서 그 두 벽을 다 지나간다.

func planRule(t *testing.T, c *config.Config, stem, summary string, related []string) {
	t.Helper()
	dir := filepath.Join(c.DefaultVaultPath(), "_meta", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rel := "[]"
	if len(related) > 0 {
		rel = "[" + strings.Join(related, ", ") + "]"
	}
	src := "---\ntype: rule\ndate: 2026-08-27\nsummary: \"" + summary + "\"\n" +
		"status: active\noutcome: pending\nsupersedes: \"\"\nrelated: " + rel +
		"\ntags: [rule]\n---\n\n## 규칙\n\n내용.\n"
	if err := os.WriteFile(filepath.Join(dir, stem+".md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ruleFixture(t *testing.T) (*config.Config, *store.Layout) {
	t.Helper()
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	planRule(t, c, "규칙-케이크는-딸기와-같이-낸다",
		"케이크에는 딸기를 같이 낸다", []string{"\"[[alpha-결정-저장엔진-2026-08-01]]\""})
	return c, l
}

// **RuleLimit 이 0 이면 규칙을 안 준다.** 기본 동작은 지금 그대로다 —
// 폴더를 만드는 것만으로 모든 호출부가 조용히 달라지면 안 된다.
func TestRuleLimitZeroMeansNone(t *testing.T) {
	c, l := ruleFixture(t)
	hits := mustRecall(t, l, c, "케이크 딸기", Options{Limit: 5, MinScore: 1, CrossProject: true})
	for _, h := range hits {
		if h.Note.IsRule() {
			t.Errorf("RuleLimit 0 인데 규칙이 왔다: %s", h.Note.Stem)
		}
	}
}

func TestRuleLimitBringsRules(t *testing.T) {
	c, l := ruleFixture(t)
	hits := mustRecall(t, l, c, "케이크 딸기",
		Options{Limit: 5, RuleLimit: 2, MinScore: 1, CrossProject: true})
	found := false
	for _, h := range hits {
		if h.Note.IsRule() {
			found = true
		}
	}
	if !found {
		t.Errorf("RuleLimit 을 줬는데 규칙이 안 왔다 (히트 %d건: %v)", len(hits), stemsOf(hits))
	}
}

// ★★ **규칙은 결정을 밀어내지 않는다.** 자리를 따로 준 이유가 그것이다.
//
// 결정과 섞어 자르면 규칙이 언제나 진다 — 규칙 요약은 한 줄이고 결정 요약은
// 중앙 184자라 head 히트 수로 겨루면 긴 쪽이 이긴다. 그런데 반대 방향도 막아야
// 한다: 규칙이 결정 슬롯을 먹으면 "지난번에 무엇을 했나" 가 사라진다.
func TestRulesNeverDisplaceDecisions(t *testing.T) {
	c, l := ruleFixture(t)
	plant(t, c, "alpha", "alpha-결정-케이크하나-2026-08-11", "케이크를 고른다")
	plant(t, c, "beta", "beta-결정-케이크둘-2026-08-12", "케이크와 딸기를 고른다")

	hits := mustRecall(t, l, c, "케이크 딸기", Options{
		Limit: 2, RuleLimit: 1, MinScore: 1, CrossProject: true,
	})
	var dec, rule int
	for _, h := range hits {
		if h.Note.IsRule() {
			rule++
		} else {
			dec++
		}
	}
	if dec != 2 {
		t.Errorf("결정 %d건 — 2건이어야 한다(규칙이 밀어냈다): %v", dec, stemsOf(hits))
	}
	if rule != 1 {
		t.Errorf("규칙 %d건 — 1건이어야 한다: %v", rule, stemsOf(hits))
	}
}

// ★★ **규칙이 맨 위다.** 주입 블록은 위에서부터 읽히고 잘릴 수 있다.
// 규칙은 여러 결정에서 증류한 것이라 한 줄당 값이 가장 크다.
func TestRulesComeFirst(t *testing.T) {
	c, l := ruleFixture(t)
	plant(t, c, "alpha", "alpha-결정-케이크하나-2026-08-11", "케이크와 딸기를 고른다")

	hits := mustRecall(t, l, c, "케이크 딸기", Options{
		Limit: 3, RuleLimit: 1, MinScore: 1, CrossProject: true,
	})
	if len(hits) < 2 {
		t.Fatalf("비교할 만큼 안 걸렸다: %v", stemsOf(hits))
	}
	if !hits[0].Note.IsRule() {
		t.Errorf("맨 위가 규칙이 아니다: %v", stemsOf(hits))
	}
}

// ★★★ **규칙에는 cwd 가점이 붙지 않는다.**
//
// 규칙의 존재 이유가 "어느 프로젝트에서 물어도 같은 자격으로 걸린다" 인데,
// 도메인을 갖는 순간 weightCwdDomain(+2)이 자기 폴더에서만 가점을 준다. 그러면
// 규칙이 다시 프로젝트의 것이 되고, 이 계층을 만든 이유가 사라진다.
//
// 파일에 domain 이 적혀 있어도 무시하는 것을 확인한다 — 사람이 습관적으로 적을
// 자리이고, 조용히 먹히면 그 규칙만 한 프로젝트에서 세진다.
func TestRuleIgnoresDomainEvenIfWritten(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	dir := filepath.Join(c.DefaultVaultPath(), "_meta", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "---\ntype: rule\ndate: 2026-08-27\ndomain: [alpha]\n" +
		"summary: \"케이크에는 딸기를 같이 낸다\"\nstatus: active\noutcome: pending\n" +
		"supersedes: \"\"\nrelated: []\ntags: [rule]\n---\n\n## 규칙\n\n내용.\n"
	if err := os.WriteFile(filepath.Join(dir, "규칙-도메인적힌것.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	rules, _, err := l.ListRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("규칙 %d건 — 1건이어야 한다", len(rules))
	}
	if len(rules[0].Meta.Domain) != 0 {
		t.Errorf("규칙에 도메인이 남았다: %v — 비워야 cwd 가점을 안 받는다", rules[0].Meta.Domain)
	}

	// 같은 점수의 규칙이 alpha 에서 물었을 때와 beta 에서 물었을 때 같아야 한다.
	inAlpha := mustRecall(t, l, c, "케이크 딸기",
		Options{Cwd: "/tmp/proj/alpha", Limit: 3, RuleLimit: 1, MinScore: 1, CrossProject: true})
	inBeta := mustRecall(t, l, c, "케이크 딸기",
		Options{Cwd: "/tmp/proj/beta", Limit: 3, RuleLimit: 1, MinScore: 1, CrossProject: true})
	ruleScore := func(hits []Hit) int {
		for _, h := range hits {
			if h.Note.IsRule() {
				return h.Score
			}
		}
		return -1
	}
	a, b := ruleScore(inAlpha), ruleScore(inBeta)
	if a <= 0 || a != b {
		t.Errorf("규칙 점수가 폴더에 따라 다르다 (alpha=%d beta=%d) — 도메인 가점이 붙었다", a, b)
	}
}

// ★★ **규칙을 참고로 그리면 안 된다.**
//
// `IsReference` 가 `type != "decision"` 하나였던 동안 규칙이 참고로 분류됐다.
// 참고는 "확정되지 않은 것" 이라는 뜻이라(그래서 status 를 안 찍는다) 정반대다 —
// 규칙은 여러 결정에서 증류한 가장 확정된 것이다.
func TestRuleIsNotAReference(t *testing.T) {
	c, l := ruleFixture(t)
	_ = c
	rules, _, err := l.ListRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) == 0 {
		t.Fatal("픽스처에 규칙이 없다")
	}
	if rules[0].IsReference() {
		t.Error("규칙이 참고로 분류된다 — 회수 블록에 [참고] 로 그려진다")
	}
	if !rules[0].IsRule() {
		t.Error("IsRule 이 false 다")
	}
}

// 주입 형식 — `[규칙]` 표식이 있고, active 규칙에는 날짜·상태를 안 찍는다.
//
// 규칙에는 "언제 이 일이 있었나" 가 없고 "지금 유효한가" 만 있다. 결정처럼
// `2026-08-27 … (active/pending)` 을 찍으면 읽는 쪽이 그것을 그날의 사건으로 읽는다.
func TestRenderInjectMarksRules(t *testing.T) {
	_, l := ruleFixture(t)
	rules, _, err := l.ListRules()
	if err != nil {
		t.Fatal(err)
	}
	s := RenderInject(l, []Hit{{Note: rules[0], Score: 9}})
	if !strings.Contains(s, "[규칙]") {
		t.Errorf("규칙 표식이 없다:\n%s", s)
	}
	if strings.Contains(s, "active/pending") {
		t.Errorf("active 규칙에 결정용 상태가 찍혔다:\n%s", s)
	}
	if strings.Contains(s, "[참고]") {
		t.Errorf("규칙이 참고로 그려졌다:\n%s", s)
	}
	if !strings.Contains(s, "케이크에는 딸기를 같이 낸다") {
		t.Errorf("요약이 안 나왔다:\n%s", s)
	}
}

// 뒤집힌 규칙은 상태를 찍는다 — 규칙도 뒤집힌다.
func TestRenderInjectShowsRuleStatusWhenNotActive(t *testing.T) {
	_, l := ruleFixture(t)
	rules, _, err := l.ListRules()
	if err != nil {
		t.Fatal(err)
	}
	n := rules[0]
	n.Meta.Status = store.StatusSuperseded
	s := RenderInject(l, []Hit{{Note: n, Score: 1}})
	if !strings.Contains(s, store.StatusSuperseded) {
		t.Errorf("뒤집힌 규칙인데 상태가 없다:\n%s", s)
	}
}

// 폴더가 없는 것은 에러가 아니다 — 대다수 볼트에 없고 없는 것이 정상이다.
func TestListRulesMissingDirIsNotAnError(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	rules, skipped, err := l.ListRules()
	if err != nil {
		t.Fatalf("폴더가 없는데 에러가 났다: %v", err)
	}
	if len(rules) != 0 || len(skipped) != 0 {
		t.Errorf("빈 볼트인데 규칙 %d건 · 건너뜀 %d건", len(rules), len(skipped))
	}
	// 폴더가 없어도 회수가 지금과 똑같이 돈다.
	hits := mustRecall(t, l, c, "저장 엔진", Options{Limit: 3, RuleLimit: 2, MinScore: 1})
	if len(hits) == 0 {
		t.Error("규칙 폴더가 없을 때 회수가 죽었다")
	}
}

// 그 폴더의 README·초안은 규칙이 아니다 — 고장이 아니라 정상이므로 조용히 빠진다.
func TestListRulesIgnoresNonRuleDocs(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	dir := filepath.Join(c.DefaultVaultPath(), "_meta", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "# 규칙 폴더\n\n여기에 규칙을 둔다.\n")                                         // frontmatter 없음
	write("초안.md", "---\ntype: rule\nsummary: \"\"\nstatus: active\n---\n\n미정.\n")         // summary 없음
	write("결정이섞였다.md", "---\ntype: decision\nsummary: \"결정\"\nstatus: active\n---\n\n.\n") // 결정
	planRule(t, c, "규칙-진짜", "케이크에는 딸기를 같이 낸다", nil)

	rules, skipped, err := l.ListRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Errorf("참여하지 않는 문서가 '읽지 못함' 으로 잡혔다: %+v", skipped)
	}
	if len(rules) != 1 || !strings.Contains(rules[0].Stem, "진짜") {
		t.Errorf("규칙 %d건: %+v — 1건(규칙-진짜)이어야 한다", len(rules), stemsOfNotes(rules))
	}
}

// 깨진 frontmatter 는 **여전히 실패다.** 조용히 빠지면 "그런 규칙이 없다" 와
// 구별되지 않고, 규칙은 몇 건뿐이라 한 건이 빠지는 손해가 결정보다 크다.
func TestListRulesReportsBrokenFrontmatter(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	dir := filepath.Join(c.DefaultVaultPath(), "_meta", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "깨진규칙.md"),
		[]byte("---\ntype: rule\nsummary: [닫히지 않은\n---\n\n.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rules, skipped, err := l.ListRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Errorf("깨진 노트가 규칙으로 들어왔다: %+v", stemsOfNotes(rules))
	}
	if len(skipped) != 1 {
		t.Fatalf("건너뜀 %d건 — 1건이어야 한다(조용히 사라지면 '없다' 와 구별되지 않는다)", len(skipped))
	}
}

func stemsOfNotes(ns []store.Note) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.Stem)
	}
	return out
}
