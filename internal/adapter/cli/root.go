package cli

import (
	"fmt"

	"github.com/spf13/cobra"
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
	return root
}

// Execute 는 CLI 를 실행한다. 에러는 호출자가 종료 코드로 옮긴다.
func Execute() error {
	if err := newRootCmd().Execute(); err != nil {
		return fmt.Errorf("cb: %w", err)
	}
	return nil
}
