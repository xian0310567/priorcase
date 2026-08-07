package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/index"
)

func newIndexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "index",
		Short: "결정 색인을 재생성한다",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			n, err := index.Write(l)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "색인 %d행 생성\n", n)
			return nil
		},
	}
}
