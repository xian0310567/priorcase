package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/judge"
	"github.com/xian0310567/priorcase/internal/daemon"
)

// promoteBudget 은 한 건짜리 승격에 쓸 시간이다.
//
// **판별기 상한(judge.DefaultTimeout)보다 커야 한다.** 작으면 승격이 "다 돌 시간이
// 안 남았다" 며 아무것도 시작하지 않는다 — 명령이 매번 조용히 실패한다.
//
// 60초로 두고 있었다. 판별기 상한이 45초였을 때 나온 값인데, 상한을 75초로 올리면서
// (2026-08-12) 그대로 뒀다면 `prior promote` 가 통째로 멎었을 것이다. 컴파일은
// 멀쩡히 통과한다 — 그래서 아래 TestPromoteBudgetFitsJudge 가 이 관계를 지킨다.
//
// 여유는 마무리(원장 쓰기·표시 해소) 몫이다. 사람이 화면 앞에서 기다리는 명령이라
// (앱의 [결정이다] 버튼) 더 늘리지는 않는다.
const promoteBudget = judge.DefaultTimeout + 15*time.Second

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
				// 세 갈래다: 그런 구간이 없다 · 판별기가 없다 · 이미 선점됐다.
				// stderr 에 그중 무엇인지 이미 나갔으므로 여기서 겹쳐 말하지 않는다.
				return fmt.Errorf("승격이 일어나지 않았다 (%s) — 위 이유를 보라 "+
					"(구간이 없거나, 판별기를 못 찾았거나, 조금 전 시도가 선점 중이다)", id)
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
