package split

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// write 는 폴백 도메인(common)에 결정 노트 하나를 만든다.
func write(t *testing.T, l *store.Layout, stem, domain, summary, body string) string {
	t.Helper()
	p := filepath.Join(l.Vault(), domain, "decisions", stem+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	fm := "---\ntype: decision\ndate: 2026-08-28\ndomain: [" + domain + "]\n" +
		"summary: \"" + summary + "\"\nstatus: active\ntags: [decision]\n---\n\n" + body + "\n"
	if err := os.WriteFile(p, []byte(fm), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func setup(t *testing.T) (*config.Config, *store.Layout) {
	t.Helper()
	c := testutil.VaultConfig(t)
	return c, store.NewLayout(c)
}

func build(t *testing.T, c *config.Config, l *store.Layout, token, as string) *Plan {
	t.Helper()
	notes, _, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(c, l, notes, []string{token}, as)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBuildPicksOnlyFallbackNotes(t *testing.T) {
	c, l := setup(t)
	write(t, l, "common-결정-twincrew-라우터-2026-08-28", "common", "twincrew 라우터를 남긴다", "본문")
	// **선언된 도메인의 노트는 대상이 아니다.** 같은 낱말이 걸려도 그 노트는
	// 이미 제자리에 있다 — 옮기면 남의 프로젝트를 가져오는 것이 된다.
	write(t, l, "alpha-결정-twincrew-무관-2026-08-28", "alpha", "twincrew 를 언급하는 alpha 결정", "본문")

	p := build(t, c, l, "twincrew", "")
	if len(p.Moves) != 1 {
		var got []string
		for _, m := range p.Moves {
			got = append(got, m.OldStem)
		}
		t.Fatalf("옮길 노트가 %d건이다: %v — 폴백 도메인 것 1건이어야 한다", len(p.Moves), got)
	}
	if !strings.HasPrefix(p.Moves[0].OldStem, "common-") {
		t.Errorf("폴백이 아닌 노트를 골랐다: %s", p.Moves[0].OldStem)
	}
}

func TestBuildStripsDuplicatedPrefix(t *testing.T) {
	c, l := setup(t)
	write(t, l, "common-결정-twincrew-라우터-2026-08-28", "common", "twincrew 라우터", "본문")
	p := build(t, c, l, "twincrew", "")
	want := "twincrew-결정-라우터-2026-08-28"
	if p.Moves[0].NewStem != want {
		t.Errorf("새 이름 %q, want %q — slug 앞의 접두어 중복을 지워야 한다",
			p.Moves[0].NewStem, want)
	}
}

func TestApplyMovesRewritesDomainAndLinks(t *testing.T) {
	c, l := setup(t)
	old := "common-결정-twincrew-라우터-2026-08-28"
	write(t, l, old, "common", "twincrew 라우터", "본문")
	// 이 노트를 가리키는 다른 프로젝트의 문서. 별칭·절 앵커도 살아남아야 한다.
	citer := write(t, l, "beta-결정-인용-2026-08-28", "beta", "인용한다",
		"앞 결정 [["+old+"]] 과 [["+old+"|별칭]] 과 [["+old+"#절]] 을 본다")

	p := build(t, c, l, "twincrew", "")
	if len(p.Relinks) != 1 {
		t.Fatalf("링크를 고칠 문서가 %d건 — 1건이어야 한다", len(p.Relinks))
	}
	if p.Relinks[0].Count != 3 {
		t.Errorf("고칠 링크가 %d개 — 3개(맨링크·별칭·앵커)여야 한다", p.Relinks[0].Count)
	}
	if err := Apply(p); err != nil {
		t.Fatal(err)
	}

	// 옛 파일은 사라지고 새 파일이 생겼다.
	if _, err := os.Stat(p.Moves[0].From); !os.IsNotExist(err) {
		t.Errorf("옛 파일이 남아 있다: %s", p.Moves[0].From)
	}
	b, err := os.ReadFile(p.Moves[0].To)
	if err != nil {
		t.Fatalf("새 파일이 없다: %v", err)
	}
	// **frontmatter 의 domain 도 바뀌어야 한다.** 폴더가 아니라 domain 배열이
	// 회수 경로라, 파일만 옮기면 회수는 그 노트를 계속 common 것으로 읽는다.
	if !strings.Contains(string(b), "domain: [twincrew]") {
		t.Errorf("domain 이 안 바뀌었다:\n%s", b)
	}

	citerBody, err := os.ReadFile(citer)
	if err != nil {
		t.Fatal(err)
	}
	got := string(citerBody)
	if strings.Contains(got, old) {
		t.Errorf("옛 stem 을 가리키는 링크가 남았다:\n%s", got)
	}
	for _, want := range []string{
		"[[twincrew-결정-라우터-2026-08-28]]",
		"[[twincrew-결정-라우터-2026-08-28|별칭]]",
		"[[twincrew-결정-라우터-2026-08-28#절]]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("링크 %q 가 없다:\n%s", want, got)
		}
	}

	// 회수가 새 도메인으로 읽는지 — 여기까지 와야 이 기능이 값을 낸다.
	c.Domain = append(c.Domain, config.Domain{Prefix: "twincrew", Folder: "twincrew"})
	notes, _, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range notes {
		if n.Stem == p.Moves[0].NewStem {
			found = true
			if len(n.Meta.Domain) != 1 || n.Meta.Domain[0] != "twincrew" {
				t.Errorf("옮긴 노트의 domain 이 %v 다", n.Meta.Domain)
			}
		}
	}
	if !found {
		t.Error("옮긴 노트가 목록에 없다 — 폴더나 이름이 규약과 어긋났다")
	}
}

func TestBuildRefusesExistingDomain(t *testing.T) {
	c, l := setup(t)
	write(t, l, "common-결정-alpha-무언가-2026-08-28", "common", "alpha 어쩌고", "본문")
	notes, _, _ := l.List()
	if _, err := Build(c, l, notes, []string{"alpha"}, ""); err == nil {
		t.Error("이미 있는 도메인으로 떼어내려는데 에러가 안 났다")
	}
}

func TestBuildRefusesFallbackItself(t *testing.T) {
	c, l := setup(t)
	notes, _, _ := l.List()
	if _, err := Build(c, l, notes, []string{"common"}, ""); err == nil {
		t.Error("폴백 도메인 자신으로 떼어내려는데 에러가 안 났다")
	}
}

func TestApplyIsNoopWithoutMoves(t *testing.T) {
	if err := Apply(&Plan{}); err != nil {
		t.Errorf("옮길 것이 없는데 에러가 났다: %v", err)
	}
}
