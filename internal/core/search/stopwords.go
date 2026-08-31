package search

// stopwords 는 회수 키워드에서 제거할 단어다. 정확히 한 줄 전체가 일치할 때만 제거한다.
// "결정" 이 반드시 들어 있어야 한다 — 없으면 모든 결정 문서가 매칭된다.
var stopwords = map[string]bool{
	"결정": true, "그리고": true, "그래서": true, "하지만": true, "그런데": true,
	"이것": true, "저것": true, "그것": true, "여기": true, "거기": true,
	"어떻게": true, "무엇": true, "언제": true, "어디": true, "누가": true,
	"하다": true, "한다": true, "했다": true, "있다": true, "없다": true,
	"같다": true, "된다": true, "됐다": true, "보다": true, "주다": true,
	"진행": true,
	"때문": true, "위해": true, "대해": true, "관련": true, "정도": true,
	"경우": true, "부분": true,

	// ── 활용형 ────────────────────────────────────────────────────────
	//
	// **위의 사전형만 있으면 대화체 프롬프트에서 아무것도 못 막는다.** 사람은
	// "있다" 라고 치지 않고 "있어"·"있는" 이라고 친다. 실측(2026-08-31, 실제
	// 프롬프트 597개 × 실볼트 540건)으로 이것들이 head 히트를 만드는 크기가 이랬다:
	//
	//	으로 39.6% · 아니 22.6% · 하고 21.9% · 아니라 20.0% · 이다 16.5% ·
	//	하지 10.7% · 같은 8.7% · 하면 6.3% · 있어 4.8% · 있는 3.7% · 없어 3.7%
	//
	// `있어` 는 질의 95개에 나오고 노트 26건의 head 안쪽에("…있어야…") 걸린다.
	// scoreAll 의 게이트 § 이 실측한 "내용어가 하나도 없는데 후보 11건" 이 이것이다.
	//
	// # 규칙: 위에 있는 사전형의 활용형만 넣는다
	//
	// 이 목록이 커지다 내용어를 삼키는 것이 유일한 실패 모드다(바로 아래 § 이
	// 그 사고를 적고 있다). 그래서 **새 낱말을 넣지 않는다** — 이미 기능어로
	// 판정한 하다·있다·없다·같다·되다·아니다·무엇의 활용형만 편다.
	//
	// `먼저`·`전부`·`다른`·`하나` 는 df 가 비슷하게 크지만 **넣지 않았다.**
	// 이 볼트에서 그것들은 내용어다 — `규칙-한쪽-손해가-0이면-검증보다-먼저-넣는다`
	// 가 제목에 `먼저` 를 쓴다. 판정 기준은 빈도가 아니라 품사다.
	"있어": true, "있는": true, "있을": true, "있고": true, "있어야": true,
	"없어": true, "없는": true, "없을": true, "없고": true, "없어야": true,
	"같은": true, "같이": true, "같아": true, "같다면": true,
	"하고": true, "하지": true, "하면": true, "해서": true, "하는": true,
	"해야": true, "하니": true, "해도": true,
	"되는": true, "되면": true, "되고": true, "돼야": true,
	"아니": true, "아닌": true, "아니라": true, "아니면": true,
	"이다": true, "이라": true, "인데": true, "이고": true,
	"무슨": true, "어떤": true,

	// ── 시간·지시 부사 ────────────────────────────────────────────────
	//
	// 주제를 담지 않는데 **드물어서 변별어로 오인된다.** 희소성 게이트(scoreAll)가
	// 켜진 뒤 이 부류가 가장 아팠다: `이제` 는 df 1.1% 라 변별어로 잡히고,
	// 우연히 걸린 노트 6건이 주제어를 맞힌 노트와 같은 자격으로 상위를 먹었다.
	//
	// 실측(2026-08-31, 대화체 단일주제 542건) — 이 다섯 줄만 넣었을 때:
	//
	//	          못찾음   @1    @3    MRR
	//	넣기 전     2%     3%   56%   0.330
	//	넣은 뒤     2%    53%   97%   0.751
	//
	// **정확히 이것이 희소성 게이트의 실패 모드다.** 우연한 매칭은 본디 드물다.
	// 그래서 df 로는 못 거르고 품사로 걸러야 한다 — 게이트를 풀수록 이 목록이
	// 정확해야 한다.
	"이제": true, "지금": true, "아직": true, "방금": true, "이번": true,

	// `으로` 는 조사인데 **단독 토큰으로 살아남는다.** keywords.go 의 josa 는 끝에
	// 붙은 것만 자르고, 잘라서 2글자 미만이 되면 "뗀 것이 조사가 아니었다" 로 보고
	// 원형을 되살린다 — 조사가 홀로 있을 때 그 폴백이 정확히 조사를 살려 낸다.
	"으로": true, "로서": true, "로써": true, "에서": true, "에게": true,

	// **내용어를 여기 넣지 마라.** 예전에는 확인·작업·파일·내용·방법·문제·상태가
	// 여기 있었다. 이 볼트는 소프트웨어 결정을 담으므로 그것들은 기능어가 아니라
	// **주제어**다 — 질의에서 통째로 빠지는 바람에 `prior recall "상태 파일을 볼트
	// 밖으로 옮긴다"` 가 정확히 그 주제의 노트를 못 찾았고, 노트에 `상태파일`
	// 태그를 달아도 소용없었다.
	//
	// 실측(197건 head 코퍼스)으로 df 를 재 보니 흔해서 뺄 만한 것도 아니었다 —
	// 파일 10.7% · 확인 8.1% · 상태 4.1% · 문제 3.0% · 방법 1.5% · 작업 1.0% ·
	// 내용 0.5%. 가장 흔한 "파일" 조차 불용어가 아닌 "볼트"(11.7%)보다 드물다.
	//
	// 위에 남은 것은 진짜 기능어다 — 그것들은 주제를 안 담는다.
	"the": true, "and": true, "for": true, "with": true, "this": true,
	"that": true, "from": true, "have": true, "not": true, "you": true,

	// 영어 기능어. 실측으로 `can you add a spinner on the login button` 이 무관한
	// 결정 5건을 주입했다 — 대부분 이런 낱말이 본문 어딘가에 걸린 것이다.
	// 대화체 프롬프트는 기능어가 내용어보다 많다.
	"are": true, "was": true, "were": true, "been": true, "being": true,
	"can": true, "could": true, "will": true, "would": true, "should": true,
	"may": true, "might": true, "must": true, "shall": true,
	"add": true, "get": true, "set": true, "put": true, "make": true, "made": true,
	"use": true, "used": true, "using": true, "need": true, "want": true,
	"let": true, "give": true, "take": true, "know": true, "think": true,
	"all": true, "any": true, "some": true, "each": true, "every": true,
	"more": true, "most": true, "much": true, "many": true, "very": true,
	"here": true, "there": true, "then": true, "than": true, "when": true,
	"what": true, "which": true, "who": true, "how": true, "why": true, "where": true,
	"into": true, "onto": true, "over": true, "under": true, "about": true,
	"out": true, "off": true, "but": true, "or": true, "if": true, "so": true,
	"ok": true, "okay": true, "yes": true, "no": true, "please": true, "thanks": true,
	"it": true, "its": true, "they": true, "them": true, "their": true,
	"we": true, "our": true, "us": true, "me": true, "my": true, "your": true,
	"is": true, "as": true, "at": true, "by": true, "in": true, "on": true,
	"to": true, "of": true, "do": true, "does": true, "did": true, "done": true,
	"be": true, "has": true, "had": true, "an": true, "a": true,
}
