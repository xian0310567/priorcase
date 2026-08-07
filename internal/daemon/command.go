package daemon

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/transcript/claudecode"
)

// NewCommand 는 `cb watch` 를 만든다. mcp.NewCommand 와 같은 이유로 cli 가 아니라
// 여기 있다 — 어댑터가 이 패키지를 import 하면 §4.1 이 깨진다. 조립은 cmd/cb 가 한다.
func NewCommand() *cobra.Command {
	var backfill bool
	var stateDir, root string

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "놓친 기록을 줍는 데몬 (단일 인스턴스)",
		Long: "transcript 를 감시하다가 기록되지 않은 결정 구간을 표시한다.\n\n" +
			"**LLM 을 부르지 않는다.** '이 구간에 결정이 있었을 수 있다' 는 표시만 남기고, " +
			"판별은 다음 세션의 에이전트가 한다 — 그 모델이 이미 전체 맥락을 갖고 있다.\n\n" +
			"기본적으로 기동 **이후**의 대화만 본다. 켜기 전 기록까지 훑으려면 --backfill.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}
			c, err := config.Load(path)
			if err != nil {
				return err
			}
			if stateDir == "" {
				if stateDir, err = DefaultDir(); err != nil {
					return err
				}
			}
			if root == "" {
				if root, err = claudecode.DefaultRoot(); err != nil {
					return err
				}
			}

			out, errW := cmd.OutOrStdout(), cmd.ErrOrStderr()
			err = Run(cmd.Context(), Options{
				StateDir:       stateDir,
				TranscriptRoot: root,
				Config:         c,
				Backfill:       backfill,
				// 백그라운드라 조용히 실패하면 아무도 모른다. 전부 흘려보낸다.
				OnEvent: func(e Event) {
					switch e.Kind {
					case "error":
						if e.Err != nil {
							fmt.Fprintf(errW, "cb watch: %s: %v\n", e.Path, e.Err)
						} else {
							fmt.Fprintf(errW, "cb watch: %s\n", e.Note)
						}
					case "scan":
						r := e.Result
						if r.Turns == 0 {
							return
						}
						msg := fmt.Sprintf("훑음 %s — 발화 %d", e.Path, r.Turns)
						if r.Flagged {
							msg += fmt.Sprintf(" · 표시함 (시그널 %v)", r.Signals)
						}
						if r.Excluded {
							msg += " · 제외 구역이라 표시 안 함"
						}
						if !r.Advanced {
							msg += fmt.Sprintf(" · ⚠️ 체크포인트 미전진 (깨진 줄 %d)", r.Bad)
						}
						fmt.Fprintln(out, msg)
					default:
						fmt.Fprintf(out, "cb watch: %s\n", e.Note)
					}
				},
			})
			if errors.Is(err, ErrAlreadyRunning) {
				return fmt.Errorf("%w — 상태 디렉토리 %s", err, stateDir)
			}
			return err
		},
	}
	f := cmd.Flags()
	f.BoolVar(&backfill, "backfill", false, "기동 전 기록도 처음부터 훑는다")
	f.StringVar(&stateDir, "state-dir", "", "상태 파일 위치 (기본: $XDG_STATE_HOME/casebook)")
	f.StringVar(&root, "transcript-root", "", "transcript 루트 (기본: ~/.claude/projects)")
	return cmd
}
