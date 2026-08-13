package search

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
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

	// IncludeReferences 가 true 면 **참고 문서도 회수 대상**이다
	// (store.Layout.ListReferences 참고).
	//
	// 기본은 false 다 — 결정만 봐야 하는 자리가 있다. capture 의 중복 대조가
	// 그렇다: "비슷한 기존 **결정**이 있나" 를 묻는 자리에 기획 초안이 끼면
	// 사람이 중복으로 오판한다.
	IncludeReferences bool
}

// Recall 은 프롬프트에 관련된 결정을 점수순으로 주고, 회수 대상에서 아예 빠진
// (읽지 못한) 노트를 두 번째 값으로 함께 준다.
//
// 볼트를 읽지 못하면 에러를 준다 — 빈 결과로 뭉개지 않는다. "관련 결정이 없다"
// 와 "볼트를 못 읽었다" 는 훅 주입 경로에서 정반대 의미다: 전자는 참이지만
// 후자를 침묵시키면 에이전트가 과거 결정을 못 본 채 "없다" 로 읽는다.
// 같은 l.List() 를 부르는 prior index 는 이미 에러로 죽으므로, 두 명령이 같은
// 에러 정책을 쓰게 맞춘다.
//
// 건너뛴 노트도 같은 이유로 돌려준다. 폴더 전체를 못 읽는 것(에러)과 노트
// 몇 건을 못 읽는 것(건너뜀)은 정도의 차이일 뿐, 둘 다 "이 프롬프트에 관련된
// 결정이 있었는데 못 봤을 수 있다" 는 같은 사실을 뜻한다. 다만 후자로는 죽지
// 않는다 — 읽힌 것만이라도 회수하는 편이 낫다.
func Recall(l *store.Layout, c *config.Config, prompt string, o Options) ([]Hit, []store.SkippedNote, error) {
	keywords := ExtractKeywords(prompt)
	if len(keywords) == 0 {
		return nil, nil, nil
	}
	notes, skipped, err := l.List()
	if err != nil {
		return nil, nil, err
	}
	if o.IncludeReferences {
		refs, rskipped, rerr := l.ListReferences()
		if rerr != nil {
			return nil, nil, rerr
		}
		notes = append(notes, refs...)
		skipped = append(skipped, rskipped...)
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
	return hits, skipped, nil
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
		// **domain 과 보일러플레이트 태그는 head 에서 뺀다.**
		//
		// 도메인은 이미 weightCwdDomain·weightMention 이 따로 센다. head 에도 넣으면
		// 이중 계산인 데다, 질의에 도메인 이름이 스치기만 해도 그 도메인 **전 노트**가
		// headHits ≥ 1 이 되어 `headHits == 0` 필터가 통째로 무력해진다.
		//
		// `decision` 태그는 capture 가 모든 노트에 붙인다(실볼트 67/67). 그 낱말이
		// 질의에 들어오면 전 노트가 걸린다 — 불용어에 "결정" 이 있는 것과 같은 이유다.
		head := strings.ToLower(strings.Join([]string{
			n.Stem, n.Meta.Summary,
			strings.Join(contentTags(n.Meta.Tags), " "),
		}, " "))
		body := strings.ToLower(string(n.Body))

		headHits, bodyHits := 0, 0
		for _, k := range keywords {
			if matches(head, k) {
				headHits++
			}
			if matches(body, k) {
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
//
// 정렬 키(Score)와 필터 임계값(MinScore)이 지금은 같은 필드라서, 절단과
// 필터의 순서를 바꿔도 결과가 같다 — 내림차순으로 정렬된 배열에서 MinScore
// 이상인 항목은 항상 앞쪽의 연속 구간(prefix)을 이루기 때문이다. 그래서
// 지금 당장은 이 순서가 관측 가능한 차이를 만들지 않는다(테스트로 순서
// 자체를 못 박을 수 없는 이유이기도 하다 — search_test.go 의 TestTrim 참고).
//
// 하지만 나중에 2차 정렬 키(예: 날짜)나 점수와 무관한 필터(예: 도메인)가
// 생기면 이 동치는 깨진다. 필터를 절단보다 먼저 두는 쪽이 "Limit 은 유효한
// 결과의 개수"라는 의미에 맞으므로, 지금부터 이 순서를 지킨다.
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
	b.WriteString(l.Lang().T("[과거 결정 참조]", "[Past decisions]") + "\n")
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
		// **참고는 결정처럼 그리면 안 된다.**
		//
		// 참고에는 status·outcome 이 없다. 위에서 빈 값을 active/pending 으로
		// 채우므로 그대로 내면 **기획 초안이 확정된 결정으로 보인다** — 에이전트는
		// 그걸 근거로 삼는다. 상태를 떼고 참고임을 앞에 붙인다.
		if h.Note.IsReference() {
			fmt.Fprintf(&b, "- %s %s → %s\n",
				l.Lang().T("[참고]", "[reference]"), summary, l.RelPath(h.Note.Path))
			continue
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

// boilerplateTags 는 거의 모든 노트가 갖는 태그다. 검색 신호가 아니다.
var boilerplateTags = map[string]bool{"decision": true, "결정": true}

// contentTags 는 보일러플레이트를 뺀 태그다.
func contentTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if !boilerplateTags[strings.ToLower(t)] {
			out = append(out, t)
		}
	}
	return out
}
