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
	f.StringVar(&r.Supersedes, "supersedes", "", "이 결정이 뒤집는 결정의 stem")
	// **review 는 --supersedes 없이 번복 이유를 남길 수 있는 유일한 경로다.**
	// 대체할 새 결정이 있으면 사유는 뒤집히는 옛 노트에 붙지만, 측정으로 가정이 깨져
	// 그냥 그만두는 번복이 실제로 더 흔하다 — 그때 사유가 붙을 곳은 이 노트 자신뿐이고,
	// capture 에는 그 자리가 없다. 그 경우 --status superseded(또는 regretted)도 함께
	// 줘야 한다(capture.Review 가 요구한다). 안 그러면 "사유는 있는데 여전히 active" 인
	// 노트가 회수에 감점 없이 만점으로 올라온다.
	f.StringVar(&r.SupersedeReason, "reason", "",
		"무엇이 이 판단을 뒤집었는가 — 측정 결과·계기를 한 줄로 (--supersedes 없으면 --status 도 함께)")
	return cmd
}
