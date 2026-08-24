package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

func writeDoc(t *testing.T, vault, rel, body string) {
	t.Helper()
	p := filepath.Join(vault, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// summary 가 있는 문서가 선언 안 된 폴더에 있으면 **경고**다 — 선언 한 줄로 고쳐진다.
// 2026-08-24 에 OCC 16건·영상제작 9건이 정확히 이 상태로 조용히 빠져 있었다.
func TestUnscannedFolderWithSummaryIsActionable(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	writeDoc(t, l.Vault(), "gamma/00-개요.md", "---\ntitle: 감마\nsummary: \"감마 프로젝트 개요\"\n---\n\n# 감마\n")
	writeDoc(t, l.Vault(), "gamma/01-노트.md", "---\ntitle: 노트\n---\n\n본문\n")

	got := find(t, Vault(c, l), "훑지 않는 폴더")
	if got.Level != Warn {
		t.Fatalf("level = %v, want Warn (%s)", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "gamma 1/2건") {
		t.Errorf("어느 폴더에 몇 건인지 말하지 않았다: %s", got.Detail)
	}
	if got.Fix == "" {
		t.Error("고치는 법을 주지 않았다")
	}
}

// summary 가 하나도 없는 폴더는 **경고가 아니다** — 모양이 다른 시스템이고 선언해도
// 회수가 쓸 것이 없다. 고칠 수 없는 것을 경고로 내면 경고를 무시하는 법을 가르친다.
func TestUnscannedFolderWithoutSummaryIsJustAFact(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	writeDoc(t, l.Vault(), "NOI/tasks/T-1.md", "---\nid: T-1\ntype: task\ntitle: 어떤 일\n---\n\n본문\n")
	writeDoc(t, l.Vault(), "NOI/tasks/T-2.md", "---\nid: T-2\ntype: task\n---\n\n본문\n")

	got := find(t, Vault(c, l), "훑지 않는 폴더")
	if got.Level != OK {
		t.Fatalf("level = %v, want OK (%s)", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "NOI 2건") {
		t.Errorf("범위 밖 폴더와 건수를 말하지 않았다: %s", got.Detail)
	}
}

// 고칠 수 있는 것이 있으면 그것을 말한다 — 범위 밖 사실 보고에 묻히면 안 된다.
func TestUnscannedActionableWinsOverOutOfShape(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	writeDoc(t, l.Vault(), "NOI/a.md", "---\nid: T-1\ntype: task\n---\n본문\n")
	writeDoc(t, l.Vault(), "gamma/00-개요.md", "---\nsummary: \"쓸 만한 문서\"\n---\n본문\n")

	got := find(t, Vault(c, l), "훑지 않는 폴더")
	if got.Level != Warn || !strings.Contains(got.Detail, "gamma") {
		t.Errorf("고칠 수 있는 쪽을 말해야 한다: level=%v %s", got.Level, got.Detail)
	}
}

// 기계 폴더는 세지 않는다 — 회수 대상이 아닌 것이 정상이다.
func TestUnscannedIgnoresMachineryDirs(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	writeDoc(t, l.Vault(), "_meta/00-규약.md", "---\nsummary: \"규약\"\n---\n본문\n")
	writeDoc(t, l.Vault(), "_templates/knowledge.md", "---\ntype: knowledge\n---\n본문\n")

	got := find(t, Vault(c, l), "훑지 않는 폴더")
	if got.Level != OK || !strings.Contains(got.Detail, "없다") {
		t.Errorf("기계 폴더를 셌다: level=%v %s", got.Level, got.Detail)
	}
}

func TestHasGist(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name, body string
		want       bool
	}{
		{"summary 있음", "---\nsummary: \"한 줄\"\n---\n본문", true},
		{"summary 비었음", "---\nsummary: \"\"\n---\n본문", false},
		{"summary 없음", "---\ntitle: 제목\n---\n본문", false},
		{"frontmatter 없음", "# 그냥 문서\n", false},
		{"본문에 summary 라는 글자", "---\ntitle: 제목\n---\nsummary: 이건 본문이다", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, "x.md")
			if err := os.WriteFile(p, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := hasGist(p); got != tc.want {
				t.Errorf("hasGist = %v, want %v", got, tc.want)
			}
		})
	}
}
