// Command cb 는 casebook 의 단일 바이너리다.
//
// **조립 루트다.** 어댑터들을 여기서 한 루트 명령에 모은다. 어댑터끼리는 서로를
// 모르고(§4.1), 누가 누구를 붙일지는 이 파일만 안다.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/xian0310567/casebook/internal/adapter/cli"
	"github.com/xian0310567/casebook/internal/adapter/hook"
	"github.com/xian0310567/casebook/internal/adapter/mcp"
	"github.com/xian0310567/casebook/internal/daemon"
)

func main() {
	// cb watch 는 장기 실행 프로세스다. Ctrl-C·SIGTERM 에 ctx 가 닫혀야 감시 루프가
	// 스스로 빠져나온다. 두 번째 신호에는 즉시 죽는다(NotifyContext 의 기본 동작).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := cli.NewRootCmd()
	root.AddCommand(mcp.NewCommand(cli.Version))
	root.AddCommand(daemon.NewCommand())
	root.AddCommand(daemon.NewPendingCommand())
	root.AddCommand(hook.NewCommand())
	root.AddCommand(hook.NewInitCommand())
	root.AddCommand(hook.NewDoctorCommand())

	err := cli.Run(ctx, root)
	if err == nil {
		return
	}
	// cb doctor 는 진단 결과를 이미 stdout 에 찍었다. 종료 코드만 옮기고 입을 다문다 —
	// 여기서 또 내면 같은 말이 두 번 나온다.
	if code, silent := hook.DiagnosticExit(err); silent {
		os.Exit(code)
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
