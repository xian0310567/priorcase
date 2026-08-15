package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/capture"
)

func newReviewCmd() *cobra.Command {
	var r capture.ReviewRequest
	cmd := &cobra.Command{
		Use:   "review <stem>",
		Short: "기존 결정의 outcome·회고·supersedes 를 갱신한다",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r.Stem = args[0]
			_, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			rr, err := capture.Review(l, r)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "갱신됨: %s\n", r.Stem)
			// 갱신은 됐지만 색인은 불완전할 수 있다 — capture 와 같은 안내를 낸다.
			warnSkipped(cmd.ErrOrStderr(), l, rr.Skipped)
			warnPreserved(cmd.ErrOrStderr(), l, rr.IndexPreserved)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&r.Outcome, "outcome", "", "pending | good | bad")
	f.StringVar(&r.Status, "status", "", "active | superseded | regretted")
	f.StringVar(&r.Summary, "summary", "", "한 줄 요약을 고친다 (회수에 주입되는 유일한 줄)")
	f.StringVar(&r.Retrospective, "retro", "", "## 회고 에 붙일 내용")
	f.StringSliceVar(&r.Supersedes, "supersedes", nil, "이 결정이 뒤집는 결정의 stem (반복 가능)")
	return cmd
}
