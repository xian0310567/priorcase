package eval

import "github.com/xian0310567/priorcase/internal/core/store"

// ★★★ **그래프 확장을 만들기 전에 천장부터 잰다.**
//
// 자문 문서가 제안하는 graph-assisted recall 은 이렇게 돈다:
//
//	어휘 검색 → 상위 N 씨앗 → 1홉 이웃 → 재랭킹 → 주입
//
// 그 구조가 도울 수 있는 **최대치**는 정해져 있다: *정답이 지금 주입되지 않지만
// 주입되는 것들의 1홉 이웃인* 질의의 비율이다. 그보다 잘할 수는 없다 — 이웃이
// 아니면 확장이 닿지 않는다.
//
// 이 수가 작으면 **만들 이유가 없다.** 그래프 빌더·재랭킹·안전장치 세 개를 넣고
// A/B 를 돌리는 비용이 천장보다 크다.
//
// # ★★★ 실제로 만들어 재 봤고, **기각했다** (2026-09-07)
//
// 천장이 30% 라 만들 값이 있어 보였다. 자문 문서의 설계 그대로 구현했다 —
// 어휘 상위 5를 씨앗으로 1홉 이웃을 뽑고, 독립 씨앗 2표 이상을 요구하고,
// 그래프만으로 올라온 후보는 최종 세 자리 중 하나까지만 차지하게 했다.
//
// **① 확장(후보 더하기)은 변화가 정확히 0 이었다.** MRR 0.251 → 0.251, 새로 찾음 0.
// 질의에 대화 맥락을 붙인 뒤로 어휘 후보 풀이 수백 건이라 **이웃이 이미 전부
// 풀 안에 있다.** 놓친 정답은 후보에 *없어서*가 아니라 *밀려서* 안 나온다.
//
// **② 재랭킹(풀 안 이웃에 가점)은 단조롭게 나빠졌다.** 가점을 훑었다:
//
//	가점   MRR            질의 단위 좋아짐/나빠짐
//	 2    0.251→0.248     0 / 0
//	 5    0.251→0.247     1 / 3
//	10    0.251→0.226     4 / 9
//	20    0.251→0.207     4 / 14
//	40    0.251→0.152     6 / 21
//
// **신호를 세게 줄수록 손해가 커진다.** 이 볼트는 링크가 876개에 평균 차수 2.6 이라
// "씨앗의 이웃" 이 너무 넓다 — 정답보다 잡음을 먼저 끌어올린다. 30% 천장은
// *정답이 이웃 안에 있다* 는 뜻이었지 *이웃 중에 정답이 많다* 는 뜻이 아니었다.
//
// 그래서 **기제는 걷어내고 이 측정기만 남겼다.** 링크 밀도가 다른 볼트에서는
// 다를 수 있다 — 그때 다시 만들지 말지는 이 천장을 먼저 재서 정한다.
//
// # ⚠️ 라벨의 출처 노트를 반드시 빼야 한다
//
// 라벨은 "결정 N 을 쓴 대화에서 N 이 근거로 삼은 노트 T" 다. 그런데 **N 은 지금
// 볼트에 있고 T 를 직접 가리킨다.** N 이 씨앗으로 잡히면 T 는 언제나 1홉이라
// 천장이 100% 로 나온다 — 정답지를 특징으로 쓰는 것이다.
//
// 질의 시점에 N 은 아직 없었다. 그래서 씨앗에서 N 을 빼고 잰다.

// Neighborhood 는 stem → 1홉 이웃(양방향)이다.
func Neighborhood(notes []store.Note) map[string]map[string]bool {
	exists := make(map[string]bool, len(notes))
	for _, n := range notes {
		exists[n.Stem] = true
	}
	g := make(map[string]map[string]bool, len(notes))
	link := func(a, b string) {
		if g[a] == nil {
			g[a] = map[string]bool{}
		}
		g[a][b] = true
	}
	for _, n := range notes {
		for _, t := range n.Links() {
			if t == n.Stem || !exists[t] {
				continue
			}
			// **양방향으로 넣는다.** 확장은 "이것과 이어진 것" 을 찾는 것이지
			// 링크를 쓴 사람의 방향을 따르는 것이 아니다.
			link(n.Stem, t)
			link(t, n.Stem)
		}
	}
	return g
}

// Ceiling 은 그래프 확장이 도울 수 있는 최대치다.
type Ceiling struct {
	// Groups 는 본 질의 수, AlreadyIn 은 이미 상위3에 정답이 있는 질의 수다.
	Groups, AlreadyIn int
	// Reachable 은 정답이 상위3 밖이지만 **상위3의 1홉 이웃**인 질의 수다.
	// 이것이 그래프 확장이 새로 건질 수 있는 최대치다.
	Reachable int
	// Unreachable 은 1홉으로도 안 닿는 질의 수다. 확장으로는 못 고친다.
	Unreachable int
	// SeedLeak 는 **라벨의 출처 노트가 씨앗에 잡힌** 질의 수다 (이 파일의 ⚠️).
	// 그 노트를 빼지 않으면 천장이 부풀려진다 — 얼마나 부풀 뻔했는지 보여 준다.
	SeedLeak int
}

// GraphCeiling 은 세트 하나의 천장을 잰다.
//
// exclude 는 사례 ID → 씨앗에서 빼야 할 stem 이다. 라벨러가 "이 정답은 어느
// 결정에서 나왔나" 를 알고 있으므로 호출부가 준다.
func GraphCeiling(rep Report, set string, g map[string]map[string]bool, exclude map[string]string) Ceiling {
	var c Ceiling
	type key struct{ set, group string }
	// 같은 질의의 사례들을 묶는다 — 주입은 질의 단위로 일어난다.
	seedsOf := map[key][]string{}
	wantsOf := map[key][]string{}
	skipOf := map[key]map[string]bool{}
	var order []key
	for _, r := range rep.Results {
		if r.Set != set {
			continue
		}
		k := key{r.Set, r.Group}
		if _, ok := seedsOf[k]; !ok {
			order = append(order, k)
			skipOf[k] = map[string]bool{}
		}
		seedsOf[k] = r.TopStems // 같은 질의면 같다
		wantsOf[k] = append(wantsOf[k], r.Want)
		if src := exclude[r.ID]; src != "" {
			skipOf[k][src] = true
		}
	}

	for _, k := range order {
		c.Groups++
		seeds, wants, skip := seedsOf[k], wantsOf[k], skipOf[k]
		in := false
		for _, w := range wants {
			for _, s := range seeds {
				if s == w {
					in = true
				}
			}
		}
		if in {
			c.AlreadyIn++
			continue
		}
		leaked, reach := false, false
		for _, s := range seeds {
			if skip[s] {
				leaked = true
				continue // ⚠️ 라벨의 출처 노트는 질의 시점에 없었다
			}
			for _, w := range wants {
				if g[s][w] {
					reach = true
				}
			}
		}
		if leaked {
			c.SeedLeak++
		}
		if reach {
			c.Reachable++
		} else {
			c.Unreachable++
		}
	}
	return c
}
