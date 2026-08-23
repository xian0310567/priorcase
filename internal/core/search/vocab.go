package search

import (
	"strings"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// TagVocabulary 는 태그를 **회수에 새 낱말을 더하는 것과 아닌 것**으로 가른다.
//
// # 왜 이걸 재나
//
// 회수는 `stem + summary + contentTags` 를 head 로 합쳐 본다(scoreAll). 태그의
// 낱말이 이미 제목이나 요약에 있으면, 그 태그를 달든 안 달든 **걸리는 질의가 똑같다.**
// 적는 사람은 회수 어휘를 넓혔다고 믿는데 실제로는 아무 일도 안 일어난다.
//
// 실볼트 실측(2026-08-23): 태그 달린 결정 노트 278건 중 12건(4%)이 그런 태그만 달고 있었고,
// 태그가 더하는 새 낱말은 중앙값 2개였다 — 규약이 요구하는 "회수 키워드 6~10개,
// 동의어와 상위어를 같이" 와 한참 멀다.
//
// 이게 왜 아픈지는 같은 날 재현했다. "웹소설을 AI로 여러 편 찍어내면 경쟁력이 있나"
// 는 관련 규칙을 찾아내는데, **같은 질문을 바꿔 말한** "작품을 많이 만들어서
// 승부하면 되나" 는 볼트 전체에서 0건이었다. 개념은 있는데 낱말이 없다.
//
// # 판정 규칙
//
// 태그 하나가 head 의 나머지(stem + summary)에 **이미 매칭되면 죽은 태그**다.
// 매칭은 회수와 **같은 함수**(Matches)를 쓴다 — 여기서 따로 구현하면 판정이
// 실제 회수 동작과 조용히 갈라진다.
//
// 보수적으로 본다: 태그 문자열 전체가 이미 걸릴 때만 죽었다고 한다. 부분만 겹치는
// 것(예: "볼트동기화" 와 요약의 "동기화")은 살아 있는 것으로 센다 — 애매한 것을
// 죽었다고 말하면 그 경고는 곧 무시당한다.
// markerTags 는 **주제가 아니라 분류를 말하는 태그**다. 회수 어휘를 넓히는 셈에서 뺀다.
//
// 점수 쪽 boilerplateTags 와 따로 두는 이유가 있다. `lesson` 은 head 에 남아야 한다 —
// `prior recall lesson` 이나 "교훈" 을 찾는 질의가 정당하고, 규약도 그것을 교차
// 프로젝트 회상의 장치로 쓴다. 하지만 **"이 결정을 나중에 무슨 낱말로 찾을까" 의
// 답은 아니다.**
//
// 이걸 안 빼면 검사가 반쯤 무력해진다: 실볼트 304건 중 151건(49%)이 lesson 을 달고
// 있어서, 나머지 태그가 전부 헛돌아도 lesson 하나로 통과한다.
var markerTags = map[string]bool{"lesson": true, "교훈": true}

func TagVocabulary(n store.Note) (fresh, dead []string) {
	var tags []string
	for _, t := range contentTags(n.Meta.Tags) {
		if !markerTags[strings.ToLower(strings.TrimSpace(t))] {
			tags = append(tags, t)
		}
	}
	if len(tags) == 0 {
		return nil, nil
	}
	// head 에서 태그를 뺀 나머지. 태그끼리 서로를 가리면 안 되므로 태그는 안 넣는다.
	rest := strings.ToLower(n.Stem + " " + n.Meta.Summary)
	for _, t := range tags {
		if Matches(rest, strings.ToLower(strings.TrimSpace(t))) {
			dead = append(dead, t)
			continue
		}
		fresh = append(fresh, t)
	}
	return fresh, dead
}
