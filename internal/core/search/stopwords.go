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
}
