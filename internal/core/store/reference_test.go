package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
)

// ★★★ **참고 문서는 `summary` 가 있을 때만 회수 대상이다.**
//
// 실측(2026-08-13): 볼트 245건 중 결정이 147건, 나머지 98건은 영영 안 걸렸다.
// 실제 질의 51개로 재 보니 그것들을 넣었을 때 **1위가 바뀐 것이 9건이고 결정이
// 밀려난 것은 1건**뿐이었다 — 비어 있던 자리를 채운다.
//
// 다만 통째로 넣지는 않는다. `summary` 를 다는 것이 **참여 신호**다. 안 그러면
// 초안·메모·생성물까지 다 들어와 소음이 되고, 무엇보다 회수기가 head 에 아무것도
// 없는 문서를 버리므로 실제로는 아무 일도 안 일어난다.
func TestListReferencesNeedsSummary(t *testing.T) {
	l, vault := refFixture(t)

	write(t, filepath.Join(vault, "proj", "01-설계.md"),
		"---\ntype: spec\nsummary: \"캐릭터 컨셉 설계 — 페르소나 3안\"\n---\n\n# 설계\n본문\n")
	write(t, filepath.Join(vault, "proj", "02-메모.md"),
		"---\ntags: [x]\n---\n\n# 메모\nsummary 가 없다\n")
	write(t, filepath.Join(vault, "proj", "03-빈요약.md"),
		"---\ntype: log\nsummary: \"  \"\n---\n\n# 빈 요약\n")

	got := stems(refList(t, l))
	if want := []string{"01-설계"}; !equal(got, want) {
		t.Errorf("참고 %v — %v 여야 한다", got, want)
	}
}

// ★★★ **결정 노트는 참고 목록에 안 들어간다.** 두 번 세면 회수 상위가 같은
// 노트로 채워지고, 무엇보다 참고는 쓰기·검증·색인 대상이 아니다.
func TestListReferencesExcludesDecisions(t *testing.T) {
	l, vault := refFixture(t)
	write(t, filepath.Join(vault, "proj", "decisions", "proj-결정-저장엔진-2026-08-01.md"),
		"---\ntype: decision\ndate: 2026-08-01\ndomain: [proj]\nsummary: \"저장 엔진\"\nstatus: active\noutcome: pending\n---\n\n## 결정\n")
	write(t, filepath.Join(vault, "proj", "01-설계.md"),
		"---\ntype: spec\nsummary: \"설계\"\n---\n\n# 설계\n")

	got := stems(refList(t, l))
	if want := []string{"01-설계"}; !equal(got, want) {
		t.Errorf("참고 %v — 결정이 섞였다", got)
	}
}

// ★★ **결정 폴더 밖에 놓인 결정 노트도 참고로 세면 안 된다.**
//
// 파일을 옮기다 잘못 두는 일이 있고, 그때 같은 노트가 결정으로도 참고로도
// 들어오면 회수 상위 3이 같은 문서 둘로 채워진다. 폴더 건너뛰기만으로는 못
// 막는다 — 판정은 frontmatter 의 type 이 한다.
func TestListReferencesExcludesMisplacedDecision(t *testing.T) {
	l, vault := refFixture(t)
	// decisions/ 밖에 놓인 결정 노트
	write(t, filepath.Join(vault, "proj", "proj-결정-잘못둠-2026-08-01.md"),
		"---\ntype: decision\ndate: 2026-08-01\ndomain: [proj]\nsummary: \"잘못 둔 결정\"\nstatus: active\noutcome: pending\n---\n\n## 결정\n")
	write(t, filepath.Join(vault, "proj", "01-설계.md"),
		"---\ntype: spec\nsummary: \"설계\"\n---\n\n# 설계\n")

	got := stems(refList(t, l))
	if want := []string{"01-설계"}; !equal(got, want) {
		t.Errorf("참고 %v — 폴더 밖 결정 노트가 섞였다", got)
	}
}

// ★★★ **설정에 없는 폴더는 안 본다.**
//
// 볼트에는 도메인 폴더 밖의 것이 있다 — 지침 문서(CLAUDE.md) · 생성 색인
// (_meta/00-결정-색인.md) · 이 시스템이 손대면 안 되는 구역(NOI).
//
// 특히 **생성 색인은 전 노트의 요약을 모아 둔 파일**이라, 회수에 들어오면
// 어떤 질의에도 걸려 언제나 1위가 된다. 도메인 폴더만 보면 그것들이 자동으로
// 빠진다 — 제외 목록을 따로 관리하면 새 자리가 생길 때마다 빠뜨린다.
func TestListReferencesOnlyWalksDomainFolders(t *testing.T) {
	l, vault := refFixture(t)
	write(t, filepath.Join(vault, "proj", "01-설계.md"), "---\nsummary: \"설계\"\n---\n\n# 설계\n")
	write(t, filepath.Join(vault, "CLAUDE.md"), "---\nsummary: \"볼트 지침\"\n---\n\n# 지침\n")
	write(t, filepath.Join(vault, "_meta", "00-결정-색인.md"), "---\nsummary: \"색인\"\n---\n\n# 색인\n")
	write(t, filepath.Join(vault, "NOI", "규칙.md"), "---\nsummary: \"NOI\"\n---\n\n# 규칙\n")

	got := stems(refList(t, l))
	if want := []string{"01-설계"}; !equal(got, want) {
		t.Errorf("참고 %v — 도메인 폴더 밖을 봤다", got)
	}
}

// ★★ **도메인은 폴더에서 온다.** 참고 문서에 domain frontmatter 를 요구하면
// 98건을 손으로 고쳐야 하고, 폴더가 이미 그 사실을 말해 준다.
func TestListReferencesDerivesDomainFromFolder(t *testing.T) {
	l, vault := refFixture(t)
	write(t, filepath.Join(vault, "proj", "01-설계.md"), "---\nsummary: \"설계\"\n---\n\n# 설계\n")
	write(t, filepath.Join(vault, "other", "02-기획.md"), "---\nsummary: \"기획\"\n---\n\n# 기획\n")

	byStem := map[string][]string{}
	for _, n := range refList(t, l) {
		byStem[n.Stem] = n.Meta.Domain
	}
	if d := byStem["01-설계"]; !equal(d, []string{"proj"}) {
		t.Errorf("01-설계 의 도메인 %v — [proj] 여야 한다", d)
	}
	if d := byStem["02-기획"]; !equal(d, []string{"other"}) {
		t.Errorf("02-기획 의 도메인 %v — [other] 여야 한다", d)
	}
}

// ★★★ **frontmatter 가 없는 것은 고장이 아니다.**
//
// 볼트에는 평범한 마크다운이 있다 (실측 98건 중 2건). 그건 참여하지 않는
// 문서일 뿐인데, 읽기 실패로 세면 **매 프롬프트마다 "결정 노트를 읽지 못했다"
// 경고가 뜬다** — 실제로 그랬다. 늘 뜨는 경고는 무시하는 법을 가르치고,
// 그러면 진짜 깨진 노트도 같이 묻힌다.
func TestListReferencesIgnoresPlainMarkdown(t *testing.T) {
	l, vault := refFixture(t)
	write(t, filepath.Join(vault, "proj", "01-설계.md"), "---\nsummary: \"설계\"\n---\n\n# 설계\n")
	write(t, filepath.Join(vault, "proj", "02-그냥글.md"), "# 그냥 글\n\nfrontmatter 가 없다\n")

	notes, skipped, err := l.ListReferences()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Errorf("참고 %d건 — 1건이어야 한다", len(notes))
	}
	if len(skipped) != 0 {
		t.Errorf("건너뜀 %d건 — frontmatter 없는 것은 고장이 아니다: %v", len(skipped), skipped)
	}
}

// ★★ **읽다 실패한 것을 조용히 버리지 않는다.** 참고가 빠지면 "그 문서에 그런
// 말이 없다" 와 구별되지 않는다.
func TestListReferencesReportsSkipped(t *testing.T) {
	l, vault := refFixture(t)
	write(t, filepath.Join(vault, "proj", "01-설계.md"), "---\nsummary: \"설계\"\n---\n\n# 설계\n")
	write(t, filepath.Join(vault, "proj", "02-깨짐.md"), "---\nsummary: [닫히지 않은\n---\n\n# 깨짐\n")

	notes, skipped, err := l.ListReferences()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Errorf("참고 %d건 — 1건이어야 한다", len(notes))
	}
	if len(skipped) != 1 {
		t.Errorf("건너뛴 것 %d건 — 1건이어야 한다 (조용히 버리면 안 된다)", len(skipped))
	}
}

// ── 픽스처 ────────────────────────────────────────────────────────────────

func refFixture(t *testing.T) (*Layout, string) {
	t.Helper()
	vault := t.TempDir()
	c := &config.Config{
		Vaults:        []config.Vault{{Name: config.DefaultVaultName, Path: vault}},
		DefaultDomain: "proj",
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-작업-로그.md",
			Index:        "_meta/00-결정-색인.md",
		},
		Domain: []config.Domain{
			{Prefix: "proj", Folder: "proj"},
			{Prefix: "other", Folder: "other"},
		},
	}
	return NewLayout(c), vault
}

func refList(t *testing.T, l *Layout) []Note {
	t.Helper()
	notes, _, err := l.ListReferences()
	if err != nil {
		t.Fatal(err)
	}
	return notes
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stems(ns []Note) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.Stem)
	}
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != b[i] {
			return false
		}
	}
	return true
}
