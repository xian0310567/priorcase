package search

import (
	"strings"
	"unicode"
)

// matches 는 키워드가 텍스트에 걸리는지 본다. **언어에 따라 규칙이 다르다.**
//
// 한국어·일본어·중국어는 띄어쓰기 없이 복합어를 만든다 — "저장엔진" 이 한 토큰이라
// "저장" 으로 물으면 부분 매칭이어야 걸린다. 경계 매칭만 쓰면 한국어 회수가 죽는다.
//
// 반면 ASCII 는 부분 매칭이 재앙이다. 실측으로 `ok` 가 볼트 66건 중 5건을 반환했는데
// 전부 `hooks`·`token` 같은 낱말 **안쪽**에 걸린 것이었다. `use` 는 `used`·`because`
// 안에 산다. 대화체 프롬프트는 이런 짧은 낱말투성이라 오탐이 곧 소음이 된다.
//
// 그래서 토큰이 ASCII 로만 이뤄졌으면 낱말 경계를 요구하고, CJK 글자를 하나라도
// 포함하면 부분 매칭을 허용한다.
func matches(text, keyword string) bool {
	if keyword == "" {
		return false
	}
	if hasWideScript(keyword) {
		return strings.Contains(text, keyword)
	}
	return containsWord(text, keyword)
}

// hasWideScript 는 띄어쓰기로 낱말을 가르지 않는 문자(한글·한자·가나)를 포함하는지 본다.
func hasWideScript(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Han, r) ||
			unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			return true
		}
	}
	return false
}

// containsWord 는 낱말 경계에 둘러싸인 keyword 를 찾는다.
//
// 경계는 "영숫자가 아닌 것" 이다. 하이픈·언더스코어·점을 경계로 치므로
// `use-postgres` 는 `postgres` 로 걸리고, `used` 는 `use` 로 안 걸린다 —
// 파일명 stem 과 태그가 하이픈으로 이어져 있어서 이 쪽이 맞다.
func containsWord(text, keyword string) bool {
	from := 0
	for {
		i := strings.Index(text[from:], keyword)
		if i < 0 {
			return false
		}
		i += from
		if isBoundary(text, i-1) && isBoundary(text, i+len(keyword)) {
			return true
		}
		from = i + 1
		if from >= len(text) {
			return false
		}
	}
}

// isBoundary 는 그 자리가 낱말 밖인지 본다. 문자열 끝도 경계다.
func isBoundary(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	c := s[i]
	// ASCII 영숫자만 낱말 안쪽으로 친다. 멀티바이트 글자는 첫 바이트가 0x80 이상인데,
	// 그건 CJK 이거나 악센트 문자라 ASCII 낱말의 경계로 봐도 된다.
	return !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z')
}
