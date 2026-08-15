// Package search 는 결정 회수를 담당한다.
// 점수 함수는 나중에 임베딩으로 교체할 수 있게 이 패키지 안에 격리한다.
package search

import (
	"regexp"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// punct 는 토큰 분리자다. 하이픈·em/en dash 도 분리자로 친다.
var punct = regexp.MustCompile(`[\[\](){}<>"'` + "`" + `,.;:!?/\\|=+*&^%$#@~\-—–\s]+`)

// josa 는 한국어 조사다. 끝에 붙은 것만 자른다.
var josa = regexp.MustCompile(`(을|를|이|가|은|는|에서|에게|부터|까지|으로|로서|로써|로|와|과|의|도|만|이나|나|라도|처럼|보다|한테|께서|이라|라고|든지|하고|해서)$`)

// minTokenRunes 는 토큰 최소 길이다. **글자** 기준이다.
//
// 예전에는 바이트 기준이었다 — 옛 셸 구현의 `awk length()` 가 바이트를 세서
// 동작을 보존하려던 것이다. 그 결과 한글 1글자(3바이트)가 통과했고, 실측으로
// `prior recall "다"` 가 볼트 66건 중 45건을 반환했다. 조사 제거 뒤 남는 한 글자는
// 검색어가 아니라 잡음이다.
//
// ASCII 1글자는 예전에도 탈락했으므로 이 변경으로 잃는 것은 한글 1음절뿐이다.
const minTokenRunes = 2

// ExtractKeywords 는 프롬프트에서 검색 키워드를 뽑는다.
// 결과는 바이트 순 정렬 + 중복 제거된다.
func ExtractKeywords(prompt string) []string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range punct.Split(store.NFC(prompt), -1) {
		tok := josa.ReplaceAllString(raw, "")
		// **조사를 떼어 너무 짧아지면 뗀 것이 조사가 아니었다.** 원형을 쓴다.
		//
		// 안 그러면 낱말이 통째로 사라진다: "경로" → "경"(1글자) → 탈락.
		// 실측으로 `prior recall "경로"` 가 볼트 전체에서 0건이었고, 노트에
		// `경로` 태그를 달아도 그 낱말로는 영영 못 찾았다. 같은 부류가 흔하다 —
		// 파이(→파)·정도(→정)·포도(→포)·사과(→사). 전부 끝 글자가 조사와 겹치는
		// 평범한 명사다.
		if len([]rune(tok)) < minTokenRunes {
			tok = raw
		}
		if len([]rune(tok)) < minTokenRunes {
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
