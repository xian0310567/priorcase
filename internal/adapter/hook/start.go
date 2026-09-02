package hook

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/i18n"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/daemon"
)

// recentN 은 세션 진입에 이름까지 적는 결정 수다. 이 블록은 세션당 한 번 실리고
// 갱신되지 않으므로, 길어지면 그 자체가 소음이 된다.
const recentN = 5

// ── 세션 진입 예산 ────────────────────────────────────────────────────
//
// **자리 수가 아니라 글자 수로 잡는다.** 요약 길이가 제각각이라 건수 상한은 조용히
// 드리프트한다 — 같은 "5건" 이 어떤 볼트에서는 400자이고 어떤 볼트에서는 2,000자다.
// 회수 점수가 head 길이를 BM25 로 정규화하는 것과 같은 이유다(search.go 의 §).
//
// # 실측 (2026-09-02, 실볼트 결정 651건 · 규칙 11건)
//
//	SessionStart 현행   1,602자 = 고정 계약부 545 + 최근 결정 5건 1,057 (66%)
//	규칙 11건 요약 합계  1,103자 (중앙 88자 · 최대 184)
//
// 그래서 **콜드스타트 교체는 예산 중립이다** — 최근 결정 5건이 나가고 규칙이 들어온다.
const (
	// **웜스타트에는 상한을 두지 않는다.** 상수를 하나 선언해 놓고 그 경로에서
	// 강제하지 않으면 다음 사람이 지켜지는 줄 안다 — 실제로 웜스타트 실측이
	// 1,830자로 여기 적으려던 1,800을 이미 넘는다(미확인 구간 알림이 붙을 때다).
	//
	// 자를 근거도 없다. 최근 결정 5건은 자리별 상대값이 1.00·1.00·0.81·0.88·0.75 로
	// 꼬리가 안 죽었고, 뒤에 붙는 것들(판 갈림 경고·미확인 구간)은 **있을 때만 붙는
	// 사건**이라 자르면 안 되는 것들이다. 그래서 예산은 콜드스타트에만 있다.

	// coldBudget 은 **콜드스타트에만** 주는 상한이다.
	//
	// # 왜 이 자리에만 예산이 있고, 왜 이 값인가
	//
	// 셋 다 실측이다.
	//
	//  1. 콜드스타트가 내주는 자리(최근 결정 5건)는 **관련도가 0으로 측정됐다** —
	//     cosbot 에서 5건 중 0건이 그 프로젝트와 관련 있었다. 경쟁 상대가 없다.
	//  2. 콜드스타트에는 **UserPromptSubmit 회수가 받쳐 주지 못한다.** 걸릴 어휘가
	//     아직 볼트에 없어서다. 볼트의 교차 프로젝트 지식이 들어올 유일한 통로다.
	//  3. **드물다.** 14일에 새 작업 디렉토리 19개다. 누적 비용이 작다.
	//
	// # 왜 2,400인가
	//
	// 1,800 으로 뒀더니 **이 기능을 만든 이유인 규칙이 정확히 잘렸다** — 실볼트에서
	// 규칙 11건 중 10건이 실리고 `규칙-브라우저가-필요하면-orca-CLI…`(184자, 가장 긴
	// 것)가 떨어졌다. 그 규칙 하나가 안 실려서 cosbot 사고가 났는데 고치려고 만든
	// 블록이 그것부터 버린 것이다. 2,400 이면 실볼트 11건(1,817자)이 다 들어가고
	// 중앙 88자 기준 여섯 건쯤 여유가 있다.
	coldBudget = 2400

	// coldReserve 는 규칙 뒤에 오는 것들(판 갈림 경고·미확인 구간)을 위해 남기는 몫이다.
	//
	// **규칙 예산을 상수로 박으면 안 된다.** 고정 계약부는 언어와 갈래(제외 구역·
	// 도메인 없음)에 따라 길이가 다르고, 경고는 있을 때만 붙는다. 상수로 두면 어떤
	// 조합에서 총량이 조용히 넘는데, 그게 정확히 첫 구현에서 났다(1,872자).
	// 그래서 규칙이 쓸 자리는 **그 시점에 남은 것**으로 계산한다.
	coldReserve = 250
)

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

	// **콜드스타트에는 최근 결정 대신 규칙 본문을 싣는다** (coldRules 의 §).
	//
	// 실은 것이 있으면 아래 두 블록을 다 건너뛴다 — 안내문은 규칙을 실제로 준
	// 자리에서 중복이고, 최근 결정은 이 갈래에서 관련도가 0으로 실측된 자리다.
	cold := !excluded && o.Config.DomainForCwdDeclared(o.Input.Cwd) == ""
	shipped := false
	if cold {
		shipped = o.coldRules(&b, lang)
	}

	// **`[규칙]` 줄은 배경이 아니다.**
	//
	// 실측(2026-08-27): 주입된 노트를 어시스턴트가 이후에 언급한 것이 4.4%였다.
	// 즉 읽는 쪽의 기본값은 "참고용 배경" 이다. 규칙은 그 기본값으로 읽히면
	// 안 되므로 여기서 한 번 말해 둔다 — 규칙은 여러 결정에서 증류한 것이고
	// 도메인이 없어 이 프로젝트에도 그대로 걸린다.
	//
	// 규칙이 없는 볼트에는 안 싣는다. 이 블록은 세션당 한 번 실리고 갱신되지
	// 않으므로 쓸 것이 없는 안내는 그 자체가 소음이다 — 발견 표면은 doctor 다.
	if !excluded && !shipped {
		if rules, _, rerr := o.Layout.ListRules(); rerr == nil && len(rules) > 0 {
			fmt.Fprintf(&b, lang.T(
				"\n**회수가 `[규칙]` 로 주는 %d건은 프로젝트 밖의 판단 기준이다.** 도메인이 없어\n"+
					"어디서 물어도 같은 자격으로 걸린다 — 그 줄은 참고가 아니라 **지켜야 하는 제약**이다.\n"+
					"규칙을 새로 세웠으면 `%s/` 에 한 줄 요약과 출처 결정을 걸어 남겨라.\n",
				"\n**The %d `[rule]` lines recall gives you are judgment criteria from outside this project.**\n"+
					"They carry no domain, so they match from anywhere — treat them as **constraints, not background**.\n"+
					"When you settle a new rule, leave it in `%s/` with a one-line summary and the decisions it came from.\n"),
				len(rules), o.Layout.RulesDirRel())
		}
	}

	if !shipped && len(notes) > 0 {
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

// coldRules 는 **볼트 이력이 없는 프로젝트**에 규칙 본문을 싣는다.
// 실은 것이 있으면 true 를 준다 — 그때는 호출부가 최근 결정을 생략한다.
//
// # 고치려는 고장 (2026-09-02 실측)
//
// 새 프로젝트 `~/project/cosbot` 에서 "지라·슬랙·구글챗에서 내용을 확인해줘" 라고
// 물었더니 어시스턴트가 **"지라·슬랙·구글챗 MCP 도구가 이 세션에 붙어 있지
// 않습니다"** 라고 답했다. 볼트에는 "orca 브라우저로 접근한다" 가 결정 8건으로
// 들어 있었는데도 그랬다. 훅은 정상 작동했다 — 회수가 orca 결정을 **9위·18위·20위·
// 49위**로 물어왔고 주입 슬롯 3칸에 못 들었을 뿐이다.
//
// 그때 SessionStart 가 실은 것: 고정 계약부 545자 + 최근 결정 5건 1,057자.
// 그 5건은 EWS·식약처·novels·VPN 이었다 — **cosbot 관련 0건.** 그리고 규칙은
// "회수가 `[규칙]` 로 주는 11건은 지켜야 하는 제약이다" 라고 **설명만 하고 안 줬다.**
//
// # 왜 랭킹으로는 못 고치는가
//
// 이력이 있는 프로젝트는 UserPromptSubmit 회수가 받쳐 준다. 콜드스타트는 받쳐 주는
// 것이 없다 — **걸릴 어휘 자체가 아직 볼트에 없다.** 점수식을 아무리 고쳐도 없는
// 노트를 못 꺼낸다. 실측한 콜드스타트 빈도는 14일에 새 작업 디렉토리 19개다.
//
// # 왜 최근 결정을 내주는가
//
// **최신성은 관련성과 무관하다.** 위 실측이 그것이다(5건 중 0건 관련). 반면 규칙은
// 도메인이 없어 **정의상 어느 프로젝트에서나 유효하다** — 콜드스타트에 실을 자격이
// 있는 것은 그것뿐이다. 이력이 있는 프로젝트는 건드리지 않는다: 최근 결정 5건이
// 자리별로 고르게 값을 한다는 실측(상대값 1.00·1.00·0.81·0.88·0.75)이 있고,
// 거기서는 UPS 가 규칙 슬롯 2칸을 이미 주므로 또 실으면 중복이다.
//
// # 왜 짧은 것부터인가
//
// 최신순은 위에서 값이 0으로 실측됐으므로 못 쓴다. 짧은 것부터면 같은 예산에 더
// 많이 실린다.
//
// **다만 이 순서는 옳다고 증명된 것이 아니다.** 예산을 1,800 으로 뒀을 때 실볼트에서
// 잘린 한 건이 하필 `규칙-브라우저가-필요하면-orca-CLI…`(184자, 가장 긴 것)였다 —
// 이 블록을 만든 이유가 바로 그 규칙이 안 실려서였는데 그것부터 버렸다. 길이는
// 중요도의 대리 지표가 아니고, 여기서는 **가장 구체적인 규칙이 가장 길었다.**
//
// 지금은 coldBudget 을 키워 실볼트 전건이 들어가므로 이 순서가 실제로 무엇을 버리는
// 일이 없다. 규칙이 예산을 넘길 만큼 늘면 그때는 순서가 아니라 **규칙 집합을 증류해야
// 한다는 신호**다 — 규칙은 여러 결정에서 뽑아낸 것이라 수십 건이 되는 것 자체가
// 이상하다. '자주 걸린 것 우선' 이 답일 수 있으나 규칙 계층이 도입된 지 얼마 안 돼
// 표본이 없다. 규칙별 주입·사용 카운터가 쌓이면 그때 갈아탄다.
func (o Options) coldRules(b *strings.Builder, lang i18n.Lang) bool {
	rules, _, err := o.Layout.ListRules()
	if err != nil || len(rules) == 0 {
		return false
	}
	live := rules[:0:0]
	for _, r := range rules {
		// 뒤집힌 규칙은 안 싣는다. 이 블록은 세션당 한 번 실리고 갱신되지 않아
		// 오래 산다 — brief 가 같은 이유로 같은 필터를 쓴다.
		if r.Meta.Status == store.StatusSuperseded || r.Meta.Status == store.StatusRetracted {
			continue
		}
		if strings.TrimSpace(r.Meta.Summary) == "" {
			continue
		}
		live = append(live, r)
	}
	if len(live) == 0 {
		return false
	}
	sort.SliceStable(live, func(i, j int) bool {
		return len([]rune(live[i].Meta.Summary)) < len([]rune(live[j].Meta.Summary))
	})

	// **남은 자리로 계산한다** (coldReserve 의 §). 머리글과 꼬리글도 예산 안이다.
	head := lang.T(
		"\n### 이 프로젝트에는 볼트 이력이 없다 — 어디서나 걸리는 규칙 %d건\n"+
			"아래는 참고가 아니라 **지켜야 하는 제약**이다. 도메인이 없어 어느 프로젝트에서나 걸린다.\n\n",
		"\n### This project has no vault history — %d rules that apply everywhere\n"+
			"These are **constraints, not background**. They carry no domain, so they hold in every project.\n\n")
	room := coldBudget - coldReserve - len([]rune(b.String())) - len([]rune(head))
	if room <= 0 {
		return false
	}

	var body strings.Builder
	shown := 0
	for _, r := range live {
		line := "- " + strings.TrimSpace(r.Meta.Summary) + "\n"
		if len([]rune(body.String()))+len([]rune(line)) > room {
			break
		}
		body.WriteString(line)
		shown++
	}
	if shown == 0 {
		return false
	}

	fmt.Fprintf(b, head, shown)
	b.WriteString(body.String())
	// **잘랐으면 잘랐다고 말한다.** 조용히 자르면 "규칙이 이게 전부" 로 읽힌다.
	if shown < len(live) {
		fmt.Fprintf(b, lang.T(
			"\n… 그 밖 %d건. 전부 보려면 `prior recall <주제>` 나 `%s/` 를 열어라.\n",
			"\n… and %d more. See them with `prior recall <topic>` or open `%s/`.\n"),
			len(live)-shown, o.Layout.RulesDirRel())
	} else {
		b.WriteString(lang.T(
			"\n이 프로젝트의 결정이 쌓이면 여기는 최근 결정으로 바뀐다 — 지금 더 파려면 `prior recall <주제>`.\n",
			"\nOnce this project accumulates decisions, this block becomes recent decisions — to dig now, run `prior recall <topic>`.\n"))
	}
	return true
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
