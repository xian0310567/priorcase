package search

import (
	"fmt"
	"math"
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
	weightCwdDomain = 2 // cwd 도메인이 노트 domain 에 있다
	weightMention   = 6 // 프롬프트가 도메인 접두어를 직접 언급했다 (도메인마다 누적)
	weightHead      = 3 // stem+summary+tags 에서 키워드 히트
	weightBody      = 1 // 본문에서 키워드 히트

	// weightSynonym 은 질의어 대신 **형제 낱말**이 head 에 걸렸을 때다 (synonym.go).
	//
	// **weightHead 보다 반드시 작아야 한다.** 같아지면 "그 낱말로 물었다" 와
	// "비슷한 말로 물었다" 가 구별되지 않아, 패러프레이즈로 걸린 노트가 정확히
	// 맞은 노트를 밀어낸다. 회수 슬롯이 3개뿐이라 재정렬이 곧 탈락이다.
	// TestExactHitOutranksSynonymHit 이 이 관계를 못 박는다.
	//
	// # 실측 (2026-08-24, 실볼트 306노트 · 정답을 아는 질의 10개)
	//
	// 정답의 순위를 셋으로 비교했다. 0 은 후보에 아예 없는 것이다.
	//
	//	                        표없음   w=1   w=2
	//	정답 못 찾음(0)            4      1     1
	//	찾은 것의 평균 순위       2.3    2.9   2.7
	//
	// **표를 켜서 밀려난 정답은 0건이다** — 표 없이 찾던 여섯 질의의 순위가
	// 5·1·1·1·4·2 로 전부 그대로였고, 0건이던 세 질의가 정답을 찾았다.
	// (평균 순위가 오른 것은 악화가 아니라 새로 찾은 3건이 2~7위로 들어온
	// 산술 효과다. 이 표에서 봐야 하는 숫자는 못 찾음 4→1 이다.)
	//
	// 2 를 고른 이유: 1 에서는 "작품을 많이 만들어서 승부하면 되나" 의 정답이
	// 4위였다. 동의어 히트 둘(2점)이 **우연한 정확 히트 하나**(3점)보다 낮았기
	// 때문인데, 큐레이션된 패러프레이즈 두 개가 우연히 스친 낱말 하나보다 약한
	// 증거라고 볼 근거가 없다. 2 로 올리면 그 질의가 2위가 되고 나머지 아홉은
	// 한 칸도 안 움직였다. 3(=weightHead)은 위 계약을 깨므로 상한이 2다.
	weightSynonym = 2

	penaltySuperseded = 5 // 뒤집힌 결정
)

// ── head 길이 정규화 ──────────────────────────────────────────────────
//
// **head 히트는 head 가 길수록 우연히 늘어난다.** CJK 를 부분문자열로 매칭하므로
// (match.go 의 근거) head 가 길면 그물이 커지고, 그 그물은 관련성과 무관하다.
//
// # 고장의 크기 (2026-08-27 실측)
//
// 실볼트 결정 420건과 Claude Code 트랜스크립트 894개(212MB)에서 실제 훅 주입을
// 셌다. 요약 길이 4분위별 평균 주입 횟수가 이랬다:
//
//	0~66자 0.4회 · 66~101자 2.4회 · 101~155자 4.7회 · 155~1760자 18.7회
//
// **47배**다. 결과로 상위 5개 노트가 전체 주입의 40.3%, 상위 10개가 52.7%를
// 먹었고 결정 420건 중 228건(54%)이 단 한 번도 안 떴다. 주입은 상위 3줄뿐이라
// 이 편향이 곧 탈락이다 — 교차 프로젝트 회수가 55.8%나 되는데도 어시스턴트가
// 실제로 그 노트를 언급한 것은 4.4%뿐인 이유가 여기다.
//
// 기제는 게이트를 우회하는 것이 아니라 게이트를 **통과해 버리는** 것이다.
// minHeadHits=2 는 우연한 히트를 막으려고 있는데, 요약이 1,429자면 우연한 히트
// 둘은 그냥 생긴다. 실측한 판이 이렇다 — editup 세션의
// `"백오피스 로그 조회가 안 돼서 원인 찾는 중이야"` 가 `novels-결정-노벨피아규정…`
// 을 부른 것은 `로그` 와 요약 안쪽의 **"정산조회"** 두 낱말이었다.
//
// # 왜 BM25 형태인가
//
// 이건 색인 검색이 오래전에 만난 문제고 BM25 의 길이 정규화가 정확히 이걸 위해
// 있다. tf 를 안 쓰는(한 질의어를 한 번만 세는) 이 점수식에서 BM25 의 항은
// 길이 인자 하나로 줄어든다:
//
//	norm = (k+1) / (1 + k*((1-b) + b*len/ref))
//
// # 왜 1.0 에서 캡하는가
//
// BM25 원형은 짧은 문서를 1.0 **위로** 올린다(b=0.75 에서 상한 1.69). 그걸 그대로
// 쓰면 제목만 있는 껍데기 노트가 가점을 받는데, 그 노트들이 안 걸리는 이유는
// 길이가 아니라 **어휘가 없어서**라 가점으로는 아무것도 안 고쳐진다. 그건
// 동의어 표와 규칙 노트가 할 일이다.
//
// 실용적인 이유가 더 크다: 캡을 씌우면 이 변경이 **"긴 head 감점" 하나**로 좁혀져
// ref 아래의 노트는 점수가 한 점도 안 바뀐다. 픽스처 노트 4건이 71~91자라
// 기존 순위 계약 테스트가 전부 손 안 대고 그대로 성립한다.
const (
	// refHeadRunes 는 감점이 시작되는 head 길이다. **글자 수**다.
	//
	// 실볼트 head(stem+summary+contentTags) 글자수 분포(2026-08-27, 결정 414건):
	// 최소 80 · p10 121 · p25 145 · **중앙 184** · p75 263 · p90 458 · p99 1405 ·
	// 최대 1848.
	//
	// 200 을 고른 이유는 스윕에 있다 (lengthNorm 주석의 표). 중앙값 바로 위이고,
	// **정답 순위가 나빠지지 않는 가장 센 설정**이다 — 180 으로 내리면 편향이
	// 4.7배까지 더 줄지만 slug 정답질의 MRR 이 0.921 아래(0.919)로 처음 내려간다.
	refHeadRunes = 200

	// normK·normB 는 BM25 의 k1·b 다.
	//
	// normB 는 정규화의 세기다 — 0 이면 길이를 무시하고 1 이면 길이에 완전 비례한다.
	// BM25 기본값은 0.75 인데 **여기서는 0.5 다.** 이 점수식은 tf 를 안 세고
	// head 가 제목·요약·태그뿐이라 원 논문의 문서보다 훨씬 짧고 밀도가 높다.
	// 0.75 를 쓰면 편향이 0.5배 더 줄지만 정답질의 두 세트의 MRR 이 같이 내려간다
	// (0.922→0.908, 0.952→0.936). 근거는 lengthNorm 주석의 표다.
	normK = 1.2
	normB = 0.5
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
	// Seen 은 **이 세션에 이미 주입된 노트**라는 뜻이다. 점수와 무관하게 호출자가
	// 나중에 단다(hook/seen.go). 켜지면 RenderInject 가 요약을 빼고 자리만 남긴다 —
	// 모델이 이미 갖고 있는 문장을 다시 밀어 넣지 않기 위해서다.
	Seen bool
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

	// RuleLimit 은 **규칙 노트에 따로 주는 자리 수**다. 0 이면 규칙을 안 준다
	// (store.Layout.ListRules 의 § 참고).
	//
	// **별도 플래그를 두지 않는다.** IncludeReferences 처럼 켜는 불리언을 하나 더
	// 두면 0 과 false 가 같은 뜻이 되어 두 자리에서 같은 것을 말한다.
	//
	// # 왜 자리를 따로 주나
	//
	// 결정과 섞어 자르면 **규칙이 언제나 진다.** 규칙의 요약은 한 줄이고 결정의
	// 요약은 중앙 184자·최대 1,848자라, 같은 목록에서 head 히트 수로 겨루면
	// 긴 쪽이 이긴다 — 그게 length_test.go 가 고친 바로 그 편향이다. 길이 정규화가
	// 그 편향을 5배로 줄였지만 없앤 것은 아니고, 애초에 규칙과 결정은 **경쟁하는
	// 관계가 아니다.** 규칙은 "무엇을 하라" 이고 결정은 "지난번에 무엇을 했다" 다.
	//
	// # 주입 예산
	//
	// 훅 주입은 결정 3줄 + 참고 2줄이었고 실측 중앙값이 2,552자다(2026-08-27,
	// 실제 주입 블록 879개). 규칙 2줄을 더하면 줄은 2개 늘지만 규칙 요약은 한 줄
	// 규약이라 늘어나는 글자가 200자 안쪽이다. 그 값으로 교차 프로젝트 전이를 산다.
	RuleLimit int
}

// Recall 은 프롬프트에 관련된 결정을 점수순으로 주고, 회수 대상에서 아예 빠진
// (읽지 못한) 노트를 두 번째 값으로 함께 준다.
//
// 볼트를 읽지 못하면 에러를 준다 — 빈 결과로 뭉개지 않는다. "관련 결정이 없다"
// 와 "볼트를 못 읽었다" 는 훅 주입 경로에서 정반대 의미다: 전자는 참이지만
// 후자를 침묵시키면 에이전트가 과거 결정을 못 본 채 "없다" 로 읽는다.
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
	var rules []store.Note
	if o.RuleLimit > 0 {
		ru, ruskipped, ruerr := l.ListRules()
		if ruerr != nil {
			return nil, nil, ruerr
		}
		rules = ru
		skipped = append(skipped, ruskipped...)
	}

	cwdDomain := ""
	if o.Cwd != "" {
		cwdDomain = c.DomainForCwd(o.Cwd)
	}
	mentioned := mentionedDomains(c, keywords)

	// **도메인 언급 판정에는 동의어를 쓰지 않는다.** 도메인 접두어는 뜻이 있는
	// 낱말이 아니라 이름이고, 이름에 동의어를 붙이면 +6 이 엉뚱한 도메인 전체에
	// 걸린다 — weightMention 이 가장 센 가중치라 피해가 가장 크다.
	syn := LoadSynonyms(l)

	hits := scoreAll(notes, keywords, cwdDomain, mentioned, syn)

	if !o.CrossProject && cwdDomain != "" {
		scoped := filterByDomain(hits, cwdDomain)
		if len(scoped) > 0 {
			hits = scoped // 결과가 있으면 넓히지 않는다 — 현행 셸 동작
		}
	}

	hits = trim(hits, o)

	// **규칙을 맨 앞에 둔다.** 사람도 에이전트도 위에서부터 읽고, 주입 블록은
	// 잘릴 수 있다. 규칙은 여러 결정에서 증류한 것이라 한 줄당 값이 가장 크고,
	// "지난번에 무엇을 했다" 보다 "무엇을 하라" 가 먼저 읽혀야 한다.
	//
	// 규칙에는 cwd·언급 가점이 붙지 않는다 — 도메인이 없기 때문이다(ListRules 의 §).
	// 그래서 순수하게 낱말이 겹치는 정도로만 뽑힌다.
	if len(rules) > 0 {
		ro := o
		ro.Limit = o.RuleLimit
		hits = append(trim(scoreAll(rules, keywords, cwdDomain, mentioned, syn), ro), hits...)
	}

	// **참고는 자기 자리에서 자른다.** 결정과 섞어 자르면 밀어낸다 (§ ReferenceLimit).
	// 결정을 참고보다 앞에 둔다 — 확정된 것이 먼저다.
	if len(refs) > 0 {
		ro := o
		ro.Limit = o.ReferenceLimit
		rhits := trim(scoreAll(refs, keywords, cwdDomain, mentioned, syn), ro)
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

// lengthNorm 은 head 길이에 따른 감점 계수다. **1.0 을 넘지 않는다.**
//
// # 스윕 (2026-08-27, 실볼트 결정 414건 · 실제 프롬프트 500개 · 정답질의 720건)
//
// 프롬프트는 트랜스크립트에서 `promptSource: typed` 인 것만 cwd 와 함께 뽑았다 —
// **합성 질의로는 이 고장이 재현되지 않는다.** 고장의 원인이 대화체 프롬프트의
// 우연한 히트라서, 사람이 골라 넣은 질의로만 재면 편향이 보이지 않는다.
//
// 정답질의는 두 벌이다. `slug` 는 파일명에서 뽑아 길이에 중립이고, `요약뒤쪽` 은
// 요약의 **마지막 1/3** 에서 뽑아 긴 요약의 꼬리가 살아 있는지 본다.
//
//	안                Q4/Q1 편향   서로 다른 문서   slug MRR   요약뒤쪽 MRR   나빠짐/좋아짐
//	없음 (현행)          10.0배          227        0.921       0.945        — / —
//	b=0.5 ref=260        5.9배          259        0.924       0.953       3/9 · 2/7
//	**b=0.5 ref=200**    5.0배          258        0.922       0.952       6/9 · 4/8
//	b=0.5 ref=180        4.7배          261        0.919       0.949       8/9 · 4/6
//	b=0.75 ref=180       4.2배          266        0.908       0.936      17/10 · 13/8
//	b=1.0 ref=180        4.0배          271        0.904       0.936      20/10 · 12/8
//	b=0.5 ref=120        3.8배          266        0.899       0.936      30/9 · 12/5
//
// 단조 교환이다 — 세게 걸면 편향은 계속 줄고 정답 순위는 계속 나빠진다. 그래서
// **정답이 나빠지지 않는 가장 센 점**을 골랐다. ref=200/b=0.5 에서 두 세트의 MRR 이
// 모두 현행 이상이고(0.921→0.922, 0.945→0.952) 나빠진 질의보다 좋아진 질의가 많다.
// 아예 못 찾게 된 정답은 slug 0건, 요약뒤쪽 1건이다.
//
// 4분위 평균 주입 횟수(같은 프롬프트 500개, 주입 줄 수는 1,391 로 동일):
//
//	              Q1(88~153자)  Q2(~192)  Q3(~271)  Q4(276~1856)
//	현행              0.71        0.90      2.37       7.06
//	ref=200/b=0.5     1.07        1.35      3.25       5.38
//
// 상위 5가 **질적으로** 바뀐 것이 이 표보다 중요하다. 현행에서는 editup 프롬프트가
// 지배하는 세트인데도 1·2·5위가 `노벨피아규정`(요약 1,429자) · `eu-compliance-widget`
// (1,020자) · `전체이용가`(1,318자) 였다. 정규화 뒤 상위 5는 전부 editup 결정이다 —
// 즉 빠진 것은 관련 있는 교차 프로젝트 회수가 아니라 **길이로 이기던 잡음**이다.
//
// **잘라내기 안(요약을 N자로 절단해 head 를 만든다)은 기각했다.** 절단선 뒤의 낱말이
// head 에서 통째로 사라지고, 요약은 본문에 복사돼 있지 않아 body 히트로도 못 걸린다.
// 위 표의 `요약뒤쪽` 세트 306건이 정확히 그것을 재는 자리이고, 정규화는 그 세트의
// MRR 을 **올리면서**(0.945→0.952) 같은 편향을 잡는다. 낱말을 하나도 안 버린다.
//
// # 다시 재는 법
//
// 위 절대값은 그날의 볼트 스냅샷(결정 414건)에 묶인 것이라 **다시 재면 달라진다.**
// 볼트가 커지면 4분위 경계가 움직이고 편향 배수도 같이 움직인다 — 실제로 같은 날
// 결정이 417건이 됐을 때 무정규화 기준선이 10.0배에서 **14.4배**로 올라갔다.
// 그래서 봐야 하는 것은 절대값이 아니라 **같은 스냅샷의 A/B** 다:
//
//	                    무정규화   b=0.5 ref=200
//	Q4/Q1 편향            14.4배        6.7배
//	회수된 서로 다른 문서    226         258
//	집중도 상위5           12.0%        10.2%
//	Q1 평균 주입(0회 비율)  0.51(81%)   0.83(70%)
//	slug MRR              0.918        0.918
//	요약뒤쪽 MRR           0.954        0.957
//
// 재는 하네스는 `TestRealVaultRecallBias` 다 (realvault_test.go). 프롬프트 세트가
// 없으면 편향 지표는 건너뛴다 — 합성 질의로는 이 고장이 재현되지 않는다.
func lengthNorm(headRunes int) float64 {
	if headRunes <= refHeadRunes {
		return 1
	}
	n := (normK + 1) / (1 + normK*((1-normB)+normB*float64(headRunes)/refHeadRunes))
	if n > 1 {
		return 1
	}
	return n
}

// headHitFloor 는 head 히트가 있었는데 반올림으로 0 이 되는 것을 막는 바닥이다.
//
// **감점의 목적은 순위를 낮추는 것이지 안 보이게 하는 것이 아니다** —
// supersededFloor 와 같은 이유이고, 같은 값이다. 바닥이 없으면 head 1,800자짜리
// 노트의 head 히트 하나(3 × 0.21 = 0.63 → 0)가 사라지고, 아래 `score > 0` 과
// MinScore 가 그 노트를 결과에서 통째로 지운다. 긴 요약은 나쁜 습관이지만
// 회수 불가 사유는 아니다.
const headHitFloor = 1

// headScore 는 head·동의어 히트의 점수를 head 길이로 정규화해서 준다.
//
// **동의어 히트도 같이 정규화한다.** 그것도 head 에 걸린 것이라 같은 길이 편향을
// 받는다 (synonym.go 는 head 문자열을 그대로 본다).
//
// body 히트는 정규화하지 않는다. 본문 길이는 여기서 재는 값이 아니고, 본문은
// 이미 가중치가 1 이라 순위를 뒤집는 힘이 없다 — 그리고 본문 길이까지 재기
// 시작하면 "짧게 쓴 결정문이 유리하다" 는 잘못된 유인이 생긴다.
func headScore(headHits, synHits, headRunes int) int {
	raw := weightHead*headHits + weightSynonym*synHits
	if raw == 0 {
		return 0
	}
	s := int(math.Round(float64(raw) * lengthNorm(headRunes)))
	if s < headHitFloor {
		s = headHitFloor
	}
	return s
}

func scoreAll(notes []store.Note, keywords []string, cwdDomain string, mentioned map[string]bool, syn Synonyms) []Hit {
	var hits []Hit
	for _, n := range notes {
		// **철회된 노트는 아예 안 본다.**
		//
		// 이 시스템에는 "잘못 기록된 노트를 걷어낼 경로" 가 없었다. regretted 를
		// 걸어도 점수가 1점도 안 깎이고, superseded 로 내려도 -5 뿐이라 MinScore:1
		// 인 훅 주입에서 절대 안 빠졌다. 자동 기록이 도는 이상 오기록은 시간문제다.
		//
		// **regretted 와 다른 것이다.** regretted 는 "했는데 나빴다" 라서 계속 떠야
		// 한다 — 같은 실수를 되풀이하지 않으려면 눈앞에 있어야 하고, RenderInject 가
		// 경고 줄을 붙이는 것이 그 설계다. retracted 는 "애초에 결정이 아니었다"
		// 라서 떠 있을 이유가 없다.
		//
		// 감점이 아니라 배제인 이유: 감점은 "덜 중요하다" 는 뜻인데, 철회는
		// "이건 근거가 아니다" 는 뜻이다. 점수 몇 점으로 표현할 수 있는 것이 아니다.
		// 파일은 지우지 않는다 — 볼트에 둔 것을 지우지 않는다는 규칙은 여기도 같다.
		if n.Meta.Status == store.StatusRetracted {
			continue
		}
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

		headHits, bodyHits, synHits := 0, 0, 0
		for _, k := range keywords {
			// **정확히 맞은 것이 우선이고, 한 질의어는 한 번만 센다.**
			// 형제까지 따로 세면 표를 크게 쓴 낱말이 점수를 독식한다 (synonym.go).
			switch {
			case matches(head, k):
				headHits++
			case syn.hits(head, k):
				synHits++
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
		// **동의어 히트는 이 게이트를 통과시킨다.** 게이트가 막으려는 것은 CJK
		// 부분문자열이 만드는 *우연한* 히트인데, 형제 낱말은 사람이 표에 직접
		// 적었을 때만 걸리므로 우연이 아니다 — 큐레이션이 필터다 (synonym.go).
		// 통과시키지 않으면 이 기능이 아무것도 못 한다: 패러프레이즈 질의는
		// 정의상 정확한 히트가 0 이고, 대화체라 게이트가 2를 요구한다.
		if headHits+synHits < minHeadHits(len(keywords)) {
			continue
		}

		score := headScore(headHits, synHits, len([]rune(head))) + weightBody*bodyHits
		if cwdDomain != "" && contains(n.Meta.Domain, cwdDomain) {
			score += weightCwdDomain
		}
		for d := range mentioned {
			if contains(n.Meta.Domain, d) {
				score += weightMention
			}
		}
		if n.Meta.Status == store.StatusSuperseded {
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
		// **이미 이 세션에 주입된 것은 요약을 빼고 자리만 남긴다.**
		//
		// 모델이 이미 그 문장을 갖고 있으므로 두 번째부터는 아무것도 더해 주지
		// 않는다. 그래도 줄 자체는 남긴다 — "지금 이게 관련 있다" 는 신호는 매번
		// 필요하고, 경로가 있어야 열어 볼 수 있다. 상태는 남긴다: 싸고, 그 사이
		// regretted 로 바뀌었으면 그게 제일 중요한 정보다.
		if h.Seen {
			if h.Note.IsRule() {
				fmt.Fprintf(&b, "- %s %s → %s\n",
					l.Lang().T("[규칙]", "[rule]"),
					l.Lang().T("(앞서 주입)", "(already shown)"), l.RelPath(h.Note.Path))
				if status == "regretted" || outcome == "bad" {
					warn = true
				}
				continue
			}
			if h.Note.IsReference() {
				fmt.Fprintf(&b, "- %s %s → %s\n",
					l.Lang().T("[참고]", "[reference]"),
					l.Lang().T("(앞서 주입)", "(already shown)"), l.RelPath(h.Note.Path))
			} else {
				fmt.Fprintf(&b, "- %s %s (%s/%s) → %s\n", date,
					l.Lang().T("(앞서 주입)", "(already shown)"),
					status, outcome, l.RelPath(h.Note.Path))
			}
			if status == "regretted" || outcome == "bad" {
				warn = true
			}
			continue
		}
		// **참고는 결정처럼 그리면 안 된다.**
		//
		// 참고에는 status·outcome 이 없다. 위에서 빈 값을 active/pending 으로
		// 채우므로 그대로 내면 **기획 초안이 확정된 결정으로 보인다** — 에이전트는
		// 그걸 근거로 삼는다. 상태를 떼고 참고임을 앞에 붙인다.
		// **규칙은 날짜가 아니라 표식으로 그린다.** 규칙에는 "언제 이 일이 있었나" 가
		// 없고 "지금 유효한가" 만 있다. 그래서 active 면 상태를 아예 안 찍고,
		// 뒤집혔을 때만 찍는다 — 규칙도 뒤집힌다(status: superseded).
		if h.Note.IsRule() {
			if status == "active" && outcome == "pending" {
				fmt.Fprintf(&b, "- %s %s → %s\n",
					l.Lang().T("[규칙]", "[rule]"), summary, l.RelPath(h.Note.Path))
			} else {
				fmt.Fprintf(&b, "- %s %s (%s) → %s\n",
					l.Lang().T("[규칙]", "[rule]"), summary, status, l.RelPath(h.Note.Path))
			}
			if r := overturnLine(l, m, summary); r != "" {
				b.WriteString(r)
			}
			if status == "regretted" || outcome == "bad" {
				warn = true
			}
			continue
		}
		if h.Note.IsReference() {
			fmt.Fprintf(&b, "- %s %s → %s\n",
				l.Lang().T("[참고]", "[reference]"), summary, l.RelPath(h.Note.Path))
			continue
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
