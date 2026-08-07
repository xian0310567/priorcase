// Package mcp 는 casebook 을 MCP 서버로 낸다.
//
// 어댑터 규칙(스펙 §4.1): 이 패키지는 internal/core 만 부른다. core 는 이 패키지를
// 모르고, 이 패키지는 다른 어댑터(cli·hook)를 모른다.
package mcp

import (
	"fmt"
	"strings"

	"github.com/xian0310567/casebook/internal/core/store"
)

// buildInstructions 는 initialize 응답에 실릴 본문을 만든다.
//
// **최근 결정 덤프가 아니라 행동 계약이다** (스펙 §8). MCP 에는 서버가 대화 중간에
// 텍스트를 밀어넣을 채널이 없어서, 세션당 한 번뿐인 이 자리에 요약을 쏟아부어도
// 주제가 바뀌는 순간 낡는다. 대신 "언제 무엇을 부르라"를 심어 두면 에이전트가
// 필요한 시점에 스스로 회수한다.
//
// 볼트 읽기가 실패해도 에러를 돌려주지 않고 본문에 적어 넣는다. 이 함수의 호출자는
// 서버 생성자뿐이라 에러를 받아도 할 수 있는 일이 "기동 실패" 밖에 없는데, 그러면
// 볼트가 잠깐 안 읽힌다는 이유로 casebook_capture 까지 통째로 사라진다. 게다가
// instructions 는 에이전트에게 닿는 가장 이른 채널이므로, 여기 적는 것이 stderr 로
// 흘리는 것보다 훨씬 크게 알리는 것이다 — 조용히 넘어가는 것과는 다르다.
func buildInstructions(l *store.Layout) (string, []store.SkippedNote) {
	notes, skipped, err := l.List()

	var b strings.Builder
	b.WriteString("casebook — 이 워크스페이스의 과거 결정을 기록하고 회수한다.\n\n")

	if err != nil {
		fmt.Fprintf(&b, "⚠️ 볼트를 읽지 못했다 (%v). 회수가 비어 나올 수 있다 — "+
			"설정의 vault 경로를 확인하라.\n\n", err)
	}

	if len(notes) > 0 {
		fmt.Fprintf(&b, "**새 작업이나 주제로 넘어갈 때마다 먼저 `casebook_recall(주제)` 를 부른다.**\n"+
			"지금 볼트에 결정 %d건이 쌓여 있다. 부르지 않으면 이미 뒤집힌 결정을 다시 제안하게 된다.\n\n",
			len(notes))
	}

	b.WriteString("**되돌리기 어려운 선택을 했으면 그 자리에서 `casebook_capture` 를 부른다.**\n" +
		"아키텍처·스키마·외부 서비스·가격처럼 나중에 \"왜 이렇게 했지\"를 묻게 될 선택이 대상이다.\n" +
		"대안을 검토해 하나를 골랐거나, 실측으로 통념이 깨졌을 때도 해당한다.\n" +
		"자잘한 것까지 남기면 회수가 오히려 어려워진다.\n\n")

	b.WriteString("**결과가 판명됐거나 결정을 뒤집었으면 `casebook_review` 로 갱신한다.**\n" +
		"뒤집힌 결정이 그대로 남아 있으면 회수가 오염된다.\n")

	if len(skipped) > 0 {
		fmt.Fprintf(&b, "\n⚠️ 결정 노트 %d건을 읽지 못해 회수 대상에서 빠져 있다. "+
			"`casebook_recall` 이 그 목록을 알려준다.\n", len(skipped))
	}

	return b.String(), skipped
}
