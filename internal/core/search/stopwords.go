package search

// stopwords 는 회수 키워드에서 제거할 단어다. 정확히 한 줄 전체가 일치할 때만 제거한다.
// "결정" 이 반드시 들어 있어야 한다 — 없으면 모든 결정 문서가 매칭된다.
var stopwords = map[string]bool{
	"결정": true, "그리고": true, "그래서": true, "하지만": true, "그런데": true,
	"이것": true, "저것": true, "그것": true, "여기": true, "거기": true,
	"어떻게": true, "무엇": true, "언제": true, "어디": true, "누가": true,
	"하다": true, "한다": true, "했다": true, "있다": true, "없다": true,
	"같다": true, "된다": true, "됐다": true, "보다": true, "주다": true,
	"확인": true, "진행": true, "작업": true, "파일": true, "내용": true,
	"때문": true, "위해": true, "대해": true, "관련": true, "정도": true,
	"경우": true, "부분": true, "방법": true, "문제": true, "상태": true,
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
