package search

import (
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/core/store"
)

func TestRecallScoring(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	hits := Recall(l, c, "저장 엔진을 무엇으로 골랐지", Options{Limit: 3, MinScore: 1})
	if len(hits) == 0 {
		t.Fatal("매칭이 없다")
	}
	if !strings.Contains(hits[0].Note.Stem, "저장엔진") {
		t.Errorf("1위가 저장엔진이 아니다: %s (score=%d)", hits[0].Note.Stem, hits[0].Score)
	}
}

func TestRecallNoMatchReturnsEmpty(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	if hits := Recall(l, c, "완전히 무관한 주제 짜장면", Options{Limit: 3, MinScore: 1}); len(hits) != 0 {
		t.Errorf("무관한 프롬프트에 %d건 매칭: %+v", len(hits), hits)
	}
}

func TestSupersededPenalty(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	// 픽스처의 alpha-결정-스키마 는 superseded 다. 같은 검색에서 잡히는
	// active 노트보다 점수가 낮아야 감점이 적용된 것이다.
	//
	// ★ 자기 리뷰: 브리프 원문 프롬프트("스키마 단일 테이블 저장 엔진")로는
	// alpha-스키마(superseded, head 3건+body 2건=11점-패널티5=6)와
	// alpha-저장엔진(active, head 2건=6점)이 정확히 6:6 으로 동점이 나서
	// "sup.Score < act.Score" 단언이 실패했다(실측 확인). "테이블" 키워드를
	// 빼면 sup 의 head/body 히트가 각각 1건씩 줄어(raw 11→7, -5=2) act(6)
	// 보다 확실히 낮아진다. 감점 로직 자체는 옳고 픽스처와의 조합에서만
	// 우연히 동점이 났던 것이라 프롬프트를 조정했다.
	hits := Recall(l, c, "스키마 단일 저장 엔진", Options{CrossProject: true, Limit: 10, MinScore: 1})
	var sup, act *Hit
	for i := range hits {
		switch hits[i].Note.Meta.Status {
		case "superseded":
			if sup == nil {
				sup = &hits[i]
			}
		case "active":
			if act == nil {
				act = &hits[i]
			}
		}
	}
	if sup == nil {
		t.Fatal("superseded 노트가 결과에 없다 — 픽스처나 검색이 잘못됐다")
	}
	if act == nil {
		t.Fatal("active 노트가 결과에 없다 — 비교 대상이 없다")
	}
	if sup.Score >= act.Score {
		t.Errorf("superseded(%d) 가 active(%d) 보다 낮지 않다 — 감점이 적용되지 않았다",
			sup.Score, act.Score)
	}
}

// ★ 교차 프로젝트 회상 — 현행 셸의 결함을 테스트로 고정한다
func TestCrossProjectRecall(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	// cwd 는 alpha. 프롬프트는 alpha 노트("저장 엔진")와 common 노트("로케일 바이트")
	// 를 동시에 겨냥한다.
	//
	// ★ 자기 리뷰: 브리프 원문 프롬프트("로케일 의존 도구 바이트")는 alpha 도메인에
	// 단 1건도 매칭시키지 못했다(실측 확인). scoreAll 은 cwd 와 무관하게 전역에서
	// 채점하고, off 경로는 "cwd 도메인으로 좁힌 결과가 비어 있을 때만 전체로
	// 넓힌다"(브리프 Options.CrossProject 주석) — alpha 매칭이 0건이면 좁힌 결과가
	// 항상 비므로 off 도 자동으로 전체로 넓혀져 on 과 결과가 같아져 버린다.
	// 이래서는 두 플래그의 차이를 전혀 못 본다. cwd 도메인(alpha)에 최소 1건은
	// 걸리게 해야 "1건이라도 있으면 절대 안 넓힌다"는 버그가 실제로 재현된다 —
	// 그래서 프롬프트에 "저장 엔진"을 추가했다.
	prompt := "저장 엔진 로케일 바이트"
	cwd := "/tmp/proj/alpha"

	off := Recall(l, c, prompt, Options{Cwd: cwd, CrossProject: false, Limit: 3, MinScore: 1})
	on := Recall(l, c, prompt, Options{Cwd: cwd, CrossProject: true, Limit: 3, MinScore: 1})

	if len(on) == 0 {
		t.Fatal("CrossProject=true 인데 common 문서를 못 찾았다")
	}
	if len(off) >= len(on) {
		t.Errorf("CrossProject=false 가 더 많이 찾았다 — 필터가 동작하지 않는다 (off=%d on=%d)",
			len(off), len(on))
	}
}

func TestRenderInject(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	hits := Recall(l, c, "로케일 의존 도구 바이트", Options{CrossProject: true, Limit: 3, MinScore: 1})
	out := RenderInject(l, hits)
	if !strings.HasPrefix(out, "[과거 결정 참조]\n") {
		t.Errorf("헤더가 없다:\n%s", out)
	}
	if !strings.Contains(out, "(active/bad)") {
		t.Errorf("status/outcome 표기가 없다:\n%s", out)
	}
	// outcome: bad 가 있으면 경고 줄이 붙는다
	if !strings.Contains(out, "아쉬운 결과로 기록된 건이 있음") {
		t.Errorf("bad outcome 경고가 없다:\n%s", out)
	}
}

func TestRenderInjectEmpty(t *testing.T) {
	if out := RenderInject(nil, nil); out != "" {
		t.Errorf("매칭 없을 때 출력이 있다: %q", out)
	}
}

// ★ 지시 4: stem 에는 도메인 접두어와 "-결정-" 이 항상 들어 있어서, "결정" 이
// 불용어가 아니면 모든 노트가 매칭될 뻔했다. "결정" 은 stopwords 에 있으므로
// ExtractKeywords 가 빈 결과를 주고 Recall 은 즉시 nil 을 반환해야 한다.
func TestStopwordOnlyPromptMatchesNothing(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	hits := Recall(l, c, "결정", Options{CrossProject: true, Limit: 10, MinScore: 1})
	if len(hits) != 0 {
		t.Errorf("불용어 '결정' 만으로 %d건 매칭 — stem 의 '-결정-' 표식이 새고 있다: %+v", len(hits), hits)
	}
}

// ★ 지시 2: trim() 은 정렬 → MinScore 필터 → Limit 절단 순이어야 한다.
// 정렬 → Limit 절단 → MinScore 필터 순이면, MinScore 미만 항목이 Limit 자리를
// 차지한 뒤 걸러져 결과가 Limit 보다 적어질 수 있다. MinScore 를 만족하는
// 항목이 Limit 개 이상 있으면, 결과는 정확히 Limit 개여야 하고 전부
// MinScore 이상이어야 한다.
func TestTrimFillsLimitAfterMinScoreFilter(t *testing.T) {
	hits := []Hit{
		{Note: store.Note{Path: "a-low"}, Score: 2}, // MinScore 미만
		{Note: store.Note{Path: "b-hi"}, Score: 10},
		{Note: store.Note{Path: "c-hi"}, Score: 9},
		{Note: store.Note{Path: "d-hi"}, Score: 8},
		{Note: store.Note{Path: "e-low"}, Score: 1}, // MinScore 미만
	}
	out := trim(hits, Options{Limit: 3, MinScore: 5})
	if len(out) != 3 {
		t.Fatalf("MinScore 이상이 3건 이상인데 Limit=3 결과가 %d건: %+v", len(out), out)
	}
	for _, h := range out {
		if h.Score < 5 {
			t.Errorf("MinScore(5) 미만 항목이 결과에 섞였다: %+v", h)
		}
	}
}
