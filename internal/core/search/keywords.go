// Package search 는 결정 회수를 담당한다.
// 점수 함수는 나중에 임베딩으로 교체할 수 있게 이 패키지 안에 격리한다.
package search

import (
	"regexp"
	"sort"
	"strings"

	"github.com/xian0310567/casebook/internal/core/store"
)

// punct 는 토큰 분리자다. 하이픈·em/en dash 도 분리자로 친다.
var punct = regexp.MustCompile(`[\[\](){}<>"'` + "`" + `,.;:!?/\\|=+*&^%$#@~\-—–\s]+`)

// josa 는 한국어 조사다. 끝에 붙은 것만 자른다.
var josa = regexp.MustCompile(`(을|를|이|가|은|는|에서|에게|부터|까지|으로|로서|로써|로|와|과|의|도|만|이나|나|라도|처럼|보다|한테|께서|이라|라고|든지|하고|해서)$`)

// minTokenBytes 는 토큰 최소 길이다. **바이트** 기준이다 —
// 현행 셸의 awk length() 가 바이트를 세므로, rune 으로 바꾸면 회수 결과가 달라진다.
// 한글 1글자(3바이트)는 통과하고 ASCII 1글자는 탈락하는 비대칭이 의도된 동작이다.
const minTokenBytes = 2

// ExtractKeywords 는 프롬프트에서 검색 키워드를 뽑는다.
// 결과는 바이트 순 정렬 + 중복 제거된다.
func ExtractKeywords(prompt string) []string {
	seen := map[string]bool{}
	var out []string
	for _, tok := range punct.Split(store.NFC(prompt), -1) {
		tok = josa.ReplaceAllString(tok, "")
		if len(tok) < minTokenBytes {
			continue
		}
		tok = strings.ToLower(tok)
		if stopwords[tok] || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	sort.Strings(out) // LC_ALL=C sort -u 와 동일한 바이트 순
	return out
}
