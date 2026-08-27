package store

import (
	"regexp"
	"strings"
)

// 볼트에서 **가장 가까운 문서 이름**을 찾는다 — 제안용이다.
//
// `capture` 가 대상 없는 related 를 뺄 때, `health` 가 본문의 깨진 위키링크를
// 보고할 때 둘 다 쓴다. **한 자리에 두는 이유**는 두 곳에서 따로 구현하면 문턱과
// 거리 계산이 갈라져 "capture 는 제안했는데 doctor 는 안 한다" 가 되기 때문이다.
//
// **제안이지 교정이 아니다.** 잘못 이은 링크는 끊긴 링크보다 나쁘다 — 끊긴 것은
// 옵시디언에서 회색으로 보이지만 잘못 이은 것은 아무도 의심하지 않는다.

// NearestSimilarity 는 제안을 낼 최소 닮음이다.
//
// 0.6 은 실볼트 19건을 전부 맞히면서 애매한 후보를 하나도 안 낸 값이다
// (2026-08-27, 같은 문턱으로 돌린 교정 스크립트가 "애매해서 건너뜀 0건").
// 낮추면 무관한 이름을 들이밀고, 그건 없는 것보다 나쁘다 — 사람이 그걸 믿는다.
const NearestSimilarity = 0.6

// stemCore 는 이름에서 **판별력 있는 부분**만 남긴다 — 도메인 접두어와 결정 표식,
// 날짜 꼬리를 뗀다.
//
// # 왜 필요한가
//
// 이 볼트의 규약이 `{domain}-결정-{slug}-{date}` 라서 이름의 절반이 공통 상용구다.
// 그대로 재면 **완전히 무관한 두 이름이 가깝게 보인다** — 테스트가 실제로 잡았다:
// `alpha-결정-완전히다른주제짜장면탕수육-2026-01-01` 과 `alpha-결정-저장엔진-2026-08-01`
// 이 문턱을 넘었다. 공유하는 19글자가 전부 상용구인데도 그렇다.
//
// 떼고 나면 반대로 **좋은 제안이 강해진다.** 실볼트 예:
//
//	editup-decision-3줄요약-유실-우리로그-318대20         (옛 표식·날짜 없음)
//	editup-결정-3줄요약-유실-우리로그-318대20-2026-08-19   (실재)
//
// 상용구를 떼면 slug 가 정확히 같아져 닮음이 1.0 이 된다. 옛 규약·날짜 누락처럼
// **상용구만 어긋난 사례가 실볼트 깨진 링크의 절반**이라 이 정규화가 곧 정확도다.
func stemCore(s string) string {
	if m := stemMarker.FindStringIndex(s); m != nil {
		s = s[m[1]:]
	}
	return stemDate.ReplaceAllString(s, "")
}

var (
	// 앞의 `{domain}-결정-` · `{domain}-decision-` 을 뗀다. 표식 둘 다 받는 이유는
	// 볼트에 두 규약이 섞여 있고, 규약이 어긋난 링크를 잇는 것이 이 함수의 일이라서다.
	stemMarker = regexp.MustCompile(`^[^-]+-(결정|decision)-`)
	// 뒤의 `-YYYY-MM-DD` 를 뗀다.
	stemDate = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)
)

// NearestStem 은 볼트에서 가장 가까운 이름이다. 충분히 가깝지 않으면 빈 문자열이다.
func NearestStem(target string, stems map[string]bool) string {
	t := []rune(strings.ToLower(stemCore(NFC(target))))
	best, bestScore := "", NearestSimilarity
	for s := range stems {
		c := []rune(strings.ToLower(stemCore(s)))
		// 길이가 크게 다르면 볼 것도 없다. 403개 × 60자라 비용이 문제는 아니지만,
		// 이 가지치기가 "짧은 이름이 아무 데나 가깝게 보이는" 것도 같이 막는다.
		if d := len(c) - len(t); d > len(t)/2 || -d > len(t)/2 {
			continue
		}
		dist := editDistance(t, c)
		n := len(t)
		if len(c) > n {
			n = len(c)
		}
		if n == 0 {
			continue
		}
		if score := 1 - float64(dist)/float64(n); score > bestScore {
			best, bestScore = s, score
		}
	}
	return best
}

// editDistance 는 **글자** 단위 레벤슈타인이다.
//
// 바이트로 재면 한글 한 글자가 3바이트라 거리가 세 배로 부풀고, 한 글자 오타가
// 전혀 다른 이름처럼 보인다 — 실볼트 오타가 거의 다 한글 한 글자짜리라 그러면
// 이 기능이 아무 제안도 못 낸다.
func editDistance(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(min(cur[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
