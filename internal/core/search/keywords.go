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
//
// **긴 것을 앞에 둔다.** `$` 로 묶여 있어 대개 알아서 갈리지만, 순서를 지켜 두면
// 나중에 앵커를 건드릴 때 조용히 짧은 쪽이 이기는 일이 없다.
//
// ⚠️ **`에` 가 빠져 있었다.** `에서`·`에게` 는 있는데 맨 `에` 가 없어서, 한국어에서
// 가장 흔한 조사 하나가 그대로 남았다. 실측으로 물렸다: "npm에 배포는 못하는거지" 의
// 키워드가 `npm` 이 아니라 **`npm에`** 로 나와서, 태그에 `npm` 을 달고 있는
// `priorcase-결정-npm배포-OIDC-…` 노트가 head 히트를 하나만 받았다.
//
// 한·영 섞인 볼트에서 이게 특히 아프다. 한국어는 라틴 낱말에 조사를 붙여 쓰므로
// (`npm에`·`git으로`·`API를`) 조사를 못 자르면 **영어 낱말이 통째로 죽는다** —
// 그리고 그 낱말이야말로 볼트에서 가장 잘 걸리는 검색어다.
var josa = regexp.MustCompile(`(에서는|에서|에게|에는|에도|밖에|으로|로서|로써|이라도|라도|이나|이랑|든지|처럼|한테|께서|조차|마저|부터|까지|하고|해서|이라|라고|을|를|이|가|은|는|와|과|의|도|만|나|로|랑|뿐|에)$`)

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

// PromptTokens 는 **불용어를 빼기 전** 토큰 수다. "이건 대화체인가" 를 이것으로 잰다.
//
// # 왜 ExtractKeywords 의 결과로 세면 안 되는가
//
// minHeadHits 는 질의가 대화체면 head 히트 둘을 요구한다. 그 판정을 키워드 수로
// 하고 있었는데, **불용어 목록이 커지면 같은 문장의 키워드 수가 줄어든다.**
// 2026-08-31 에 활용형 불용어를 넣자 실제로 뒤집혔다:
//
//	"무슨 작업을 하다가 멈춘것같은데 확인해줄 수 있어"
//	  전: 키워드 4개(멈춘것같은데·무슨·있어·확인해줄) → 대화체 → 둘 요구 → 후보 0
//	  후: 키워드 3개(멈춘것같은데·작업·확인해줄)     → 골라 넣은 질의로 오판 →
//	      하나만 요구 → `작업` 하나로 후보 22건
//
// 불용어를 **잘 지웠기 때문에** 게이트가 풀린 것이다. 두 장치가 같은 수를 보고
// 서로 반대로 움직이면 한쪽을 고칠 때마다 다른 쪽이 조용히 망가진다.
//
// 원문 토큰 수는 불용어 목록과 무관하다. `prior recall "볼트 동기화"` 는 2 이고
// 위 문장은 7 이라 경계(4)가 둘을 그대로 가른다.
func PromptTokens(prompt string) int {
	n := 0
	for _, raw := range punct.Split(store.NFC(prompt), -1) {
		if len([]rune(raw)) >= minTokenRunes {
			n++
		}
	}
	return n
}
