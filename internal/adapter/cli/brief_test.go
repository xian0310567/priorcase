package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

func briefLayout(t *testing.T) *store.Layout {
	t.Helper()
	return store.NewLayout(testutil.VaultConfig(t))
}

func writeRule(t *testing.T, l *store.Layout, stem, summary, status string) {
	t.Helper()
	p := filepath.Join(l.RulesDir(), stem+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ntype: rule\nsummary: \"" + summary + "\"\nstatus: " + status + "\n---\n\n본문\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBriefCarriesRulesAndProcedures(t *testing.T) {
	l := briefLayout(t)
	writeRule(t, l, "규칙-밖으로-나가는-행동", "밖으로 나가는 행동은 매번 승인받는다", "active")
	// 뒤집힌 규칙은 안 실린다 — 브리지는 세션당 한 번이라 낡은 것이 오래 산다.
	writeRule(t, l, "규칙-옛것", "이건 뒤집혔다", "superseded")

	p := filepath.Join(l.Vault(), "alpha", "decisions", "alpha-결정-도구-2026-08-28.md")
	body := "---\ntype: decision\ndate: 2026-08-28\ndomain: [alpha]\n" +
		"summary: \"브라우저는 오르카로 연다\"\nstatus: active\ntags: [decision]\n---\n\n" +
		"## 절차\n\n```bash\norca tab create --url 'https://example.com'\n```\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := renderBrief(l, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"prior recall", // 회수하는 법
		"밖으로 나가는 행동은 매번 승인받는다", // 살아 있는 규칙
		"`orca`",                 // 이 환경에 있는 명령
		"alpha-결정-도구-2026-08-28", // 본문을 열 자리
	} {
		if !strings.Contains(got, want) {
			t.Errorf("브리프에 %q 가 없다:\n%s", want, got)
		}
	}
	if strings.Contains(got, "이건 뒤집혔다") {
		t.Errorf("뒤집힌 규칙이 실렸다:\n%s", got)
	}
}

// 다른 도메인의 절차는 안 싣는다 — 이 파일은 그 프로젝트의 지침에 들어간다.
func TestBriefScopesProceduresToDomain(t *testing.T) {
	l := briefLayout(t)
	p := filepath.Join(l.Vault(), "beta", "decisions", "beta-결정-도구-2026-08-28.md")
	body := "---\ntype: decision\ndate: 2026-08-28\ndomain: [beta]\n" +
		"summary: \"베타 도구\"\nstatus: active\ntags: [decision]\n---\n\n" +
		"```bash\nbetatool run\n```\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := renderBrief(l, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "betatool") {
		t.Errorf("남의 도메인 절차가 실렸다:\n%s", got)
	}
}

// 규칙도 절차도 없으면 회수하는 법만 남는다 — 빈 절을 만들지 않는다.
func TestBriefWithoutRulesOrProcedures(t *testing.T) {
	l := briefLayout(t)
	got, err := renderBrief(l, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "prior recall") {
		t.Errorf("회수 안내가 없다:\n%s", got)
	}
	for _, no := range []string{"### 규칙", "### 이 프로젝트에서 쓸 수 있는 것"} {
		if strings.Contains(got, no) {
			t.Errorf("빈 절 %q 가 있다:\n%s", no, got)
		}
	}
}
