package mcp

import (
	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
)

// NewCommand 는 `cb mcp` 서브커맨드를 만든다.
//
// 이 명령이 cli 어댑터가 아니라 여기 있는 이유: cli 가 이걸 들고 있으면 cli 가
// mcp 를 import 하게 되고, 어댑터끼리 모른다는 §4.1 이 깨진다. 조립은 조립 루트
// (cmd/cb)가 한다 — internal/arch 의 테스트가 이 경계를 강제한다.
//
// 설정 로딩이 cli.loadFrom 과 비슷해 보이지만 복제가 아니다. 경로 해석 규칙
// (플래그 → CASEBOOK_CONFIG → XDG)은 core 의 config.Load 안에 하나로 있고,
// 여기 있는 것은 그걸 부르는 배선뿐이다.
func NewCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "MCP 서버를 stdio 로 띄운다 (호스트가 실행한다)",
		Long: "MCP 서버를 stdio 로 띄운다.\n\n" +
			"사람이 직접 실행할 일은 없다 — MCP 호스트가 이 프로세스를 띄우고 " +
			"stdin/stdout 으로 JSON-RPC 를 주고받는다. 그래서 이 명령이 도는 동안 " +
			"stdout 은 프로토콜 전용이다. 진단 출력은 전부 stderr 로 나간다.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}
			c, err := config.Load(path)
			if err != nil {
				return err
			}
			return Serve(cmd.Context(), New(c, store.NewLayout(c), version))
		},
	}
}
