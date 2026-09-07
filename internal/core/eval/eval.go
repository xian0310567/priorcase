// Package eval 은 **회수가 실제로 정답을 꺼내는지** 잰다.
//
// # 왜 상설 패키지인가
//
// 이 자리는 두 번 임시로 만들어졌고 두 번 다 버려졌다. 2026-08-27 의 길이 편향
// 측정은 테스트 안에 녹아 있어 다른 질문에 못 썼고, 2026-09-02 의 `cmd/rankprobe`
// 는 측정을 마치고 삭제됐다 — 그 노트가 스스로 *"수치보다 평가 하네스가 자산이다"*
// 라고 적어 놓고 도구를 버렸다. 그래서 같은 질문("맥락을 붙이면 나빠지는가")을
// 다시 물었을 때 처음부터 다시 만들어야 했다.
//
// # 왜 다시 재야 하는가 — 상수가 볼트 크기에 묶여 있다
//
// 점수식 상수(refHeadRunes·weightSynonym·rareDFRatio)는 전부 **그날의 볼트 스냅샷**
// 에서 골랐다. 볼트가 자라면 문서빈도 분포가 통째로 움직인다. 실측된 드리프트가
// 이렇다: `지라` 의 df 가 결정 414건 시절 2.0% 였다가 649건에서 **5.2%** 가 됐고,
// 변별어 경계 3.0% 를 넘어가면서 그 낱말로 걸리던 규칙이 조용히 안 걸리게 됐다.
//
// 즉 이 하네스는 한 번 쓰고 버리는 것이 아니라 **볼트가 커질 때마다 도는 자**다.
//
// # 세 가지 세트를 쓰고, 셋을 섞어 하나의 수로 만들지 않는다
//
//	slug/요약뒤쪽  합성    정답이 확실하고 어휘가 촘촘하다 → 회귀 가드
//	링크쌍         프록시  사람이 이은 A→B. 자동 생성되지만 **낙관적이다**
//	실프롬프트     실물    사람이 실제로 친 말. 라벨을 손으로 붙여야 한다
//
// 프록시 하나만 보면 결론이 낙관으로 기운다는 것을 이 프로젝트가 이미 두 번 겪었다
// (`규칙-대리지표는-실물로-다시-잰다`). 같은 변경에서 링크쌍은 어휘 미스 29.8% 를,
// 실물 라벨은 76.9% 를 냈다 — **2.6배**다. 그래서 Report 는 세트별로만 말하고
// 종합 점수를 만들지 않는다.
package eval

import (
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// Case 는 "이 질의를 넣으면 이 노트가 나와야 한다" 한 건이다.
type Case struct {
	// Set 은 어느 데이터셋에서 왔는지다. 보고가 세트별로 갈리므로 필수다.
	Set string
	// ID 는 실행 사이에 **안정적인** 키다. A/B 대조가 이걸로 짝을 짓는다 —
	// 순서로 짝지으면 세트 하나가 한 건 늘어난 순간 전체가 어긋난다.
	ID string
	// Query 는 회수에 넣을 말이다.
	Query string
	// Cwd 는 그 질의가 어느 프로젝트에서 나왔는지다. 비어 있으면 cwd 가점이 없다.
	Cwd string
	// Context 는 그 질의를 둘러싼 대화다 (search.Options.Context).
	//
	// **Query 에 이어 붙이지 않고 따로 둔다.** 붙여 버리면 맥락 낱말과 프롬프트
	// 낱말이 구별되지 않아 무게를 달리 줄 수 없고, 그 무게가 재려는 것 중 하나다.
	Context string
	// Want 는 정답 노트의 stem 이다.
	Want string
	// ★ Group 은 **같은 질의를 공유하는 사례들**을 묶는 키다. 비어 있으면 ID 다.
	//
	// # 왜 필요한가 — 쌍 단위로 세면 개선이 손해로 보인다
	//
	// 프롬프트 하나에 정답이 여럿인 것이 보통이다(그 결정이 근거로 삼은 노트가
	// 여럿이었으니까). 그런데 주입은 **세 자리뿐**이라, 정답 A 가 2위였다가 B·C 가
	// 1·2위로 올라오면서 A 가 4위로 밀리면 쌍 단위로는 "새로 찾음 2 · 밀려남 1" 이다.
	// 실제로는 **주입되는 정답이 하나에서 둘로 늘어난** 것인데 손해가 하나 적힌다.
	//
	// 실측으로 그 차이가 컸다. 같은 변경을 두 자로 재니:
	//
	//	쌍 단위    상위3 진입 28 · 밀려남 7   (4:1)
	//	프롬프트 단위  좋아짐 18 · 나빠짐 2   (9:1)
	//
	// 사람이 겪는 것은 뒤쪽이다 — 한 프롬프트에서 주입 세 줄을 받고, 그 안에
	// 쓸 만한 것이 있느냐 없느냐다.
	Group string
}

// Result 는 한 사례의 결과다. Rank 0 은 **상한 안에서 못 찾았다**는 뜻이다.
type Result struct {
	ID    string
	Set   string
	Group string
	Query string
	Want  string
	Rank  int
	// Top 은 실제로 1위를 먹은 노트다. 왜 밀렸는지 보려면 이게 있어야 한다.
	Top string
	// TopStems 는 **실제로 주입되는 줄**이다 (상위 3). 그래프 확장의 천장을 재려면
	// "무엇이 씨앗이 되나" 를 알아야 한다 (GraphCeiling 의 §).
	TopStems []string

	// ★ Scores 는 상위 몇 건의 **점수**다 (topScores 개까지).
	//
	// 순위만 들고 있으면 "주입할 만한가" 를 물을 수 없다. 지금 회수는
	// `MinScore=1 · Limit=3` 고정이라 **정답이 없어도 매번 세 줄을 낸다** —
	// 게이트를 넘는 후보가 중앙 67건이므로 사실상 언제나 채워진다. 그 세 줄이
	// 쓸 만한지 판정하려면 점수의 모양(1위가 얼마나 센가 · 2·3위와 벌어졌나)이
	// 필요하다.
	Scores []int
	// PromptRares 는 Scores 와 같은 순서의 변별어 히트 수다.
	PromptRares []int
	// Terms 는 그 질의가 실제로 쓴 낱말 수다. 점수는 낱말 수에 비례해 커지므로
	// (맥락을 붙이면 특히), 절대 점수만으로는 질의끼리 견줄 수 없다.
	Terms int
}

// topScores 는 Result.Scores 에 담는 상위 건수다. 상위 3이 주입되고 그 바로
// 아래(4위)가 "1위가 도드라지나" 를 재는 기준선이라 넷이면 되는데, 하나 더 담아
// 꼬리의 모양까지 본다.
const topScores = 5

// SetReport 는 세트 하나의 요약이다.
type SetReport struct {
	Set  string
	N    int
	Miss int
	At1  int
	At3  int
	MRR  float64

	// ── 프롬프트(그룹) 단위 ────────────────────────────────────────
	// Groups 는 서로 다른 질의 수, GroupAny3 은 그중 **상위3에 정답이 하나라도
	// 든** 질의 수다. 주입이 세 줄이므로 사람이 실제로 겪는 수는 이쪽이다
	// (Case.Group 의 §).
	Groups    int
	GroupAny3 int
	GroupAny  int
}

// Report 는 실행 하나의 전부다.
type Report struct {
	Sets    []SetReport
	Results []Result
}

// Ranked 는 순위 한 칸이다.
type Ranked struct {
	Stem  string
	Score int
	// PromptRare 는 **사람이 친 낱말 중 변별어로 걸린 수**다
	// (search.Hit.PromptRare 의 §). 점수와 달리 볼트 크기·낱말 수에 안 끌려다녀서
	// 문턱 후보로 견줄 값이 있다.
	PromptRare int
}

// Ranker 는 질의 하나를 돌려 순위와 점수를 준다. Terms 는 그 질의가 실제로 쓴
// 낱말 수다 (Result.Terms 의 §).
//
// **함수로 받는 이유는 이 패키지가 검색 구현을 모르게 하려는 것이다.** A/B 는
// 호출부가 서로 다른 Ranker 를 넘겨 만든다 — 여기에 옵션 분기를 두기 시작하면
// 이 하네스가 재려는 대상을 자기가 흉내 내게 된다.
type Ranker func(c Case) (hits []Ranked, terms int)

// Run 은 사례를 전부 돌려 보고를 만든다.
func Run(cases []Case, rank Ranker) Report {
	var rep Report
	agg := map[string]*SetReport{}
	var order []string
	for _, c := range cases {
		got, terms := rank(c)
		g := c.Group
		if g == "" {
			g = c.ID
		}
		r := Result{ID: c.ID, Set: c.Set, Group: g, Query: c.Query, Want: c.Want, Terms: terms}
		if len(got) > 0 {
			r.Top = got[0].Stem
		}
		for i := 0; i < len(got) && i < topScores; i++ {
			r.Scores = append(r.Scores, got[i].Score)
			r.PromptRares = append(r.PromptRares, got[i].PromptRare)
		}
		for i := 0; i < len(got) && i < 3; i++ {
			r.TopStems = append(r.TopStems, got[i].Stem)
		}
		for i, h := range got {
			if h.Stem == c.Want {
				r.Rank = i + 1
				break
			}
		}
		rep.Results = append(rep.Results, r)

		s, ok := agg[c.Set]
		if !ok {
			s = &SetReport{Set: c.Set}
			agg[c.Set] = s
			order = append(order, c.Set)
		}
		s.N++
		switch {
		case r.Rank == 0:
			s.Miss++
		default:
			s.MRR += 1 / float64(r.Rank)
			if r.Rank == 1 {
				s.At1++
			}
			if r.Rank <= 3 {
				s.At3++
			}
		}
	}
	// 그룹 단위는 전건을 본 뒤에야 셀 수 있다 — 같은 그룹의 사례가 흩어져 있다.
	type gk struct{ set, group string }
	any3 := map[gk]bool{}
	anyF := map[gk]bool{}
	for _, r := range rep.Results {
		k := gk{r.Set, r.Group}
		if _, ok := any3[k]; !ok {
			any3[k], anyF[k] = false, false
		}
		if r.Rank > 0 {
			anyF[k] = true
			if r.Rank <= 3 {
				any3[k] = true
			}
		}
	}
	for k, ok := range any3 {
		s := agg[k.set]
		s.Groups++
		if ok {
			s.GroupAny3++
		}
		if anyF[k] {
			s.GroupAny++
		}
	}
	for _, k := range order {
		s := agg[k]
		if s.N > 0 {
			s.MRR /= float64(s.N)
		}
		rep.Sets = append(rep.Sets, *s)
	}
	return rep
}

// Delta 는 기준선 대비 변화다.
//
// ★ **이 구조체가 이 패키지의 존재 이유다.** 새 알고리즘을 넣을지 정하는 기준은
// "MRR 이 올랐나" 가 아니라 **"올랐고, 밀어낸 것이 없나"** 다. 회수 슬롯이 3개뿐이라
// 순위 하락이 곧 탈락이고, 평균은 그 탈락을 숨긴다 — 정답 열 개를 4위에서 3위로
// 올리면서 하나를 3위에서 4위로 떨어뜨리면 MRR 은 오르는데 **잃은 그 하나가 실제
// 피해**다. `규칙-대리지표는-실물로-다시-잰다` 가 같은 것을 말한다.
type Delta struct {
	Set string
	// Gained 는 못 찾던 것을 찾게 된 건수, Lost 는 찾던 것을 놓친 건수다.
	Gained, Lost int
	// IntoTop3 / OutOfTop3 은 **주입되는가**의 변화다. 상위 3이 곧 주입이라
	// 이쪽이 MRR 보다 직접적이다.
	IntoTop3, OutOfTop3 int
	// Better / Worse 는 순위가 움직인 건수다 (찾은 것끼리만).
	Better, Worse int
	// MRRBefore / MRRAfter 는 같은 세트의 MRR 이다.
	MRRBefore, MRRAfter float64
	// ★ GroupBetter / GroupWorse 는 **프롬프트 단위**로 상위3 안의 정답 수가
	// 늘었나 줄었나다. 판정은 이 두 수로 한다 (Case.Group 의 §).
	GroupBetter, GroupWorse int

	// Regressions 는 나빠진 사례다. 수만 보면 무엇을 잃었는지 모른다.
	Regressions []Regression
}

// Regression 은 나빠진 사례 하나다.
type Regression struct {
	ID                  string
	Query               string
	Want                string
	Before, After       int
	TopBefore, TopAfter string
}

// Diff 는 기준선과 이번 판을 세트별로 견준다. 양쪽에 다 있는 ID 만 본다 —
// 한쪽에만 있는 사례는 볼트가 바뀐 것이지 알고리즘이 바뀐 것이 아니다.
func Diff(before, after Report) []Delta {
	bi := map[string]Result{}
	for _, r := range before.Results {
		bi[r.ID] = r
	}
	bm := map[string]float64{}
	for _, s := range before.Sets {
		bm[s.Set] = s.MRR
	}
	am := map[string]float64{}
	for _, s := range after.Sets {
		am[s.Set] = s.MRR
	}

	agg := map[string]*Delta{}
	var order []string
	for _, a := range after.Results {
		b, ok := bi[a.ID]
		if !ok {
			continue
		}
		d, ok := agg[a.Set]
		if !ok {
			d = &Delta{Set: a.Set, MRRBefore: bm[a.Set], MRRAfter: am[a.Set]}
			agg[a.Set] = d
			order = append(order, a.Set)
		}
		switch {
		case b.Rank == 0 && a.Rank > 0:
			d.Gained++
		case b.Rank > 0 && a.Rank == 0:
			d.Lost++
		case a.Rank < b.Rank:
			d.Better++
		case a.Rank > b.Rank:
			d.Worse++
		}
		bTop3, aTop3 := b.Rank > 0 && b.Rank <= 3, a.Rank > 0 && a.Rank <= 3
		switch {
		case !bTop3 && aTop3:
			d.IntoTop3++
		case bTop3 && !aTop3:
			d.OutOfTop3++
		}
		// **놓친 것과 상위3에서 밀려난 것만 사례로 남긴다.** 4위가 5위가 된 것은
		// 주입에 영향이 없고, 그것까지 실으면 진짜 피해가 목록에 묻힌다.
		if (b.Rank > 0 && a.Rank == 0) || (bTop3 && !aTop3) {
			d.Regressions = append(d.Regressions, Regression{
				ID: a.ID, Query: a.Query, Want: a.Want,
				Before: b.Rank, After: a.Rank,
				TopBefore: b.Top, TopAfter: a.Top,
			})
		}
	}
	// 그룹 단위 비교. 양쪽에 다 있는 사례만 세는 것은 위와 같다.
	type gk struct{ set, group string }
	bg, ag := map[gk]int{}, map[gk]int{}
	for _, r := range before.Results {
		k := gk{r.Set, r.Group}
		if _, ok := bg[k]; !ok {
			bg[k] = 0
		}
		if r.Rank > 0 && r.Rank <= 3 {
			bg[k]++
		}
	}
	for _, r := range after.Results {
		if _, ok := bi[r.ID]; !ok {
			continue
		}
		k := gk{r.Set, r.Group}
		if _, ok := ag[k]; !ok {
			ag[k] = 0
		}
		if r.Rank > 0 && r.Rank <= 3 {
			ag[k]++
		}
	}
	for k, a := range ag {
		b, ok := bg[k]
		d := agg[k.set]
		if !ok || d == nil {
			continue
		}
		switch {
		case a > b:
			d.GroupBetter++
		case a < b:
			d.GroupWorse++
		}
	}
	out := make([]Delta, 0, len(order))
	for _, k := range order {
		out = append(out, *agg[k])
	}
	return out
}

// ── 데이터셋 ──────────────────────────────────────────────────────────

// SlugCases 는 파일명 slug 로 자기 자신을 찾는 질의다.
//
// **길이에 중립적이다** — 요약이 길든 짧든 slug 는 비슷한 길이라, 길이 정규화를
// 만질 때 이 세트가 안 흔들리면 정확한 질의는 안 깨진 것이다.
func SlugCases(notes []store.Note) []Case {
	var out []Case
	for _, n := range notes {
		q := SlugQuery(n.Stem)
		if q == "" {
			continue
		}
		out = append(out, Case{Set: "slug", ID: "slug:" + n.Stem, Query: q, Want: n.Stem})
	}
	return out
}

// TailCases 는 요약의 **마지막 1/3** 으로 자기 자신을 찾는 질의다.
//
// 이 세트가 따로 있어야 "요약을 N자로 잘라 head 를 만든다" 는 안을 기각할 근거가
// 생긴다 — 절단선 뒤의 낱말이 head 에서 사라지는 것을 재는 자리가 여기다.
func TailCases(notes []store.Note) []Case {
	var out []Case
	for _, n := range notes {
		q := TailQuery(n.Meta.Summary)
		if q == "" {
			continue
		}
		out = append(out, Case{Set: "요약뒤쪽", ID: "tail:" + n.Stem, Query: q, Want: n.Stem})
	}
	return out
}

// LinkCases 는 **사람이 이은 A→B** 를 질의로 만든다. A 의 요약으로 물어 B 를
// 찾는지 본다.
//
// ⚠️ **이 세트는 낙관적이다.** 질의가 볼트 어휘로 촘촘히 쓰인 요약 문장이라 어휘
// 기반 랭커에 유리하다. 같은 변경에서 이 세트가 어휘 미스 29.8% 를 낼 때 실물
// 라벨은 76.9% 였다. 그래서 **이 수를 단독으로 결론에 쓰지 않는다.**
//
// 자기 자신을 가리키는 링크와 볼트에 없는 대상은 뺀다. 후자는 회수의 잘못이 아니다.
func LinkCases(notes []store.Note) []Case {
	stems := map[string]bool{}
	for _, n := range notes {
		stems[n.Stem] = true
	}
	seen := map[string]bool{}
	var out []Case
	for _, n := range notes {
		q := strings.TrimSpace(n.Meta.Summary)
		if q == "" {
			continue
		}
		for _, tgt := range n.Links() {
			if tgt == n.Stem || !stems[tgt] {
				continue
			}
			id := "link:" + n.Stem + "→" + tgt
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, Case{
				Set: "링크쌍", ID: id, Query: q, Want: tgt,
				// **cwd 를 주지 않는다.** A 의 도메인을 cwd 로 주면 같은 도메인의
				// B 가 +2 를 공짜로 받아, 재려는 것이 어휘가 아니라 도메인이 된다.
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// RuleCases 는 **규칙이 증류돼 나온 결정**으로 그 규칙을 찾는지 본다.
//
// # 왜 방향이 거꾸로인가
//
// 결정이 규칙을 가리키는 링크는 이 볼트에 **2개뿐**이다(결정 685건 중). 사람은
// 규칙을 근거로 적지 않는다 — 규칙은 지키는 것이지 인용하는 것이 아니라서다.
// 반대 방향은 12개 규칙 **전부**가 출처 결정을 26개 걸고 있다.
//
// 그래서 라벨을 이렇게 읽는다: *"이 결정의 주제가 다시 나오면 그 결정에서 증류한
// 규칙이 걸려야 한다."* 그것이 규칙 계층을 만든 이유 그대로다.
//
// ⚠️ **풀이 12건뿐이라 우연이 세다.** 자리가 2개이므로 아무거나 찍어도 @2 가
// 17% 다. 이 세트의 수는 그 바닥과 견줘 읽어야 한다.
func RuleCases(rules, decisions []store.Note) []Case {
	byStem := map[string]bool{}
	for _, d := range decisions {
		byStem[d.Stem] = true
	}
	summary := map[string]string{}
	for _, d := range decisions {
		summary[d.Stem] = strings.TrimSpace(d.Meta.Summary)
	}
	var out []Case
	seen := map[string]bool{}
	for _, r := range rules {
		for _, src := range r.Links() {
			if !byStem[src] || summary[src] == "" {
				continue
			}
			id := "rule:" + src + "→" + r.Stem
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, Case{Set: "규칙", ID: id, Query: summary[src], Want: r.Stem})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ReferenceCases 는 결정이 근거로 건 **참고 문서**를 그 결정의 요약으로 찾는지 본다.
//
// 링크쌍과 같은 프록시이고 같은 낙관을 갖는다 (LinkCases 의 ⚠️).
func ReferenceCases(decisions, refs []store.Note) []Case {
	isRef := map[string]bool{}
	for _, r := range refs {
		isRef[r.Stem] = true
	}
	var out []Case
	seen := map[string]bool{}
	for _, d := range decisions {
		q := strings.TrimSpace(d.Meta.Summary)
		if q == "" {
			continue
		}
		for _, tgt := range d.Links() {
			if !isRef[tgt] {
				continue
			}
			id := "ref:" + d.Stem + "→" + tgt
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, Case{Set: "참고", ID: id, Query: q, Want: tgt})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SlugQuery 는 `{domain}-결정-{slug}-{date}` 에서 slug 토큰 3개를 뽑는다.
//
// **도메인 접두어(첫 토큰)는 뺀다.** weightMention(+6)이 가장 센 가중치라, 남기면
// 그 도메인 전체가 가점을 받아 측정이 도메인 이름 하나에 지배된다.
func SlugQuery(stem string) string {
	var toks []string
	for _, p := range strings.Split(stem, "-") {
		if p == "" || p == "결정" || p == "decision" || allDigits(p) || len([]rune(p)) < 3 {
			continue
		}
		toks = append(toks, p)
	}
	if len(toks) <= 1 {
		return ""
	}
	toks = toks[1:]
	if len(toks) > 3 {
		toks = toks[:3]
	}
	return strings.Join(toks, " ")
}

// TailQuery 는 요약의 **마지막 1/3** 에서 긴 낱말 3개를 뽑는다.
//
// **낱말 쪼개기는 회수의 것을 그대로 쓴다** (search.ExtractKeywords). 여기서 다시
// 구현하면 재는 자가 재는 대상과 달라지고, 그 어긋남은 조용하다.
func TailQuery(summary string) string {
	r := []rune(summary)
	if len(r) < 60 {
		return "" // 짧은 요약에는 "뒤쪽" 이 없다
	}
	fields := search.ExtractKeywords(string(r[len(r)*2/3:]))
	sort.Slice(fields, func(i, j int) bool {
		if len(fields[i]) != len(fields[j]) {
			return len(fields[i]) > len(fields[j])
		}
		return fields[i] < fields[j]
	})
	var toks []string
	for _, f := range fields {
		if len([]rune(f)) < 3 {
			continue
		}
		toks = append(toks, f)
		if len(toks) == 3 {
			break
		}
	}
	if len(toks) < 2 {
		return ""
	}
	return strings.Join(toks, " ")
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
