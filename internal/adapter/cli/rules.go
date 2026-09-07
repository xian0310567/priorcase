package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/stats"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// `prior rules` 는 지금 있는 규칙과 **아직 규칙이 안 된 교훈 묶음**을 보여 준다
// (core/stats/rules.go 의 §).
//
// **규칙을 만들지 않는다.** 후보 목록까지다 — 규칙은 도메인이 없어 어느
// 프로젝트에서 물어도 걸리므로 틀렸을 때의 반경이 결정보다 훨씬 크다.
func newRulesCmd() *cobra.Command {
	var asJSON bool
	var limit int
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "규칙과 규칙 후보를 보여 준다 (승격은 사람이 한다)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			cp, err := search.LoadCorpus(l)
			if err != nil {
				return err
			}
			cands := stats.RuleCandidates(cp.Notes, cp.Rules)
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					Rules      []string              `json:"rules"`
					Candidates []stats.RuleCandidate `json:"candidates"`
				}{stemsOf(cp.Rules), cands})
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "규칙 %d건\n", len(cp.Rules))
			for _, r := range cp.Rules {
				fmt.Fprintf(out, "  %s\n    %s\n", r.Stem, cut(r.Meta.Summary, 100))
			}
			if len(cands) == 0 {
				fmt.Fprintln(out, "\n규칙 후보 없다 — lesson 태그가 세 건 넘게 모인 주제가 없다.")
				return nil
			}
			fmt.Fprintf(out, "\n규칙 후보 %d건 — 아직 어느 규칙의 출처도 아닌 lesson 결정을 태그로 묶었다\n", len(cands))
			for i, c := range cands {
				if limit > 0 && i >= limit {
					fmt.Fprintf(out, "\n  … %d건 더 (--limit 0 으로 전부)\n", len(cands)-i)
					break
				}
				fmt.Fprintf(out, "\n  #%s — 결정 %d건 · 도메인 %d개\n", c.Tag, len(c.Stems), c.Domains)
				for j := range c.Stems {
					if j >= 3 {
						fmt.Fprintf(out, "      … %d건 더\n", len(c.Stems)-j)
						break
					}
					fmt.Fprintf(out, "      %s\n", cut(c.Summaries[j], 96))
				}
			}
			fmt.Fprintln(out, "\n규칙으로 올릴 만하면 _meta/rules/ 에 한 줄 요약과 출처 결정을 걸어 만들어라.")
			fmt.Fprintln(out, "**자동으로 만들지 않는다** — 규칙은 도메인이 없어 틀렸을 때의 반경이 결정보다 크다.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "기계 판독용")
	cmd.Flags().IntVar(&limit, "limit", 8, "보여 줄 후보 수 (0 이면 전부)")
	return cmd
}

func stemsOf(ns []store.Note) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.Stem)
	}
	return out
}
