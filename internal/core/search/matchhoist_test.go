package search

import (
	"fmt"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// 넓은 문자 판정을 노트 루프 밖으로 뺀 것을 고정한다.
//
// `hasWideScript` 는 **질의어에만 달렸는데** 노트마다 다시 계산되고 있었다.
// 볼트 614건 × 질의어 35개 × Recall 610회 = 1,300만 번이고, 매번 룬을 풀어
// 유니코드 표를 네 번 찾는다. 동의어 형제를 밖으로 뺀 것과 같은 종류다
// (hitsSiblings 의 §).
//
// 위험은 하나뿐이고 조용하다: **다른 낱말의 판정을 들고 들어가는 것.** 그러면
// 한글 낱말이 낱말 경계 규칙(ASCII)으로 걸리거나 그 반대가 되는데, 둘 다
// 에러가 아니라 "결과가 조금 달라짐" 으로 나타난다.

// ★ 미리 잰 판정과 그때그때 잰 판정이 언제나 같아야 한다.
func TestHoistedWideFlagAgreesWithMatches(t *testing.T) {
	texts := []string{
		"오르카 브라우저로 지라를 연다",
		"use-postgres 를 골랐다 (used 가 아니다)",
		"orca browser 와 지라 slack 혼용",
		"",
	}
	keywords := []string{"오르카", "브라우저", "지라", "use", "used", "orca", "slack",
		"승부", "승부하면", "postgres", "browser"}

	for _, text := range texts {
		for _, k := range keywords {
			want := matches(text, k)
			got := matchesIn(text, k, hasWideScript(k))
			if got != want {
				t.Errorf("matches(%q, %q)=%v 인데 미리 잰 쪽은 %v", text, k, want, got)
			}
		}
	}
}

// ★ 섞인 질의에서 각 낱말이 **자기** 규칙으로 걸려야 한다.
//
// 이 시험이 잡는 고장: 첫 낱말의 판정을 전체에 써 버리는 것. 한글 낱말 뒤에
// 오는 `use` 가 부분문자열로 걸려 `used` 를 맞히거나, 그 반대가 된다.
func TestMixedScriptKeywordsKeepTheirOwnRule(t *testing.T) {
	mk := func(summary, body string) store.Note {
		return store.Note{
			Path: "/v/d/" + summary + ".md", Stem: summary,
			Meta: store.Meta{Summary: summary, Date: "2026-08-31", Status: "active"},
			Body: []byte(body),
		}
	}
	// `승부하면` 은 한글이라 부분문자열로 `승부` 에 걸린다.
	// `used` 는 ASCII 라 낱말 경계 때문에 `use` 에 **안** 걸린다.
	notes := []store.Note{
		mk("승부하면 used 가 나온다", ""),
		mk("승부 use 를 쓴다", ""),
	}
	// 두 낱말이 모두 걸려야 게이트를 넘도록 흔한 낱말을 하나 더 얹는다.
	hits := scoreAll(prepare(notes, ""), []string{"승부", "use"}, 2, "", nil, Synonyms{})

	got := map[string]bool{}
	for _, h := range hits {
		got[h.Note.Stem] = true
	}
	if !got["승부 use 를 쓴다"] {
		t.Error("`승부`·`use` 가 그대로 있는 노트가 안 걸렸다")
	}
	if !got["승부하면 used 가 나온다"] {
		t.Error("`승부하면` 이 `승부` 에 안 걸렸다 — 한글은 부분문자열이어야 한다")
	}
	// `used` 는 `use` 로 안 걸리므로 첫 노트의 히트는 `승부` 하나뿐이다.
	// 그 사실을 점수로 확인한다: 둘 다 걸린 노트가 더 높아야 한다.
	var a, b int
	for _, h := range hits {
		if h.Note.Stem == "승부하면 used 가 나온다" {
			a = h.Score
		} else {
			b = h.Score
		}
	}
	if a >= b {
		t.Errorf("`used` 가 `use` 로 걸렸다 — 낱말 경계 규칙이 무시됐다 (%d 대 %d)", a, b)
	}
}

// ★ 질의어 순서를 바꿔도 결과가 같아야 한다. 판정을 미리 재는 배열의 첨자가
// 어긋나면 여기서 드러난다.
func TestScoreIsIndependentOfKeywordOrder(t *testing.T) {
	notes := make([]store.Note, 0, 12)
	for i := 0; i < 12; i++ {
		notes = append(notes, store.Note{
			Path: fmt.Sprintf("/v/d/n%d.md", i), Stem: fmt.Sprintf("n%d", i),
			Meta: store.Meta{
				Summary: fmt.Sprintf("노트 %d 오르카 browser 지라 slack", i%3),
				Date:    "2026-08-31", Status: "active",
			},
			Body: []byte("오르카 browser 본문 지라"),
		})
	}
	kw := []string{"오르카", "browser", "지라", "slack"}
	rev := []string{"slack", "지라", "browser", "오르카"}

	sum := func(ks []string) map[string]int {
		out := map[string]int{}
		for _, h := range scoreAll(prepare(notes, ""), ks, 8, "", nil, Synonyms{}) {
			out[h.Note.Stem] = h.Score
		}
		return out
	}
	a, b := sum(kw), sum(rev)
	if len(a) == 0 {
		t.Fatal("후보가 0건 — 픽스처가 게이트를 못 넘는다")
	}
	for stem, sa := range a {
		if b[stem] != sa {
			t.Errorf("%s: 정순 %d · 역순 %d — 순서에 따라 달라지면 안 된다", stem, sa, b[stem])
		}
	}
}
