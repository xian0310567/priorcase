// Package stats 는 **볼트가 좋은 기억을 만들고 있는지** 잰다.
//
// # doctor·eval 과 무엇이 다른가
//
//	doctor  서류가 정합한가        링크가 걸리나 · 스키마가 맞나 · 훅이 배선됐나
//	eval    회수가 정답을 꺼내나    질의를 넣고 순위를 본다
//	stats   기억이 자라고 있나      결과를 아는가 · 이어져 있는가 · 고아인가
//
// 셋 다 초록인데 이 시스템이 쓸모없을 수 있다. 결정을 685건 쌓아도 **결과가 적힌
// 것이 1.3%** 면 그건 기억이 아니라 로그다. doctor 는 그동안 계속 초록이었다.
//
// # 단일 점수를 만들지 않는다
//
// `볼트 점수 82/100` 같은 수는 **무엇을 고쳐야 하는지를 숨긴다.** 고아 비율과
// 결과 기록률은 성질이 다른 문제이고 고치는 방법도 다르다. 하나로 접으면 올라간
// 이유도 내려간 이유도 못 읽는다. 지표를 그대로 보여 준다.
//
// # 볼트를 고치지 않는다
//
// 읽기만 한다. 이 명령이 쓰기를 하기 시작하면 "재는 행위가 재는 대상을 바꾸는"
// 상태가 되고, 그러면 두 번째 실행이 첫 번째와 다른 것을 잰다.
package stats

import (
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// Report 는 볼트 하나의 상태다.
type Report struct {
	Decisions Decisions `json:"decisions"`
	Learning  Learning  `json:"learning"`
	Graph     Graph     `json:"graph"`
	Quality   Quality   `json:"quality"`
}

// Decisions 는 결정의 수와 상태 분포다.
type Decisions struct {
	Total      int `json:"total"`
	Active     int `json:"active"`
	Superseded int `json:"superseded"`
	Regretted  int `json:"regretted"`
	Retracted  int `json:"retracted"`
	// Other 는 우리가 모르는 status 다. **0 이 아니면 판이 갈렸다는 신호다** —
	// 더 새 priorcase 가 쓴 노트이거나 사람이 손으로 적은 값이다.
	Other int `json:"other"`
}

// Learning 은 **판단이 학습으로 바뀌고 있는가**다.
//
// 이 무리가 이 패키지의 존재 이유다. 결정을 쌓는 것은 쉽고, 그 결정이 맞았는지
// 나중에 아는 것은 아무도 안 한다 — 실측으로 결과가 적힌 결정이 157건 중 2건이었다.
type Learning struct {
	OutcomeKnown   int `json:"outcome_known"`
	OutcomePending int `json:"outcome_pending"`

	// 아래 셋은 **결과를 판정할 재료가 있는가**다 (store.Meta 의 §).
	// 기대를 안 적어 두면 나중 회고가 지금 아는 것으로 되짚게 된다.
	WithExpected      int `json:"with_expected"`
	WithSuccessMetric int `json:"with_success_metric"`
	WithReviewAfter   int `json:"with_review_after"`

	// ReviewsDue 는 review_after 가 지났는데 결과가 아직 없는 결정이다.
	ReviewsDue int `json:"reviews_due"`
	// Retrospectives 는 본문에 회고가 적힌 결정 수다.
	Retrospectives int `json:"retrospectives"`
}

// Graph 는 결정들이 **서로 이어져 있는가**다.
//
// 고아 결정은 회수에서 불리하고(교차 프로젝트 전이가 링크를 타고 일어난다) 무엇보다
// "이 판단이 무엇 위에 서 있나" 를 잃는다. 실측으로 링크 작성률이 떨어지고 있었다:
// 2026-08 53.8% → 2026-09 29.8%.
type Graph struct {
	// Orphans 는 **들어오는 링크도 나가는 링크도 없는** 결정이다.
	Orphans int `json:"orphans"`
	// Edges 는 볼트 안 결정끼리의 링크 수다 (중복 제거, 방향 있음).
	Edges int `json:"edges"`
	// AvgDegree / MedianDegree 는 결정 하나가 닿는 다른 결정 수다 (양방향 합).
	AvgDegree    float64 `json:"avg_degree"`
	MedianDegree int     `json:"median_degree"`
	// Dangling 은 **볼트에 없는 것을 가리키는** 링크다. doctor 가 고치라고 말한다.
	Dangling int `json:"dangling"`
	// CrossDomain 은 도메인을 넘는 링크 수다 — 프로젝트를 잇는 유일한 수단이다.
	CrossDomain int `json:"cross_domain"`
}

// Quality 는 데이터 자체의 상태다.
type Quality struct {
	Unreadable int `json:"unreadable"`
	References int `json:"references"`
	Rules      int `json:"rules"`
	// NoTags 는 태그가 `decision` 표식뿐인 결정이다 — 회수 어휘를 하나도 안 넓힌다.
	NoTags int `json:"no_tags"`
}

// Input 은 이미 읽어 둔 볼트다.
//
// **여기서 디스크를 읽지 않는다.** 호출부가 이미 Corpus 를 들고 있고, 다시 읽으면
// 같은 볼트를 두 번 훑는다 — 결정 685건에서 그것이 초 단위다.
type Input struct {
	Decisions  []store.Note
	References []store.Note
	Rules      []store.Note
	Skipped    []store.SkippedNote
	// Today 는 YYYY-MM-DD 다. review_after 비교에 쓴다 — **호출부가 준다.**
	// 여기서 time.Now 를 부르면 같은 볼트가 날마다 다른 수를 낸다는 것이
	// 코드에서 안 보인다.
	Today string

	// ★ VaultStems 는 **볼트에 실재하는 모든 문서**의 stem 이다 (store.AllStems).
	//
	// 끊긴 링크를 세려면 이것이 있어야 한다. 결정·참고·규칙만 알고 세면 작업
	// 로그처럼 **실재하지만 회수 대상이 아닌 문서**를 가리키는 링크가 전부
	// 끊긴 것으로 나온다 — 실볼트에서 그 오차가 36건이었고, doctor 는 같은 볼트를
	// "링크가 전부 걸린다" 로 보고 있었다. 두 진단이 반대로 말하면 사람은 둘 다
	// 안 믿는다.
	//
	// nil 이면 끊긴 링크를 **세지 않는다**(0). 모르는 것을 0 으로 두는 것과
	// 틀린 수를 내는 것 중에는 앞이 낫다.
	VaultStems map[string]bool
}

// Compute 는 보고를 만든다. 볼트를 건드리지 않는다.
func Compute(in Input) Report {
	var r Report
	r.Quality.Unreadable = len(in.Skipped)
	r.Quality.References = len(in.References)
	r.Quality.Rules = len(in.Rules)

	stems := make(map[string]bool, len(in.Decisions))
	domainOf := make(map[string]string, len(in.Decisions))
	for _, n := range in.Decisions {
		stems[n.Stem] = true
		if len(n.Meta.Domain) > 0 {
			domainOf[n.Stem] = n.Meta.Domain[0]
		}
	}

	deg := make(map[string]int, len(in.Decisions))
	seenEdge := map[string]bool{}

	for _, n := range in.Decisions {
		r.Decisions.Total++
		switch n.Meta.Status {
		case "active":
			r.Decisions.Active++
		case "superseded":
			r.Decisions.Superseded++
		case "regretted":
			r.Decisions.Regretted++
		case "retracted":
			r.Decisions.Retracted++
		default:
			r.Decisions.Other++
		}

		if n.Meta.Outcome == "pending" || n.Meta.Outcome == "" {
			r.Learning.OutcomePending++
		} else {
			r.Learning.OutcomeKnown++
		}
		if strings.TrimSpace(n.Meta.Expected) != "" {
			r.Learning.WithExpected++
		}
		if strings.TrimSpace(n.Meta.SuccessMetric) != "" {
			r.Learning.WithSuccessMetric++
		}
		if d := strings.TrimSpace(n.Meta.ReviewAfter); d != "" {
			r.Learning.WithReviewAfter++
			// **결과가 이미 적힌 것은 안 센다** — retro 의 큐와 같은 규칙이다.
			// 여기서만 다르게 세면 "밀린 회고 7건" 이 큐의 수와 안 맞는다.
			if in.Today != "" && d <= in.Today &&
				(n.Meta.Outcome == "pending" || n.Meta.Outcome == "") {
				r.Learning.ReviewsDue++
			}
		}
		if hasRetro(n.Body) {
			r.Learning.Retrospectives++
		}
		if len(contentTags(n.Meta.Tags)) == 0 {
			r.Quality.NoTags++
		}

		for _, tgt := range n.Links() {
			if tgt == n.Stem {
				continue
			}
			if !stems[tgt] {
				// 결정이 아닌 대상(규칙·참고·작업 로그·기획 문서)은 끊긴 것이
				// 아니다. 볼트에 **없을 때만** 센다 (Input.VaultStems 의 §).
				if in.VaultStems != nil && !in.VaultStems[tgt] {
					r.Graph.Dangling++
				}
				continue
			}
			key := n.Stem + "\x00" + tgt
			if seenEdge[key] {
				continue
			}
			seenEdge[key] = true
			r.Graph.Edges++
			deg[n.Stem]++
			deg[tgt]++
			if a, b := domainOf[n.Stem], domainOf[tgt]; a != "" && b != "" && a != b {
				r.Graph.CrossDomain++
			}
		}
	}

	degrees := make([]int, 0, len(in.Decisions))
	sum := 0
	for _, n := range in.Decisions {
		d := deg[n.Stem]
		if d == 0 {
			r.Graph.Orphans++
		}
		degrees = append(degrees, d)
		sum += d
	}
	if len(degrees) > 0 {
		sort.Ints(degrees)
		r.Graph.AvgDegree = float64(sum) / float64(len(degrees))
		r.Graph.MedianDegree = degrees[len(degrees)/2]
	}
	return r
}

// hasRetro 는 본문에 회고 절이 **내용과 함께** 있는지 본다.
//
// 제목만 있고 비어 있는 것은 안 센다 — capture 의 기본 본문 틀에 `## 회고` 가
// 들어 있어서, 제목만 세면 **전건이 회고를 가진 것으로 나온다.**
func hasRetro(body []byte) bool {
	lines := strings.Split(string(body), "\n")
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "#") || !strings.Contains(t, "회고") &&
			!strings.Contains(strings.ToLower(t), "retro") {
			continue
		}
		for _, rest := range lines[i+1:] {
			s := strings.TrimSpace(rest)
			if s == "" {
				continue
			}
			if strings.HasPrefix(s, "#") {
				break // 다음 절 — 이 절은 비어 있다
			}
			return true
		}
	}
	return false
}

// contentTags 는 `decision` 표식을 뺀 태그다. 회수가 head 에 싣는 것과 같은 기준이다.
func contentTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "", "decision", "결정", "rule":
		default:
			out = append(out, t)
		}
	}
	return out
}

// Pct 는 백분율이다. 분모가 0 이면 0 이다 — 새 볼트에서 NaN 이 나가면 앱이 터진다.
func Pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(n) / float64(total)
}
