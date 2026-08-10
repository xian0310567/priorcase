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
	statuses = map[string]bool{"active": true, "superseded": true, "regretted": true}
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
		return fmt.Errorf("status 가 허용값(active/superseded/regretted) 밖이다: %q", m.Status)
	}
	if !IsFuture(m) && !outcomes[m.Outcome] {
		return fmt.Errorf("outcome 이 허용값(pending/good/bad) 밖이다: %q", m.Outcome)
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
