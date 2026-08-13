package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/search"
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
				// 사람이 찾을 때도 참고 문서를 본다 — 훅과 같은 코퍼스를 봐야
				// "훅은 주는데 recall 은 안 준다" 가 안 생긴다.
				IncludeReferences: true,
				ReferenceLimit:    limit,
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
				// **저자는 사람이 물을 때만 보여 준다.** 주입(②)은 매 프롬프트마다 도는
				// 자리라 토큰을 쓰고, 에이전트에게 "누가" 는 덜 중요하다. 사람에게는
				// 반대다 — "왜 이렇게 했지" 다음 질문이 "누가 정했지" 이고, 그 사람에게
				// 묻는 것이 노트를 다시 읽는 것보다 빠를 때가 많다.
				if a := h.Note.Meta.Author; a != "" {
					fmt.Fprintf(out, "     — %s\n", a)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "human", "출력 형식: human | inject")
	cmd.Flags().BoolVar(&crossProject, "cross-project", true, "cwd 도메인 밖의 결정도 찾는다")
	cmd.Flags().IntVar(&limit, "limit", 3, "최대 결과 수")
	return cmd
}
