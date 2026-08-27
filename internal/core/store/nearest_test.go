package store

import "testing"

// 글자 단위여야 한다 — 바이트로 재면 한글 한 글자 오타가 거리 3이 되어 제안이 안 나온다.
func TestEditDistanceCountsRunesNotBytes(t *testing.T) {
	if d := editDistance([]rune("브라우저"), []rune("봌라우잠")); d != 2 {
		t.Errorf("거리 = %d, want 2 (두 글자 다름)", d)
	}
	if d := editDistance([]rune("충족"), []rune("추족")); d != 1 {
		t.Errorf("거리 = %d, want 1", d)
	}
}

// 무관한 이름은 제안하지 않는다 — 없는 것보다 나쁘다.
func TestNoSuggestionForUnrelatedName(t *testing.T) {
	stems := map[string]bool{"alpha-결정-저장엔진-2026-08-01": true}
	if got := NearestStem("완전히-무관한-주제-짜장면-2026-01-01", stems); got != "" {
		t.Errorf("무관한 이름을 제안했다: %q", got)
	}
}
