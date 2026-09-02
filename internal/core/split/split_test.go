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
	p, err := Build(c, l, l, notes, []string{token}, as)
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
	if _, err := Build(c, l, l, notes, []string{"alpha"}, ""); err == nil {
		t.Error("이미 있는 도메인으로 떼어내려는데 에러가 안 났다")
	}
}

func TestBuildRefusesFallbackItself(t *testing.T) {
	c, l := setup(t)
	notes, _, _ := l.List()
	if _, err := Build(c, l, l, notes, []string{"common"}, ""); err == nil {
		t.Error("폴백 도메인 자신으로 떼어내려는데 에러가 안 났다")
	}
}

func TestApplyIsNoopWithoutMoves(t *testing.T) {
	if err := Apply(&Plan{}); err != nil {
		t.Errorf("옮길 것이 없는데 에러가 났다: %v", err)
	}
}

// ★ 볼트를 건너 옮긴다.
//
// 개인 볼트에 섞여 있는 회사 결정을 회사 볼트로 보내는 것이 이 기능의 첫 용도다
// (코드주권 결정 2026-08-31). 파일이 다른 볼트에 생기고, **원래 볼트에 남은 노트가
// 걸어 둔 위키링크도 고쳐져야 한다** — 한쪽만 고치면 링크가 조용히 끊긴다.
func TestBuildAcrossVaults(t *testing.T) {
	c, src := setup(t)
	workDir := t.TempDir()
	c.Vaults = append(c.Vaults, config.Vault{Name: "work", Path: workDir})
	dst := store.NewLayoutFor(c, config.Vault{Name: "work", Path: workDir})

	old := "common-결정-twincrew-라우터-2026-08-28"
	write(t, src, old, "common", "twincrew 라우터", "본문")
	// 개인 볼트에 남는 노트가 그것을 가리킨다.
	citer := write(t, src, "beta-결정-인용-2026-08-28", "beta", "인용한다",
		"앞 결정 [["+old+"]] 을 본다")

	notes, _, err := src.List()
	if err != nil {
		t.Fatal(err)
	}
	p, err := Build(c, src, dst, notes, []string{"twincrew"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Moves) != 1 {
		t.Fatalf("옮길 노트가 %d건", len(p.Moves))
	}
	if !strings.HasPrefix(p.Moves[0].To, workDir) {
		t.Errorf("도착 경로가 회사 볼트 밖이다: %s", p.Moves[0].To)
	}
	if len(p.Relinks) != 1 {
		t.Fatalf("링크를 고칠 문서가 %d건 — 원래 볼트의 인용 1건이어야 한다", len(p.Relinks))
	}
	if err := Apply(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Moves[0].To); err != nil {
		t.Errorf("회사 볼트에 파일이 없다: %v", err)
	}
	if _, err := os.Stat(p.Moves[0].From); !os.IsNotExist(err) {
		t.Errorf("개인 볼트에 옛 파일이 남았다")
	}
	b, err := os.ReadFile(citer)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), old) {
		t.Errorf("볼트를 건넜는데 원래 볼트의 링크가 안 고쳐졌다:\n%s", b)
	}
}

// 없는 볼트 이름은 조용히 기본 볼트로 떨어지지 않는다.
func TestVaultNamedRejectsUnknown(t *testing.T) {
	c, _ := setup(t)
	if _, err := c.VaultNamed("없는볼트"); err == nil {
		t.Error("모르는 볼트 이름인데 에러가 안 났다 — 회사 결정이 개인 볼트에 쌓인다")
	}
}

// ★★ **`related` 가 대괄호 없이 쓰여 있어도 고쳐야 한다.**
//
// 2026-09-02 실볼트에서 물렸다. `젠틀파이` 9건을 옮긴 뒤 doctor 가 끊어진 링크 4개를
// 냈는데, 원인은 그 노트들의 frontmatter 가 이렇게 쓰여 있던 것이다:
//
//	related: ["common-결정-EWS는-…", "common-결정-코스봇-인프라는-…"]
//
// `[[ ]]` 가 없다. `wikiLink` 정규식은 대괄호만 잡으므로 이 값들은 손대지 않았고,
// 대상이 개명되면서 그대로 끊겼다. **옮기는 행위가 링크를 깨뜨린 것이다.**
//
// apply.go 의 주석은 "frontmatter 의 related 도 `\"[[stem]]\"` 문자열이라 모양이
// 같다" 고 적었는데, 그 전제가 볼트 전체에 성립하지 않았다 — `store.parseLink` 는
// 맨 값도 링크로 읽어 주기 때문에(모양이 나빠도 버리지 않는다) 그렇게 쓰인 노트가
// 실제로 생긴다.
func TestApplyRewritesBareRelatedValues(t *testing.T) {
	c, l := setup(t)
	old := "common-결정-twincrew-라우터-2026-08-28"
	write(t, l, old, "common", "twincrew 라우터", "본문")

	// 대괄호 **없이** 가리키는 노트. 옮기는 대상 밖(beta)에 둔다.
	p := filepath.Join(l.Vault(), "beta", "decisions", "beta-결정-맨링크-2026-08-28.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	fm := "---\ntype: decision\ndate: 2026-08-28\ndomain: [beta]\n" +
		"summary: \"맨 값으로 가리킨다\"\nstatus: active\n" +
		"related: [\"" + old + "\"]\ntags: [decision]\n---\n\n본문\n"
	if err := os.WriteFile(p, []byte(fm), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := build(t, c, l, "twincrew", "")
	if err := Apply(plan); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), old) {
		t.Errorf("대괄호 없는 related 가 옛 stem 을 가리킨 채 남았다 — 옮기는 행위가 링크를 깨뜨린다:\n%s", got)
	}
	if !strings.Contains(string(got), "twincrew-결정-라우터-2026-08-28") {
		t.Errorf("새 stem 으로 안 바뀌었다:\n%s", got)
	}
}
