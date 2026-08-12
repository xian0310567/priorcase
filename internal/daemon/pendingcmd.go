package daemon

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// NewPendingCommand 는 `prior pending` 이다.
//
// MCP 에는 priorcase_pending 도구가 있는데 CLI 에는 없었다. 훅 주입이 "prior pending 으로
// 지워라" 라고 안내하려면 그 명령이 실제로 있어야 한다 — 없는 명령을 안내하면
// 안내 전체의 신뢰가 깎인다.
func NewPendingCommand() *cobra.Command {
	var stateDir, resolve, cfgPath string
	var full bool

	cmd := &cobra.Command{
		Use:   "pending",
		Short: "기록되지 않은 결정 구간을 보고 지운다",
		Long: "안전망이 표시한 구간을 본다. 실제 결정이면 `prior capture` 로 남기고, " +
			"아니면 --resolve 로 지운다.\n\n" +
			"그대로 두면 매 프롬프트마다 다시 뜬다 — 그게 이 표시의 목적이다.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if stateDir == "" {
				d, err := DefaultDir()
				if err != nil {
					return err
				}
				stateDir = d
			}
			if resolve != "" {
				if err := ResolvePending(stateDir, resolve); err != nil {
					return err
				}
				fmt.Fprintf(out, "지웠다: %s\n", resolve)
				return nil
			}

			items, err := ReadPending(stateDir)
			if err != nil {
				return err
			}

			// **볼트 대조를 붙인다.** 판별기는 기존 결정 목록을 받아 중복을 거르는데
			// 사람은 못 받았다 — 실측에서 확인 큐 7건 중 4건이 이 대조만으로 판정됐다.
			//
			// 볼트를 못 읽어도 목록은 낸다. 대조가 없는 것은 불편이고, 구간을 아예
			// 안 보여 주는 것은 안전망이 멎는 것이다.
			var l *store.Layout
			var c *config.Config
			if cc, cerr := config.Load(cfgPath); cerr == nil {
				c, l = cc, store.NewLayout(cc)
			} else if len(items) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"설정을 못 읽어 볼트 대조를 건너뛴다: %v\n", cerr)
			}
			if len(items) == 0 {
				fmt.Fprintln(out, "기록되지 않은 구간이 없다.")
				return nil
			}
			gave := 0
			for _, p := range items {
				if p.GaveUp() {
					gave++
				}
			}
			fmt.Fprintf(out, "기록되지 않은 구간 %d건:\n", len(items))
			for _, p := range items {
				d := p.Domain
				if d == "" {
					d = "(도메인 미상)"
				}
				// **포기한 구간은 그렇다고 말해야 한다.** 안 그러면 사람은 "곧
				// 자동으로 처리되겠지" 하고 기다리는데 영영 안 온다. 자동 기록이
				// 이 도구의 알파이자 오메가라서 더 그렇다 — 안 도는 자리를 조용히
				// 두면 도는 줄 안다.
				mark := ""
				if p.GaveUp() {
					mark = fmt.Sprintf(" · ⚠️ 자동 처리 포기 (판별기 %d회 실패)", p.Fails)
				} else if p.Fails > 0 {
					mark = fmt.Sprintf(" · 판별기 %d/%d회 실패", p.Fails, MaxJudgeFails)
				}
				fmt.Fprintf(out, "\n%s\n  %s · %s · 발화 %d · 시그널 %s%s\n",
					p.ID(), p.When(), d, p.Turns, strings.Join(p.Signals, "·"), mark)
				// **점수와 함께 보여 주되 판정하지 않는다.** 회수는 언제나 무언가를
				// 돌려주므로("이미 기록됨" 이라고 단정하면) 그 단정이 틀렸을 때 사람이
				// 확인을 건너뛴다. 실측에서 일치가 없는 발췌도 1위 54점을 받았다.
				if l != nil && strings.TrimSpace(p.Excerpt) != "" {
					hits, _, serr := search.Similar(l, c, p.Excerpt)
					switch {
					case serr != nil:
						fmt.Fprintf(out, "  (볼트 대조 실패: %v)\n", serr)
					case len(hits) > 0:
						fmt.Fprintln(out, "  비슷한 기존 결정:")
						for _, h := range hits {
							fmt.Fprintf(out, "    %3d  %s\n", h.Score, h.Note.Stem)
							if sm := h.Note.Meta.Summary; sm != "" {
								fmt.Fprintf(out, "         %s\n", clipRunes(sm, 90))
							}
						}
					}
				}
				if ex := strings.TrimSpace(p.Excerpt); ex != "" {
					if !full {
						if r := []rune(ex); len(r) > 300 {
							ex = string(r[:300]) + "…"
						}
					}
					fmt.Fprintf(out, "  ---\n%s\n  ---\n", indent(ex))
				}
			}
			if gave > 0 {
				fmt.Fprintf(out, "\n⚠️ %d건은 판별기가 %d회 연속 실패해 자동 처리를 그만뒀다 — "+
					"기다려도 안 온다. 사람이 봐야 한다.\n", gave, MaxJudgeFails)
			}
			fmt.Fprintln(out, "\n실제 결정이면 prior capture 로 남겨라.")
			fmt.Fprintln(out, "결정이 아니면 prior pending --resolve <id> 로 지워라.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&resolve, "resolve", "", "지울 구간의 id")
	f.BoolVar(&full, "full", false, "발췌를 자르지 않고 다 보여 준다")
	f.StringVar(&cfgPath, "config", "", "설정 파일 경로 (볼트 대조에 쓴다)")
	f.StringVar(&stateDir, "state-dir", "", "상태 디렉토리 (기본: $XDG_STATE_HOME/priorcase)")
	return cmd
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}

// clipRunes 는 룬 단위로 자른다. 바이트로 자르면 한글이 깨진다.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
