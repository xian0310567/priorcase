package capture

import (
	"os"
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/core/store"
)

func TestReviewUpdatesOutcome(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	stem := "alpha-결정-저장엔진-2026-08-01"
	if _, err := Review(l, ReviewRequest{
		Stem: stem, Outcome: "good", Retrospective: "잘 됐다.",
	}); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	p, err := l.ResolveStem(stem)
	if err != nil {
		t.Fatal(err)
	}
	n, err := l.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if n.Meta.Outcome != "good" {
		t.Errorf("outcome = %q, want good", n.Meta.Outcome)
	}
	if !strings.Contains(string(n.Body), "잘 됐다.") {
		t.Errorf("회고가 본문에 안 들어갔다:\n%s", n.Body)
	}
}

func TestReviewRejectsMissingTarget(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	_, err := Review(l, ReviewRequest{Stem: "alpha-결정-없는것-2026-01-01", Outcome: "good"})
	if err == nil {
		t.Fatal("없는 대상을 통과시켰다")
	}
	if !strings.Contains(err.Error(), "대상 없음") {
		t.Errorf("에러 메시지가 진단에 도움이 안 된다: %v", err)
	}
}

func TestReviewRejectsTraversal(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	if _, err := Review(l, ReviewRequest{Stem: "../CLAUDE", Outcome: "good"}); err == nil {
		t.Fatal("경로 순회를 통과시켰다")
	}
}

func TestReviewSupersedesBothSides(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	newStem := "alpha-결정-스키마-2026-08-02"
	oldStem := "alpha-결정-저장엔진-2026-08-01"
	if _, err := Review(l, ReviewRequest{Stem: newStem, Supersedes: oldStem}); err != nil {
		t.Fatal(err)
	}
	read := func(stem string) store.Note {
		p, err := l.ResolveStem(stem)
		if err != nil {
			t.Fatal(err)
		}
		n, err := l.Read(p)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := read(newStem).Meta.Supersedes; got != "[["+oldStem+"]]" {
		t.Errorf("새 노트 supersedes = %q", got)
	}
	old := read(oldStem)
	if old.Meta.Status != "superseded" {
		t.Errorf("옛 노트 status = %q, want superseded", old.Meta.Status)
	}
	found := false
	for _, r := range old.Meta.Related {
		if r == "[["+newStem+"]]" {
			found = true
		}
	}
	if !found {
		t.Errorf("옛 노트 related 에 후속 문서가 없다: %v", old.Meta.Related)
	}
}

func TestReviewRejectsBadValues(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	stem := "alpha-결정-저장엔진-2026-08-01"
	if _, err := Review(l, ReviewRequest{Stem: stem, Outcome: "maybe"}); err == nil {
		t.Fatal("허용값 밖 outcome 을 통과시켰다")
	}
	if _, err := Review(l, ReviewRequest{Stem: stem, Status: "unknown"}); err == nil {
		t.Fatal("허용값 밖 status 를 통과시켰다")
	}
	// 파일이 안 망가졌는지
	p, _ := l.ResolveStem(stem)
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "outcome: pending") {
		t.Errorf("거부됐는데 파일이 변했다:\n%s", data)
	}
}
