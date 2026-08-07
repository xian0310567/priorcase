package index

import (
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/core/store"
)

func TestBuild(t *testing.T) {
	l := fixtureLayout(t) // fixture_test.go 의 헬퍼
	out, n, err := Build(l)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "| 날짜 | domain | summary | status | outcome | 링크 |") {
		t.Errorf("헤더가 없다:\n%s", s)
	}
	if n != 4 {
		t.Errorf("행 %d개, want 4:\n%s", n, s)
	}
	// 최신순 정렬
	iNew := strings.Index(s, "2026-08-04")
	iOld := strings.Index(s, "2026-08-01")
	if iNew < 0 || iOld < 0 || iNew > iOld {
		t.Errorf("최신순 정렬이 아니다:\n%s", s)
	}
}

// TestBuildEscapesPipeAndNewlineInSummary 는 summary 안의 파이프와 개행이
// 색인 표의 열을 밀어내지 않는지 확인한다.
//
// 브리프 원안의 검사(strings.Contains(s, "임베디드 DB | 로"))는 픽스처
// summary 에 파이프가 아예 없어서 항상 통과하는 무의미한 검사였다. 여기서는
// 파이프와 개행을 실제로 포함한 노트를 만들어 그 행을 뽑아낸 뒤, 이스케이프된
// 파이프(`\|`)를 markdown 표 파서처럼 구분자가 아닌 것으로 취급하고 나서
// strings.Split 결과가 정확히 6개 열(경계의 빈 문자열 2개 포함 8파트)인지
// 검사한다. 이스케이프를 고려하지 않고 그냥 "|" 로만 쪼개면, 이스케이프가
// 제대로 됐는지 여부와 무관하게 원본 파이프 개수만큼 항상 더 쪼개져서 이
// 테스트가 지키려는 불변식을 검증할 수 없다.
func TestBuildEscapesPipeAndNewlineInSummary(t *testing.T) {
	l := fixtureLayout(t)

	path, err := l.DecisionPath("alpha", "파이프이스케이프", "2026-08-05")
	if err != nil {
		t.Fatal(err)
	}
	n := store.Note{
		Path: path,
		Meta: store.Meta{
			Type:    "decision",
			Date:    "2026-08-05",
			Domain:  []string{"alpha"},
			Summary: "성능 | 속도 우선\n두 번째 줄은 참고용",
			Status:  "active",
			Outcome: "pending",
		},
		Body: []byte("## 결정\n\n파이프 이스케이프 테스트.\n"),
	}
	if err := l.Write(n); err != nil {
		t.Fatal(err)
	}

	out, rows, err := Build(l)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 5 {
		t.Fatalf("행 %d개, want 5 (기존 4 + 방금 추가한 1):\n%s", rows, out)
	}
	s := string(out)

	var row string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "| 2026-08-05") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("2026-08-05 행을 못 찾았다:\n%s", s)
	}
	if strings.Contains(row, "\n") {
		t.Errorf("행에 개행이 그대로 남아있다: %q", row)
	}

	// 이스케이프된 파이프(백슬래시+파이프)를 지워, markdown 표 파서가 그걸
	// 구분자로 보지 않는 동작을 흉내낸다. 그 다음 순수 구조적 파이프만으로
	// 쪼개 열 개수를 센다.
	unescaped := strings.ReplaceAll(row, `\|`, "")
	cols := strings.Split(unescaped, "|")
	if len(cols) != 8 { // 시작/끝 경계의 빈 문자열 2개 + 실제 6개 열
		t.Errorf("이스케이프에도 불구하고 열이 밀렸다: %d 파트\n원본 행: %s", len(cols), row)
	}
}
