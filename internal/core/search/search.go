package search

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
)

// 점수 가중치 — 현행 셸 sb_search 에서 이식했다.
const (
	weightCwdDomain   = 4 // cwd 도메인이 노트 domain 에 있다
	weightMention     = 6 // 프롬프트가 도메인 접두어를 직접 언급했다 (도메인마다 누적)
	weightHead        = 3 // stem+summary+tags+domain 에서 키워드 히트
	weightBody        = 1 // 본문에서 키워드 히트
	penaltySuperseded = 5 // 뒤집힌 결정
)

type Hit struct {
	Note  store.Note
	Score int
}

type Options struct {
	Cwd string
	// CrossProject 가 false 면 cwd 도메인 폴더 결과가 완전히 빌 때만 전체로 넓힌다
	// (현행 셸 동작). true 면 항상 전체를 본다.
	CrossProject bool
	Limit        int
	MinScore     int
}

// Recall 은 프롬프트에 관련된 결정을 점수순으로 준다.
func Recall(l *store.Layout, c *config.Config, prompt string, o Options) []Hit {
	keywords := ExtractKeywords(prompt)
	if len(keywords) == 0 {
		return nil
	}
	notes, err := l.List()
	if err != nil {
		return nil
	}

	cwdDomain := ""
	if o.Cwd != "" {
		cwdDomain = c.DomainForCwd(o.Cwd)
	}
	mentioned := mentionedDomains(c, keywords)

	hits := scoreAll(notes, keywords, cwdDomain, mentioned)

	if !o.CrossProject && cwdDomain != "" {
		scoped := filterByDomain(hits, cwdDomain)
		if len(scoped) > 0 {
			hits = scoped // 결과가 있으면 넓히지 않는다 — 현행 셸 동작
		}
	}

	hits = trim(hits, o)
	return hits
}

func mentionedDomains(c *config.Config, keywords []string) map[string]bool {
	m := map[string]bool{}
	for _, d := range c.Domain {
		for _, k := range keywords {
			if k == strings.ToLower(d.Prefix) {
				m[d.Prefix] = true
			}
		}
	}
	return m
}

func scoreAll(notes []store.Note, keywords []string, cwdDomain string, mentioned map[string]bool) []Hit {
	var hits []Hit
	for _, n := range notes {
		head := strings.ToLower(strings.Join([]string{
			n.Stem, n.Meta.Summary,
			strings.Join(n.Meta.Tags, " "),
			strings.Join(n.Meta.Domain, " "),
		}, " "))
		body := strings.ToLower(string(n.Body))

		headHits, bodyHits := 0, 0
		for _, k := range keywords {
			if strings.Contains(head, k) {
				headHits++
			}
			if strings.Contains(body, k) {
				bodyHits++
			}
		}
		// head 히트가 없으면 점수 0 — 본문만 스치는 문서를 버린다
		if headHits == 0 {
			continue
		}

		score := weightHead*headHits + weightBody*bodyHits
		if cwdDomain != "" && contains(n.Meta.Domain, cwdDomain) {
			score += weightCwdDomain
		}
		for d := range mentioned {
			if contains(n.Meta.Domain, d) {
				score += weightMention
			}
		}
		if n.Meta.Status == "superseded" {
			score -= penaltySuperseded
		}
		if score > 0 {
			hits = append(hits, Hit{Note: n, Score: score})
		}
	}
	return hits
}

func filterByDomain(hits []Hit, domain string) []Hit {
	var out []Hit
	for _, h := range hits {
		if contains(h.Note.Meta.Domain, domain) {
			out = append(out, h)
		}
	}
	return out
}

// trim 은 정렬 → MinScore 필터 → Limit 절단 순으로 다듬는다.
// 필터를 절단보다 먼저 해야 한다 — 절단을 먼저 하면 MinScore 미만 항목이
// Limit 자리를 차지한 채로 잘려 나간 뒤에야 걸러지므로, 그 뒤에 있는
// (아직 세지 않은) MinScore 이상 항목이 결과에 못 들어가 결과 수가
// Limit 보다 적어질 수 있다.
func trim(hits []Hit, o Options) []Hit {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Note.Path > hits[j].Note.Path // 셸의 sort -rn 동점 처리와 동일
	})
	if o.MinScore > 0 {
		var out []Hit
		for _, h := range hits {
			if h.Score >= o.MinScore {
				out = append(out, h)
			}
		}
		hits = out
	}
	if o.Limit > 0 && len(hits) > o.Limit {
		hits = hits[:o.Limit]
	}
	return hits
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

const warnLine = "위 결정 중 아쉬운 결과로 기록된 건이 있음. " +
	"유사 선택 시 회고를 먼저 읽고 수정안을 제안할 것."

// RenderInject 는 훅 주입용 형식으로 렌더링한다. 매칭이 없으면 빈 문자열.
func RenderInject(l *store.Layout, hits []Hit) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[과거 결정 참조]\n")
	warn := false
	for _, h := range hits {
		m := h.Note.Meta
		date, status, outcome := m.Date, m.Status, m.Outcome
		if date == "" {
			date = "-"
		}
		if status == "" {
			status = "active"
		}
		if outcome == "" {
			outcome = "pending"
		}
		summary := m.Summary
		if summary == "" {
			summary = h.Note.Stem
		}
		fmt.Fprintf(&b, "- %s %s (%s/%s) → %s\n",
			date, summary, status, outcome, l.RelPath(h.Note.Path))
		if status == "regretted" || outcome == "bad" {
			warn = true
		}
	}
	if warn {
		b.WriteString(warnLine + "\n")
	}
	return b.String()
}
