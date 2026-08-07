package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/capture"
)

func newReviewCmd() *cobra.Command {
	var r capture.ReviewRequest
	cmd := &cobra.Command{
		Use:   "review <stem>",
		Short: "기존 결정의 outcome·회고·supersedes 를 갱신한다",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r.Stem = args[0]
			l, err := layoutFrom(cmd)
			if err != nil {
				return err
			}
			if err := capture.Review(l, r); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "갱신됨: %s\n", r.Stem)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&r.Outcome, "outcome", "", "pending | good | bad")
	f.StringVar(&r.Status, "status", "", "active | superseded | regretted")
	f.StringVar(&r.Retrospective, "retro", "", "## 회고 에 붙일 내용")
	f.StringVar(&r.Supersedes, "supersedes", "", "이 결정이 뒤집는 결정의 stem")
	return cmd
}
