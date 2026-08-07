package capture

import (
	"os"
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/index"
	"github.com/xian0310567/casebook/internal/core/search"
	"github.com/xian0310567/casebook/internal/core/store"
)

// englishLayout 은 영어 파일명 템플릿을 쓰는 빈 볼트를 만든다.
// 결정 표식이 설정에서 유도되므로 "-decision-" 이 된다.
func englishLayout(t *testing.T) (*store.Layout, *config.Config) {
	t.Helper()
	c := &config.Config{
		Vault: t.TempDir(),
		Naming: config.Naming{
			DecisionFile: "{domain}-decision-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-worklog.md",
			Index:        "_meta/00-decision-index.md",
		},
		Domain: []config.Domain{{Prefix: "alpha", Folder: "alpha"}},
	}
	return store.NewLayout(c), c
}

// TestEnglishTemplateRoundTrip 은 영어 템플릿 설정에서 capture → index →
// recall → review 왕복이 되는지 본다.
//
// 표식이 세 곳(store 의 상수·파일 필터, schema 의 별도 리터럴)에 하드코딩돼
// 있던 시절에는 이 왕복의 첫 단계에서 죽었다: DecisionPath 는 영어 파일명을
// 만드는데 schema.Validate 는 한국어 표식을 요구했다. 스펙 §5 가 "국제화가
// 여기서 열린다" 고 한 자리가 실제로는 막혀 있었다는 뜻이다.
func TestEnglishTemplateRoundTrip(t *testing.T) {
	l, c := englishLayout(t)
	if got := l.DecisionMarker(); got != "-decision-" {
		t.Fatalf("DecisionMarker() = %q, want %q — 표식이 설정에서 유도되지 않는다", got, "-decision-")
	}

	// 1) capture
	first, err := Do(l, c, Request{
		Domain: "alpha", Slug: "storage engine", Summary: "pick an embedded database",
		Date: "2026-08-01", Body: []byte("## Decision\n\nUse SQLite.\n"),
	})
	if err != nil {
		t.Fatalf("capture 실패: %v", err)
	}
	if !strings.HasSuffix(first.Path, "alpha-decision-storage-engine-2026-08-01.md") {
		t.Errorf("파일명이 영어 템플릿을 안 따른다: %s", first.Path)
	}

	// 2) index — 새 노트가 표식 필터를 통과해 색인에 들어가야 한다
	if _, err := index.Write(l); err != nil {
		t.Fatalf("index 실패: %v", err)
	}
	idx, err := os.ReadFile(l.IndexPath())
	if err != nil {
		t.Fatalf("색인이 없다: %v", err)
	}
	if !strings.Contains(string(idx), "pick an embedded database") {
		t.Errorf("색인에 새 결정이 없다 — List() 의 표식 필터가 영어 파일명을 걸렀다:\n%s", idx)
	}

	// 3) recall
	hits, skipped, err := search.Recall(l, c, "storage engine database", search.Options{
		CrossProject: true, Limit: 3, MinScore: 1,
	})
	if err != nil {
		t.Fatalf("recall 실패: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("건너뛴 노트가 있다 — 영어 템플릿 볼트는 전부 정본형이다: %+v", skipped)
	}
	if len(hits) == 0 {
		t.Fatal("recall 이 방금 기록한 결정을 못 찾았다")
	}

	// 4) capture --supersedes → 양방향 갱신
	firstNote, err := l.Read(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Do(l, c, Request{
		Domain: "alpha", Slug: "storage engine revisited", Summary: "switch to an embedded KV store",
		Date: "2026-08-02", Supersedes: firstNote.Stem, Body: []byte("## Decision\n\nUse BoltDB.\n"),
	})
	if err != nil {
		t.Fatalf("capture --supersedes 실패: %v", err)
	}
	old, err := l.Read(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	if old.Meta.Status != "superseded" {
		t.Errorf("옛 노트 status = %q, want superseded", old.Meta.Status)
	}

	// 5) review — outcome 갱신과 회고 추가
	newNote, err := l.Read(second.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Review(l, ReviewRequest{
		Stem: newNote.Stem, Outcome: "good", Retrospective: "Worked out fine.",
	}); err != nil {
		t.Fatalf("review 실패: %v", err)
	}
	after, err := l.Read(second.Path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Meta.Outcome != "good" {
		t.Errorf("outcome = %q, want good", after.Meta.Outcome)
	}
	if !strings.Contains(string(after.Body), "Worked out fine.") {
		t.Errorf("회고가 본문에 없다:\n%s", after.Body)
	}
}

// TestKoreanTemplateStillWorksWithDerivedMarker 는 표식을 설정에서 유도한 뒤에도
// 기본 한국어 템플릿이 그대로 동작하는지 확인한다 (회귀 방지).
func TestKoreanTemplateStillWorksWithDerivedMarker(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	if got := l.DecisionMarker(); got != "-결정-" {
		t.Fatalf("DecisionMarker() = %q, want %q", got, "-결정-")
	}
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "표식 유도 확인", Summary: "표식을 설정에서 유도해도 동작한다",
		Date: "2026-08-07", Body: []byte("## 결정\n"),
	})
	if err != nil {
		t.Fatalf("capture 실패: %v", err)
	}
	if !strings.HasSuffix(res.Path, "alpha-결정-표식-유도-확인-2026-08-07.md") {
		t.Errorf("파일명이 규약과 다르다: %s", res.Path)
	}
}
