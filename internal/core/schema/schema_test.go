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

// ★ **번복 사유가 붙은 노트는 active 로 돌아갈 수 없다.**
//
// 일반 전이표(superseded → active 금지 같은)를 만들지 않은 이유는
// checkOverturnConsistency 의 주석에 있다 — Validate 는 이전 값을 모르고, 과하게
// 막으면 사람이 옵시디언에서 손으로 되돌리는 길이 막힌다. 대신 노트 하나 안에서
// 닫히는 모순 하나만 본다.
func TestValidateRejectsActiveWithSupersedeReason(t *testing.T) {
	m := base()
	m.SupersededReason = "측정에서 가정이 깨졌다"

	if err := Validate(koMarker, "alpha-결정-x-2026-08-07", m); err == nil {
		t.Fatal("번복 사유가 있는데 status:active 인 노트를 통과시켰다 — " +
			"회수에서 감점 없이 만점으로 올라온다")
	}

	// 뒤집힌 상태로 표시돼 있으면 통과한다.
	for _, s := range []string{"superseded", "regretted"} {
		m.Status = s
		if err := Validate(koMarker, "alpha-결정-x-2026-08-07", m); err != nil {
			t.Errorf("status=%q 를 거부했다: %v", s, err)
		}
	}

	// **되돌리는 길은 열려 있다** — 사유를 지우면 active 로 돌아간다.
	// 이게 없으면 사용자에게 "파일을 직접 열어 고쳐라" 라고 말하는 도구가 된다.
	m.Status, m.SupersededReason = "active", ""
	if err := Validate(koMarker, "alpha-결정-x-2026-08-07", m); err != nil {
		t.Errorf("사유를 지운 되돌리기를 막았다: %v", err)
	}
}

// TestFutureSchemaSkipsOverturnRule 은 더 새 판으로 쓰인 노트에는 이 규칙을 안
// 대는지 본다. 열거값 검사를 건너뛰는 것과 같은 이유다 — 우리가 모르는 규칙으로
// 쓰인 노트를 우리 규칙으로 거부하면 남의 결정을 지우는 것과 같다.
func TestFutureSchemaSkipsOverturnRule(t *testing.T) {
	m := base()
	m.SupersededReason = "미래 판이 쓴 사유"
	m.Schema = Current + 1
	if err := Validate(koMarker, "alpha-결정-x-2026-08-07", m); err != nil {
		t.Errorf("더 새 판 노트를 거부했다: %v", err)
	}
}
