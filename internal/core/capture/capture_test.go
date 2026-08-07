package capture

import (
	"os"
	"strings"
	"testing"
)

func TestDoCreatesNote(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "새 결정", Summary: "새 결정을 내렸다",
		Date: "2026-08-07", Tags: []string{"decision", "alpha"},
		Body: []byte("## 결정\n\n내용.\n"),
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if !strings.HasSuffix(res.Path, "alpha-결정-새-결정-2026-08-07.md") {
		t.Errorf("경로가 규약과 다르다: %s", res.Path)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `summary: "새 결정을 내렸다"`) {
		t.Errorf("frontmatter 가 정본 형식이 아니다:\n%s", data)
	}
}

func TestDoRejectsSchemaViolation(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	// summary 가 비면 거부
	if _, err := Do(l, c, Request{Domain: "alpha", Slug: "x", Date: "2026-08-07"}); err == nil {
		t.Fatal("summary 없는 요청을 통과시켰다")
	}
	// 알 수 없는 도메인은 거부
	if _, err := Do(l, c, Request{Domain: "없음", Slug: "x", Summary: "s", Date: "2026-08-07"}); err == nil {
		t.Fatal("알 수 없는 도메인을 통과시켰다")
	}
}

func TestDoRejectsDuplicate(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	r := Request{Domain: "alpha", Slug: "중복", Summary: "s", Date: "2026-08-07",
		Body: []byte("## 결정\n")}
	if _, err := Do(l, c, r); err != nil {
		t.Fatal(err)
	}
	if _, err := Do(l, c, r); err == nil {
		t.Fatal("같은 경로에 두 번 썼다")
	}
}

func TestDoReturnsRelated(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "저장 엔진 재검토", Summary: "저장 엔진을 다시 본다",
		Date: "2026-08-07", Body: []byte("## 결정\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// 편승: 기록하면 관련 과거 결정이 딸려 나온다
	if len(res.Related) == 0 {
		t.Error("관련 결정이 반환되지 않았다 — 편승이 동작하지 않는다")
	}
}

func TestDoUpdatesIndex(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	if _, err := Do(l, c, Request{
		Domain: "alpha", Slug: "색인 확인", Summary: "색인이 갱신되는지 본다",
		Date: "2026-08-07", Body: []byte("## 결정\n"),
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(l.IndexPath())
	if err != nil {
		t.Fatalf("색인이 없다: %v", err)
	}
	if !strings.Contains(string(data), "색인이 갱신되는지 본다") {
		t.Error("새 결정이 색인에 없다")
	}
}
