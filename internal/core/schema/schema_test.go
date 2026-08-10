package schema

import "testing"

import "github.com/xian0310567/priorcase/internal/core/store"

// koMarker 는 기본 한국어 템플릿에서 유도되는 표식이다. 이 파일 안에서만
// 리터럴로 둔다 — 제품 코드의 정본은 설정의 decision_file 템플릿이다.
const koMarker = "-결정-"

func base() store.Meta {
	return store.Meta{
		Type: "decision", Date: "2026-08-07", Domain: []string{"alpha"},
		Summary: "요약", Status: "active", Outcome: "pending",
	}
}

// TestDateRegexAcceptsShapeOnly 는 dateRe 가 모양만 본다는 사실을 기록해 둔다 —
// 2026-02-30(2월 30일, 실존하지 않는 날짜)도 정규식은 통과시킨다.
// 실제 존재하는 날짜인지는 Validate 안에서 time.Parse 로 따로 확인한다.
func TestDateRegexAcceptsShapeOnly(t *testing.T) {
	if !dateRe.MatchString("2026-02-30") {
		t.Fatalf("dateRe 가 모양만 보는 정규식이라는 전제가 깨졌다")
	}
}

func TestValidateAccepts(t *testing.T) {
	if err := Validate(koMarker, "alpha-결정-x-2026-08-07", base()); err != nil {
		t.Fatalf("정상 노트를 거부했다: %v", err)
	}
}

// TestValidateUsesGivenMarker 는 표식이 인자로 들어온다는 사실을 못박는다:
// 영어 템플릿에서 유도된 "-decision-" 표식이면 영어 stem 이 통과하고,
// 같은 stem 을 한국어 표식으로 검사하면 거부돼야 한다. 어느 한쪽이라도
// 리터럴 "-결정-" 을 쓰고 있으면 이 테스트가 깨진다.
func TestValidateUsesGivenMarker(t *testing.T) {
	const enStem = "alpha-decision-x-2026-08-07"
	if err := Validate("-decision-", enStem, base()); err != nil {
		t.Errorf("영어 표식으로 영어 stem 을 거부했다: %v", err)
	}
	if err := Validate(koMarker, enStem, base()); err == nil {
		t.Error("한국어 표식인데 영어 stem 을 통과시켰다 — 표식이 인자에서 오지 않는다")
	}
	if err := Validate("", "alpha-결정-x-2026-08-07", base()); err == nil {
		t.Error("빈 표식을 통과시켰다 — 표식 없이는 규약을 판정할 수 없다")
	}
}

func TestValidateRejects(t *testing.T) {
	tests := []struct {
		name string
		stem string
		mut  func(*store.Meta)
	}{
		{"접두어와 domain 첫값 불일치", "beta-결정-x-2026-08-07", func(m *store.Meta) {}},
		{"domain 이 비었다", "alpha-결정-x-2026-08-07", func(m *store.Meta) { m.Domain = nil }},
		{"summary 가 비었다", "alpha-결정-x-2026-08-07", func(m *store.Meta) { m.Summary = "" }},
		{"status 허용값 밖", "alpha-결정-x-2026-08-07", func(m *store.Meta) { m.Status = "unknown" }},
		{"outcome 허용값 밖", "alpha-결정-x-2026-08-07", func(m *store.Meta) { m.Outcome = "maybe" }},
		{"date 형식 오류", "alpha-결정-x-2026-08-07", func(m *store.Meta) { m.Date = "2026/08/07" }},
		{"type 이 decision 이 아니다", "alpha-결정-x-2026-08-07", func(m *store.Meta) { m.Type = "note" }},
		{"파일명 날짜와 date 불일치", "alpha-결정-x-2026-08-01", func(m *store.Meta) {}},
		{"실존하지 않는 날짜(2월 30일)", "alpha-결정-x-2026-02-30", func(m *store.Meta) { m.Date = "2026-02-30" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := base()
			tt.mut(&m)
			if err := Validate(koMarker, tt.stem, m); err == nil {
				t.Errorf("통과했다: stem=%q meta=%+v", tt.stem, m)
			}
		})
	}
}
