package hook

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/daemon"
)

// recentN 은 세션 진입에 이름까지 적는 결정 수다. 이 블록은 세션당 한 번 실리고
// 갱신되지 않으므로, 길어지면 그 자체가 소음이 된다.
const recentN = 5

// sessionStart 는 MCP 의 initialize.instructions 에 해당한다. 같은 일을 하는 두 경로가
// 있는 것이라 **내용도 같은 성격이어야 한다** — 결정 덤프가 아니라 행동 계약이다.
func (o Options) sessionStart() error {
	notes, skipped, listErr := o.Layout.List()
	lang := o.Layout.Lang()

	var b strings.Builder
	b.WriteString("## priorcase\n\n")

	domain := o.Config.DomainForCwd(o.Input.Cwd)
	excluded := o.Input.Cwd != "" && o.Config.IsExcluded(o.Input.Cwd)

	switch {
	case excluded:
		// 자체 스키마 구역이다. 여기에 결정을 쓰면 그 저장소의 규약을 깬다.
		b.WriteString(lang.T(
			"**이 디렉토리는 priorcase 제외 구역이다.** 결정 노트를 만들지 마라.\n"+
				"회수는 계속 동작한다 — 읽기 전용이라 이 구역의 규약을 건드리지 않는다.\n",
			"**This directory is a priorcase exclusion zone.** Do not create decision notes here.\n"+
				"Recall still works — it is read-only, so it cannot touch this zone's conventions.\n"))
	case domain == "":
		b.WriteString(lang.T(
			"이 디렉토리는 볼트 프로젝트에 매핑되지 않는다 — 결정이 생기면 "+
				"`common` 도메인으로 기록한다.\n",
			"This directory maps to no vault project — record any decisions "+
				"under the `common` domain.\n"))
	default:
		fmt.Fprintf(&b, lang.T(
			"이 프로젝트의 도메인 접두어는 `%s` 다.\n",
			"The domain prefix for this project is `%s`.\n"), domain)
	}

	if listErr != nil {
		fmt.Fprintf(&b, lang.T(
			"\n⚠️ 볼트를 읽지 못했다 (%v). 회수가 비어 나올 수 있다.\n",
			"\n⚠️ Could not read the vault (%v). Recall may come back empty.\n"), listErr)
	}

	if !excluded {
		// MCP instructions 와 **같은 계약을 말해야 한다** (이 함수 주석 참고).
		// 한쪽만 고치면 MCP 를 안 쓰는 설치와 쓰는 설치가 서로 다른 규칙으로 돈다.
		b.WriteString(lang.T(
			"\n**기록에는 등급이 둘이다. 둘 다 네 일이다.**\n"+
				"- `prior note` — 확정 전의 것 전부. 검토한 대안과 **왜 기각했는지**, 측정값과 방법,\n"+
				"  걸린 제약, 아직 못 정한 것. 회수에 자동 주입되지 않으니 **자주 남겨도 손해가 없다.**\n"+
				"- `prior capture` — 확정된 결정. 되돌리기 어려운 선택, 고른 대안, 실측으로 깨진 통념,\n"+
				"  그리고 코드를 읽어도 알 수 없는 조직·프로세스 제약.\n"+
				"\n애매하면 `prior note` 다. \"아직 확정 안 됐으니 나중에\" 로 미루지 마라 —\n"+
				"그렇게 미룬 것이 지금까지 전부 사라졌다. **결론만 쓰지 말고 근거와 기각한 대안을 같이 남겨라.**\n",
			"\n**Recording has two tiers. Both are your job.**\n"+
				"- `prior note` — everything before it settles. Alternatives weighed and **why each was ruled out**,\n"+
				"  measurements and method, constraints you hit, what is still open. Recall never auto-injects it,\n"+
				"  so **noting often costs nothing.**\n"+
				"- `prior capture` — settled decisions. Hard-to-reverse choices, the option you picked, assumptions\n"+
				"  a measurement overturned, and organizational or process constraints no code-reading reveals.\n"+
				"\nWhen in doubt, `prior note`. Do not defer with \"nothing is settled yet\" —\n"+
				"everything deferred that way has been lost so far. **Record the rationale and the rejected options, not just the conclusion.**\n"))
	}

	if len(notes) > 0 {
		fmt.Fprintf(&b, lang.T(
			"\n### 최근 결정 (전체 %d건)\n\n",
			"\n### Recent decisions (%d total)\n\n"), len(notes))
		for _, n := range recent(notes) {
			date, sum := n.Meta.Date, n.Meta.Summary
			if date == "" {
				date = "-"
			}
			if sum == "" {
				sum = n.Stem
			}
			fmt.Fprintf(&b, "- %s `%s` %s\n", date, strings.Join(n.Meta.Domain, "·"), sum)
		}
		b.WriteString(lang.T(
			"\n주제가 바뀔 때마다 관련 결정이 자동으로 주입된다 — 더 파야 하면 `prior recall <주제>`.\n",
			"\nRelated decisions are injected automatically whenever the topic shifts — to dig further, run `prior recall <topic>`.\n"))
	}

	// **갈래마다 읽는 쪽이 할 일이 정반대다.** 이 블록은 stdout 이라 통째로 에이전트
	// 컨텍스트가 된다 — 2026-08-21 에 frontmatter 를 옛 모양으로 되돌린 바로 그 주체가
	// 읽는 자리다. 그때는 "읽지 못했다" 만 있었고, 그걸 본 쪽은 파일을 열어 "고쳤다".
	newer, broken := 0, 0
	for _, s := range skipped {
		if s.LooksNewer() {
			newer++
			continue
		}
		broken++
	}
	if newer > 0 {
		fmt.Fprintf(&b, lang.T(
			"\n⚠️ 결정 노트 %s이 **더 새 판**으로 쓰여 있어 못 읽는다 — 회수에서 빠져 있다.\n"+
				"**그 노트를 고치지 마라.** 다른 머신이 더 새 prior 로 쓴 것이고, 옛 모양으로\n"+
				"되돌리면 거기 쌓인 것을 지운다. `prior` 를 올려라 (`prior doctor` 가 목록을 준다).\n",
			"\n⚠️ %s written by a **newer priorcase** and cannot be read — missing from recall.\n"+
				"**Do not edit those notes.** Another machine wrote them with a newer prior; rewriting\n"+
				"them in the old shape destroys what it recorded. Upgrade `prior` (`prior doctor` lists them).\n"),
			lang.Count(newer, "건", "decision note", "decision notes"))
	}
	if broken > 0 {
		fmt.Fprintf(&b, lang.T(
			"\n⚠️ 결정 노트 %s을 읽지 못해 회수에서 빠져 있다. `prior doctor` 가 이유를 알려준다.\n",
			"\n⚠️ %s missing from recall — could not be read. `prior doctor` explains why.\n"),
			lang.Count(broken, "건", "decision note", "decision notes"))
	}
	b.WriteString(o.pendingBlock())

	fmt.Fprint(o.Out, b.String())
	return nil
}

// recent 는 날짜 내림차순 상위 recentN 건을 준다.
func recent(notes []store.Note) []store.Note {
	sorted := make([]store.Note, len(notes))
	copy(sorted, notes)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Meta.Date > sorted[j].Meta.Date })
	if len(sorted) > recentN {
		sorted = sorted[:recentN]
	}
	return sorted
}

// pendingBlock 은 데몬이 표시한 미확인 구간을 알린다.
//
// MCP 쪽과 같은 규율을 지킨다: **"0건" 과 "확인할 수 없다" 는 다른 사실이다.**
// 상태 파일이 깨졌는데 조용히 넘어가면 안전망이 죽은 것을 할 일이 없는 것으로 읽는다.
func (o Options) pendingBlock() string {
	if o.StateDir == "" {
		return ""
	}
	lang := o.Layout.Lang()
	items, err := daemon.ReadPending(o.StateDir)
	if err != nil {
		return fmt.Sprintf(lang.T(
			"\n⚠️ 미확인 구간을 확인할 수 없다 (%v). "+
				"**놓친 기록을 줍는 안전망이 지금 꺼져 있다.**\n",
			"\n⚠️ Cannot check for unreviewed segments (%v). "+
				"**The safety net that catches missed records is off right now.**\n"), err)
	}
	// 면제된 구간은 세션 진입 안내에서 뺀다 — 이 블록도 사람이 청하지 않았는데
	// 들이미는 통로다 (hook/recall.go 의 nudge 와 같은 이유, daemon.ForNudge 주석).
	//
	// **읽기 실패 분기 뒤에 둔다.** 위의 "확인할 수 없다" 는 면제와 무관한 사실이고,
	// 그걸 면제 필터에 태우면 상태 파일이 깨진 것이 조용해진다.
	items = daemon.ForNudge(items)
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, lang.T(
		"\n⚠️ **데몬이 표시한 미확인 구간이 %s 있다.** 이전 세션에서 결정을 내리고도\n"+
			"기록하지 않고 지나간 자리다. 확인해서 실제 결정이면 `prior capture` 로 남겨라.\n",
		"\n⚠️ **The daemon flagged %s.** These are spots where an earlier session made a\n"+
			"decision and moved on without recording it. Check them, and record the real ones with `prior capture`.\n"),
		lang.Count(len(items), "건", "unreviewed segment", "unreviewed segments"))
	for i, p := range items {
		if i >= recentN {
			fmt.Fprintf(&b, lang.T("  … 그 밖 %d건\n", "  … and %d more\n"), len(items)-recentN)
			break
		}
		d := p.Domain
		if d == "" {
			d = lang.T("(도메인 미상)", "(domain unknown)")
		}
		fmt.Fprintf(&b, lang.T(
			"  - %s %s · 발화 %d · 시그널 %s\n",
			"  - %s %s · %d turns · signals %s\n"),
			p.When(), d, p.Turns, strings.Join(p.Signals, "·"))
	}
	return b.String()
}
