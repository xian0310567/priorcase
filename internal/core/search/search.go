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
	// **weightCwdDomain 은 weightHead 보다 작아야 한다.**
	//
	// 예전에는 4 였다. 그러면 "이 폴더에 산다" 는 사실 하나가 **질의어를 하나 더
	// 맞힌 것보다 세다.** 실측(2026-08-15) 교차 프로젝트 질의 12개 중 8개에서
	// 상위 3이 바뀌었다 — "월드 저장 포맷" 질의에서 nova 결정 3건(고유 8/8/8)이
	// 통째로 탈락하고 priorcase 잡음(고유 5/4/4)이 그 자리를 먹었다. 훅 주입이
	// 3줄뿐이라 재정렬이 곧 탈락이다.
	//
	// 0 으로 두지 않는 이유: 같은 점수면 지금 하는 일 쪽이 맞다. 동점을 가르는
	// 정도로만 남긴다. TestKeywordHitOutweighsCwdDomain 이 이 관계를 못 박는다.
	weightCwdDomain   = 2 // cwd 도메인이 노트 domain 에 있다
	weightMention     = 6 // 프롬프트가 도메인 접두어를 직접 언급했다 (도메인마다 누적)
	weightHead        = 3 // stem+summary+tags 에서 키워드 히트
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

	// ReferenceLimit 은 **참고에 따로 주는 자리 수**다. 0 이면 참고를 안 준다.
	//
	// **한 목록에 섞어 자르면 참고가 결정을 밀어낸다.** H1 제목을 head 로 가정해
	// 측정했을 때는 밀려남이 2%(51건 중 1건)라 괜찮아 보였는데, 실제 요약을 달고
	// 다시 재니 **15%(53건 중 8건)** 였다 — 요약은 제목보다 키워드가 촘촘해 훨씬
	// 센 경쟁자다. 그중에는 "승격 준비해줘" 질의에서 `승격전-적대감사를-기본절차로`
	// 가 밀린 판이 있었다. 이 시스템이 존재하는 이유가 그런 결정을 그 순간에
	// 들이미는 것이다.
	ReferenceLimit int
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
	var refs []store.Note
	if o.IncludeReferences && o.ReferenceLimit > 0 {
		r, rskipped, rerr := l.ListReferences()
		if rerr != nil {
			return nil, nil, rerr
		}
		refs = r
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

	// **참고는 자기 자리에서 자른다.** 결정과 섞어 자르면 밀어낸다 (§ ReferenceLimit).
	// 결정을 앞에 둔다 — 사람도 에이전트도 위에서부터 읽는다.
	if len(refs) > 0 {
		ro := o
		ro.Limit = o.ReferenceLimit
		rhits := trim(scoreAll(refs, keywords, cwdDomain, mentioned), ro)
		hits = append(hits, rhits...)
	}
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
// 이상인 항목은 항상 앞쪽의 연속 구간(prefix)을 이루기 때문이다.
//
// 필터를 절단보다 먼저 두는 쪽이 "Limit 은 유효한 결과의 개수"라는 의미에
// 맞으므로 이 순서를 지킨다. (이 주석은 "나중에 2차 정렬 키(예: 날짜)가
// 생기면 동치가 깨진다" 고 예고해 뒀고, 아래에서 실제로 그렇게 됐다.)
//
// # 동점은 최근 결정이 이긴다
//
// 예전에는 경로 문자열 내림차순이었다(옛 셸의 `sort -rn` 동작 보존). 그건
// 사실상 무작위다 — 파일명이 `<도메인>-결정-<slug>-<날짜>` 라 slug 의 가나다순이
// 이기고 날짜는 맨 뒤에 있어 거의 영향을 못 준다. **점수식에 시간 항이 하나도
// 없는 시스템에서 동점 처리가 시간을 볼 유일한 자리**인데 그걸 안 쓰고 있었다.
// 주입은 상위 3줄뿐이라 동점 하나가 곧 탈락이다.
//
// 날짜가 없는 노트는 빈 문자열이라 자연히 맨 뒤로 간다. 날짜까지 같으면 경로로
// 가른다 — 결과가 실행마다 달라지면 안 되기 때문이다.
func trim(hits []Hit, o Options) []Hit {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if hits[i].Note.Meta.Date != hits[j].Note.Meta.Date {
			return hits[i].Note.Meta.Date > hits[j].Note.Meta.Date
		}
		return hits[i].Note.Path > hits[j].Note.Path
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
