package search

import (
	"strings"
	"testing"
)

// ★★ **긴 요약은 관련성이 아니다.**
//
// 이 파일이 못 박는 것은 하나다: head 히트의 값이 head 길이에 따라 깎인다.
//
// 실측(2026-08-27)으로 그게 안 되던 동안 값은 이랬다 — 실볼트 결정 420건에서
// 요약 길이 4분위별 평균 주입 횟수가 0.4 · 2.4 · 4.7 · **18.7**회(47배)였고,
// 상위 5개 노트가 전체 주입의 40.3%를 먹었고 228건(54%)은 한 번도 안 떴다.
// 주입이 상위 3줄뿐이라 이 편향이 곧 탈락이다.

// 캡 계약 — **정규화는 절대 가점이 되지 않는다.**
//
// 이 계약이 이 변경의 안전벨트다. ref 아래의 노트는 점수가 한 점도 안 바뀌므로
// 기존 순위 계약(weightCwdDomain < weightHead, supersededFloor < weightHead,
// weightSynonym < weightHead)이 전부 그대로 성립한다. 캡이 풀리면 짧은 head 가
// 가점을 받아 그 부등식들이 조용히 깨진다.
func TestLengthNormNeverBoosts(t *testing.T) {
	for _, n := range []int{0, 1, 50, refHeadRunes - 1, refHeadRunes, refHeadRunes + 1, 1000, 100000} {
		if got := lengthNorm(n); got > 1 {
			t.Errorf("lengthNorm(%d) = %v — 1.0 을 넘었다(가점이 됐다)", n, got)
		}
	}
	// ref 이하는 정확히 1이어야 한다 — "한 점도 안 바뀐다" 가 그 뜻이다.
	for _, n := range []int{0, 1, 80, 91, refHeadRunes} {
		if got := lengthNorm(n); got != 1 {
			t.Errorf("lengthNorm(%d) = %v, 1 이어야 한다 (ref 아래는 감점 없음)", n, got)
		}
	}
}

// 길수록 작아진다. 단조성이 깨지면 "길면 불리하다" 가 구간마다 뒤집힌다.
func TestLengthNormShrinksMonotonically(t *testing.T) {
	prev := lengthNorm(refHeadRunes)
	for n := refHeadRunes + 10; n <= 4000; n += 10 {
		got := lengthNorm(n)
		if got > prev {
			t.Fatalf("lengthNorm(%d)=%v 가 이전 값 %v 보다 크다 — 단조 감소가 깨졌다", n, got, prev)
		}
		prev = got
	}
	if prev >= 0.5 {
		t.Errorf("head 4000자의 계수가 %v 다 — 감점이 사실상 없다", prev)
	}
}

// ★ **이 변경의 본계약.** 히트 수가 같으면 짧은 쪽이 이긴다.
//
// 실볼트에서 이게 없던 동안 벌어진 일: editup 세션의
// `"백오피스 로그 조회가 안 돼서 원인 찾는 중이야"` 가 `novels-결정-노벨피아규정…`
// (요약 1,429자)을 불렀다. 걸린 낱말은 `로그` 와 요약 안쪽의 **"정산조회"** 였다.
// 우연한 히트 둘이 minHeadHits 게이트를 통과해 6점을 받고 슬롯을 먹었다.
func TestLongSummaryLosesToShortOneOnEqualHits(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	// 두 노트 모두 질의어 둘(케이크·딸기)이 head 에 걸린다. 다른 것은 길이뿐이다.
	plant(t, c, "alpha", "alpha-결정-짧은요약-2026-08-10", "케이크와 딸기를 고른다")
	plant(t, c, "beta", "beta-결정-긴요약-2026-08-10",
		"케이크와 딸기를 고른다. "+strings.Repeat("여기에는 이 결정과 무관한 배경 설명이 길게 이어진다. ", 30))

	hits := mustRecall(t, l, c, "케이크 딸기", Options{CrossProject: true, Limit: 5, MinScore: 1})
	var short, long *Hit
	for i := range hits {
		switch {
		case strings.Contains(hits[i].Note.Stem, "짧은요약"):
			short = &hits[i]
		case strings.Contains(hits[i].Note.Stem, "긴요약"):
			long = &hits[i]
		}
	}
	if short == nil || long == nil {
		t.Fatalf("두 노트가 다 걸리지 않았다: %+v", hits)
	}
	if short.Score <= long.Score {
		t.Errorf("짧은 요약(%d) 이 긴 요약(%d) 을 못 이겼다 — 길이 정규화가 안 걸렸다",
			short.Score, long.Score)
	}
	if hits[0].Note.Stem != short.Note.Stem {
		t.Errorf("1위가 %q 다 — 짧은 요약이어야 한다", hits[0].Note.Stem)
	}
}

// **감점은 순위를 낮추는 것이지 안 보이게 하는 것이 아니다.**
//
// supersededFloor 와 같은 이유다. 바닥이 없으면 head 가 아주 긴 노트의 히트
// 하나가 반올림으로 0 이 되고, `score > 0` 과 MinScore(실호출이 전부 1이다)가
// 그 노트를 결과에서 통째로 지운다. 긴 요약은 나쁜 습관이지 회수 불가 사유가 아니다.
func TestHeadHitFloorKeepsVeryLongNoteVisible(t *testing.T) {
	// head 히트 하나가 반올림으로 0 이 되는 길이를 실제로 만든다.
	long := 20000
	if raw := float64(weightHead) * lengthNorm(long); raw >= 0.5 {
		t.Skipf("head %d자에서도 반올림이 0 이 안 된다(%v) — 이 테스트의 전제가 사라졌다", long, raw)
	}
	if got := headScore(1, 0, long); got < headHitFloor {
		t.Errorf("headScore(1,0,%d) = %d — 바닥(%d) 아래로 내려가 노트가 사라진다",
			long, got, headHitFloor)
	}
}

// 바닥은 **감점 없는 최소 점수보다 작아야** "언제나 맨 끝" 이 성립한다.
// supersededFloor 와 같은 부등식이고, 같은 이유로 상수 수준에서 못 박는다.
func TestHeadHitFloorStaysBelowAnyFullHit(t *testing.T) {
	if headHitFloor >= weightHead {
		t.Fatalf("headHitFloor(%d) 가 weightHead(%d) 이상이다 — 긴 노트가 슬롯을 빼앗는다",
			headHitFloor, weightHead)
	}
}

// ref 아래에서는 점수식이 예전과 **한 점도** 다르지 않다.
// 이 성질이 깨지면 기존 계약 테스트들이 재는 값이 조용히 달라진다.
func TestShortHeadScoreIsUnchanged(t *testing.T) {
	cases := []struct{ headHits, synHits int }{{1, 0}, {2, 0}, {3, 0}, {0, 1}, {1, 1}, {2, 2}}
	for _, tc := range cases {
		want := weightHead*tc.headHits + weightSynonym*tc.synHits
		if got := headScore(tc.headHits, tc.synHits, refHeadRunes); got != want {
			t.Errorf("headScore(%d,%d,%d) = %d, 정규화 전과 같은 %d 여야 한다",
				tc.headHits, tc.synHits, refHeadRunes, got, want)
		}
	}
}

// 히트가 없으면 0 이다 — 바닥이 "히트 없는 노트" 를 살려내면 안 된다.
func TestHeadScoreZeroWithoutHits(t *testing.T) {
	for _, n := range []int{10, refHeadRunes, 5000} {
		if got := headScore(0, 0, n); got != 0 {
			t.Errorf("headScore(0,0,%d) = %d, 0 이어야 한다", n, got)
		}
	}
}
