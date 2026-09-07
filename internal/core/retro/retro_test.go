package retro

import (
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

func fixtureLayoutConfig(t *testing.T) (*store.Layout, *config.Config) {
	t.Helper()
	c := testutil.VaultConfig(t)
	return store.NewLayout(c), c
}

// ★★★ **조용히 잘 돌아간 결정은 다른 방아쇠로는 영영 안 열린다.**
//
// 앞의 둘(재회수·뒤집힘)은 **다른 사건이 일어나야** 열린다. 그래서 예상대로
// 굴러간 결정 — 정확히 "예상이 맞았나" 를 물어야 하는 것 — 이 큐에 안 뜬다.
// review_after 가 그 구멍을 메운다 (retro.ReasonScheduled 의 §).
func TestScheduledReviewOpensTheQueue(t *testing.T) {
	restore := clock
	clock = func() time.Time { return time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { clock = restore })

	for _, tc := range []struct {
		name  string
		after string
		want  bool
	}{
		{"지났다", "2026-10-01", true},
		{"오늘이다", "2026-10-05", true}, // "10월 1일 이후에 보자" 는 그날 봐도 된다
		{"아직이다", "2026-12-01", false},
		{"안 적었다", "", false},
	} {
		n := store.Note{Meta: store.Meta{ReviewAfter: tc.after}}
		if got := scheduledDue(n, clock().Format("2006-01-02")); got != tc.want {
			t.Errorf("%s (review_after=%q): %v, %v 여야 한다", tc.name, tc.after, got, tc.want)
		}
	}
}

// **결과가 이미 적힌 것은 날짜가 지나도 안 묻는다.** 답한 결정이 매번 다시
// 올라오면 답할수록 큐가 안 줄어드는 것처럼 보인다.
func TestScheduledDoesNotReopenAnsweredDecisions(t *testing.T) {
	restore := clock
	clock = func() time.Time { return time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { clock = restore })

	l, c := fixtureLayoutConfig(t)
	notes, _, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) == 0 {
		t.Fatal("픽스처에 결정이 없다")
	}
	n := notes[0]
	n.Meta.ReviewAfter = "2026-09-01" // 지났다
	n.Meta.Outcome = "good"           // 그런데 이미 판명됐다
	if err := l.Write(n); err != nil {
		t.Fatal(err)
	}
	due, _, err := Due(l, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range due {
		if it.Stem == n.Stem {
			t.Errorf("결과가 적힌 결정이 예정 회고로 다시 떴다: %+v", it)
		}
	}
}
