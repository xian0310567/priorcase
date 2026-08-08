package search

import (
	"reflect"
	"testing"
)

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"조사 절단", "테스트이나 저장엔진을 골랐다", []string{"골랐다", "저장엔진", "테스트"}},
		{"구두점 분리", "a/b, c.d", []string{}}, // 전부 1바이트라 길이 필터 탈락
		{"하이픈 분리", "저장-엔진", []string{"엔진", "저장"}},
		// ★ 계약이 뒤집혔다 (2026-08-09). 예전에는 **바이트** 기준이라 한글 1음절이
		// 통과했다 — 옛 셸의 `awk length()` 동작을 보존하려던 것이다. 그 결과
		// `cb recall "다"` 가 실볼트 66건 중 45건을 반환했다. 조사를 떼고 남는 한
		// 글자는 검색어가 아니라 잡음이라, 이제 **글자** 기준으로 둘 다 탈락한다.
		{"길이 필터는 글자 기준 — 한글 1음절도 탈락", "셸 a", []string{}},
		{"두 글자부터 통과", "훅 엔진 db", []string{"db", "엔진"}}, // 훅=1글자 탈락, 엔진·db=2글자 통과
		{"중복 제거와 정렬", "엔진 엔진 저장", []string{"엔진", "저장"}},
		// 지시 1: punct 정규식에 \s 가 포함되어 있어 탭·개행도 분리자로 동작해야 한다.
		{"탭과 개행 토큰화", "테스트\t저장소\n엔진", []string{"엔진", "저장소", "테스트"}},
		// 지시 3: josa 정규식은 "이나" 를 "나" 보다 먼저 나열한다. Go 정규식은 leftmost-first 라
		// 이 순서가 결과를 바꾼다. "저장소나" 는 "이나" 가 아니라 "나" 만 잘려야 한다
		// (문자열이 "이나" 로 끝나지 않고 "나" 로만 끝나므로).
		{"조사 나 만 절단", "저장소나", []string{"저장소"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractKeywords(tt.in)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractKeywords(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestStopwordsRemoveExactLines(t *testing.T) {
	// "결정" 은 불용어다 — 없으면 모든 결정 문서와 매칭된다
	for _, k := range ExtractKeywords("결정 저장엔진") {
		if k == "결정" {
			t.Fatal("불용어 '결정' 이 남았다")
		}
	}
	// 부분 일치가 아니라 정확히 한 줄 전체 일치만 제거한다
	found := false
	for _, k := range ExtractKeywords("결정사항 검토") {
		if k == "결정사항" {
			found = true
		}
	}
	if !found {
		t.Fatal("'결정사항' 이 부분 일치로 제거됐다 — 정확 일치만 제거해야 한다")
	}
}

// 지시 4: 빈 프롬프트·공백만 있는 프롬프트는 nil 을 반환해야 한다.
// 다음 태스크(회수 검색)가 len(keywords) == 0 으로 조기 반환하므로,
// nil 과 빈 슬라이스를 구분하지 않고 둘 다 안전하게 처리되는지 여기서 확실히 해 둔다.
func TestExtractKeywordsEmptyReturnsNil(t *testing.T) {
	if got := ExtractKeywords(""); got != nil {
		t.Fatalf("빈 프롬프트: got %#v, want nil", got)
	}
	if got := ExtractKeywords("   \t\n  "); got != nil {
		t.Fatalf("공백만 있는 프롬프트: got %#v, want nil", got)
	}
}

// ★ 대화체 프롬프트는 기능어가 내용어보다 많다.
//
// 실측으로 `can you add a spinner on the login button` 이 무관한 결정 5건을
// 주입했다. 훅은 **매 프롬프트마다** 도는데, 무관한 주입은 곧 "무시하는 법" 을
// 가르친다 — 이 프로젝트가 죄목으로 드는 상태다.
func TestConversationalEnglishIsFilteredOut(t *testing.T) {
	for _, tc := range []struct {
		prompt string
		want   []string
	}{
		{"can you add a spinner on the login button", []string{"button", "login", "spinner"}},
		{"ok thanks that works", []string{"works"}},
		{"please make it a bit more compact", []string{"bit", "compact"}},
		{"we should use postgres here", []string{"postgres"}},
	} {
		got := ExtractKeywords(tc.prompt)
		if len(got) != len(tc.want) {
			t.Errorf("ExtractKeywords(%q) = %v, want %v", tc.prompt, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ExtractKeywords(%q) = %v, want %v", tc.prompt, got, tc.want)
				break
			}
		}
	}
}
