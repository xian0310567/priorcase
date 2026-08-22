package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/sync"
	"github.com/xian0310567/priorcase/internal/daemon"
)

// newSyncCmd 는 `prior sync` 다.
//
// **볼트를 여러 머신에서 쓰는 사람을 위한 것이다.** 집에서 내린 결정이 회사에서
// 회수되려면 볼트가 옮겨져야 하는데, 볼트는 그냥 마크다운 디렉토리라 git 이면
// 충분하다. 우리가 더할 것은 "매번 기억하지 않아도 되게" 뿐이다.
//
// **실패해도 종료 코드는 0 이다.** 훅이 이걸 부르고, 훅은 무슨 일이 있어도 대화를
// 막지 않는다 — 회사망에서 push 가 막혔다고 세션이 죽으면 안 된다. 대신 무엇이
// 실패했는지 반드시 찍고, 도장을 남겨 doctor 가 나중에 읽게 한다.
func newSyncCmd() *cobra.Command {
	var pullOnly, pushOnly bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "볼트를 git 리모트와 맞춘다 (여러 머신에서 쓸 때)",
		Long: "볼트를 git 리모트와 맞춘다.\n\n" +
			"기본은 pull 뒤 push 다. 리모트가 없거나 git 저장소가 아니면 조용히 건너뛴다 —\n" +
			"한 머신에서만 쓰는 볼트가 그렇고, 그건 고장이 아니라 설정이다.\n\n" +
			"**상태 디렉토리는 동기화하지 않는다.** 미확인 구간이 그 머신의 transcript\n" +
			"절대 경로를 키로 쓰므로, 옮기면 영영 해소되지 않는 유령 항목이 생긴다.",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			rs := sync.All(c, sync.Options{Stamp: sync.ThisBuild()}, !pushOnly, !pullOnly, sync.CommitMessage(time.Now()))
			renderSync(cmd.OutOrStdout(), rs)

			sd, _ := daemon.DefaultDir() // 못 구해도 동기화는 했다. 도장만 못 남긴다.
			failed := stampSync(sd, rs)
			if failed {
				// 종료 코드는 0 이다 (위 §). 사람이 볼 수 있게 표시만 한다.
				fmt.Fprintln(cmd.ErrOrStderr(),
					"동기화에 실패한 볼트가 있다 — prior doctor 가 계속 알린다")
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&pullOnly, "pull", false, "가져오기만 한다")
	f.BoolVar(&pushOnly, "push", false, "보내기만 한다")
	cmd.MarkFlagsMutuallyExclusive("pull", "push")
	return cmd
}

// renderSync 는 볼트별로 한 줄씩 낸다.
func renderSync(w io.Writer, rs []sync.VaultResult) {
	for _, v := range rs {
		name := v.Name
		if len(rs) == 1 {
			name = "볼트"
		}
		fmt.Fprintf(w, "%s %s: %s\n", syncMark(v), name, summarizeSync(v))
	}
}

func syncMark(v sync.VaultResult) string {
	switch {
	case v.Failed():
		return "⚠"
	case v.OK():
		return "✓"
	}
	return "·" // 건너뜀 — 고장이 아니다
}

// summarizeSync 는 pull·push 결과를 한 줄로 접는다.
func summarizeSync(v sync.VaultResult) string {
	var parts []string
	for _, r := range v.Results {
		switch {
		case r.Err != nil:
			parts = append(parts, r.Err.Error())
		case r.Skipped != "":
			parts = append(parts, r.Skipped)
		case r.Pulled:
			parts = append(parts, "가져옴")
		case r.Pushed && r.Files > 0:
			parts = append(parts, fmt.Sprintf("%d개 보냄", r.Files))
		case r.Pushed:
			parts = append(parts, "보낼 것 없음")
		}
	}
	if len(parts) == 0 {
		return "할 것 없음"
	}
	return strings.Join(parts, " · ")
}

// stampSync 는 마지막 시도를 상태 디렉토리에 남기고 실패 여부를 준다.
// 도장은 doctor 가 **조용한 실패**를 읽는 유일한 근거다.
func stampSync(stateDir string, rs []sync.VaultResult) bool {
	var bad []string
	for _, v := range rs {
		if v.Failed() {
			bad = append(bad, v.Name)
		}
	}
	if stateDir != "" {
		detail := ""
		if len(bad) > 0 {
			detail = "실패한 볼트: " + strings.Join(bad, ", ")
		}
		_ = sync.WriteStamp(stateDir, sync.Stamp{
			At: time.Now(), OK: len(bad) == 0, Detail: detail,
		})
	}
	return len(bad) > 0
}
