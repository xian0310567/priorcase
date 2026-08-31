package search

import (
	"fmt"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// ★ 이 파일이 잠그는 것은 **2026-08-31 재현 고장**이다.
//
//	"나 이제 뭐 해야하지?? 지라, 슬랙, 구글챗을 확인해서 찾아봐줘"
//
// 볼트에 셋의 접근 방법이 각각 다른 노트로 있는데, 옛 게이트가 대화체 질의에
// head 히트 2개를 요구해서 **셋 다 탈락했다.** 노트마다 자기 낱말 하나씩만 맞기
// 때문이다. 실측으로 대화체 단일주제 질의의 89% 가 정답을 못 찾고 있었다.

// synthNotes 는 문서빈도를 마음대로 정할 수 있는 합성 볼트다.
//
// 픽스처 볼트(4건)로는 이 계약을 못 잠근다 — df ≥ 2 인 낱말이 `alpha` 하나뿐인데
// 그건 도메인 접두어라 weightMention(+6)이 같이 붙어 게이트만 재는 것이 불가능하다.
func synthNotes(rareIn, commonIn int) []store.Note {
	notes := make([]store.Note, 0, 10)
	for i := 0; i < 10; i++ {
		summary := fmt.Sprintf("노트 %d 의 내용", i)
		if i < commonIn {
			summary += " 흔한낱말"
		}
		if i < rareIn {
			summary += " 드문낱말"
		}
		notes = append(notes, store.Note{
			Path: fmt.Sprintf("/v/d/n%d.md", i),
			Stem: fmt.Sprintf("n%d", i),
			Meta: store.Meta{Summary: summary, Date: "2026-08-31", Status: "active"},
		})
	}
	return notes
}

// conversational 은 대화체 판정을 넘기는 토큰 수다 (실제 질의는 7~11개다).
const conversational = 7

func TestRareWordAlonePassesGate(t *testing.T) {
	// 드문낱말은 10건 중 1건에만 있다 → 변별어. 그 하나로 게이트를 넘어야 한다.
	notes := synthNotes(1, 5)
	hits := scoreAll(notes, []string{"드문낱말"}, conversational, "", nil, Synonyms{}, "")
	if len(hits) != 1 {
		t.Fatalf("변별어 하나로 후보가 %d건 — 1건이어야 한다. "+
			"이것이 막히면 '지라' 하나로 물었을 때 지라 노트가 안 나온다", len(hits))
	}
}

func TestCommonWordAloneFailsGate(t *testing.T) {
	// 흔한낱말은 10건 중 5건에 있다 → 변별어가 아니다. 하나로는 못 넘어야 한다.
	//
	// **이 계약을 풀면 내용어 없는 대화체 질의가 볼트 절반을 끌어온다.** 실볼트
	// 실측으로 `작업`(df 4.1%)이 변별어가 되는 설정에서 후보 22건이 돌아왔다.
	notes := synthNotes(1, 5)
	if hits := scoreAll(notes, []string{"흔한낱말"}, conversational, "", nil, Synonyms{}, ""); len(hits) != 0 {
		t.Errorf("흔한 낱말 하나로 후보가 %d건 — 0건이어야 한다", len(hits))
	}
}

func TestTwoCommonWordsStillPassGate(t *testing.T) {
	// **옛 계약은 그대로 남는다.** 흔한 낱말 둘로 통과하던 질의는 지금도 통과한다.
	notes := synthNotes(1, 5)
	hits := scoreAll(notes, []string{"흔한낱말", "내용"}, conversational, "", nil, Synonyms{}, "")
	if len(hits) == 0 {
		t.Error("흔한 낱말 둘인데 후보가 0건 — 옛 게이트가 통과시키던 것을 막았다")
	}
}

func TestShortQueryStillNeedsOneHit(t *testing.T) {
	// `prior recall "저장 엔진"` 처럼 사람이 골라 넣은 질의는 하나로 만족한다.
	notes := synthNotes(1, 5)
	hits := scoreAll(notes, []string{"흔한낱말"}, 2, "", nil, Synonyms{}, "")
	if len(hits) != 5 {
		t.Errorf("짧은 질의에서 후보가 %d건 — 5건이어야 한다", len(hits))
	}
}

func TestRareHitOutranksCommonHit(t *testing.T) {
	// 변별어 히트 하나가 흔한 낱말 히트 하나보다 높아야 한다. 안 그러면 후보에는
	// 들어와도 상위 3(훅이 싣는 전부)에 못 들어 실제로는 없는 것과 같다.
	//
	// 실측(2026-08-31): 게이트만 고쳤을 때 대화체 정답의 상위3 진입은 44% 였고,
	// weightRare 를 얹어 97% 가 됐다.
	rare := headScore(1, 1, 0, refHeadRunes)   // 히트 하나인데 그것이 변별어
	common := headScore(1, 0, 0, refHeadRunes) // 히트 하나인데 흔한 낱말
	if rare <= common {
		t.Errorf("변별어 히트 %d점 ≤ 흔한 낱말 히트 %d점 — 순위가 안 갈린다", rare, common)
	}
	// **동의어 히트보다는 세다.** 정확히 맞은 것이 언제나 앞선다는 계약
	// (weightSynonym 의 §)은 변별어 가점이 붙어도 그대로다.
	if syn := headScore(0, 0, 1, refHeadRunes); rare <= syn {
		t.Errorf("변별어 히트 %d점 ≤ 동의어 히트 %d점", rare, syn)
	}
}

func TestRareThresholdHasFloor(t *testing.T) {
	// **풀마다 따로 잰다.** 비율만 쓰면 규칙 풀(9건)에서는 어떤 낱말도 변별어가
	// 되지 못해 게이트가 영영 2로 남는다 — 규칙은 도메인이 없어 낱말만으로
	// 걸리므로 그러면 규칙 회수가 통째로 죽는다.
	for _, tc := range []struct{ pool, want int }{
		{0, 1}, {1, 1}, {9, 1}, {33, 1}, {34, 1}, {100, 3}, {540, 16},
	} {
		if got := rareThreshold(tc.pool); got != tc.want {
			t.Errorf("rareThreshold(%d) = %d, want %d", tc.pool, got, tc.want)
		}
	}
}

// ★ 대화체 판정은 **불용어를 빼기 전** 토큰으로 한다.
//
// 키워드 수로 재면 불용어 목록을 고칠 때마다 이 경계가 조용히 움직인다.
// 2026-08-31 에 활용형 불용어를 넣자 아래 첫 문장의 키워드가 4→3 으로 줄어
// "골라 넣은 질의" 로 오판됐고, 게이트가 1로 풀려 후보 22건이 돌아왔다.
func TestConversationalDetectionSurvivesStopwordChanges(t *testing.T) {
	for _, tc := range []struct {
		prompt string
		want   int
	}{
		{"무슨 작업을 하다가 멈춘것같은데 확인해줄 수 있어", 2},
		{"나 이제 뭐 해야하지?? 지라, 슬랙, 구글챗을 확인해서 찾아봐줘", 2},
		{"볼트 동기화", 1},
		{"저장 엔진", 1},
		{"npm 배포 인증", 1},
	} {
		if got := minHeadHits(PromptTokens(tc.prompt)); got != tc.want {
			t.Errorf("minHeadHits(PromptTokens(%q)) = %d, want %d (토큰 %d개)",
				tc.prompt, got, tc.want, PromptTokens(tc.prompt))
		}
	}
}
