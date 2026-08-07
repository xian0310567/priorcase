package hook

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/daemon"
)

// NewCommand 는 `cb hook <event>` 를 만든다. mcp·daemon 과 같은 이유로 cli 가 아니라
// 여기 있다 — 어댑터끼리 서로를 import 하지 않는다 (§4.1).
func NewCommand() *cobra.Command {
	var stateDir string

	cmd := &cobra.Command{
		Use:   "hook <event>",
		Short: "Claude Code 훅 래퍼 (호스트가 실행한다)",
		Long: "Claude Code 훅으로 실행된다. 입력은 stdin JSON.\n\n" +
			"쓸 수 있는 이벤트: " + eventList() + "\n\n" +
			"**무슨 일이 있어도 종료 코드는 0이다.** 훅이 실패해서 대화가 막히면 " +
			"사용자는 casebook 을 고치는 게 아니라 지운다. 진단은 stderr 로 나간다.\n" +
			"user-prompt-submit·session-start 의 stdout 은 에이전트 컨텍스트로 주입되므로 " +
			"진단이 한 줄도 섞이지 않는다.",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		// RunE 가 아니라 Run 이다 — cobra 가 에러를 종료 코드로 옮기지 못하게 한다.
		Run: func(cmd *cobra.Command, args []string) {
			errW := cmd.ErrOrStderr()
			warn := func(err error) {
				if err != nil {
					fmt.Fprintf(errW, "cb hook %s: %v\n", args[0], err)
				}
			}

			in, perr := ParseInput(cmd.InOrStdin())
			warn(perr)

			o := Options{
				Event:    Event(args[0]),
				Input:    in,
				StateDir: stateDir,
				Out:      cmd.OutOrStdout(),
				Err:      errW,
			}
			if o.StateDir == "" {
				if d, err := daemon.DefaultDir(); err == nil {
					o.StateDir = d
				}
			}

			path, _ := cmd.Flags().GetString("config")
			c, err := config.Load(path)
			if err != nil {
				// 설정이 없으면 할 수 있는 일이 없다. 조용히 끝내되 이유는 남긴다.
				warn(err)
				return
			}
			o.Config, o.Layout = c, store.NewLayout(c)
			warn(Run(cmd.Context(), o))
		},
	}
	cmd.Flags().StringVar(&stateDir, "state-dir", "", "데몬 상태 디렉토리 (기본: $XDG_STATE_HOME/casebook)")
	return cmd
}
