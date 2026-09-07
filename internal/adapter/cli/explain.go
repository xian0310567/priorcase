package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/search"
)

// renderExplain 은 점수 한 건이 어디서 왔는지 그린다.
//
// # 무엇을 보여 주고 무엇을 숨기나
//
// 질의어가 수백 개일 수 있다 (맥락을 붙이면 그렇다). 전부 그리면 사람이 못 읽으므로
// **걸린 낱말만** 그리고, 안 걸린 것은 수만 센다. 다만 "왜 저게 안 나왔나" 를 볼
// 때는 안 걸린 낱말이 답이라, `--explain-all` 로 전부 볼 수 있게 둔다.
//
// 정렬은 **기여도 순**이다. 점수에 가장 크게 보탠 낱말이 위에 온다 — 사람이 알고
// 싶은 것은 "무엇이 이 노트를 끌어올렸나" 이고, 알파벳순은 그 질문에 답하지 않는다.
func renderExplain(w io.Writer, h search.Hit, all bool) {
	t := h.Trace
	if t == nil {
		return
	}
	fmt.Fprintf(w, "     게이트  %d / %d %s\n", t.GateScore, t.GateNeed, passMark(t.GatePass))

	type row struct {
		search.TermTrace
		gain int
	}
	var rows []row
	unhit := 0
	for _, tt := range t.Terms {
		g := 0
		switch {
		case tt.Head:
			g = 3 // weightHead
			if tt.Rare {
				g += 2 // weightRare
			}
		case tt.Synonym:
			g = 2 // weightSynonym
		}
		if tt.Body {
			g++ // weightBody
		}
		if g == 0 && !all {
			unhit++
			continue
		}
		rows = append(rows, row{tt, g})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].gain > rows[j].gain })

	for _, r := range rows {
		where := "—"
		switch {
		case r.Head:
			where = "head"
		case r.Synonym:
			where = "동의어"
		}
		if r.Body {
			if where == "—" {
				where = "본문"
			} else {
				where += "+본문"
			}
		}
		kind := "변별어"
		if !r.Rare {
			kind = "흔함"
		}
		fmt.Fprintf(w, "       %-16s %-8s %-6s df %3d\n", cut(r.Word, 16), where, kind, r.DF)
	}
	if unhit > 0 {
		fmt.Fprintf(w, "       (안 걸린 낱말 %d개 — 전부 보려면 --explain-all)\n", unhit)
	}

	fmt.Fprintf(w, "     head    히트 %d (변별어 %d · 동의어 %d) → 원점수 %d\n",
		t.HeadHits, t.RareHits, t.SynHits, t.HeadRaw)
	fmt.Fprintf(w, "     길이    %d자 → 정규화 ×%.3f → %d점\n", t.HeadRunes, t.LengthNorm, t.HeadScore)
	var parts []string
	add := func(label string, v int) {
		if v != 0 {
			parts = append(parts, fmt.Sprintf("%s %+d", label, v))
		}
	}
	add("본문", t.BodyHits)
	add("cwd도메인", t.CwdBonus)
	add("도메인언급", t.MentionBonus)
	add("뒤집힘", -t.StatusPenalty)
	if len(parts) > 0 {
		fmt.Fprintf(w, "     가감    %s\n", strings.Join(parts, " · "))
	}
	fmt.Fprintf(w, "     최종    %d\n", t.Final)
}

func passMark(ok bool) string {
	if ok {
		return "통과"
	}
	return "탈락"
}
