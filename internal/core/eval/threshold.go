package eval

import (
	"fmt"
	"sort"
)

// ★★★ **"세 줄을 낼 만한가" 를 재는 자리다.**
//
// # 고치려는 고장
//
// 회수는 `MinScore=1 · Limit=3` 고정이라 **정답이 없어도 매 프롬프트마다 세 줄을
// 낸다.** 게이트를 넘는 후보가 중앙 67건이고 후보 3건 이하는 1.4%뿐이라
// (2026-09-02 실측) 그 세 자리는 사실상 언제나 채워진다.
//
// 맥락을 붙여 상위3 정답률을 25%→55% 로 올린 뒤에도 **45% 의 질의에서는 세 줄이
// 전부 잡음**이다. 잡음이 매 발화마다 나가면 모델은 그 블록을 무시하는 법을 배운다 —
// 교차 프로젝트 주입 55.8% 대 어시스턴트 인용 4.4% 라는 격차의 한 갈래가 이것이다.
//
// # 이득은 상한이고 비용은 정확하다 — 이 비대칭을 섞지 마라
//
// 라벨 세트는 **정답이 있는 질의만** 담는다. 그래서:
//
//   - **비용(Lost)은 정확하다.** 정답이 상위3에 있었는데 문턱이 그 줄을 죽였다면
//     그것은 확실한 손해다.
//   - **이득(NoiseSilenced)은 상한이다.** 라벨된 정답이 상위3에 없다고 해서 그
//     세 줄이 **전부** 쓸모없었다고 단정할 수 없다 — 라벨은 그 결정이 `related` 로
//     엮은 노트뿐이고, 볼트에는 엮이지 않은 관련 노트가 더 있을 수 있다.
//
// 그래서 판정은 **"비용이 0 에 가까운 가장 센 문턱"** 으로 한다. 이득을 최대화하는
// 문턱을 고르면 상한을 실측으로 착각하게 된다 (`규칙-대리지표는-실물로-다시-잰다`).

// Silence 는 문턱 하나의 손익이다.
type Silence struct {
	// Signal 은 무엇으로 재는지, Cut 은 그 값의 경계다. 신호값이 Cut **미만**이면
	// 침묵시킨다.
	Signal string
	Cut    float64

	// NoiseSilenced / NoiseTotal — 정답이 상위3에 **없던** 질의 중 침묵시킨 수.
	// **이득의 상한이다** (위 §).
	NoiseSilenced, NoiseTotal int
	// Lost / GoodTotal — 정답이 상위3에 **있던** 질의 중 침묵시켜 잃은 수.
	// **정확한 비용이다.**
	Lost, GoodTotal int
}

// signal 은 한 질의의 점수 모양에서 문턱용 값을 뽑는다.
//
// 넷을 재는 이유는 어느 것이 갈리는지 모르기 때문이다. 절대 점수는 낱말 수에
// 끌려다니고(맥락을 붙이면 점수가 통째로 커진다), 정규화는 그 끌림을 뺀 것이고,
// 벌어짐은 "1위가 도드라지나" 를 본다 — `Similar` 의 주석이 표본 4개로 못 정한
// 그 신호이고, 여기서는 질의 60개로 잰다.
type signal struct {
	name string
	of   func(r Result) (float64, bool)
}

func signals() []signal {
	at := func(s []int, i int) int {
		if i < len(s) {
			return s[i]
		}
		return 0
	}
	return []signal{
		// ★ 유일하게 **자에 안 끌려다니는** 신호다. 나머지 넷은 전부 점수에서
		// 파생되는데, 점수는 낱말 수(맥락을 붙이면 수백 개)와 볼트 크기에 비례해
		// 커져서 이 볼트에서 고른 경계가 다른 볼트에서는 뜻을 잃는다.
		{"1위 변별어수", func(r Result) (float64, bool) {
			if len(r.PromptRares) == 0 {
				return 0, false
			}
			return float64(r.PromptRares[0]), true
		}},
		// 상위 3 전체에 변별어가 하나도 없으면 "지금 물은 것" 과 겹치는 증거가
		// 아예 없다는 뜻이다. 1위만 보는 것보다 관대하다.
		{"상위3 변별어합", func(r Result) (float64, bool) {
			if len(r.PromptRares) == 0 {
				return 0, false
			}
			sum := 0
			for i := 0; i < len(r.PromptRares) && i < 3; i++ {
				sum += r.PromptRares[i]
			}
			return float64(sum), true
		}},
		{"1위 점수", func(r Result) (float64, bool) {
			if len(r.Scores) == 0 {
				return 0, false
			}
			return float64(r.Scores[0]), true
		}},
		{"1위/낱말수", func(r Result) (float64, bool) {
			if len(r.Scores) == 0 || r.Terms == 0 {
				return 0, false
			}
			return float64(r.Scores[0]) / float64(r.Terms), true
		}},
		{"1위−4위", func(r Result) (float64, bool) {
			if len(r.Scores) == 0 {
				return 0, false
			}
			return float64(r.Scores[0] - at(r.Scores, 3)), true
		}},
		{"1위÷4위", func(r Result) (float64, bool) {
			if len(r.Scores) == 0 {
				return 0, false
			}
			d := at(r.Scores, 3)
			if d == 0 {
				return 99, true // 4위가 없다 = 후보가 적다 = 매우 도드라진다
			}
			return float64(r.Scores[0]) / float64(d), true
		}},
	}
}

// groupShape 는 질의(그룹) 하나의 판정 재료다.
type groupShape struct {
	res  Result // 그 그룹의 대표 사례 — 점수 모양은 질의가 같으면 같다
	good bool   // 상위3에 정답이 하나라도 들었나
}

// shapes 는 세트 하나를 질의 단위로 접는다.
func shapes(rep Report, set string) []groupShape {
	byGroup := map[string]*groupShape{}
	var order []string
	for _, r := range rep.Results {
		if r.Set != set {
			continue
		}
		g, ok := byGroup[r.Group]
		if !ok {
			g = &groupShape{res: r}
			byGroup[r.Group] = g
			order = append(order, r.Group)
		}
		if r.Rank > 0 && r.Rank <= 3 {
			g.good = true
		}
	}
	out := make([]groupShape, 0, len(order))
	for _, k := range order {
		out = append(out, *byGroup[k])
	}
	return out
}

// Thresholds 는 신호마다 **비용이 0 인 가장 센 문턱**과 그 주변을 준다.
//
// 후보 경계는 실제로 관측된 값에서 뽑는다 — 임의의 눈금을 훑으면 이 볼트에만
// 맞는 수를 고르게 되고, 관측값 사이의 빈 구간은 어차피 같은 결과를 낸다.
func Thresholds(rep Report, set string) []Silence {
	gs := shapes(rep, set)
	if len(gs) == 0 {
		return nil
	}
	var noiseTotal, goodTotal int
	for _, g := range gs {
		if g.good {
			goodTotal++
		} else {
			noiseTotal++
		}
	}

	var out []Silence
	for _, sig := range signals() {
		seen := map[float64]bool{}
		var cuts []float64
		for _, g := range gs {
			v, ok := sig.of(g.res)
			if !ok || seen[v] {
				continue
			}
			seen[v] = true
			cuts = append(cuts, v)
		}
		sort.Float64s(cuts)
		for _, cut := range cuts {
			s := Silence{
				Signal: sig.name, Cut: cut,
				NoiseTotal: noiseTotal, GoodTotal: goodTotal,
			}
			for _, g := range gs {
				v, ok := sig.of(g.res)
				if ok && v >= cut {
					continue // 문턱을 넘었다 — 그대로 낸다
				}
				if g.good {
					s.Lost++
				} else {
					s.NoiseSilenced++
				}
			}
			out = append(out, s)
		}
	}
	return out
}

// BestCuts 는 신호마다 **비용(Lost)이 허용치 이하인 것 중 이득이 가장 큰** 문턱을
// 하나씩 고른다. 아무것도 못 고르면 그 신호는 빠진다 — 이득이 0 인 문턱을
// 보여 주는 것은 "이 신호는 안 갈린다" 를 표로 위장하는 것이다.
func BestCuts(rep Report, set string, maxLost int) []Silence {
	byName := map[string]Silence{}
	var order []string
	for _, s := range Thresholds(rep, set) {
		if s.Lost > maxLost || s.NoiseSilenced == 0 {
			continue
		}
		cur, ok := byName[s.Signal]
		if !ok {
			order = append(order, s.Signal)
		}
		// 이득이 크고, 같으면 비용이 작은 쪽.
		if !ok || s.NoiseSilenced > cur.NoiseSilenced ||
			(s.NoiseSilenced == cur.NoiseSilenced && s.Lost < cur.Lost) {
			byName[s.Signal] = s
		}
	}
	out := make([]Silence, 0, len(order))
	for _, k := range order {
		if v, ok := byName[k]; ok {
			out = append(out, v)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].NoiseSilenced > out[j].NoiseSilenced })
	return out
}

// String 은 표 한 줄이다.
func (s Silence) String() string {
	return fmt.Sprintf("%-12s ≥%-7.2f  잡음 침묵 %2d/%2d (%3.0f%%)  ·  정답 잃음 %d/%d",
		s.Signal, s.Cut, s.NoiseSilenced, s.NoiseTotal,
		100*float64(s.NoiseSilenced)/float64(max1(s.NoiseTotal)), s.Lost, s.GoodTotal)
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
