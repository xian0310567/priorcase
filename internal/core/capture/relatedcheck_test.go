package capture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// **대상이 없는 related 는 빼고 알린다.** 이것이 이 기능의 전부다 —
// 2026-08-27 실볼트에서 깨진 참조 19건이 전부 이 검사가 없어서 쌓였다.
func TestRelatedWithMissingTargetIsDroppedAndReported(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "링크검사", Summary: "대상 없는 related 를 뺀다",
		Date: "2026-08-27", Body: []byte("## 결정\n"),
		Related: []string{"[[alpha-결정-저장엔진-2026-08-01]]", "[[이런-노트는-없다-2026-01-01]]"},
	})
	if err != nil {
		t.Fatalf("기록이 실패했다 — related 하나 때문에 막으면 결정이 사라진다: %v", err)
	}
	if len(res.DroppedRelated) != 1 || res.DroppedRelated[0].Value != "이런-노트는-없다-2026-01-01" {
		t.Fatalf("빼 버린 값을 안 알린다: %+v", res.DroppedRelated)
	}
	n, err := l.Read(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Meta.Related) != 1 || !strings.Contains(n.Meta.Related[0], "저장엔진") {
		t.Errorf("frontmatter 에 남은 related = %v — 멀쩡한 것만 남아야 한다", n.Meta.Related)
	}
}

// **자동 교정하지 않는다.** 잘못 이은 링크는 끊긴 링크보다 나쁘다 —
// 끊긴 것은 회색으로 보이지만 잘못 이은 것은 아무도 의심하지 않는다.
func TestNearMissIsSuggestedNotApplied(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "제안만", Summary: "가까운 이름은 제안만 한다",
		Date: "2026-08-27", Body: []byte("## 결정\n"),
		Related: []string{"[[alpha-결정-저장엔지-2026-08-01]]"}, // 한 글자 오타
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.DroppedRelated) != 1 {
		t.Fatalf("오타를 안 잡았다: %+v", res.DroppedRelated)
	}
	if !strings.Contains(res.DroppedRelated[0].Suggest, "저장엔진") {
		t.Errorf("가까운 이름을 제안하지 않는다: %q", res.DroppedRelated[0].Suggest)
	}
	n, _ := l.Read(res.Path)
	for _, r := range n.Meta.Related {
		if strings.Contains(r, "저장엔진-") {
			t.Errorf("제안을 **적용**해 버렸다 — 자동 교정은 하지 않는다: %v", n.Meta.Related)
		}
	}
}

// **결정 아닌 문서도 정당한 대상이다.** 결정 폴더만 보면 프로젝트 개요·규약 링크가
// 전부 거짓 양성이 된다 (2026-08-24 에 그 실수로 75건을 오보했다).
func TestRelatedMayPointAtNonDecisionDoc(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	ref := filepath.Join(l.Vault(), "alpha", "00-alpha-프로젝트-개요.md")
	if err := os.WriteFile(ref, []byte("---\nsummary: \"개요\"\n---\n\n# 개요\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "참고링크", Summary: "참고 문서를 가리킨다",
		Date: "2026-08-27", Body: []byte("## 결정\n"),
		Related: []string{"[[00-alpha-프로젝트-개요]]"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.DroppedRelated) != 0 {
		t.Errorf("참고 문서를 없는 것으로 봤다: %+v", res.DroppedRelated)
	}
}

// 형식 위반(경로 순회)은 여전히 **에러**다 — 그건 오타가 아니라 공격 표면이다.
func TestPathTraversalStillFails(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	if _, err := Do(l, c, Request{
		Domain: "alpha", Slug: "순회", Summary: "경로 순회는 막는다",
		Date: "2026-08-27", Body: []byte("## 결정\n"),
		Related: []string{"[[../../CLAUDE]]"},
	}); err == nil {
		t.Error("경로 순회를 통과시켰다")
	}
}

// review 로 다시 걸 수 있어야 한다 — 알림만 하고 고칠 문이 없으면 반쪽이다.
func TestReviewCanAddRelatedLater(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "나중에건다", Summary: "나중에 related 를 건다",
		Date: "2026-08-27", Body: []byte("## 결정\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	n, _ := l.Read(res.Path)
	before := len(n.Meta.Related)
	rr, err := Review(l, ReviewRequest{Stem: n.Stem, Related: []string{"alpha-결정-저장엔진-2026-08-01"}})
	if err != nil {
		t.Fatalf("review 실패: %v", err)
	}
	if len(rr.DroppedRelated) != 0 {
		t.Errorf("멀쩡한 대상을 뺐다: %+v", rr.DroppedRelated)
	}
	after, _ := l.Read(res.Path)
	if len(after.Meta.Related) != before+1 {
		t.Errorf("related 가 안 붙었다: %v", after.Meta.Related)
	}
	if !strings.HasPrefix(after.Meta.Related[0], "[[") {
		t.Errorf("위키링크로 안 걸렸다: %v", after.Meta.Related)
	}
}

// review 의 related 는 **덧붙인다. 덮어쓰지 않는다.**
func TestReviewRelatedAppendsNotReplaces(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	res, _ := Do(l, c, Request{
		Domain: "alpha", Slug: "덧붙이기", Summary: "덧붙인다",
		Date: "2026-08-27", Body: []byte("## 결정\n"),
		Related: []string{"[[alpha-결정-저장엔진-2026-08-01]]"},
	})
	n, _ := l.Read(res.Path)
	if _, err := Review(l, ReviewRequest{Stem: n.Stem, Related: []string{"alpha-결정-스키마-2026-08-02"}}); err != nil {
		t.Fatal(err)
	}
	after, _ := l.Read(res.Path)
	if len(after.Meta.Related) != 2 {
		t.Errorf("덮어썼다: %v", after.Meta.Related)
	}
}
