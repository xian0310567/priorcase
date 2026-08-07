package hook

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/daemon"
)

// recentN 은 세션 진입에 이름까지 적는 결정 수다. 이 블록은 세션당 한 번 실리고
// 갱신되지 않으므로, 길어지면 그 자체가 소음이 된다.
const recentN = 5

// sessionStart 는 MCP 의 initialize.instructions 에 해당한다. 같은 일을 하는 두 경로가
// 있는 것이라 **내용도 같은 성격이어야 한다** — 결정 덤프가 아니라 행동 계약이다.
func (o Options) sessionStart() error {
	notes, skipped, listErr := o.Layout.List()

	var b strings.Builder
	b.WriteString("## casebook\n\n")

	domain := o.Config.DomainForCwd(o.Input.Cwd)
	excluded := o.Input.Cwd != "" && o.Config.IsExcluded(o.Input.Cwd)

	switch {
	case excluded:
		// 자체 스키마 구역이다. 여기에 결정을 쓰면 그 저장소의 규약을 깬다.
		b.WriteString("**이 디렉토리는 casebook 제외 구역이다.** 결정 노트를 만들지 마라.\n" +
			"회수는 계속 동작한다 — 읽기 전용이라 이 구역의 규약을 건드리지 않는다.\n")
	case domain == "":
		b.WriteString("이 디렉토리는 볼트 프로젝트에 매핑되지 않는다 — 결정이 생기면 " +
			"`common` 도메인으로 기록한다.\n")
	default:
		fmt.Fprintf(&b, "이 프로젝트의 도메인 접두어는 `%s` 다.\n", domain)
	}

	if listErr != nil {
		fmt.Fprintf(&b, "\n⚠️ 볼트를 읽지 못했다 (%v). 회수가 비어 나올 수 있다.\n", listErr)
	}

	if !excluded {
		b.WriteString("\n**되돌리기 어려운 선택을 했으면 그 자리에서 `cb capture` 를 부른다.**\n" +
			"아키텍처·스키마·외부 서비스·가격처럼 나중에 \"왜 이렇게 했지\"를 묻게 될 선택이 대상이다.\n" +
			"자잘한 것까지 남기면 회수가 오히려 어려워진다.\n")
	}

	if len(notes) > 0 {
		fmt.Fprintf(&b, "\n### 최근 결정 (전체 %d건)\n\n", len(notes))
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
		b.WriteString("\n주제가 바뀔 때마다 관련 결정이 자동으로 주입된다 — 더 파야 하면 `cb recall <주제>`.\n")
	}

	if len(skipped) > 0 {
		fmt.Fprintf(&b, "\n⚠️ 결정 노트 %d건을 읽지 못해 회수에서 빠져 있다. `cb index` 가 목록을 알려준다.\n",
			len(skipped))
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
	items, err := daemon.ReadPending(o.StateDir)
	if err != nil {
		return fmt.Sprintf("\n⚠️ 미확인 구간을 확인할 수 없다 (%v). "+
			"**놓친 기록을 줍는 안전망이 지금 꺼져 있다.**\n", err)
	}
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n⚠️ **데몬이 표시한 미확인 구간이 %d건 있다.** 이전 세션에서 결정을 내리고도\n"+
		"기록하지 않고 지나간 자리다. 확인해서 실제 결정이면 `cb capture` 로 남겨라.\n", len(items))
	for i, p := range items {
		if i >= recentN {
			fmt.Fprintf(&b, "  … 그 밖 %d건\n", len(items)-recentN)
			break
		}
		d := p.Domain
		if d == "" {
			d = "(도메인 미상)"
		}
		fmt.Fprintf(&b, "  - %s %s · 발화 %d · 시그널 %s\n",
			p.When(), d, p.Turns, strings.Join(p.Signals, "·"))
	}
	return b.String()
}
