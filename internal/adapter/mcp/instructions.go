// Package mcp 는 casebook 을 MCP 서버로 낸다.
//
// 어댑터 규칙(스펙 §4.1): 이 패키지는 internal/core 만 부른다. core 는 이 패키지를
// 모르고, 이 패키지는 다른 어댑터(cli·hook)를 모른다.
package mcp

import (
	"fmt"
	"strings"

	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/daemon"
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
func buildInstructions(l *store.Layout, pend pendingView) (string, []store.SkippedNote) {
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

	b.WriteString(pend.render())

	if len(skipped) > 0 {
		fmt.Fprintf(&b, "\n⚠️ 결정 노트 %d건을 읽지 못해 회수 대상에서 빠져 있다. "+
			"`casebook_recall` 이 그 목록을 알려준다.\n", len(skipped))
	}

	return b.String(), skipped
}

// pendingView 는 데몬이 표시한 미확인 구간을 instructions 에 실을 형태로 들고 있다.
//
// Err 를 따로 두는 이유: **"미확인 0건" 과 "확인할 수 없다" 는 다른 사실이다.**
// 상태 파일이 깨졌는데 0건으로 보여 주면, 안전망이 죽은 것을 안전망이 할 일이
// 없는 것으로 읽게 된다. 안전망의 침묵은 안전망이 없는 것보다 나쁘다.
type pendingView struct {
	Items   []daemon.Pending
	Err     error
	Enabled bool // 데몬 연동이 켜져 있나 (stateDir 가 있나)
}

// maxListed 는 instructions 에 이름까지 적는 최대 건수다. instructions 는 세션당 한 번
// 실리고 갱신되지 않으므로, 길어지면 그 자체가 소음이 된다. 나머지는 도구로 본다.
const maxListed = 5

func (v pendingView) render() string {
	if !v.Enabled {
		return ""
	}
	if v.Err != nil {
		return fmt.Sprintf("\n⚠️ 미확인 구간을 확인할 수 없다 (%v). 데몬 상태 파일 문제이니 "+
			"기록·회수에는 지장이 없지만, **놓친 기록을 줍는 안전망은 지금 꺼져 있다.**\n", v.Err)
	}
	if len(v.Items) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n⚠️ **데몬이 표시한 미확인 구간이 %d건 있다.** 이전 세션에서 결정을 내리고도\n"+
		"기록하지 않고 지나간 자리다. 확인해서 실제 결정이면 `casebook_capture` 로 남기고,\n"+
		"아니면 `casebook_pending` 으로 지워라 — 쌓아 두면 다음 세션에도 그대로 뜬다.\n",
		len(v.Items))
	for i, p := range v.Items {
		if i >= maxListed {
			fmt.Fprintf(&b, "  … 그 밖 %d건 (`casebook_pending` 으로 전체를 본다)\n", len(v.Items)-maxListed)
			break
		}
		domain := p.Domain
		if domain == "" {
			domain = "(도메인 미상)"
		}
		fmt.Fprintf(&b, "  - %s %s · 발화 %d · 시그널 %s\n",
			p.At.Format("2006-01-02"), domain, p.Turns, strings.Join(p.Signals, "·"))
	}
	return b.String()
}
