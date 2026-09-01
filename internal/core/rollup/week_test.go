package rollup

import (
	"testing"
	"time"
)

// monday 는 테스트 날짜를 시각으로 만든다. rollup_test.go 의 `day` 는 로그
// 블록을 만드는 다른 헬퍼라 이름을 겹치지 않게 둔다.
func monday(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// ★ 밀린 정도를 재는 이유: doctor 가 "밀렸다" 를 말하려면 **얼마나** 밀렸는지를
// 알아야 한다. 지난주 한 건에 매주 경고를 띄우면 그 경고는 곧 무시당한다
// (Check.Fix 주석의 같은 논리).
func TestWeekBehind(t *testing.T) {
	now := monday(t, "2026-09-01") // 2026-W36 (화)
	for _, tc := range []struct {
		week string
		want int
	}{
		{"2026-W36", 0}, // 진행 중인 주
		{"2026-W35", 1}, // 지난주 — 아직 밀린 것이 아니다
		{"2026-W34", 2},
		{"2026-W32", 4},
	} {
		if got := (Week{Week: tc.week}).Behind(now); got != tc.want {
			t.Errorf("%s: %d 주 뒤 — %d 여야 한다", tc.week, got, tc.want)
		}
	}
}

// ★ 연말연시가 이 계산의 유일한 함정이고 **가짜가 아니다** — 2026 년은 W53 이
// 있어서 2026-W53 다음이 2027-W01 이다. 문자열 뺄셈으로는 53→1 을 잇지 못한다.
func TestWeekBehindCrossesYear(t *testing.T) {
	now := monday(t, "2027-01-04") // 2027-W01 (월)
	if got := (Week{Week: "2026-W53"}).Behind(now); got != 1 {
		t.Fatalf("2026-W53 이 %d 주 뒤 — 1 이어야 한다 (바로 앞 주다)", got)
	}
	if got := (Week{Week: "2026-W52"}).Behind(now); got != 2 {
		t.Fatalf("2026-W52 가 %d 주 뒤 — 2 여야 한다", got)
	}
}

// ★ 못 읽는 주 문자열은 **밀리지 않은 것으로 친다.** 반대로 하면 깨진 한 줄이
// doctor 를 영영 노랗게 만들고, 그 경고에는 고칠 방법이 없다.
func TestWeekBehindRejectsGarbage(t *testing.T) {
	now := monday(t, "2026-09-01")
	for _, wk := range []string{"", "2026-08", "지난주", "2026-Wxx"} {
		if got := (Week{Week: wk}).Behind(now); got != 0 {
			t.Errorf("%q: %d — 0 이어야 한다", wk, got)
		}
	}
}

// ★ 미래로 찍힌 주도 0 이다. 작업 로그에 앞선 날짜가 적히는 일이 있는데
// (계획을 미리 적는다) 그건 "밀렸다" 의 반대다.
func TestWeekBehindIgnoresFuture(t *testing.T) {
	if got := (Week{Week: "2026-W40"}).Behind(monday(t, "2026-09-01")); got != 0 {
		t.Fatalf("미래 주가 %d — 0 이어야 한다", got)
	}
}
