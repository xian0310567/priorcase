// Package schema 는 결정 노트의 불변식을 강제한다.
// 쓰기 경로가 하나뿐이므로 여기를 통과하지 않은 노트는 볼트에 들어갈 수 없다.
package schema

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/xian0310567/priorcase/internal/core/store"
)

var (
	dateRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	statuses = map[string]bool{
		store.StatusActive: true, store.StatusSuperseded: true,
		store.StatusRegretted: true, store.StatusRetracted: true,
	}
	outcomes = map[string]bool{"pending": true, "good": true, "bad": true}
)

// Validate 는 stem 과 meta 가 서로 정합한지 본다.
//
// marker 는 결정 노트 파일명의 표식이다("-결정-", 영어 템플릿이면 "-decision-").
// 정본은 설정의 decision_file 템플릿이고 store.Layout.DecisionMarker 가 그걸
// 유도해 준다 — schema 는 config 를 몰라야 하므로 인자로 받는다. 여기에 리터럴을
// 두면 파일명을 만드는 쪽(store)과 검사하는 쪽(schema)이 갈라져, 템플릿을 바꾼
// 순간 capture 가 통째로 죽는다.
// Current 는 지금 이 바이너리가 쓰는 스키마 판이다.
//
// **올릴 때 규칙**: 옛 바이너리가 읽어도 데이터를 잃지 않는 변경이면 올리지 않아도
// 된다(잉여 키는 Extra 가 보존한다). 올려야 하는 것은 **기존 필드의 의미나 허용값을
// 바꿀 때**다 — 그때 옛 바이너리가 "허용값 밖" 으로 거부하는 것을 막아야 한다.
const Current = 1

// IsFuture 는 이 노트가 더 새 판으로 쓰였는지 본다.
func IsFuture(m store.Meta) bool { return m.Schema > Current }

// Validate 는 stem 과 meta 가 서로 정합한지 본다.
//
// **더 새 판으로 쓰인 노트는 열거값을 검사하지 않는다.** 우리가 모르는 status·outcome
// 이 들어 있을 수 있고, 모르는 것을 틀렸다고 하면 남의 결정을 거부하게 된다. 구조
// (type·date·domain·summary)는 그대로 본다 — 그건 판이 올라가도 안 바뀐다.
func Validate(marker, stem string, m store.Meta) error {
	if m.Type != "decision" {
		return fmt.Errorf("type 은 decision 이어야 한다: %q", m.Type)
	}
	if !dateRe.MatchString(m.Date) {
		return fmt.Errorf("date 는 YYYY-MM-DD 여야 한다: %q", m.Date)
	}
	// 정규식은 모양만 본다 — 2026-02-30 같은 실존하지 않는 날짜도 통과시킨다.
	// time.Parse 로 달력상 실제 날짜인지까지 확인한다.
	if _, err := time.Parse("2006-01-02", m.Date); err != nil {
		return fmt.Errorf("date 가 실존하지 않는 날짜다: %q", m.Date)
	}
	if len(m.Domain) == 0 {
		return fmt.Errorf("domain 이 비었다")
	}
	if strings.TrimSpace(m.Summary) == "" {
		return fmt.Errorf("summary 가 비었다 — 회수 시 이것만 주입되므로 필수다")
	}
	if !IsFuture(m) && !statuses[m.Status] {
		return fmt.Errorf("status 가 허용값(active/superseded/regretted/retracted) 밖이다: %q", m.Status)
	}
	if !IsFuture(m) && !outcomes[m.Outcome] {
		return fmt.Errorf("outcome 이 허용값(pending/good/bad) 밖이다: %q", m.Outcome)
	}
	if err := checkOverturnConsistency(m); err != nil {
		return err
	}
	marker = store.NFC(marker)
	if marker == "" {
		return fmt.Errorf("결정 표식이 비었다 — 설정의 decision_file 템플릿을 확인하라")
	}
	stem = store.NFC(stem)
	i := strings.Index(stem, marker)
	if i <= 0 {
		return fmt.Errorf("stem 이 규약(%s)에 맞지 않는다: %q", marker, stem)
	}
	if prefix := stem[:i]; prefix != m.Domain[0] {
		return fmt.Errorf("파일명 접두어(%q)와 domain 첫 값(%q)이 다르다", prefix, m.Domain[0])
	}
	if !strings.HasSuffix(stem, "-"+m.Date) {
		return fmt.Errorf("파일명 날짜와 date(%q)가 다르다: %q", m.Date, stem)
	}
	return nil
}

// checkOverturnConsistency 는 **번복 사유가 붙은 노트가 active 로 되돌아가는 것**을 막는다.
//
// # 왜 전이표를 안 만들었나
//
// 원래 요구는 "status 전이 규칙"(예: superseded → active 금지)이었다. 두 가지 이유로
// 일반 전이표를 만들지 않았다.
//
//  1. **Validate 는 이전 값을 모른다.** 인자는 (marker, stem, meta) 뿐이고, 호출부
//     넷(capture.Do, capture.Review 의 새·옛 노트, promote)이 전부 "지금 쓸 노트"만
//     넘긴다. 전이를 보려면 시그니처에 from 을 더해야 하는데, 그러면 손으로 고친
//     파일을 다시 읽어 쓰는 경로(index·health)가 from 을 만들어 낼 수 없다.
//  2. **과하게 막으면 사람이 손으로 고치는 길을 막는다.** 옵시디언에서 status 를
//     잘못 눌렀다가 되돌리는 것은 정상 작업이다. superseded → active 를 무조건
//     막으면 그 되돌리기가 prior 로는 영영 안 된다 — 사용자에게 "파일을 직접
//     열어 고쳐라" 라고 말하는 도구가 된다.
//
// 대신 **노트 하나 안에서 닫히는 모순 하나만** 본다: superseded_reason 이 있는데
// status 가 active 인 상태. 이건 손으로 되돌리다 만 흔적이고, 회수에서 정확히
// 최악이다 — 뒤집힌 결정이 감점(search.penaltySuperseded) 없이 만점으로 올라온다.
// 사용자 정책이 "방치된 오래된 결정이 recall 을 오염시킨다" 로 못 박은 그 상태다.
//
// 되돌리는 길은 여전히 열려 있다: 사유를 지우고 active 로 바꾸면 통과한다. "번복을
// 취소하려면 번복 사유도 같이 지워라" 는 요구는 과하지 않다 — 남아 있으면 그게 거짓말이다.
//
// superseded_reason 은 새 키라 기존 노트에는 없다(실볼트 18노트 전부). 즉 이 규칙이
// 오늘 거부하는 기존 노트는 0건이다. 판(schema.Current)을 올리지 않는 이유이기도
// 하다: 기존 필드의 의미도 허용값도 안 바꿨고, 옛 바이너리는 이 키를 Extra 로 보존한다.
func checkOverturnConsistency(m store.Meta) error {
	// 더 새 판은 우리가 모르는 규칙으로 쓰였다 — 열거값과 같은 이유로 안 본다.
	if IsFuture(m) {
		return nil
	}
	if strings.TrimSpace(m.SupersededReason) == "" {
		return nil
	}
	if m.Status == "active" {
		return fmt.Errorf(
			"번복 사유(superseded_reason)가 있는데 status 가 active 다 — "+
				"뒤집힌 결정이 회수에서 감점 없이 올라온다. 되돌리려면 사유도 함께 지워라: %q",
			m.SupersededReason)
	}
	return nil
}
