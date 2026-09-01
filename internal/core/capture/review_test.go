package capture

import (
	"os"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/store"
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
	if _, err := Review(l, ReviewRequest{Stem: newStem, Supersedes: []string{oldStem}}); err != nil {
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
	if got := read(newStem).Meta.Supersedes; len(got) != 1 || got[0] != "[["+oldStem+"]]" {
		t.Errorf("새 노트 supersedes = %v", got)
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

// ★★ **summary 를 고칠 길이 있어야 한다.**
//
// 회수에 주입되는 것은 summary 한 줄뿐이다 (search.scoreAll 의 head 는 파일명·
// summary·tags 이고, 주입 블록에 나가는 것은 summary 다). 여기에 틀린 사실이
// 박히면 그 오류가 앞으로 계속 대화에 실려 나간다 — 본문은 아무도 안 열어 볼 수
// 있어도 이 줄은 반드시 읽힌다.
//
// 실제로 그 상태를 만났다. 2026-08-12 에 시뮬레이션 숫자가 틀린 채로 summary 에
// 박혔는데, 회고 절에 정정을 적어도 회수는 여전히 틀린 한 줄을 주입했다.
// review 에 그걸 고칠 플래그가 없었다.
func TestReviewCanCorrectSummary(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	stem := "alpha-결정-저장엔진-2026-08-01"
	read := func() store.Note {
		t.Helper()
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

	before := read()
	if _, err := Review(l, ReviewRequest{Stem: stem, Summary: "고쳐진 한 줄"}); err != nil {
		t.Fatal(err)
	}
	after := read()
	if after.Meta.Summary != "고쳐진 한 줄" {
		t.Errorf("Summary = %q, 고쳐졌어야 한다", after.Meta.Summary)
	}
	// **다른 필드는 건드리지 않는다.** summary 만 고치러 온 사람이 outcome 이
	// 초기화되는 것을 알아채지 못한다.
	if after.Meta.Outcome != before.Meta.Outcome || after.Meta.Status != before.Meta.Status {
		t.Errorf("summary 만 고쳤는데 다른 필드가 바뀌었다: outcome %q→%q status %q→%q",
			before.Meta.Outcome, after.Meta.Outcome, before.Meta.Status, after.Meta.Status)
	}
	if string(after.Body) != string(before.Body) {
		t.Error("summary 만 고쳤는데 본문이 바뀌었다")
	}

	// 빈 문자열은 "변경 없음" 이다 — 안 그러면 outcome 만 고치려는 호출이
	// summary 를 지운다.
	if _, err := Review(l, ReviewRequest{Stem: stem, Outcome: "good"}); err != nil {
		t.Fatal(err)
	}
	if again := read(); again.Meta.Summary != "고쳐진 한 줄" {
		t.Errorf("빈 Summary 가 기존 값을 지웠다: %q", again.Meta.Summary)
	}
}

// ★ 철회에는 이유가 있어야 한다.
//
// 철회된 노트는 파일로 남지만 회수에서 통째로 빠진다. 왜 뺐는지가 안 적히면
// 나중에 옵시디언에서 그 노트를 연 사람은 status 한 줄만 보고 아무것도 알 수
// 없다 — "조용히 틀리느니 시끄럽게 멈춘다" 가 여기서는 "빼는 이유를 남겨라" 다.
func TestReviewRefusesRetractionWithoutReason(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	_, err := Review(l, ReviewRequest{
		Stem: "alpha-결정-저장엔진-2026-08-01", Status: "retracted",
	})
	if err == nil {
		t.Fatal("이유 없는 철회를 통과시켰다")
	}
	if !strings.Contains(err.Error(), "회고") && !strings.Contains(err.Error(), "이유") {
		t.Errorf("무엇이 필요한지 안 알려 준다: %v", err)
	}
}

func TestReviewAcceptsRetractionWithReason(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	if _, err := Review(l, ReviewRequest{
		Stem: "alpha-결정-저장엔진-2026-08-01", Status: "retracted",
		Retrospective: "판별기가 진행 상황 보고를 결정으로 잘못 만들었다",
	}); err != nil {
		t.Fatalf("이유를 줬는데 거부했다: %v", err)
	}
	p, _ := l.ResolveStem("alpha-결정-저장엔진-2026-08-01")
	n, err := l.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if n.Meta.Status != "retracted" {
		t.Errorf("status = %q, want retracted", n.Meta.Status)
	}
}

// ★ **태그는 통째로 간다.** Related 와 반대다.
//
// doctor 의 회수 어휘 검사가 잡는 것은 **헛도는 태그**(제목·요약에 이미 있어 회수에
// 아무것도 안 더하는 것)이고, 그걸 고치는 일은 대개 **빼는 것**이다. 덧붙이기만
// 되면 그 검사를 영영 만족시킬 수 없다. 그 검사는 예전에 "파일의 tags 를 직접
// 고쳐라" 고 안내했는데, 그 손 편집이 frontmatter 를 깨는 것이 이 도구가 막으려는
// 일이었다 (2026-09-01).
func TestReviewReplacesTags(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	const stem = "alpha-결정-저장엔진-2026-08-01"

	if _, err := Review(l, ReviewRequest{Stem: stem, Tags: []string{"영속성", "새태그"}}); err != nil {
		t.Fatal(err)
	}
	n := noteByStem(t, l, stem)
	// **decision 표식은 남는다** — capture 가 모든 노트에 붙이고, 지우면 그 노트가
	// 결정 목록에서 통째로 빠진다.
	if !hasTag(n.Meta.Tags, "decision") {
		t.Errorf("decision 표식이 사라졌다: %v", n.Meta.Tags)
	}
	for _, want := range []string{"영속성", "새태그"} {
		if !hasTag(n.Meta.Tags, want) {
			t.Errorf("태그 %q 가 없다: %v", want, n.Meta.Tags)
		}
	}
	// 갈아 끼우기이므로 예전 태그는 사라져야 한다.
	if hasTag(n.Meta.Tags, "저장엔진") {
		t.Errorf("덧붙이기가 됐다 — 옛 태그가 남았다: %v", n.Meta.Tags)
	}
}

func TestReviewNilTagsLeavesThemAlone(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	const stem = "alpha-결정-저장엔진-2026-08-01"
	before := noteByStem(t, l, stem).Meta.Tags
	if _, err := Review(l, ReviewRequest{Stem: stem, Outcome: "good"}); err != nil {
		t.Fatal(err)
	}
	after := noteByStem(t, l, stem).Meta.Tags
	if len(before) != len(after) {
		t.Errorf("태그를 안 줬는데 바뀌었다: %v → %v", before, after)
	}
}

// 빈 슬라이스는 "전부 지운다" 다 — nil(변경 없음)과 달라야 한다.
func TestReviewEmptyTagsClearsButKeepsMarker(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	const stem = "alpha-결정-저장엔진-2026-08-01"
	if _, err := Review(l, ReviewRequest{Stem: stem, Tags: []string{}}); err != nil {
		t.Fatal(err)
	}
	got := noteByStem(t, l, stem).Meta.Tags
	if len(got) != 1 || !hasTag(got, "decision") {
		t.Errorf("태그가 %v — decision 하나만 남아야 한다", got)
	}
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if strings.EqualFold(strings.TrimSpace(t), want) {
			return true
		}
	}
	return false
}

// noteByStem 은 볼트에서 그 노트를 다시 읽는다 — 디스크에 실제로 쓰였는지 본다.
func noteByStem(t *testing.T, l *store.Layout, stem string) store.Note {
	t.Helper()
	notes, _, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range notes {
		if n.Stem == stem {
			return n
		}
	}
	t.Fatalf("그런 노트가 없다: %s", stem)
	return store.Note{}
}
