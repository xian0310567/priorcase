// Package mcp 는 priorcase 을 MCP 서버로 낸다.
//
// 어댑터 규칙(스펙 §4.1): 이 패키지는 internal/core 만 부른다. core 는 이 패키지를
// 모르고, 이 패키지는 다른 어댑터(cli·hook)를 모른다.
package mcp

import (
	"fmt"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/i18n"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/daemon"
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
// 볼트가 잠깐 안 읽힌다는 이유로 priorcase_capture 까지 통째로 사라진다. 게다가
// instructions 는 에이전트에게 닿는 가장 이른 채널이므로, 여기 적는 것이 stderr 로
// 흘리는 것보다 훨씬 크게 알리는 것이다 — 조용히 넘어가는 것과는 다르다.
func buildInstructions(l *store.Layout, pend pendingView) (string, []store.SkippedNote) {
	notes, skipped, err := l.List()

	// **이 본문은 모델이 읽는 행동 계약이다.** 대화 언어와 어긋나면 도구 선택과
	// 응답 언어가 같이 나빠지므로 `lang` 을 따라간다 (tools.go 와 같은 이유).
	lang := l.Lang()

	var b strings.Builder
	b.WriteString(lang.T(
		"priorcase — 이 워크스페이스의 과거 결정을 기록하고 회수한다.\n\n",
		"priorcase — records and recalls this workspace's past decisions.\n\n"))

	if err != nil {
		fmt.Fprintf(&b, lang.T(
			"⚠️ 볼트를 읽지 못했다 (%v). 회수가 비어 나올 수 있다 — "+
				"설정의 vault 경로를 확인하라.\n\n",
			"⚠️ The vault could not be read (%v). Recall may come back empty — "+
				"check the vault path in your config.\n\n"), err)
	}

	if len(notes) > 0 {
		fmt.Fprintf(&b, lang.T(
			"**새 작업이나 주제로 넘어갈 때마다 먼저 `priorcase_recall(주제)` 를 부른다.**\n"+
				"지금 볼트에 결정 %d건이 쌓여 있다. 부르지 않으면 이미 뒤집힌 결정을 다시 제안하게 된다.\n\n",
			"**Call `priorcase_recall(topic)` first every time you move to a new task or topic.**\n"+
				"The vault currently holds %d decisions. Skip the call and you will propose something that was already overturned.\n\n"),
			len(notes))
	}

	// **기록 계약은 등급 둘을 같이 말해야 한다.**
	//
	// 옛 문구는 `priorcase_capture` 하나만 알려 주면서 "자잘한 것까지 남기면 회수가
	// 오히려 어려워진다" 로 끝났다. 그 브레이크는 명령으로 박혀 있었고, 정작 사용자가
	// 볼트에 남긴 "대안·기각 이유·번복까지 남겨라" 정책은 `[과거 결정]` 목록의
	// **데이터 한 줄**로만 들어왔다. 비대칭이 반대 방향이었다.
	//
	// 실측 결과가 그 비대칭 그대로다: 훅이 6턴 연속으로 미기록을 알렸는데 에이전트가
	// "아직 확정되지 않았다" 며 미뤘고, 그동안의 조사·측정·기각 이유가 하나도 안 남았다.
	// 브레이크만 있고 갈 곳이 없었기 때문이다. 이제 갈 곳(작업 로그)이 있다.
	b.WriteString(lang.T(
		"**기록에는 등급이 둘이다. 둘 다 네 일이다.**\n\n"+
			"1. `priorcase_note` — **확정 전의 것 전부.** 검토한 대안과 각각을 **왜 기각했는지**, "+
			"측정한 값과 그 방법, 걸린 제약, 아직 못 정한 것과 그것이 풀리는 조건.\n"+
			"   작업 로그에 쌓이고 회수에 자동 주입되지 않으므로 **자주 불러도 아무것도 나빠지지 않는다.**\n"+
			"   \"아직 안 정했으니 나중에\" 로 미루지 마라 — 그렇게 미룬 것이 지금까지 전부 사라졌다.\n"+
			"2. `priorcase_capture` — **확정된 결정.** 되돌리기 어려운 선택, 대안 중 고른 것, "+
			"실측으로 통념이 깨진 것, 그리고 **코드를 읽어도 알 수 없는 조직·프로세스 제약.**\n"+
			"   회수가 자동 주입하므로 여기는 확정된 것만 온다.\n\n"+
			"둘 중 어느 쪽인지 애매하면 `priorcase_note` 다. 버리는 것만은 하지 마라.\n\n"+
			"**본문에는 결론만 쓰지 마라.** 왜 그렇게 정했는지, 무엇을 함께 검토했고 왜 그것을 "+
			"버렸는지를 같이 남긴다. 결론만 남긴 기록은 다음에 같은 제안을 다시 하게 만든다.\n"+
			"다만 **대화에 없는 근거를 지어내지는 마라** — 근거가 없으면 없다고 적어라.\n\n",
		"**Recording has two tiers. Both are your job.**\n\n"+
			"1. `priorcase_note` — **everything before it settles.** Alternatives weighed and **why each was ruled out**, "+
			"measurements and how you took them, constraints you hit, what is still open and what would settle it.\n"+
			"   It lands in a worklog that recall never auto-injects, so **calling it often costs nothing.**\n"+
			"   Do not defer with \"nothing is decided yet\" — everything deferred that way has been lost so far.\n"+
			"2. `priorcase_capture` — **settled decisions.** Hard-to-reverse choices, the option you picked, "+
			"assumptions a measurement overturned, and **organizational or process constraints no amount of code-reading reveals.**\n"+
			"   Recall injects these automatically, so only settled things belong here.\n\n"+
			"When you cannot tell which tier fits, use `priorcase_note`. Just do not throw it away.\n\n"+
			"**Never record only the conclusion.** Say why you chose it, what else you weighed, and why you dropped those.\n"+
			"A conclusion-only record makes someone propose the same rejected thing again.\n"+
			"But **do not invent rationale that is not in the conversation** — if there is none, say so.\n\n"))

	// **`[규칙]` 줄은 배경이 아니다** (hook/start.go 와 같은 계약을 말해야 한다).
	if rules, _, rerr := l.ListRules(); rerr == nil && len(rules) > 0 {
		fmt.Fprintf(&b, lang.T(
			"**회수가 `[규칙]` 로 주는 %d건은 프로젝트 밖의 판단 기준이다.** 도메인이 없어 어디서 "+
				"물어도 같은 자격으로 걸린다 — 그 줄은 참고가 아니라 **지켜야 하는 제약**이다.\n"+
				"규칙을 새로 세웠으면 `%s/` 에 한 줄 요약과 출처 결정을 걸어 남겨라 "+
				"(`priorcase_capture` 는 결정용이라 규칙을 만들지 않는다).\n\n",
			"**The %d `[rule]` lines recall gives you are judgment criteria from outside this project.** "+
				"They carry no domain, so they match from anywhere — treat them as **constraints, not background**.\n"+
				"When you settle a new rule, leave it in `%s/` with a one-line summary and the decisions it came from "+
				"(`priorcase_capture` writes decisions, not rules).\n\n"),
			len(rules), l.RulesDirRel())
	}

	b.WriteString(lang.T(
		"**결과가 판명됐거나 결정을 뒤집었으면 `priorcase_review` 로 갱신한다.**\n"+
			"뒤집힌 결정이 그대로 남아 있으면 회수가 오염된다.\n"+
			"**무엇이 뒤집었는지도 같이 적어라** — 계기가 없으면 다음 사람이 그 번복을 신뢰하지 못한다.\n"+
			"결론이 바뀌었으면 `summary` 도 고쳐라. 회수가 주입하는 것은 그 한 줄뿐이라, "+
			"본문만 고치면 낡은 결론이 계속 주입된다.\n",
		"**When the outcome is known or you overturn a decision, update it with `priorcase_review`.**\n"+
			"An overturned decision left as-is pollutes recall.\n"+
			"**Say what overturned it** — without the trigger, the next person cannot trust the reversal.\n"+
			"If the conclusion changed, fix `summary` too. Recall injects only that one line, so editing the body "+
			"alone leaves the stale conclusion in circulation.\n"))

	b.WriteString(pend.render(lang))

	if len(skipped) > 0 {
		// 명사구를 미리 만들어 **양쪽이 같은 인자를 받게** 한다. 영어는 수 일치가
		// 있고 한국어는 없어서, 그대로 %d 를 쓰면 "1 decision notes" 가 나온다.
		fmt.Fprintf(&b, lang.T(
			"\n⚠️ 결정 노트 %s을 읽지 못해 회수 대상에서 빠져 있다. "+
				"`priorcase_recall` 이 그 목록을 알려준다.\n",
			"\n⚠️ %s missing from recall — could not be read. "+
				"`priorcase_recall` reports which ones.\n"),
			lang.Count(len(skipped), "건", "decision note", "decision notes"))
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

func (v pendingView) render(l i18n.Lang) string {
	if !v.Enabled {
		return ""
	}
	if v.Err != nil {
		return fmt.Sprintf(l.T(
			"\n⚠️ 미확인 구간을 확인할 수 없다 (%v). 데몬 상태 파일 문제이니 "+
				"기록·회수에는 지장이 없지만, **놓친 기록을 줍는 안전망은 지금 꺼져 있다.**\n",
			"\n⚠️ Unreviewed segments cannot be checked (%v). This is a daemon state-file problem, so "+
				"recording and recall still work, but **the safety net that catches missed records is off right now.**\n"),
			v.Err)
	}
	// **면제된 구간은 여기서 걸러진다.** instructions 는 사람이 청하지 않았는데
	// 들이미는 통로다 — 세션 진입에 한 번, 지우기 전까지 매번 같은 목록이 뜬다.
	// 반대로 `priorcase_pending` 도구는 사람이 직접 물은 것이라 거기서 거르면
	// 면제가 아니라 은폐가 된다. 그래서 필터가 도구가 아니라 이 자리에 있다.
	//
	// 채우는 쪽(server.readPending)이 아니라 **내는 쪽**에서 거른다: pendingView 를
	// 손으로 만드는 호출부(테스트·미래의 다른 진입점)가 필터를 빠뜨려도 안 새게 한다.
	items := daemon.ForNudge(v.Items)
	if len(items) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, l.T(
		"\n⚠️ **데몬이 표시한 미확인 구간이 %s 있다.** 이전 세션에서 결정을 내리고도\n"+
			"기록하지 않고 지나간 자리다. 확인해서 실제 결정이면 `priorcase_capture` 로 남기고,\n"+
			"아니면 `priorcase_pending` 으로 지워라 — 쌓아 두면 다음 세션에도 그대로 뜬다.\n",
		"\n⚠️ **The daemon flagged %s.** These are places where a decision was\n"+
			"made in an earlier session but never recorded. Check them: if it was a real decision, record it with\n"+
			"`priorcase_capture`; otherwise clear it with `priorcase_pending` — left alone they reappear every session.\n"),
		l.Count(len(items), "건", "unreviewed segment", "unreviewed segments"))
	for i, p := range items {
		if i >= maxListed {
			fmt.Fprintf(&b, l.T(
				"  … 그 밖 %d건 (`priorcase_pending` 으로 전체를 본다)\n",
				"  … and %d more (use `priorcase_pending` to see them all)\n"),
				len(items)-maxListed)
			break
		}
		domain := p.Domain
		if domain == "" {
			domain = l.T("(도메인 미상)", "(domain unknown)")
		}
		fmt.Fprintf(&b, l.T(
			"  - %s %s · 발화 %d · 시그널 %s\n",
			"  - %s %s · %d turns · signals %s\n"),
			p.When(), domain, p.Turns, strings.Join(p.Signals, "·"))
	}
	return b.String()
}
