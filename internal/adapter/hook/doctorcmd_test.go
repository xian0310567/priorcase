package hook

import (
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/core/health"
)

// report 는 손으로만 확인하고 있었다. 세 가지가 걸려 있다 —
// 한글 정렬, 고치는 법(→), 종료 코드.
func TestReportAlignsHangulColumns(t *testing.T) {
	r := &health.Report{Checks: []health.Check{
		{Name: "볼트", Level: health.OK, Detail: "A"},
		{Name: "미선언 도메인", Level: health.OK, Detail: "B"},
		{Name: "PATH", Level: health.OK, Detail: "C"},
	}}
	var b strings.Builder
	if err := report(&b, r); err != nil {
		t.Fatal(err)
	}
	// **표시 폭**으로 재야 한다. 룬 수로 재면 한글이 2칸인 것을 못 보고, 실제로는
	// 정렬됐는데 어긋났다고 나온다 — 이 테스트를 처음에 그렇게 썼다가 걸렸다.
	var cols []int
	for _, line := range strings.Split(b.String(), "\n") {
		for _, d := range []string{"A", "B", "C"} {
			if strings.HasSuffix(line, " "+d) {
				cols = append(cols, displayWidth(strings.TrimSuffix(line, d)))
			}
		}
	}
	if len(cols) != 3 {
		t.Fatalf("세 줄을 못 찾았다: %q", b.String())
	}
	for _, c := range cols[1:] {
		if c != cols[0] {
			t.Errorf("칸이 어긋났다 %v:\n%s", cols, b.String())
		}
	}
}

// 경고·오류에는 고치는 법이 따라와야 한다 — 진단만 하면 무시하는 법을 배운다.
func TestReportShowsFixOnlyForProblems(t *testing.T) {
	r := &health.Report{Checks: []health.Check{
		{Name: "정상", Level: health.OK, Detail: "d", Fix: "안 보여야 한다"},
		{Name: "경고", Level: health.Warn, Detail: "d", Fix: "이렇게 고쳐라"},
	}}
	var b strings.Builder
	_ = report(&b, r)
	if strings.Contains(b.String(), "안 보여야 한다") {
		t.Errorf("정상인데 고치는 법을 냈다:\n%s", b.String())
	}
	if !strings.Contains(b.String(), "→ 이렇게 고쳐라") {
		t.Errorf("경고에 고치는 법이 없다:\n%s", b.String())
	}
}

// 종료 코드로 옮긴다 — 자동화가 기계적으로 읽을 수 있어야 한다.
func TestReportExitCodes(t *testing.T) {
	for _, tc := range []struct {
		lv       health.Level
		wantCode int
		silent   bool
	}{
		{health.OK, 0, false},
		{health.Warn, 1, true},
		{health.Fail, 2, true},
	} {
		var b strings.Builder
		err := report(&b, &health.Report{Checks: []health.Check{{Name: "x", Level: tc.lv}}})
		code, silent := DiagnosticExit(err)
		if tc.lv == health.OK {
			if err != nil {
				t.Errorf("정상인데 에러를 냈다: %v", err)
			}
			continue
		}
		if code != tc.wantCode || !silent {
			t.Errorf("%v → code=%d silent=%v, want %d/true", tc.lv, code, silent, tc.wantCode)
		}
		// **메시지는 비어 있어야 한다** — 결과는 이미 stdout 에 찍었다.
		if err.Error() != "" {
			t.Errorf("에러 메시지가 있다 (두 번 나온다): %q", err.Error())
		}
	}
}

// 진단이 아닌 에러는 그대로 흘려보낸다.
func TestDiagnosticExitIgnoresOtherErrors(t *testing.T) {
	if code, silent := DiagnosticExit(errOther{}); code != 0 || silent {
		t.Errorf("남의 에러를 삼켰다: code=%d silent=%v", code, silent)
	}
}

type errOther struct{}

func (errOther) Error() string { return "다른 에러" }

// recentPtr 는 "모른다" 와 "0건" 을 가른다. 둘을 합치면 집계가 실패했을 뿐인데
// "일주일째 아무것도 기록 안 됐다" 는 최악의 경보가 뜬다 — 포인터로 감싼 이유다.
func TestRecentPtrSeparatesUnknownFromZero(t *testing.T) {
	if p := recentPtr(-1); p != nil {
		t.Errorf("집계 실패를 숫자로 바꿨다: %d", *p)
	}
	p := recentPtr(0)
	if p == nil || *p != 0 {
		t.Errorf("0건을 모른다로 바꿨다: %v", p)
	}
	if p := recentPtr(7); p == nil || *p != 7 {
		t.Errorf("7 이 안 왔다: %v", p)
	}
}
