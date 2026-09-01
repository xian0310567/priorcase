package search

import (
	"fmt"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// 본문 훑기를 **희소도 게이트 뒤로** 옮긴 것을 고정한다.
//
// 옮길 수 있는 근거는 하나뿐이다: 게이트에서 떨어진 노트의 bodyHits 는 어디에도
// 안 쓰인다 — 점수 식은 `gate >= need` 를 통과한 뒤에만 계산된다. 그래서 이건
// 순수한 낭비 제거이고, **점수가 한 점이라도 달라지면 그 전제가 깨진 것**이다.
//
// 아래 두 시험이 그 전제의 양쪽 끝을 잡는다. 하나만 있으면 둘 다 못 잡는다 —
// 앞 것만 있으면 본문 훑기를 통째로 지워도 통과하고, 뒤 것만 있으면 본문 점수를
// 게이트 앞에 더해 버려도 통과한다.

// bodyNotes 는 head 와 body 를 따로 정할 수 있는 합성 볼트다.
//
// `prepare` 를 거치는 것이 중요하다 — head 를 손으로 지으면 실제 회수가 보는
// 것과 조용히 갈라진다(headText 가 날짜·도메인 접두어를 벗기는 것을 안 거친다).
func bodyNotes(n int, summary func(i int) string, body string) []prepared {
	notes := make([]store.Note, 0, n)
	for i := 0; i < n; i++ {
		notes = append(notes, store.Note{
			Path: fmt.Sprintf("/v/d/n%d.md", i),
			Stem: fmt.Sprintf("n%d", i),
			Meta: store.Meta{Summary: summary(i), Date: "2026-08-31", Status: "active"},
			Body: []byte(body),
		})
	}
	return prepare(notes, "")
}

// ★ 게이트를 못 넘는 노트는 본문이 아무리 맞아도 안 나온다.
//
// 이걸 안 잡으면 최적화가 조용히 기능 변경이 된다 — 본문 점수를 게이트 **앞**에
// 더하면 흔한 낱말만 걸린 노트가 본문 히트로 문턱을 넘어 올라온다. 그건
// TestCommonWordAloneFailsGate 가 막는 고장을 뒷문으로 되살리는 것이다.
func TestBodyNeverRescuesAGatedOutNote(t *testing.T) {
	// 흔한낱말이 10건 전부에 있다 → 변별어가 아니다. 본문에는 질의어 둘이 다 있다.
	notes := bodyNotes(10,
		func(i int) string { return fmt.Sprintf("노트 %d 흔한낱말", i) },
		"드문낱말 또다른드문낱말")

	hits := scoreAll(notes, []string{"흔한낱말", "드문낱말", "또다른드문낱말"},
		conversational, "", nil, Synonyms{})
	if len(hits) != 0 {
		t.Fatalf("%d건이 올라왔다 (첫 건 %d점) — 본문은 게이트를 구제하면 안 된다",
			len(hits), hits[0].Score)
	}
}

// ★ 게이트를 넘은 노트는 본문 점수를 **그대로** 받는다.
func TestBodyStillScoresAfterTheGate(t *testing.T) {
	// 두 노트의 head 가 같으므로 점수 차는 본문에서만 온다.
	notes := bodyNotes(2, func(i int) string { return "드문낱말 흔한낱말" }, "")
	notes[0].body = "드문낱말 흔한낱말" // 질의어 둘 다 본문에 있다
	notes[1].body = "관계 없는 본문"

	hits := scoreAll(notes, []string{"드문낱말", "흔한낱말"}, conversational, "", nil, Synonyms{})
	if len(hits) != 2 {
		t.Fatalf("%d건 — 둘 다 올라와야 한다", len(hits))
	}
	byStem := map[string]int{}
	for _, h := range hits {
		byStem[h.Note.Stem] = h.Score
	}
	gap := byStem["n0"] - byStem["n1"]
	if want := 2 * weightBody; gap != want {
		t.Fatalf("본문 점수 차가 %d — %d 여야 한다 (본문 훑기가 사라졌나)", gap, want)
	}
}
