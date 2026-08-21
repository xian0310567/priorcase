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

// supersededFloor 는 뒤집힌 결정이 감점 뒤에도 내려가지 않는 바닥 점수다.
//
// **감점의 목적은 순위를 낮추는 것이지 안 보이게 하는 것이 아니었다.** 그런데
// 바닥이 없던 동안 값이 정확히 후자였다: weightHead=3 · penaltySuperseded=5 라
// head 히트가 하나면 3+bodyHits-5 가 되고, 아래 `score > 0` 과 trim 의
// MinScore(모든 실호출이 1이다)가 그 노트를 결과에서 통째로 지웠다. 즉 뒤집힌
// 결정은 head 히트가 **둘은 있어야** 회수에 뜬다.
//
// 그게 왜 문제인가 — 번복 이유를 남기는 목적의 절반이 여기서 막힌다.
// capture/supersede.go 의 markOverturned 는 "왜 뒤집혔는가" 를 summary 꼬리표에
// 붙인다. head 밖에 두면 검색이 아예 안 되기 때문이다. 그런데 그 이유에만 나오는
// 낱말은 대개 **한 개짜리 질의**로 들어온다("osxkeychain", "403"). 히트 하나짜리
// 질의가 통째로 걸러지면, 이유를 head 에 올려 놓고도 그 이유로는 못 찾는다.
//
// # 실측 (2026-08-19, 실볼트 18노트)
//
// 볼트를 복사해 codecommit 인증 결정 하나를 실제 번복 사유와 함께 superseded 로
// 만들고, 질의 49건(각 노트의 summary·태그에서 뽑은 것 + 손으로 쓴 주제 질의)을
// 슬롯 3개로 돌렸다. 세 안의 차이는 이랬다:
//
//	                                   현행   (a)감점2  (b)바닥1
//	뒤집힌 노트가 상위3에 뜬 질의        11     15        14
//	현행 상위3의 active 노트가 밀려남     -      1         0
//	뒤집힌 노트가 active 노트보다 위       10     12        10
//
// (a) 감점을 5→2 로 낮추는 안은 히트 하나 차이(weightHead=3)가 감점을 덮어써서
// 상태가 사실상 무의미해진다. 실제로 "git 인증 설정" 에서 뒤집힌 노트(9)가 살아
// 있는 결정(8)을 제치고 1위가 됐고, "git 신원 이메일 설정 커밋 회사 개인" 에서는
// 슬롯 3개 중 하나를 빼앗아 active 노트 하나를 밀어냈다. 회수의 값은 "지금 유효한
// 결정을 먼저 보여 주는 것" 이라 이 대가는 못 치른다.
//
// (b) 바닥을 두는 안은 감점을 5로 그대로 두므로 순위가 현행과 **완전히 같다**
// (역전 10건 동일 · 밀려난 노트 0건). 바뀌는 것은 하나뿐이다: 슬롯이 남았을 때
// 결과에 뜬다. 뒤집힌 노트는 바닥값 1로 앉는데 감점 없는 노트의 최소 점수가
// weightHead=3 이므로 **언제나 맨 끝**이다 — 빈 슬롯만 채우고 아무것도 밀어내지
// 못한다. 상위3이 꽉 찬 질의 28건에서 밀려난 노트가 0건인 것이 그 이유다.
//
// 바닥 없이 "감점 전 점수로 필터한다"(브리프 (b) 문구 그대로) 만으로는 안 된다.
// 점수가 -1 로 남아 trim 의 MinScore=1 이 다시 죽인다 — 실측에서 11건, 현행과
// 똑같았다. 통과시키려면 점수 자체가 바닥을 가져야 한다.
//
// 1 인 이유: MinScore 는 실호출이 전부 1이고(hook·cli·mcp·retro·capture),
// 감점 없는 최소 점수 3보다 작아야 "언제나 맨 끝" 이 성립한다. 2 도 그 조건을
// 만족하지만, 1 이면 "이 점수는 관련도가 아니라 겨우 살아남았다는 표시" 라는 뜻이
// 더 분명하다.
const supersededFloor = 1

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

// minHeadHits 는 후보로 남기려면 필요한 head 히트 수다. scoreAll 주석에 근거가 있다.
func minHeadHits(nKeywords int) int {
	if nKeywords >= conversationalKeywords {
		return 2
	}
	return 1
}

// conversationalKeywords 는 "이건 골라 넣은 질의가 아니라 대화체다" 로 보는 경계다.
//
// 실측: 이 세션의 자연어 프롬프트들이 키워드 4·9·11개를 냈고, 사람이 CLI 에 치는
// 질의는 2~3개였다. 4를 경계로 두면 둘이 갈린다.
const conversationalKeywords = 4

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
		// **히트 하나로는 부족하다 — 질의가 길 때는 둘을 요구한다.**
		//
		// CJK 는 부분 문자열로 매칭하므로(match.go 의 근거) 2음절 대화체 토큰이
		// 아무 데나 걸린다. 실측: "무슨 작업을 하다가 멈춘것같은데 확인해줄 수 있어" 의
		// 키워드는 `멈춘것같은데·무슨·있어·확인해줄` 로 **내용어가 하나도 없는데**,
		// `있어` 가 10개 노트의 head 에("…있어야…" 안쪽) 걸려서 후보 11건이 나오고
		// 상위 3칸이 무관한 것으로 찼다.
		//
		// 흔한 낱말을 걸러도 안 된다 — 그 토큰들은 **드물다**(있어 3.7%, 무슨 0.4%).
		// 문서빈도 필터로는 안 잡힌다. 우연한 매칭이라 빈도가 아니라 **개수**로 걸러야 한다.
		//
		// 실측 비교(같은 질의 4개, 후보 수):
		//
		//	                내용어0    pull/push   npm배포   어휘어긋남
		//	현행(≥1)          11         24         19        32
		//	≥2                 0          1          3         1
		//	2음절 낱말경계      11         24         19        31
		//
		// ≥2 는 내용어 없는 질의에 **침묵하면서** 좋은 질의의 1위를 그대로 지켰다.
		// 낱말경계 요구는 거의 효과가 없어 기각했다.
		//
		// **질의가 짧으면 하나로 만족한다.** `prior recall "볼트 동기화"` 처럼 사람이
		// 골라 넣은 두 낱말에 둘을 요구하면 정작 정확한 질의가 아무것도 못 찾는다.
		// 대화체 프롬프트는 어미가 살아남아 거의 언제나 4개를 넘으므로, 그 경계가
		// "자동 주입" 과 "사람이 물은 것" 을 자연스럽게 가른다.
		if headHits < minHeadHits(len(keywords)) {
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
			// 감점은 순위를 낮추기 위한 것이지 숨기기 위한 것이 아니다.
			// 바닥을 두는 근거와 실측은 supersededFloor 주석에 있다.
			score -= penaltySuperseded
			if score < supersededFloor {
				score = supersededFloor
			}
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
		fmt.Fprintf(&b, "- %s %s (%s/%s) → %s\n",
			date, summary, status, outcome, l.RelPath(h.Note.Path))
		if r := overturnLine(l, m, summary); r != "" {
			b.WriteString(r)
		}
		if status == "regretted" || outcome == "bad" {
			warn = true
		}
	}
	if warn {
		b.WriteString(warnLine + "\n")
	}
	return b.String()
}

// overturnDedupRunes 는 "이 이유가 summary 에 이미 실렸나" 를 볼 때 대조하는 길이다.
//
// summary 꼬리표는 capture 가 이유를 80자로 잘라 붙인 것이라(summaryReasonRunes)
// 전문 대조로는 긴 이유에서 항상 어긋난다. 앞머리만 본다. 24자는 잘린 꼬리표
// 안에 확실히 들어가면서(80 ≫ 24) 서로 다른 이유가 우연히 겹치기에는 긴 길이다.
const overturnDedupRunes = 24

// overturnLine 은 **이 결정이 왜 뒤집혔는가** 를 회수 블록에 한 줄 더 붙인다.
//
// 없으면 에이전트가 보는 것은 `(superseded/bad)` 라는 딱지뿐이다. 그 딱지는
// "쓰지 마라" 는 말이지 "무엇을 대신 하라" 는 말이 아니라서, 읽는 쪽은 왜 버렸는지를
// 처음부터 다시 판다 — 번복 이유를 남기기로 한 목적이 바로 그걸 막는 것이었다
// (store.Meta.SupersededReason 주석).
//
// **status 가 아니라 키의 유무로 판단한다.** `superseded` 로만 좁히면 대체할 새
// 결정 없는 번복(capture/review.go 의 자기 번복 경로)이 통째로 빠진다 — 그쪽은
// status 를 `regretted` 로도 둘 수 있고, 실제로 그 경우가 더 흔하다(측정으로
// 가정이 깨져 "그냥 그만둔다" 로 끝나는 번복). 반대로 이 키는 살아 있는 결정에는
// 붙지 않는다 — capture·review 가 status 변경 없이는 받지 않는다.
//
// summary 에 이미 실려 있으면 안 찍는다. markOverturned 가 이유를 summary 꼬리표로도
// 붙이기 때문에, 그대로 두면 주입 블록에 같은 문장이 두 번 나간다. 회수는 매
// 프롬프트마다 실려 나가는 예산이라 중복 한 줄이 그냥 낭비가 아니다.
func overturnLine(l *store.Layout, m store.Meta, shown string) string {
	reason := strings.TrimSpace(m.SupersededReason)
	if reason == "" {
		return ""
	}
	head := []rune(reason)
	if len(head) > overturnDedupRunes {
		head = head[:overturnDedupRunes]
	}
	if strings.Contains(shown, string(head)) {
		return ""
	}
	return "  " + l.Lang().T("번복 이유: ", "Overturned because: ") + reason + "\n"
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
