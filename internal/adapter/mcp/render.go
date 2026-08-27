package mcp

import (
	"fmt"
	"github.com/xian0310567/priorcase/internal/core/capture"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// renderSkipped 는 읽지 못해 건너뛴 결정 노트를 **응답 본문에 실을 문자열**로 만든다.
//
// CLI 는 같은 정보를 stderr 로 낸다. MCP 에서 그렇게 하면 호스트 로그로 흘러가고
// 에이전트 컨텍스트에는 들어가지 않는다 — 회수에서 노트가 빠졌다는 사실을 정작
// 회수하는 쪽이 모르게 된다. 어댑터마다 노출 수단을 다시 고르라는 것이
// [[priorcase-결정-건너뛰기정책-침묵금지-2026-08-07]] 의 요지였고, 여기서는 본문이 답이다.
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

// renderDroppedRelated 는 **대상이 없어서 빼 버린 related** 를 도구 응답에 싣는다.
//
// **이 문자열은 에이전트가 읽고 행동을 바꾸는 지시문이다.** 조용히 빼면 에이전트는
// 링크를 걸었다고 믿고 다음 기록에서 같은 이름을 또 쓴다 — 실볼트에서 같은 오타가
// 여러 노트에 반복된 사례가 실제로 있었다(`봌라우잠` 2건, `추족으로` 2건).
// 그래서 고치는 방법까지 같이 준다.
func renderDroppedRelated(dropped []capture.DroppedLink) string {
	if len(dropped) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n⚠️ related %d건은 볼트에 그런 문서가 없어 **빼놨다**:\n", len(dropped))
	for _, d := range dropped {
		if d.Suggest != "" {
			fmt.Fprintf(&b, "  - %s\n    혹시 이것인가: %s\n", d.Value, d.Suggest)
		} else {
			fmt.Fprintf(&b, "  - %s (가까운 이름도 없다)\n", d.Value)
		}
	}
	b.WriteString("  이름은 **기억으로 쓰지 말고** priorcase_recall 이 준 stem 을 그대로 옮겨라.\n")
	b.WriteString("  맞는 이름을 찾았으면 priorcase_review 의 related 로 다시 걸어라.\n")
	return b.String()
}
