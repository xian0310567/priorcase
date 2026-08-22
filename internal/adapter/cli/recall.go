package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/worklog"
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
				// **작업 로그는 여기 안 온다.** inject 의 stdout 은 에이전트
				// 컨텍스트로 그대로 밀려 들어가는 자동 주입이고, 회수는 Limit 3 ·
				// MinScore 1 의 고정 슬롯이다. 작업 로그가 그 슬롯을 놓고 결정
				// 노트와 경쟁하면 볼트가 커질수록 결정 노트가 밀려난다 —
				// 등급을 나눈 이유가 통째로 사라진다.
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

			// **작업 로그는 사람이 물었을 때만 나온다.** 자동 주입(--format inject)에는
			// 절대 안 섞고 여기서만 붙인다 — 그것이 등급을 나눈 이유다. MCP 의
			// priorcase_recall 도 같은 규칙으로 같은 자리에 붙인다.
			//
			// 결정 노트 뒤다. 등급이 그대로 읽히는 순서여야 한다.
			//
			// 작업 로그를 못 읽어도 회수 자체는 성공시킨다 — 결정 노트는 이미 냈고,
			// 하위 계층 하나 때문에 상위 계층까지 못 주는 것이 더 나쁘다. 대신
			// 침묵하지 않는다. 경고는 stderr 다(warnSkipped 와 같은 이유).
			//
			// **--cross-project 를 여기서도 지킨다.** 예전에는 안 넘겨서 그 플래그가
			// 반쪽만 지켜졌다 — 결정 노트는 좁혀졌는데 작업 로그는 전 도메인이 나왔다.
			// "이 프로젝트만" 이라고 말한 자리에서 남의 프로젝트 메모가 나오는 것은
			// 회수 품질 문제가 아니라 약속을 어긴 것이다.
			notes, werr := worklog.Search(l, search.ExtractKeywords(query),
				worklog.Scope(c, cwd, crossProject), limit)
			if werr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "경고: 작업 로그를 찾아보지 못했다: %v\n", werr)
			}
			if len(notes) > 0 {
				fmt.Fprintln(out, "\n[작업 로그 — 확정 전 기록]")
				for _, h := range notes {
					fmt.Fprintf(out, "%3d  %s %s · %s\n     %s\n",
						h.Score, h.Date, h.Time, h.Title, l.RelPath(h.Path))
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
