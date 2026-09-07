package stats

import (
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// ★★★ **규칙 후보는 "실패가 반복됐다" 로 못 찾는다 — 재 보고 알았다.**
//
// 자문 문서의 설계는 이랬다: *같은 관련 cluster 에서 regretted/bad outcome 결정이
// 3개 이상이면 규칙 후보로 올린다.*
//
// 실볼트에서 그 조건이 **0건** 걸린다 (2026-09-07, 결정 687건):
//
//	outcome   pending 658 · good 24 · bad 5
//	status    active 636 · superseded 44 · retracted 7 · regretted 0
//	→ 한 군집에 bad/regretted 3건 이상인 군집: **0개**
//
// 두 가지가 겹쳐 그렇다. ① 결과가 적힌 결정이 4% 뿐이라 실패를 셀 재료가 없다.
// ② 군집이 단위가 못 된다 — 연결 요소 246개 중 하나가 **311건짜리 덩어리**이고
// 나머지 211개가 고아다. 그 덩어리에서 후보를 뽑으면 lesson 태그만 157건이 걸린다.
//
// # 실제로 있는 신호는 lesson 태그다
//
// MCP 지침이 *"프로젝트를 넘어 쓰일 교훈이면 lesson 을 넣는다"* 라고 시키고 있고,
// **211건이 그 태그를 달고 있다.** 그런데 규칙은 12건뿐이다. 211 → 12 의 깔때기가
// 안 돌고 있는 것이다 — 교훈이라고 표시해 놓고 아무도 증류하지 않는다.
//
// 그래서 후보를 이렇게 찾는다: **아직 어느 규칙의 출처도 아닌 lesson 결정을,
// 함께 달린 태그로 묶어 보여 준다.** 묶음이 곧 주제이고, 그 주제가 규칙 후보다.
//
// # 규칙을 만들지 않는다
//
// 후보 **목록**까지다. 규칙은 도메인이 없어 어느 프로젝트에서 물어도 걸리므로
// 틀렸을 때의 반경이 결정보다 훨씬 크다. 자동으로 쓰면 그 반경을 기계가 정하는
// 것이고, 그건 이 계층을 만든 이유를 뒤집는다.

// RuleCandidate 는 규칙이 될 만한 주제 하나다.
type RuleCandidate struct {
	// Tag 는 이 묶음을 이루는 태그다 — 사람이 직접 단 것이라 주제 이름으로 쓸 만하다.
	Tag string `json:"tag"`
	// Stems 는 그 태그를 단 lesson 결정들이다. 최근 것이 앞.
	Stems []string `json:"stems"`
	// Summaries 는 같은 순서의 요약이다. 후보를 판단하려면 제목만으로는 모자란다.
	Summaries []string `json:"summaries"`
	// Domains 는 이 묶음이 걸친 도메인 수다. **여럿일수록 규칙에 가깝다** —
	// 한 프로젝트에만 있는 교훈은 그 프로젝트의 결정으로 두는 편이 낫다.
	Domains int `json:"domains"`
}

// minCandidate 는 후보로 볼 최소 건수다.
//
// 셋인 이유는 자문 문서와 같다 — 둘은 우연일 수 있고, 셋이면 되풀이다.
// 실볼트에서 이 값으로 후보가 몇 개 나오는지는 `prior rules` 가 보여 준다.
const minCandidate = 3

// RuleCandidates 는 아직 규칙이 안 된 교훈 묶음을 준다.
//
// rules 의 출처 결정은 **뺀다** — 이미 증류된 것을 다시 후보로 올리면 목록이
// 줄지 않고, 줄지 않는 목록은 아무도 안 본다.
func RuleCandidates(decisions, rules []store.Note) []RuleCandidate {
	done := map[string]bool{}
	for _, r := range rules {
		for _, src := range r.Links() {
			done[src] = true
		}
	}

	type entry struct {
		stem, summary, date, domain string
	}
	byTag := map[string][]entry{}
	for _, n := range decisions {
		if done[n.Stem] || !hasTag(n.Meta.Tags, "lesson") {
			continue
		}
		// **철회된 것은 교훈이 아니다.** "애초에 결정이 아니었다" 는 뜻이라
		// 거기서 규칙을 뽑으면 없던 일에서 규칙을 만드는 것이다.
		if n.Meta.Status == store.StatusRetracted {
			continue
		}
		d := ""
		if len(n.Meta.Domain) > 0 {
			d = n.Meta.Domain[0]
		}
		e := entry{n.Stem, n.Meta.Summary, n.Meta.Date, d}
		for _, t := range contentTags(n.Meta.Tags) {
			t = strings.ToLower(strings.TrimSpace(t))
			if t == "lesson" {
				continue // 묶는 축 자체는 주제가 아니다
			}
			byTag[t] = append(byTag[t], e)
		}
	}

	out := make([]RuleCandidate, 0, len(byTag))
	for tag, es := range byTag {
		if len(es) < minCandidate {
			continue
		}
		sort.Slice(es, func(i, j int) bool {
			if es[i].date != es[j].date {
				return es[i].date > es[j].date
			}
			return es[i].stem < es[j].stem
		})
		doms := map[string]bool{}
		c := RuleCandidate{Tag: tag}
		for _, e := range es {
			c.Stems = append(c.Stems, e.stem)
			c.Summaries = append(c.Summaries, e.summary)
			if e.domain != "" {
				doms[e.domain] = true
			}
		}
		c.Domains = len(doms)
		out = append(out, c)
	}
	// **도메인을 많이 걸친 것이 먼저다.** 규칙의 값은 프로젝트를 넘는 데 있다 —
	// 한 프로젝트 안의 교훈은 그 프로젝트 결정으로 두는 편이 낫다.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Domains != out[j].Domains {
			return out[i].Domains > out[j].Domains
		}
		if len(out[i].Stems) != len(out[j].Stems) {
			return len(out[i].Stems) > len(out[j].Stems)
		}
		return out[i].Tag < out[j].Tag
	})
	return out
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if strings.EqualFold(strings.TrimSpace(t), want) {
			return true
		}
	}
	return false
}
