package search

import (
	"testing"

	"github.com/xian0310567/priorcase/internal/core/store"
)

func note(stem, summary string, tags ...string) store.Note {
	return store.Note{Stem: stem, Meta: store.Meta{Summary: summary, Tags: tags}}
}

// ★ **태그의 값은 "새로 걸리게 하는 질의" 다.**
//
// 회수는 `stem + summary + contentTags` 를 head 로 합쳐 본다(search.go). 태그의
// 낱말이 이미 제목이나 요약에 있으면, 그 태그를 달든 안 달든 걸리는 질의가 똑같다 —
// 적는 사람은 회수 어휘를 넓혔다고 믿지만 실제로는 아무 일도 안 일어난다.
//
// 실볼트 실측(2026-08-23): 태그 달린 결정 노트 278건 중 12건(4%)이 그런 태그만
// 달고 있었고, 태그가 더하는 새 낱말은 **중앙값 2개**였다 — 규약이 요구하는
// "회수 키워드 6~10개" 와 한참 멀다.
func TestFreshTagsExcludeWordsAlreadyInHead(t *testing.T) {
	n := note("priorcase-결정-볼트동기화-git리모트-2026-08-20",
		"볼트를 git 리모트로 동기화한다",
		"동기화", // 요약에 있다 → 죽은 태그
		"git", // 제목·요약에 있다 → 죽은 태그
		"백업",  // 어디에도 없다 → 살아 있는 태그
	)
	fresh, dead := TagVocabulary(n)

	if len(fresh) != 1 || fresh[0] != "백업" {
		t.Errorf("새 낱말을 더하는 태그 = %v, 원하는 값 [백업]", fresh)
	}
	if len(dead) != 2 {
		t.Errorf("죽은 태그 = %v, 2개여야 한다", dead)
	}
}

// 보일러플레이트는 세지 않는다. capture 가 전 노트에 붙이므로 신호가 아니다
// (search.go 의 boilerplateTags 와 같은 판정을 써야 갈리지 않는다).
func TestBoilerplateTagsAreIgnored(t *testing.T) {
	n := note("alpha-결정-저장엔진-2026-08-01", "저장 엔진을 고른다", "decision", "결정", "OPFS")

	fresh, dead := TagVocabulary(n)
	for _, d := range append(fresh, dead...) {
		if d == "decision" || d == "결정" {
			t.Errorf("보일러플레이트 %q 가 세어졌다", d)
		}
	}
	if len(fresh) != 1 || fresh[0] != "OPFS" {
		t.Errorf("fresh = %v, 원하는 값 [OPFS]", fresh)
	}
}

// 태그가 아예 없으면 둘 다 비어 있다 — "죽은 태그가 있다" 고 말하면 안 된다.
func TestNoTagsIsNotDead(t *testing.T) {
	fresh, dead := TagVocabulary(note("a-결정-b-2026-08-01", "요약"))
	if len(fresh) != 0 || len(dead) != 0 {
		t.Errorf("태그가 없는데 fresh=%v dead=%v", fresh, dead)
	}
}

// 대소문자는 회수와 같게 다룬다 — head 를 ToLower 해서 보므로 태그도 그래야 한다.
func TestTagVocabularyIsCaseInsensitive(t *testing.T) {
	n := note("x-결정-npm배포-2026-08-09", "NPM 으로 배포한다", "npm")
	fresh, dead := TagVocabulary(n)
	if len(fresh) != 0 || len(dead) != 1 {
		t.Errorf("대소문자만 다른 태그를 새 낱말로 셌다: fresh=%v dead=%v", fresh, dead)
	}
}

// ★ **`lesson` 은 구조 표식이지 회수 어휘가 아니다.**
//
// 규약이 "프로젝트를 넘어 재사용될 교훈이면 tags 에 lesson 을 넣는다" 고 해서
// 실볼트 304건 중 **151건(49%)**이 달고 있다. 그걸 내용어로 세면 절반이 무조건
// 통과하고, 정작 나머지 태그가 전부 헛돌아도 검사가 침묵한다.
//
// 점수 쪽 boilerplateTags 는 안 건드린다 — `prior recall lesson` 은 정당한 질의라
// head 에서 빼면 그게 안 걸린다. 여기서만 "어휘를 넓혔나" 의 셈에서 제외한다.
func TestLessonIsNotRetrievalVocabulary(t *testing.T) {
	n := note("alpha-결정-캐시전략-2026-08-20", "캐시 전략을 정한다", "lesson", "캐시")

	fresh, _ := TagVocabulary(n)
	if len(fresh) != 0 {
		t.Errorf("lesson 을 새 낱말로 셌다: fresh=%v", fresh)
	}
}

// 그래도 진짜 내용어가 하나라도 있으면 통과한다.
func TestLessonWithRealVocabularyPasses(t *testing.T) {
	n := note("alpha-결정-캐시전략-2026-08-20", "캐시 전략을 정한다", "lesson", "무효화")

	fresh, _ := TagVocabulary(n)
	if len(fresh) != 1 || fresh[0] != "무효화" {
		t.Errorf("fresh = %v, 원하는 값 [무효화]", fresh)
	}
}
