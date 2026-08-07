package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
)

// Version 은 릴리스 시 -ldflags 로 주입된다.
var Version = "dev"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cb",
		Short:         "casebook — 결정을 기록하고 회수한다",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}
	root.PersistentFlags().String("config", "", "설정 파일 경로 (기본: $XDG_CONFIG_HOME/casebook/config.toml)")
	root.AddCommand(newIndexCmd())
	root.AddCommand(newRecallCmd())
	root.AddCommand(newCaptureCmd())
	root.AddCommand(newReviewCmd())
	return root
}

// loadFrom 은 --config 플래그로 설정을 읽고 그 설정과 Layout 을 함께 준다.
//
// 설정 로딩 진입점이 하나뿐이어야 하는 이유: Layout 은 config.Config 를 비공개
// 필드로 감추므로 Config 도 필요한 명령(capture·recall)은 Layout 만 받아서는
// 일을 못 한다. 그래서 예전에는 index·review 만 이 헬퍼를 쓰고 capture·recall
// 은 config.Load 를 직접 불렀는데, 그러면 설정 경로 해석 규칙(플래그 →
// CASEBOOK_CONFIG → XDG)이 두 자리에 생겨 한쪽만 고치는 사고가 난다.
// 둘을 같이 돌려주면 갈래가 없어진다.
func loadFrom(cmd *cobra.Command) (*config.Config, *store.Layout, error) {
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return nil, nil, err
	}
	c, err := config.Load(path)
	if err != nil {
		return nil, nil, err
	}
	return c, store.NewLayout(c), nil
}

// Execute 는 CLI 를 실행한다. 에러는 호출자가 종료 코드로 옮긴다.
func Execute() error {
	if err := newRootCmd().Execute(); err != nil {
		return fmt.Errorf("cb: %w", err)
	}
	return nil
}
