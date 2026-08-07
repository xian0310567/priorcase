package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/index"
)

// newIndexCmd 는 `cb index` 다.
//
// 건너뛴 노트가 있어도 종료 코드는 0 이다. 근거:
//
//   - 건너뜀의 원인(구 스키마 frontmatter 등)은 cb 로 고칠 수 있는 것이 아니라
//     사람이 볼트를 손봐야 하는 것이다. 훅이나 크론에서 도는 `cb index` 가 그동안
//     계속 rc≠0 이면, 고칠 때까지 매번 실패하는 명령이 되어 오히려 무시하는 법을
//     학습시킨다.
//   - capture·review 가 내부적으로 index.Write 를 부른다. 거기서 건너뜀을
//     실패로 취급하면 "기록은 됐는데 명령은 실패" 라는 거짓 신호가 된다.
//
// 대신 침묵하지는 않는다: stdout 요약 줄에 건너뛴 건수를 박고, 어느 파일이
// 왜인지는 stderr 로 전부 낸다.
func newIndexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "index",
		Short: "결정 색인을 재생성한다",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			res, err := index.Write(l)
			if err != nil {
				return err
			}
			// 건너뛴 게 있으면 stdout 의 요약 줄에도 그 수를 박는다.
			// 상세(어느 파일이 왜)는 warnSkipped 가 stderr 로 낸다 — 요약만
			// 보고 넘어가도 "불완전하다" 는 사실은 놓칠 수 없게, 상세를 보려면
			// 스크롤 없이 바로 위 줄에 있게.
			out := cmd.OutOrStdout()
			if n := len(res.Skipped); n > 0 {
				fmt.Fprintf(out, "색인 %d행 생성 (%d건 건너뜀 — 색인이 불완전하다)\n", res.Rows, n)
			} else {
				fmt.Fprintf(out, "색인 %d행 생성\n", res.Rows)
			}
			warnSkipped(cmd.ErrOrStderr(), l, res.Skipped)
			return nil
		},
	}
}
