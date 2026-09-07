package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/eval"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// `prior eval` 은 **회수가 정답을 꺼내는지** 잰다.
//
// # doctor 와 다른 것을 본다
//
//	doctor  서류가 정합한가 (링크가 걸리나 · 스키마가 맞나 · 훅이 배선됐나)
//	eval    회수가 실제로 맞는 것을 꺼내나
//
// 둘 다 초록인데 회수가 쓸모없을 수 있다 — 실제로 그랬다. 2026-09-02 실측에서
// 교차 프로젝트 주입이 55.8% 인데 어시스턴트가 그 노트를 실제로 언급한 것은
// 4.4% 였고, doctor 는 그동안 계속 초록이었다.
//
// # 언제 도나
//
// 자동으로 돌지 않는다. **점수식 상수를 만질 때**와 **볼트가 눈에 띄게 커졌을 때**
// 사람이 부른다. 상수가 전부 그날의 볼트 스냅샷에서 골라졌고, 볼트가 자라면
// 문서빈도 분포가 통째로 움직이기 때문이다 (eval 패키지 머리말의 드리프트 실측).
func newEvalCmd() *cobra.Command {
	var (
		sets      []string
		depth     int
		jsonOut   bool
		save      string
		against   string
		showN     int
		promptsIn string
		ctxRunes  int
		ctxSrc    string
		ctxMaxKW  int
		graph     bool
		honest    bool
		gate      bool
		gateLost  int
		mkLabels  string
	)

	cmd := &cobra.Command{
		Use:   "eval",
		Short: "회수가 정답을 꺼내는지 잰다 (A/B 대조 포함)",
		Long: strings.Join([]string{
			"회수 품질을 세트별로 잰다.",
			"",
			"  slug    파일명 slug 로 자기 자신 찾기 — 길이에 중립적인 회귀 가드",
			"  tail    요약 뒤쪽 1/3 로 자기 자신 찾기 — 긴 요약의 꼬리가 사는지",
			"  link    사람이 이은 A→B 를 A 의 요약으로 찾기 — 자동 생성 프록시(낙관적이다)",
			"  rule    규칙의 출처 결정으로 그 규칙을 찾기 — 방향이 거꾸로인 이유는 코드 주석에",
			"  ref     결정이 근거로 건 참고 문서를 그 결정의 요약으로 찾기",
			"  prompt  실제 프롬프트 라벨 — 실물. --make-labels 로 만들고 --prompts 로 준다",
			"",
			"점수식을 만지기 전에 --save 로 기준선을 남기고, 만진 뒤 --against 로 견준다.",
			`판정 기준은 "MRR 이 올랐나" 가 아니라 "올랐고, 상위3에서 밀려난 것이 없나" 다.`,
		}, "\n"),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			if mkLabels != "" {
				return runMakeLabels(cmd.ErrOrStderr(), c, l, mkLabels)
			}
			return runEval(cmd.OutOrStdout(), cmd.ErrOrStderr(), c, l, evalOpts{
				sets: sets, depth: depth, jsonOut: jsonOut, save: save,
				against: against, showN: showN, promptsIn: promptsIn,
				ctxRunes: ctxRunes, ctxSrc: ctxSrc,
				ctxMaxKW: ctxMaxKW, graph: graph, honest: honest,
				gate: gate, gateLost: gateLost,
			})
		},
	}
	cmd.Flags().StringSliceVar(&sets, "set", []string{"slug", "tail", "link"},
		"돌릴 세트 (slug,tail,link,rule,ref,prompt)")
	cmd.Flags().IntVar(&depth, "depth", 20,
		"순위를 볼 상한. 이 밖은 '못 찾음' 이다")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "기계 판독용")
	cmd.Flags().StringVar(&save, "save", "", "이번 결과를 파일로 남긴다 (기준선)")
	cmd.Flags().StringVar(&against, "against", "", "기준선 파일과 견준다")
	cmd.Flags().IntVar(&showN, "show", 5, "--against 에서 보여 줄 회귀 사례 수")
	cmd.Flags().StringVar(&promptsIn, "prompts", "",
		`실물 라벨 JSON: [{"prompt":"...","cwd":"...","want":"<stem>","context":"..."}]`)
	cmd.Flags().IntVar(&ctxRunes, "context", 0,
		"라벨의 context 를 질의 앞에 몇 글자까지 붙일지 (0 이면 안 붙인다)")
	cmd.Flags().IntVar(&ctxMaxKW, "context-keywords", 0,
		"맥락에서 가져올 낱말 수 상한 (0 이면 무제한)")
	cmd.Flags().StringVar(&ctxSrc, "context-src", "all",
		"맥락에 무엇을 넣을지: all(대화 전부) | user(사람이 친 말만)")
	cmd.Flags().BoolVar(&honest, "hide-source", true,
		"라벨의 출처 결정을 볼트에서 없는 셈 친다 — 끄면 정답지를 특징으로 쓰게 된다")
	cmd.Flags().BoolVar(&graph, "graph-ceiling", false,
		"그래프 1홉 확장이 도울 수 있는 최대치를 잰다 (만들기 전에 이것부터 본다)")
	cmd.Flags().BoolVar(&gate, "threshold", false,
		"주입 문턱 후보의 손익을 잰다 (잡음 질의를 몇 건 침묵시키고 정답을 몇 건 잃나)")
	cmd.Flags().IntVar(&gateLost, "threshold-lost", 0,
		"--threshold 에서 허용할 정답 손실 건수. 0 이면 손해 없는 문턱만 고른다")
	cmd.Flags().StringVar(&mkLabels, "make-labels", "",
		"볼트의 source_session·related 와 트랜스크립트로 실물 라벨을 만들어 파일에 쓴다")
	return cmd
}

type evalOpts struct {
	sets      []string
	depth     int
	jsonOut   bool
	save      string
	against   string
	showN     int
	promptsIn string
	ctxRunes  int
	ctxSrc    string
	ctxMaxKW  int
	graph     bool
	honest    bool
	gate      bool
	gateLost  int
}

// labeled 는 실물 라벨 한 건이다.
//
// **context 를 파일이 들고 있다.** 하네스가 트랜스크립트를 다시 뒤지지 않는 이유는
// 두 가지다 — 라벨을 만든 시점의 대화가 정본이고(그 뒤 세션이 이어지면 맥락이
// 달라진다), 그리고 이 파일이 있으면 다른 머신에서도 같은 수를 다시 낼 수 있다.
type labeled struct {
	Prompt string `json:"prompt"`
	Cwd    string `json:"cwd"`
	Want   string `json:"want"`
	// Context 는 프롬프트 앞의 대화 **전부**다 — 사람·에이전트·도구 활동.
	Context string `json:"context,omitempty"`
	// Kind 는 정답이 어느 계층인지다 — decision | rule | reference.
	//
	// **회수는 셋을 각자 자기 자리에서 자른다.** 정답이 규칙인데 결정 풀만 보면
	// 영영 못 찾고, 그러면 "규칙 회수가 나쁘다" 가 아니라 "규칙을 안 켰다" 를
	// 재게 된다.
	Kind string `json:"kind,omitempty"`
	// ContextUser 는 그중 **사람이 친 말만**이다.
	//
	// 둘을 같이 담는 이유: 어느 쪽이 나은지가 측정 대상이고, 라벨을 두 번 만들면
	// 앵커가 흔들려 같은 ID 로 짝지을 수 없다. 한 파일에 둘 다 담아야 A/B 가
	// **같은 사례 위에서** 돈다.
	ContextUser string `json:"context_user,omitempty"`
	ID          string `json:"id,omitempty"`
}

func runEval(out, errw io.Writer, c *config.Config, l *store.Layout, o evalOpts) error {
	start := time.Now()
	// **한 번만 읽는다.** 사례가 1,000건을 넘으므로 사례마다 볼트를 다시 읽으면
	// 비용이 노트 수의 제곱이 된다 (search.Corpus 의 §).
	corpus, err := search.LoadCorpus(l)
	if err != nil {
		return err
	}
	if len(corpus.Notes) == 0 {
		return fmt.Errorf("결정 노트가 없다 — 볼트 경로나 도메인 선언을 보라")
	}

	want := map[string]bool{}
	for _, s := range o.sets {
		want[strings.TrimSpace(s)] = true
	}
	var cases []eval.Case
	if want["slug"] {
		cases = append(cases, eval.SlugCases(corpus.Notes)...)
	}
	if want["tail"] {
		cases = append(cases, eval.TailCases(corpus.Notes)...)
	}
	if want["link"] {
		cases = append(cases, eval.LinkCases(corpus.Notes)...)
	}
	if want["rule"] {
		cases = append(cases, eval.RuleCases(corpus.Rules, corpus.Notes)...)
	}
	if want["ref"] {
		cases = append(cases, eval.ReferenceCases(corpus.Notes, corpus.Refs)...)
	}
	if want["prompt"] {
		ls, lerr := loadLabels(o.promptsIn)
		if lerr != nil {
			return lerr
		}
		cases = append(cases, labelCases(ls, o.ctxRunes, o.ctxSrc)...)
	}
	if len(cases) == 0 {
		return fmt.Errorf("사례가 없다 — --set 을 보라 (prompt 세트는 --prompts 가 필요하다)")
	}

	rep := eval.Run(cases, func(cs eval.Case) ([]eval.Ranked, int) {
		// ★ **세트마다 보는 풀이 다르다.**
		//
		// Recall 은 세 풀(규칙·결정·참고)을 각자 자기 자리에서 자른 뒤 이어 붙인다.
		// 정답이 규칙이면 규칙 풀의 순위를 봐야 하는데, 기본값처럼 규칙을 꺼 두면
		// 그 정답은 영영 안 나온다 — 그러면 "규칙 회수가 나쁘다" 가 아니라
		// "규칙을 안 켰다" 를 재게 된다.
		opt := search.Options{
			Cwd: cs.Cwd, CrossProject: true, MinScore: 1,
			Context: cs.Context, MaxContextKeywords: o.ctxMaxKW,
			Corpus: corpus,
		}
		// ★ **라벨의 출처 결정은 질의 시점에 없었다** (search.Options.Exclude 의 §).
		// 그 노트는 정답을 직접 가리키므로, 안 빼면 그래프 확장이 정답지를
		// 특징으로 쓰게 되어 측정이 통째로 무의미해진다.
		if o.honest {
			if src := labelSource(cs.ID); src != "" {
				opt.Exclude = map[string]bool{src: true}
			}
		}
		var keep func(store.Note) bool
		switch cs.Set {
		case "규칙", "실프롬프트-규칙":
			opt.RuleLimit = o.depth
			keep = store.Note.IsRule
		case "참고", "실프롬프트-참고":
			opt.IncludeReferences, opt.ReferenceLimit = true, o.depth
			keep = store.Note.IsReference
		default:
			opt.Limit = o.depth
			keep = func(n store.Note) bool { return !n.IsRule() && !n.IsReference() }
		}
		hits, _, rerr := search.Recall(l, c, cs.Query, opt)
		if rerr != nil {
			fmt.Fprintf(errw, "경고: %q 회수 실패: %v\n", cs.Query, rerr)
			return nil, 0
		}
		out := make([]eval.Ranked, 0, len(hits))
		for _, h := range hits {
			if !keep(h.Note) {
				continue
			}
			out = append(out, eval.Ranked{
				Stem: h.Note.Stem, Score: h.Score, PromptRare: h.PromptRare})
		}
		// **재는 낱말 수는 회수가 실제로 쓴 것이어야 한다** — 여기서 다시 세면
		// 맥락 자르기·중복 제거를 흉내 내게 되고, 그 흉내가 어긋나도 조용하다.
		return out, search.QueryTerms(cs.Query, search.Options{Context: cs.Context})
	})

	if o.save != "" {
		b, merr := json.MarshalIndent(rep, "", "  ")
		if merr != nil {
			return merr
		}
		if werr := os.WriteFile(o.save, b, 0o644); werr != nil {
			return fmt.Errorf("기준선을 못 쓴다: %w", werr)
		}
		fmt.Fprintf(errw, "기준선을 %s 에 썼다 (사례 %d건)\n", o.save, len(rep.Results))
	}

	var deltas []eval.Delta
	if o.against != "" {
		base, berr := loadReport(o.against)
		if berr != nil {
			return berr
		}
		deltas = eval.Diff(base, rep)
	}

	if o.jsonOut {
		return json.NewEncoder(out).Encode(struct {
			Report eval.Report  `json:"report"`
			Deltas []eval.Delta `json:"deltas,omitempty"`
		}{rep, deltas})
	}
	renderEval(out, corpus, rep, deltas, o.showN, time.Since(start))
	if o.gate {
		renderThreshold(out, rep, o.gateLost)
	}
	if o.graph {
		renderGraphCeiling(out, rep, corpus.Notes)
	}
	return nil
}

func loadLabels(path string) ([]labeled, error) {
	if path == "" {
		return nil, fmt.Errorf("prompt 세트에는 --prompts 가 필요하다")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("라벨 파일을 못 읽는다: %w", err)
	}
	var out []labeled
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("라벨 파일이 JSON 배열이 아니다: %w", err)
	}
	return out, nil
}

// labelCases 는 실물 라벨을 사례로 바꾼다.
//
// **맥락은 프롬프트 *앞*에 붙인다.** 회수는 낱말 집합만 보므로 순서가 점수를
// 바꾸지 않지만, MaxKeywords 로 자를 때 긴 낱말부터 남기는 규칙과 겹쳐서
// 사람이 로그를 읽을 때 어느 쪽이 원본인지 헷갈리지 않게 하려는 것이다.
func labelCases(ls []labeled, ctxRunes int, src string) []eval.Case {
	out := make([]eval.Case, 0, len(ls))
	for i, x := range ls {
		ctx := x.Context
		if src == "user" {
			ctx = x.ContextUser
		}
		if ctxRunes <= 0 {
			ctx = ""
		} else if r := []rune(ctx); len(r) > ctxRunes {
			ctx = string(r[len(r)-ctxRunes:]) // **뒤에서** 자른다 — 직전 화제가 주제다
		}
		id := x.ID
		if id == "" {
			id = fmt.Sprintf("prompt:%d:%s", i, x.Want)
		}
		set := "실프롬프트"
		switch x.Kind {
		case "rule":
			set = "실프롬프트-규칙"
		case "reference":
			set = "실프롬프트-참고"
		}
		out = append(out, eval.Case{
			Set: set, ID: id, Query: x.Prompt, Context: ctx, Cwd: x.Cwd, Want: x.Want,
			// **같은 프롬프트의 정답들을 한 묶음으로 본다** (eval.Case.Group 의 §).
			Group: x.Prompt,
		})
	}
	return out
}

func loadReport(path string) (eval.Report, error) {
	var r eval.Report
	b, err := os.ReadFile(path)
	if err != nil {
		return r, fmt.Errorf("기준선을 못 읽는다: %w", err)
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("기준선이 eval 보고 JSON 이 아니다: %w", err)
	}
	return r, nil
}

func renderEval(w io.Writer, corpus *search.Corpus, rep eval.Report, deltas []eval.Delta, showN int, took time.Duration) {
	fmt.Fprintf(w, "볼트: 결정 %d건 · 참고 %d건 · 규칙 %d건 (%s)\n\n",
		len(corpus.Notes), len(corpus.Refs), len(corpus.Rules), took.Round(time.Millisecond))

	fmt.Fprintf(w, "%-12s %6s %8s %8s %8s %8s\n", "세트", "n", "못찾음", "@1", "@3", "MRR")
	for _, s := range rep.Sets {
		p := func(n int) string {
			if s.N == 0 {
				return "-"
			}
			return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(s.N))
		}
		fmt.Fprintf(w, "%-12s %6d %8s %8s %8s %8.3f", s.Set, s.N, p(s.Miss), p(s.At1), p(s.At3), s.MRR)
		// 그룹이 사례 수와 같으면(질의 하나에 정답 하나) 새로울 것이 없다.
		if s.Groups > 0 && s.Groups != s.N {
			fmt.Fprintf(w, "   질의 %d 중 %d(%.0f%%)가 상위3에 정답 하나 이상",
				s.Groups, s.GroupAny3, 100*float64(s.GroupAny3)/float64(s.Groups))
		}
		fmt.Fprintln(w)
	}

	if len(deltas) == 0 {
		return
	}
	fmt.Fprintln(w, "\n기준선 대비")
	for _, d := range deltas {
		fmt.Fprintf(w, "  %s  MRR %.3f → %.3f\n", d.Set, d.MRRBefore, d.MRRAfter)
		fmt.Fprintf(w, "    새로 찾음 %d · 놓침 %d · 순위↑ %d · 순위↓ %d\n",
			d.Gained, d.Lost, d.Better, d.Worse)
		// ★ 이 줄이 판정선이다. 상위3이 곧 주입이라, 여기서 밀려난 것이 실제 피해다.
		fmt.Fprintf(w, "    상위3 진입 %d · 상위3에서 밀려남 %d (정답쌍 단위)\n", d.IntoTop3, d.OutOfTop3)
		if d.GroupBetter+d.GroupWorse > 0 {
			// ★ 판정선. 주입이 세 줄이라 사람이 겪는 것은 프롬프트 단위다.
			fmt.Fprintf(w, "    ★ 프롬프트 단위: 좋아짐 %d · **나빠짐 %d**\n", d.GroupBetter, d.GroupWorse)
		}
		if len(d.Regressions) == 0 {
			continue
		}
		rs := append([]eval.Regression(nil), d.Regressions...)
		sort.Slice(rs, func(i, j int) bool { return rs[i].Before < rs[j].Before })
		n := showN
		if n > len(rs) {
			n = len(rs)
		}
		for _, r := range rs[:n] {
			fmt.Fprintf(w, "      %d위→%s  %s\n", r.Before, rankStr(r.After), cut(r.Query, 60))
			fmt.Fprintf(w, "        정답 %s\n", r.Want)
			if r.TopAfter != "" && r.TopAfter != r.TopBefore {
				fmt.Fprintf(w, "        1위가 %s 로 바뀜\n", r.TopAfter)
			}
		}
		if len(rs) > n {
			fmt.Fprintf(w, "      … %d건 더\n", len(rs)-n)
		}
	}
}

func rankStr(n int) string {
	if n == 0 {
		return "못찾음"
	}
	return fmt.Sprintf("%d위", n)
}

func cut(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// runMakeLabels 는 실물 라벨 파일을 만든다 (evallabel.go 의 §).
func runMakeLabels(errw io.Writer, c *config.Config, l *store.Layout, out string) error {
	corpus, err := search.LoadCorpus(l)
	if err != nil {
		return err
	}
	warnSkipped(errw, l, corpus.Skipped)
	ls, err := buildLabels(errw, c, l, corpus.Notes, corpus.Rules, corpus.Refs, labelOpts{})
	if err != nil {
		return err
	}
	if len(ls) == 0 {
		return fmt.Errorf("라벨이 한 건도 안 나왔다 — source_session 과 related 가 함께 있는 노트가 필요하다")
	}
	if err := writeLabels(out, ls); err != nil {
		return err
	}
	fmt.Fprintf(errw, "%s 에 썼다\n", out)
	return nil
}

// renderThreshold 는 문턱 후보의 손익을 그린다 (eval/threshold.go 의 §).
func renderThreshold(w io.Writer, rep eval.Report, maxLost int) {
	for _, set := range rep.Sets {
		best := eval.BestCuts(rep, set.Set, maxLost)
		fmt.Fprintf(w, "\n주입 문턱 — %s (질의 %d개: 정답이 상위3에 든 것 %d · 못 든 것 %d)\n",
			set.Set, set.Groups, set.GroupAny3, set.Groups-set.GroupAny3)
		if len(best) == 0 {
			fmt.Fprintf(w, "  정답을 %d건 이하로 잃으면서 잡음을 줄이는 문턱이 **없다** — 이 세트에서는 신호가 안 갈린다.\n", maxLost)
			continue
		}
		for _, s := range best {
			fmt.Fprintf(w, "  %s\n", s)
		}
		fmt.Fprintln(w, "  (잡음 침묵은 **이득의 상한**이다 — 라벨이 그 질의의 관련 노트를 다 담지는 않는다.")
		fmt.Fprintln(w, "   정답 잃음은 **정확한 비용**이다. 둘을 같은 무게로 읽지 마라.)")
	}
}

// labelSource 는 라벨 ID 에서 **그 정답이 나온 결정**을 뽑는다.
//
// ID 규약은 `label:<종류>:<출처>→<정답>` 이다 (evallabel.go). 그 출처 노트는
// 질의 시점에 아직 없었고 정답을 직접 가리키므로, 그래프 천장을 잴 때 씨앗에서
// 빼야 한다 (eval/graph.go 의 ⚠️).
func labelSource(id string) string {
	const p = "label:"
	if !strings.HasPrefix(id, p) {
		return ""
	}
	rest := id[len(p):]
	i := strings.Index(rest, ":")
	if i < 0 {
		return ""
	}
	rest = rest[i+1:]
	j := strings.Index(rest, "→")
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// renderGraphCeiling 은 그래프 확장의 최대치를 그린다 (eval/graph.go 의 §).
func renderGraphCeiling(w io.Writer, rep eval.Report, notes []store.Note) {
	g := eval.Neighborhood(notes)
	exclude := map[string]string{}
	for _, r := range rep.Results {
		if src := labelSource(r.ID); src != "" {
			exclude[r.ID] = src
		}
	}
	for _, set := range rep.Sets {
		c := eval.GraphCeiling(rep, set.Set, g, exclude)
		if c.Groups == 0 {
			continue
		}
		miss := c.Groups - c.AlreadyIn
		fmt.Fprintf(w, "\n그래프 1홉 천장 — %s (질의 %d개)\n", set.Set, c.Groups)
		fmt.Fprintf(w, "  이미 상위3에 정답 있음   %d\n", c.AlreadyIn)
		fmt.Fprintf(w, "  ★ 1홉으로 닿음(최대치)   %d / %d 놓친 질의 (%.0f%%)\n",
			c.Reachable, miss, 100*float64(c.Reachable)/float64(max1(miss)))
		fmt.Fprintf(w, "  1홉으로도 안 닿음        %d\n", c.Unreachable)
		if c.SeedLeak > 0 {
			fmt.Fprintf(w, "  (라벨 출처 노트가 씨앗에 잡힌 질의 %d개는 그 노트를 빼고 쟀다 —\n"+
				"   안 빼면 정답지를 특징으로 쓰는 것이라 천장이 부풀려진다)\n", c.SeedLeak)
		}
	}
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
