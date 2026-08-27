package hook

import (
	"fmt"
	"strings"
	"time"

	"github.com/xian0310567/priorcase/internal/core/retro"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/daemon"
)

// minPromptLen 은 이보다 짧은 프롬프트에서는 회수하지 않는다.
//
// "고마워"·"응"·"ㅇㅇ" 같은 프롬프트에도 훅이 발동해서, 옛 구현의 로그가
// `stage=no-match` 와 억지 매칭으로 가득했다. 짧은 프롬프트는 주제를 담지 않는다.
const minPromptLen = 8

// userPromptSubmit 은 **이 어댑터의 존재 이유**다.
//
// MCP 에는 서버가 대화 중간에 텍스트를 밀어넣는 채널이 없다. 주제가 바뀌는 순간
// 과거 결정을 강제로 들이미는 것은 호스트 훅으로만 된다 (스펙 §9 의 굵은 칸).
func (o Options) userPromptSubmit() error {
	prompt := strings.TrimSpace(o.Input.Prompt)
	if len([]rune(prompt)) < minPromptLen {
		return nil
	}

	// **제외 구역에서도 회수한다.** 쓰기 경로만 막는 것이지 읽기까지 막을 이유가 없다.
	// NOI 처럼 자체 스키마를 쓰는 저장소에서도 common 교훈은 꺼내 써야 하고, 회수는
	// 읽기 전용이라 그 저장소의 규약을 건드릴 수 없다.
	hits, skipped, err := search.Recall(o.Layout, o.Config, prompt, search.Options{
		Cwd: o.Input.Cwd, CrossProject: true, Limit: 3, MinScore: 1,
		// **참고 문서도 본다.** 실측(2026-08-13)으로 볼트 245건 중 98건이 결정이
		// 아니었고, 실제 질의 51개로 재 보니 참고를 넣었을 때 1위가 바뀐 것이
		// 9건인데 결정이 밀려난 것은 1건뿐이었다 — 비어 있던 자리를 채운다.
		//
		// 주입은 [참고] 로 갈라 보여 준다. 안 그러면 기획 초안이 확정된 결정으로
		// 읽힌다.
		IncludeReferences: true,
		// 참고에 두 자리. 매 프롬프트에 실리므로 이보다 늘리면 그 자체가
		// 컨텍스트를 잡아먹는다.
		ReferenceLimit: 2,
		// **규칙에 두 자리** (search.Options.RuleLimit 의 § 참고). 주입은 최대
		// 일곱 줄이 된다.
		//
		// 이 두 줄이 **이 시스템을 만든 이유에 가장 가까운 자리**다. 결정은
		// 프로젝트의 사건이라 다른 프로젝트에서 그 낱말로 물을 수 없는데(실측:
		// 도메인 쌍 어휘 Jaccard 평균 0.046), 규칙은 도메인이 없어 어디서 물어도
		// 같은 자격으로 걸린다. 실측(2026-08-27)으로 교차 프로젝트 주입은 이미
		// 57%였고 문제는 그것이 안 쓰이는 것(4.4%)이었다 — 사건 서술이라 쓸 수가
		// 없었다.
		RuleLimit: 2,
	})
	if err != nil {
		return err
	}

	// 건너뛴 노트는 **stderr 로만** 낸다. 여기 stdout 은 훅이 그대로 컨텍스트에
	// 밀어넣는 순수 데이터라 한 줄이라도 섞이면 "[과거 결정 참조]" 블록이 오염된다.
	warnSkipped(o.Err, o.Layout, skipped)

	// **이미 이 세션에 넣은 것은 표시만 달아 둔다** (seen.go). 실측으로 주입의
	// 42%가 재주입이었다. 무엇을 어떻게 그릴지는 아래 방출기가 정한다.
	hits = o.markSeen(hits)

	// 렌더러를 새로 만들지 않는다 — 이 프로젝트가 "방출기 두 벌" 을 죄목으로 든다.
	if s := search.RenderInject(o.Layout, hits); s != "" {
		fmt.Fprint(o.Out, s)
	}
	fmt.Fprint(o.Out, o.askOutcome(hits))
	fmt.Fprint(o.Out, o.nudge())
	return nil
}

// askOutcome 은 **방금 꺼낸 결정의 결과를 그 자리에서 묻는다.**
//
// 실측(2026-08-14): 결정 157건 중 결과가 적힌 것이 2건(1.3%)이다. 회고 큐에는
// 52건이 쌓여 있는데 아무도 안 본다 — 목록을 보러 가는 일은 따로 시간을 내는
// 일이고, 따로 시간을 내는 일은 안 일어난다.
//
// 결과를 아는 순간은 그 주제를 다시 다룰 때다. 회수가 그 결정을 이미 눈앞에
// 꺼내 놓은 자리에서 물으면 답하는 비용이 한 줄이다.
//
// 고를 것이 없으면 **아무것도 안 낸다.** 매 프롬프트마다 무언가를 묻는 것이
// 이 물음을 죽이는 가장 빠른 길이다 (판정은 retro.Ask 가 한다).
func (o Options) askOutcome(hits []search.Hit) string {
	// **한 세션에 같은 것을 두 번 묻지 않는다.**
	//
	// Ask 는 1위 노트를 고르는데, 같은 주제로 대화가 이어지면 그 노트가 계속 1위다.
	// 세션 스로틀이 없으면 매 프롬프트마다 **같은 물음이 요약까지 붙어** 반복된다.
	// 답을 모르면 매번 넘어가게 되고, 그러면 이 물음은 배경이 되어 죽는다 —
	// 이 함수 주석이 경계하는 "매 프롬프트마다 무언가를 묻는 것" 그대로다.
	//
	// Seen 은 그 노트를 이 세션에서 이미 주입했다는 뜻이고, 주입했다면 그때 물었다.
	if len(hits) > 0 && hits[0].Seen {
		return ""
	}
	notes := make([]store.Note, 0, len(hits))
	for _, h := range hits {
		notes = append(notes, h.Note)
	}
	n, ok := retro.Ask(notes, time.Now())
	if !ok {
		return ""
	}
	summary := strings.TrimSpace(n.Meta.Summary)
	if summary == "" {
		summary = n.Stem
	}
	lang := o.Layout.Lang()
	var b strings.Builder
	b.WriteString("\n" + lang.T("[결과를 아직 모르는 결정]", "[Decision with unknown outcome]") + "\n")
	if n.Meta.Status == "superseded" {
		fmt.Fprintf(&b, lang.T(
			"방금 꺼낸 %s 는 **뒤집힌 결정**인데 결과가 안 적혀 있다.\n  %s\n",
			"%s, just recalled, was **superseded** but has no recorded outcome.\n  %s\n"),
			n.Stem, summary)
	} else {
		fmt.Fprintf(&b, lang.T(
			"방금 꺼낸 %s (%s) 는 결과가 안 적혀 있다.\n  %s\n",
			"%s (%s), just recalled, has no recorded outcome.\n  %s\n"),
			n.Stem, n.Meta.Date, summary)
	}
	fmt.Fprintf(&b, lang.T(
		"**지금 대화에서 그 결과를 알 수 있으면** `prior review %s --outcome good|bad --retro \"...\"` 로 남겨라. 모르면 넘어가라.\n",
		"**If this conversation reveals how it turned out**, record it with `prior review %s --outcome good|bad --retro \"...\"`. If you cannot tell, skip it.\n"),
		n.Stem)
	return b.String()
}

// nudgeExcerpt 는 주입에 실을 발췌의 상한이다. 매 프롬프트에 들어가므로 짧아야 한다 —
// 길면 그 자체가 컨텍스트를 잡아먹고, 그러면 무시하는 법을 배운다.
const nudgeExcerpt = 700

// nudge 는 **기록되지 않은 결정이 있다고 매 프롬프트마다 들이민다.**
//
// 세션 진입 안내는 세션당 한 번뿐이라 그 뒤에 생긴 구간을 못 알린다. 회수 주입은
// 매 프롬프트마다 도는 유일한 통로이고, 그래서 여기에 얹는다.
//
// **발췌를 같이 싣는 것이 핵심이다.** "미확인 구간 1건" 만 알리면 에이전트가 확인하려고
// transcript 를 다시 읽어야 하는데, 그 비용이 크면 그냥 넘어간다. 무엇을 기록할지
// 눈앞에 있으면 부르는 것이 읽는 것보다 싸진다.
func (o Options) nudge() string {
	if o.StateDir == "" || o.Config == nil {
		return ""
	}
	items, err := daemon.ReadPending(o.StateDir)
	if err != nil {
		return ""
	}
	// **여기가 면제를 실제로 빼는 자리다.**
	//
	// 면제(credit)는 예전에 pending 자체를 지웠고, 그래서 판별기까지 그 구간을 못 봤다 —
	// 최근 7일 판정 23건에 자동 기록 0건이던 고장의 한 갈래다. 이제 면제는 구간을
	// 지우지 않고 Quiet 표시만 남기므로, **묻지 않았는데 들이미는 쪽이 직접 빼야 한다.**
	// 회수 주입은 매 프롬프트마다 도는 통로라 그중에서도 가장 시끄러운 자리다.
	//
	// 반대로 `prior pending`(사람이 직접 물었다)과 승격(daemon/promote.go)에는 이걸
	// 걸지 않는다 — 전자는 은폐고 후자는 방금 고친 그 고장이다 (daemon.ForNudge 주석).
	items = daemon.ForNudge(items)
	if len(items) == 0 {
		return ""
	}
	domain := o.Config.DomainForCwd(o.Input.Cwd)

	// 지금 작업 중인 프로젝트 것만 들이민다. 다른 프로젝트의 미확인 구간을 여기서
	// 꺼내면 맥락에 안 맞고, 맥락에 안 맞는 경고가 무시를 학습시킨다.
	var mine []daemon.Pending
	for _, p := range items {
		if p.Domain == domain && domain != "" {
			mine = append(mine, p)
		}
	}
	if len(mine) == 0 {
		return ""
	}
	p := mine[len(mine)-1] // 가장 최근 것 하나만

	ex := strings.TrimSpace(p.Excerpt)
	if r := []rune(ex); len(r) > nudgeExcerpt {
		ex = "…" + string(r[len(r)-nudgeExcerpt:])
	}

	lang := o.Layout.Lang()
	var b strings.Builder
	b.WriteString("\n" + lang.T("[기록되지 않은 결정]", "[Unrecorded decision]") + "\n")
	fmt.Fprintf(&b, lang.T(
		"%s 의 %s 구간(발화 %d)에서 결정 시그널이 잡혔는데 결정 노트가 없다.\n",
		"A decision signal was found in %s on %s (%d turns) but no decision note exists.\n"),
		p.Domain, p.When(), p.Turns)
	if len(mine) > 1 {
		fmt.Fprintf(&b, lang.T("이런 구간이 %d건 더 있다.\n",
			"%d more such segments exist.\n"), len(mine)-1)
	}
	if ex != "" {
		b.WriteString("\n--- " + lang.T("그 구간", "the segment") + " ---\n" + ex + "\n---\n")
	}
	b.WriteString("\n" + lang.T(
		"실제 결정이면 지금 `prior capture` 로 남겨라. 결정이 아니면 `prior pending --resolve` 로 지워라. 그대로 두면 매번 다시 뜬다.\n",
		"If this is a real decision, record it now with `prior capture`. If not, clear it with `prior pending --resolve`. It will keep appearing until you do.\n"))
	return b.String()
}

// warnSkipped 는 읽지 못한 노트를 stderr 로 알린다. cli 의 같은 이름 함수와 문구가
// 비슷하지만 **어댑터끼리 공유하지 않는다** (§4.1) — 공유하려면 core 로 내려야 하는데,
// 이건 표시 문제라 core 의 관심사가 아니다.
func warnSkipped(w interface{ Write([]byte) (int, error) }, l *store.Layout, skipped []store.SkippedNote) {
	if len(skipped) == 0 || w == nil {
		return
	}
	fmt.Fprintf(w, "경고: 결정 노트 %d건을 읽지 못해 회수에서 빠졌다:\n", len(skipped))
	for _, s := range skipped {
		reason := strings.ReplaceAll(strings.TrimRight(fmt.Sprint(s.Reason), "\n"), "\n", "\n      ")
		fmt.Fprintf(w, "  - %s\n      %s\n", l.RelPath(s.Path), reason)
	}
}
