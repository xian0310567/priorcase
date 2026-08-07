// Command cb 는 casebook 의 단일 바이너리다.
//
// **조립 루트다.** 어댑터들을 여기서 한 루트 명령에 모은다. 어댑터끼리는 서로를
// 모르고(§4.1), 누가 누구를 붙일지는 이 파일만 안다.
package main

import (
	"fmt"
	"os"

	"github.com/xian0310567/casebook/internal/adapter/cli"
	"github.com/xian0310567/casebook/internal/adapter/mcp"
)

func main() {
	root := cli.NewRootCmd()
	root.AddCommand(mcp.NewCommand(cli.Version))

	if err := cli.Run(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
