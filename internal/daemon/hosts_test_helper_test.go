package daemon

import (
	"path/filepath"

	"github.com/xian0310567/priorcase/internal/transcript/claudecode"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
)

// anyHost 는 아무 경로나 Claude Code 파서로 읽게 한다.
//
// 테스트는 가짜 transcript 를 t.TempDir() 에 쓰는데 그건 어느 호스트 루트에도
// 없다. 실코드는 그럴 때 **읽지 않는다** — 엉뚱한 파서로 읽으면 발화가 0개로
// 나오고 그건 "결정이 없었다" 와 구별되지 않기 때문이다. 그 방어는 옳으므로
// 테스트가 루트를 명시한다.
func anyHost(path string) []hosts.Resolved {
	return []hosts.Resolved{{
		Host: hosts.Host{
			Name: "test", DefaultRoot: func() (string, error) { return "", nil },
			List: claudecode.List, Parse: claudecode.Parse, Required: true,
		},
		Root: filepath.Dir(path),
	}}
}
