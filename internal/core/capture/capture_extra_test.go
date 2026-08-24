package capture

import (
	"reflect"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// 감사 결함 4 / 스펙 §11 "유사 slug 중복 생성 시도 → 거부": 중복 검사가
// os.Stat 하나뿐이던 시절에는 --slug 세번째 와 --slug 세-번째 가 둘 다
// 통과했다. 지금은 하이픈·공백·밑줄을 접고 대소문자를 무시한 키가 같으면
// 거부한다. (예전 TestDoAllowsNearDuplicateSlugCurrently 는 이 잘못된 동작을
// 고정하고 있었으므로 거부를 확인하는 쪽으로 뒤집었다.)
func TestDoRejectsNearDuplicateSlug(t *testing.T) {
	cases := []struct{ name, first, second string }{
		{"하이픈만 다르다", "세번째 시도", "세-번째-시도"},
		{"대소문자와 밑줄만 다르다", "Retry Policy", "retry_policy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, c := fixtureLayoutConfig(t)
			if _, err := Do(l, c, Request{Domain: "alpha", Slug: tc.first,
				Summary: "먼저 기록한다", Date: "2026-08-07", Body: []byte("## 결정\n")}); err != nil {
				t.Fatal(err)
			}
			_, err := Do(l, c, Request{Domain: "alpha", Slug: tc.second,
				Summary: "비슷한 slug 로 또 기록한다", Date: "2026-08-07", Body: []byte("## 결정\n")})
			if err == nil {
				t.Fatalf("유사 slug(%q vs %q)가 통과했다", tc.first, tc.second)
			}
			// 에러는 무엇과 충돌하는지 알려줘야 한다 — 기존 노트의 stem.
			if !strings.Contains(err.Error(), store.Slugify(tc.first)) {
				t.Errorf("에러가 충돌 상대(기존 stem)를 알려주지 않는다: %v", err)
			}
		})
	}
}

// TestDoAllowsGenuinelyDifferentSlug 는 유사 slug 검사가 과잉 거부로 가지
// 않는지 본다: 하이픈·대소문자 말고 실제 글자가 다르면 통과해야 한다.
func TestDoAllowsGenuinelyDifferentSlug(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	if _, err := Do(l, c, Request{Domain: "alpha", Slug: "저장소 정책",
		Summary: "저장소 정책을 정한다", Date: "2026-08-07", Body: []byte("## 결정\n")}); err != nil {
		t.Fatal(err)
	}
	// 끝의 "1" 은 하이픈·대소문자 차이가 아니라 실제 글자 차이다.
	if _, err := Do(l, c, Request{Domain: "alpha", Slug: "저장소 정책1",
		Summary: "저장소 정책을 다시 정한다", Date: "2026-08-07", Body: []byte("## 결정\n")}); err != nil {
		t.Fatalf("실제로 다른 slug 가 거부됐다 — 유사 검사가 과잉이다: %v", err)
	}
	// 같은 slug 라도 날짜가 다르면 다른 결정이다.
	if _, err := Do(l, c, Request{Domain: "alpha", Slug: "저장소 정책",
		Summary: "다음 날 다시 정한다", Date: "2026-08-08", Body: []byte("## 결정\n")}); err != nil {
		t.Fatalf("날짜가 다른 같은 slug 가 거부됐다: %v", err)
	}
}

// 지시 3: 편승 검색은 쓰기 *전*에 한다 — 자기 자신이 결과에 끼지 않게.
// 방금 기록한 노트가 Result.Related 에 나타나지 않는지로 이 순서를
// 못박는다. 검색이 쓰기 *후*로 바뀌면 List() 가 방금 쓴 파일을 읽어
// 자기 자신을 최고 점수로 돌려주게 되어 이 테스트가 깨진다.
func TestDoRelatedExcludesJustWrittenNote(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "저장 엔진 자기 참조 확인", Summary: "저장 엔진을 다시 본다",
		Date: "2026-08-07", Body: []byte("## 결정\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range res.Related {
		if h.Note.Path == res.Path {
			t.Errorf("편승 검색이 쓰기 후에 실행된 것으로 보인다 — 자기 자신이 "+
				"Related 에 포함됐다: %s", h.Note.Path)
		}
	}
}

// 지시 4: ensureDecisionTag 는 decision 태그를 맨 앞에 붙인다. 이미 있으면
// 그대로 둔다.
func TestEnsureDecisionTagPrependsWhenMissing(t *testing.T) {
	got := ensureDecisionTag([]string{"alpha", "저장엔진"})
	want := []string{"decision", "alpha", "저장엔진"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ensureDecisionTag() = %v, want %v", got, want)
	}
}

func TestEnsureDecisionTagLeavesExistingAlone(t *testing.T) {
	got := ensureDecisionTag([]string{"decision", "alpha", "저장엔진"})
	want := []string{"decision", "alpha", "저장엔진"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ensureDecisionTag() = %v, want %v (이미 있으면 그대로 둬야 한다)", got, want)
	}
}
