package search

import (
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// ★★★ 이 파일이 잠그는 것은 **프롬프트 한 줄로는 못 찾는 노트**다.
//
// 대화체는 대명사·생략·직전 화제 참조로 굴러가서 사람이 방금 친 한 줄에 내용어가
// 없다. 주제는 그 앞의 대화에 있다. 실볼트 실측(2026-09-04, 실물 라벨 165건 ·
// 서로 다른 질의 60개)으로 상위3에 정답이 든 질의가 **15건에서 33건**이 됐다
// (Options.Context 의 표).
//
// 아래 픽스처는 그 상황의 최소 형태다 — 질의에는 `그거` 뿐이고 주제어 `드문낱말` 은
// 직전 대화에만 있다.

func ctxNotes() []store.Note {
	return []store.Note{
		{Path: "/v/d/a.md", Stem: "a", Meta: store.Meta{
			Summary: "드문낱말 로 정한 것", Date: "2026-09-01", Status: "active"}},
		{Path: "/v/d/b.md", Stem: "b", Meta: store.Meta{
			Summary: "아무 상관 없는 다른 결정", Date: "2026-09-01", Status: "active"}},
	}
}

func ctxRecall(t *testing.T, prompt, ctx string) []Hit {
	t.Helper()
	hits, _, err := Recall(nil, &config.Config{}, prompt, Options{
		CrossProject: true, Limit: 5, MinScore: 1, Context: ctx,
		Corpus: &Corpus{Notes: ctxNotes(), pNotes: prepare(ctxNotes(), "")},
	})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	return hits
}

// 맥락이 없으면 주제어가 어디에도 없다 — 지금까지의 동작이다.
func TestWithoutContextTheTopicIsLost(t *testing.T) {
	if hits := ctxRecall(t, "그거 어떻게 하기로 했더라", ""); len(hits) != 0 {
		t.Fatalf("내용어가 없는 질의인데 %d건이 걸렸다 — 게이트가 풀렸다", len(hits))
	}
}

// ★ 맥락에 있는 주제어로 찾는다.
func TestContextSuppliesTheMissingTopic(t *testing.T) {
	hits := ctxRecall(t, "그거 어떻게 하기로 했더라", "아까 얘기한 드문낱말 말인데")
	if len(hits) == 0 {
		t.Fatal("맥락에 주제어가 있는데 한 건도 못 찾았다")
	}
	if hits[0].Note.Stem != "a" {
		t.Fatalf("1위가 %q — 주제어를 가진 a 여야 한다", hits[0].Note.Stem)
	}
}

// ★★ **맥락을 안 주면 결과가 한 점도 안 바뀐다.**
//
// 이 성질이 이 변경의 안전장치다. 훅 밖의 모든 호출(`prior recall`·MCP·retro·
// capture 중복 대조)은 맥락을 안 주므로, 여기가 성립하는 한 그 경로들은 손대지
// 않은 것과 같다.
func TestEmptyContextChangesNothing(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	for _, q := range []string{"저장 엔진", "인증", "alpha 배포 승인", "볼트 동기화"} {
		base := mustRecall(t, l, c, q, Options{CrossProject: true, Limit: 10, MinScore: 1})
		for _, ctx := range []string{"", "   ", "\n\t "} {
			got := mustRecall(t, l, c, q, Options{
				CrossProject: true, Limit: 10, MinScore: 1, Context: ctx})
			if len(got) != len(base) {
				t.Fatalf("%q: 맥락 %q 에서 결과 수가 %d → %d", q, ctx, len(base), len(got))
			}
			for i := range base {
				if got[i].Note.Stem != base[i].Note.Stem || got[i].Score != base[i].Score {
					t.Fatalf("%q: 맥락 %q 에서 %d위가 %s(%d) → %s(%d)", q, ctx, i+1,
						base[i].Note.Stem, base[i].Score, got[i].Note.Stem, got[i].Score)
				}
			}
		}
	}
}

// ★ **맥락은 게이트를 풀지 않는다.**
//
// 대화체 판정(minHeadHits)은 프롬프트의 토큰 수로 한다. 맥락까지 세면 사람이 두
// 낱말로 물어도 토큰이 수백 개가 되어 게이트가 언제나 2를 요구하고,
// `prior recall "볼트 동기화"` 의 계약이 맥락 유무로 갈린다.
func TestContextDoesNotChangeTheGateThreshold(t *testing.T) {
	notes := synthNotes(1, 5) // 드문낱말 1건 · 흔한낱말 5건
	long := ""
	for i := 0; i < 200; i++ {
		long += "채우는말 "
	}
	// 짧은 질의(토큰 1개)는 흔한 낱말 하나로도 통과한다 — 사람이 골라 넣은 질의다.
	hits, _, err := Recall(nil, &config.Config{}, "흔한낱말", Options{
		CrossProject: true, Limit: 20, MinScore: 1, Context: long,
		Corpus: &Corpus{Notes: notes, pNotes: prepare(notes, "")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 5 {
		t.Fatalf("후보 %d건 — 맥락이 길어도 짧은 질의의 게이트는 1이어야 한다 (5건 기대)",
			len(hits))
	}
}

// 프롬프트에 이미 있는 낱말이 맥락에도 있다고 두 번 세면 그 낱말이 두 배가 된다.
// scoreAll 은 한 질의어를 한 번만 세는 계약이다.
func TestContextDoesNotDoubleCountPromptWords(t *testing.T) {
	withDup := ctxRecall(t, "드문낱말 어떻게 했더라", "드문낱말 드문낱말 드문낱말")
	without := ctxRecall(t, "드문낱말 어떻게 했더라", "")
	if len(withDup) == 0 || len(without) == 0 {
		t.Fatal("둘 다 걸려야 한다")
	}
	if withDup[0].Score != without[0].Score {
		t.Fatalf("점수가 %d → %d — 맥락이 프롬프트 낱말을 다시 셌다",
			without[0].Score, withDup[0].Score)
	}
}
