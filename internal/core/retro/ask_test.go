package retro

import (
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/store"
)

var now = time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

func note(date, status, outcome string) store.Note {
	return store.Note{
		Stem: "proj-결정-x-" + date,
		Meta: store.Meta{
			Type: "decision", Date: date, Status: status, Outcome: outcome,
			Summary: "요약",
		},
	}
}

// ★★★ **뒤집힌 결정은 나이를 안 본다.**
//
// 뒤집혔다는 것 자체가 결과가 났다는 뜻이다. 하루 만에 뒤집혔어도 물을 값이
// 있고, 오히려 그때가 이유가 가장 선명하다.
func TestSupersededIsAskedRegardlessOfAge(t *testing.T) {
	n, ok := Ask([]store.Note{note("2026-08-13", "superseded", "pending")}, now)
	if !ok {
		t.Fatal("어제 뒤집힌 결정을 안 묻는다")
	}
	if n.Meta.Status != "superseded" {
		t.Errorf("엉뚱한 노트를 골랐다: %+v", n.Meta)
	}
}

// ★★★ **어제 내린 결정에 결과를 물으면 답이 "아직 모른다" 뿐이다.**
//
// 답할 수 없는 질문은 소음이고, 소음은 무시하는 법을 가르친다. 실측으로 회고
// 큐 52건의 나이가 1~13일(중앙값 5일)이라 이 문턱이 실제로 대부분을 자른다.
func TestFreshDecisionIsNotAsked(t *testing.T) {
	if _, ok := Ask([]store.Note{note("2026-08-13", "active", "pending")}, now); ok {
		t.Error("하루 된 결정에 결과를 물었다")
	}
	if _, ok := Ask([]store.Note{note("2026-08-08", "active", "pending")}, now); ok {
		t.Error("엿새 된 결정에 결과를 물었다 — 문턱은 이레다")
	}
}

// ★★ 이레가 지나면 묻는다.
func TestOldEnoughIsAsked(t *testing.T) {
	if _, ok := Ask([]store.Note{note("2026-08-07", "active", "pending")}, now); !ok {
		t.Error("이레 지난 결정을 안 묻는다")
	}
}

// ★★★ **이미 답한 것을 또 묻지 않는다.**
//
// 같은 질문이 반복되면 그것이 곧 무시하는 법을 가르치는 신호가 된다.
func TestAnsweredIsNotAskedAgain(t *testing.T) {
	for _, o := range []string{"good", "bad"} {
		if _, ok := Ask([]store.Note{note("2026-08-01", "active", o)}, now); ok {
			t.Errorf("outcome=%s 인데 또 물었다", o)
		}
	}
	// 후회로 표시한 것도 물을 것이 없다.
	if _, ok := Ask([]store.Note{note("2026-08-01", "regretted", "pending")}, now); ok {
		t.Error("regretted 인데 또 물었다")
	}
}

// ★★ 빈 outcome 은 "아직 모른다" 와 같다 (판 1 노트에는 그 키가 없었다).
func TestEmptyOutcomeCountsAsPending(t *testing.T) {
	if _, ok := Ask([]store.Note{note("2026-08-01", "active", "")}, now); !ok {
		t.Error("outcome 이 비었는데 안 물었다 — 판 1 노트를 영영 안 묻게 된다")
	}
}

// ★★★ **날짜를 모르면 묻지 않는다.**
//
// 나이를 못 재는데 물으면 방금 내린 결정에 결과를 묻게 된다.
func TestUndatedIsNotAsked(t *testing.T) {
	if _, ok := Ask([]store.Note{note("", "active", "pending")}, now); ok {
		t.Error("날짜가 없는데 물었다")
	}
	if _, ok := Ask([]store.Note{note("어제", "active", "pending")}, now); ok {
		t.Error("날짜가 깨졌는데 물었다")
	}
}

// ★★★ **1위만 본다.**
//
// 주입은 결정 셋을 싣는다. 셋 다 물으면 매 프롬프트가 질문 셋으로 덮이고,
// 그러면 사람도 에이전트도 무시하는 법을 배운다. 1위는 지금 하는 일과 가장
// 가까운 과거 결정이고, 그것 하나면 족하다.
func TestOnlyTheTopHitIsConsidered(t *testing.T) {
	hits := []store.Note{
		note("2026-08-13", "active", "pending"), // 1위 — 너무 최근이라 안 묻는다
		note("2026-08-01", "superseded", "pending"),
	}
	if _, ok := Ask(hits, now); ok {
		t.Error("1위를 건너뛰고 아래에서 골랐다")
	}
}

// ★★★ **참고 문서에는 결과랄 것이 없다.**
//
// 회수는 기획·설계 문서도 싣는다. 거기에 "결과가 좋았나" 를 물으면 답이 없고,
// 무엇보다 그것은 결정이 아니다.
func TestReferenceIsNotAsked(t *testing.T) {
	ref := store.Note{Stem: "01-설계", Meta: store.Meta{Type: "spec", Summary: "설계"}}
	if _, ok := Ask([]store.Note{ref}, now); ok {
		t.Error("참고 문서에 결과를 물었다")
	}
}

// ★★ 회수가 비면 아무것도 안 묻는다.
func TestNoHitsNoQuestion(t *testing.T) {
	if _, ok := Ask(nil, now); ok {
		t.Error("회수가 비었는데 물었다")
	}
}
