package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/daemon"
)

// newReviewedCmd 는 `prior reviewed <id>` 다.
//
// **`review --outcome good` 을 쓰지 않는 이유가 있다.** outcome 은 "그 결정이
// 결과적으로 좋았나" 이고, 회고 큐가 outcome != pending 인 노트를 영영 제외한다.
// 검토 큐의 "맞다" 는 "판별기가 사실대로 썼나" 라는 **다른 질문**이다. 둘을 한
// 값에 실으면 노트를 검증했을 뿐인데 나중에 결과를 묻는 자리가 조용히 사라진다.
//
// 표시는 볼트가 아니라 승격 원장에 남긴다 — 검토 큐가 거기서 나오므로 수명이
// 같다. 원장이 사라지면 큐도 같이 사라져서 어긋날 자리가 없다.
func newReviewedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reviewed <id>",
		Short: "판별기가 만든 노트를 사람이 검증했다고 표시한다",
		Long: "승격 ID 를 받아 '사람이 검증했다' 는 표시를 원장에 남긴다.\n\n" +
			"표시된 것은 검토 큐에서 빠진다. 결정의 결과(outcome)는 건드리지\n" +
			"않는다 — 그건 회고 큐가 나중에 물을 다른 질문이다.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			sd, err := daemon.DefaultDir()
			if err != nil {
				return err
			}
			added, err := daemon.MarkReviewed(sd, args[0])
			if err != nil {
				return err
			}
			if !added {
				// **이미 표시된 것은 오류가 아니다.** 앱의 큐가 잠깐 낡아 같은
				// 것을 두 번 보낼 수 있는데, 그때 빨간 화면을 띄우면 사람은
				// 자기가 뭘 잘못한 줄 안다.
				fmt.Fprintln(cmd.OutOrStdout(), "이미 검토 표시돼 있다")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "검토 표시했다")
			return nil
		},
	}
	return cmd
}
