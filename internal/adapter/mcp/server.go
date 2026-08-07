package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/daemon"
)

// server 는 도구 핸들러들이 공유하는 상태다. 핸들러마다 설정을 다시 읽지 않는다 —
// 한 세션 안에서 설정이 갈리면 도구마다 다른 볼트를 보게 된다.
type server struct {
	c *config.Config
	l *store.Layout
	// stateDir 는 데몬이 pending 을 쌓는 자리다. 빈 문자열이면 pending 기능이 꺼진다
	// (데몬을 안 쓰는 설치).
	stateDir string
}

// New 는 도구가 전부 붙은 MCP 서버를 만든다.
//
// version 을 인자로 받는 이유: 릴리스 버전은 ldflags 로 adapter/cli 에 주입되는데,
// 여기서 그걸 읽으려면 어댑터가 어댑터를 import 해야 한다. 문자열로 받으면 의존이
// 한 방향으로 유지된다 (cli → mcp).
//
// stateDir 는 데몬(cb watch)이 미확인 구간을 쌓는 자리다. 비우면 pending 이 꺼진다.
func New(c *config.Config, l *store.Layout, version, stateDir string) *sdk.Server {
	s := &server{c: c, l: l, stateDir: stateDir}
	instructions, _ := buildInstructions(l, s.readPending())

	srv := sdk.NewServer(
		&sdk.Implementation{Name: "casebook", Version: version},
		&sdk.ServerOptions{Instructions: instructions},
	)
	s.addTools(srv)
	return srv
}

// Serve 는 stdio 전송으로 서버를 돌린다. 호스트가 프로세스를 띄우고 종료시킨다.
//
// **이 함수가 도는 동안 stdout 은 JSON-RPC 전용이다.** core 든 어댑터든 stdout 에
// 한 줄이라도 찍으면 세션이 깨진다. 진단 출력은 전부 stderr 로 보낸다.
func Serve(ctx context.Context, srv *sdk.Server) error {
	return srv.Run(ctx, &sdk.StdioTransport{})
}

// readPending 은 데몬 상태를 읽는다. 데몬이 안 돌아도, 한 번도 안 켰어도 안전하다.
func (s *server) readPending() pendingView {
	if s.stateDir == "" {
		return pendingView{}
	}
	items, err := daemon.ReadPending(s.stateDir)
	return pendingView{Items: items, Err: err, Enabled: true}
}
