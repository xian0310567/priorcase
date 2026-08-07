package mcp

import (
	"fmt"
	"strings"

	"github.com/xian0310567/casebook/internal/core/store"
)

// renderSkipped 는 읽지 못해 건너뛴 결정 노트를 **응답 본문에 실을 문자열**로 만든다.
//
// CLI 는 같은 정보를 stderr 로 낸다. MCP 에서 그렇게 하면 호스트 로그로 흘러가고
// 에이전트 컨텍스트에는 들어가지 않는다 — 회수에서 노트가 빠졌다는 사실을 정작
// 회수하는 쪽이 모르게 된다. 어댑터마다 노출 수단을 다시 고르라는 것이
// [[casebook-결정-건너뛰기정책-침묵금지-2026-08-07]] 의 요지였고, 여기서는 본문이 답이다.
func renderSkipped(l *store.Layout, skipped []store.SkippedNote) string {
	if len(skipped) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n⚠️ 결정 노트 %d건을 읽지 못해 색인·회수에서 빠졌다:\n", len(skipped))
	for _, s := range skipped {
		reason := strings.ReplaceAll(strings.TrimRight(fmt.Sprint(s.Reason), "\n"), "\n", "\n      ")
		fmt.Fprintf(&b, "  - %s\n      %s\n", l.RelPath(s.Path), reason)
	}
	b.WriteString("정본 10키로 옮겨야 회수 대상으로 돌아온다.\n")
	return b.String()
}
