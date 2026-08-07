package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/search"
)

func newRecallCmd() *cobra.Command {
	var format string
	var crossProject bool
	var limit int

	cmd := &cobra.Command{
		Use:   "recall <query>",
		Short: "관련 과거 결정을 찾는다",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			cwd, _ := os.Getwd()

			query := args[0]
			for _, a := range args[1:] {
				query += " " + a
			}
			hits, skipped, err := search.Recall(l, c, query, search.Options{
				Cwd: cwd, CrossProject: crossProject, Limit: limit, MinScore: 1,
			})
			if err != nil {
				return err
			}
			// 회수 대상에서 빠진 노트를 알린다. 포맷과 무관하게 stderr 다 —
			// --format inject 의 stdout 은 훅이 에이전트 컨텍스트로 그대로
			// 넘기는 순수 데이터라 한 줄도 섞이면 안 된다. 그래서 "inject 면
			// 생략" 이 아니라 "언제나 stderr" 를 택했다: 생략하면 정작 컨텍스트
			// 주입 경로에서만 침묵하게 되는데, 거기가 가장 알아야 하는 자리다.
			warnSkipped(cmd.ErrOrStderr(), l, skipped)
			out := cmd.OutOrStdout()
			if format == "inject" {
				fmt.Fprint(out, search.RenderInject(l, hits))
				return nil
			}
			for _, h := range hits {
				fmt.Fprintf(out, "%3d  %s\n     %s\n", h.Score, h.Note.Stem, h.Note.Meta.Summary)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "human", "출력 형식: human | inject")
	cmd.Flags().BoolVar(&crossProject, "cross-project", true, "cwd 도메인 밖의 결정도 찾는다")
	cmd.Flags().IntVar(&limit, "limit", 3, "최대 결과 수")
	return cmd
}
