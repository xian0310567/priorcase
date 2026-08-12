package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/daemon"
)

// promoteBudget 은 한 건짜리 승격에 쓸 시간이다.
//
// 훅의 예산(90초)보다 짧다. 훅은 큐 전체를 도는데 여기는 한 건이고, 무엇보다
// **사람이 화면 앞에서 기다린다** — 앱의 [결정이다] 버튼이 이걸 부른다.
// 판별기 한 건이 실측 10초 안팎이고 상한이 45초이므로 그것을 담을 만큼만 준다.
const promoteBudget = 60 * time.Second

// PromoteResult 는 `prior promote --json` 의 출력이다. 앱과의 계약이다.
type PromoteResult struct {
	ID       string `json:"id"`
	Recorded bool   `json:"recorded"`
	// Path 는 만들어진 노트 (볼트 상대 경로). Recorded 일 때만 있다.
	Path string `json:"path,omitempty"`
	// Reason 은 기록하지 않은 이유다. 판별기가 준 말 그대로.
	Reason string `json:"reason,omitempty"`
	// Error 는 판별기를 못 불렀을 때다. **Recorded=false 와 구별해야 한다** —
	// "결정이 아니라고 판정했다" 와 "판정을 못 했다" 는 사람이 할 일이 다르다.
	Error string `json:"error,omitempty"`
}

func newPromoteCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "promote <id>",
		Short: "구간 하나를 판별기에 넘겨 결정 노트로 만든다",
		Long: "미확인 구간 하나를 지목해 판별기에 넘긴다. `prior pending` 이 주는 id 를 쓴다.\n\n" +
			"훅은 세션이 끝날 때 큐 전체를 돈다. 이 명령은 그 중 하나를 지금 처리한다 —\n" +
			"데스크탑 앱의 [결정이다] 가 이것을 부른다.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			sd, err := daemon.DefaultDir()
			if err != nil {
				return err
			}

			wd, _ := os.Getwd()
			id := args[0]
			var got *daemon.Promotion
			// 진행 보고는 언제나 stderr 다. stdout 은 JSON 전용이라 섞이면 앱이 못 읽는다.
			ctx, cancel := context.WithTimeout(cmd.Context(), promoteBudget+30*time.Second)
			defer cancel()

			daemon.Promote(ctx, daemon.PromoteOptions{
				StateDir: sd, Config: c, Layout: l,
				Only:     id,
				Budget:   promoteBudget,
				Author:   c.AuthorFor(wd),
				Err:      cmd.ErrOrStderr(),
				Label:    "prior promote",
				OnResult: func(p daemon.Promotion) { got = &p },
			})

			// **결과가 없으면 조용히 끝내지 않는다.** 판별기가 없거나 그런 구간이
			// 없으면 아무 일도 안 일어나는데, 앱은 버튼을 눌렀으므로 무언가 됐다고 본다.
			if got == nil {
				return fmt.Errorf("승격이 일어나지 않았다 (%s) — "+
					"그런 구간이 없거나 판별기를 못 찾았다. prior doctor 를 보라", id)
			}

			res := PromoteResult{
				ID: got.ID, Recorded: got.Recorded,
				Path: got.Path, Reason: got.Reason, Error: got.Err,
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(res)
			}
			switch {
			case res.Error != "":
				return fmt.Errorf("판별기를 부르지 못했다: %s", res.Error)
			case res.Recorded:
				fmt.Fprintf(cmd.OutOrStdout(), "기록됨: %s\n", res.Path)
			default:
				fmt.Fprintf(cmd.OutOrStdout(), "기록 안 함: %s\n", res.Reason)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON 으로 출력한다 (앱이 쓴다)")
	return cmd
}
