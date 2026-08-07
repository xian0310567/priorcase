package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/core/store"
)

func TestRecallScoring(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	hits := mustRecall(t, l, c, "저장 엔진을 무엇으로 골랐지", Options{Limit: 3, MinScore: 1})
	if len(hits) == 0 {
		t.Fatal("매칭이 없다")
	}
	if !strings.Contains(hits[0].Note.Stem, "저장엔진") {
		t.Errorf("1위가 저장엔진이 아니다: %s (score=%d)", hits[0].Note.Stem, hits[0].Score)
	}
}

func TestRecallNoMatchReturnsEmpty(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	if hits := mustRecall(t, l, c, "완전히 무관한 주제 짜장면", Options{Limit: 3, MinScore: 1}); len(hits) != 0 {
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
	hits := mustRecall(t, l, c, "스키마 단일 저장 엔진", Options{CrossProject: true, Limit: 10, MinScore: 1})
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

	off := mustRecall(t, l, c, prompt, Options{Cwd: cwd, CrossProject: false, Limit: 3, MinScore: 1})
	on := mustRecall(t, l, c, prompt, Options{Cwd: cwd, CrossProject: true, Limit: 3, MinScore: 1})

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
	hits := mustRecall(t, l, c, "로케일 의존 도구 바이트", Options{CrossProject: true, Limit: 3, MinScore: 1})
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

// TestRenderInjectWarnsOnRegrettedAlone 는 경고 조건의 앞 절
// (status == "regretted") 만으로도 경고 줄이 붙는지 본다.
//
// RenderInject 의 조건은 `status == "regretted" || outcome == "bad"` 인데
// 픽스처 볼트에는 outcome: bad 인 노트만 있어서 뒤 절만 테스트되고 있었다.
// 앞 절을 지워도 전체 테스트가 통과하는 상태였다는 뜻이다 — 사용자에게 나가는
// 경고 문구라 조용히 사라지면 회고를 읽어야 할 자리에서 아무 신호도 안 뜬다.
// 두 절의 독립성을 보이려고 outcome 은 일부러 good 으로 둔다.
func TestRenderInjectWarnsOnRegrettedAlone(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	hits := []Hit{{
		Score: 9,
		Note: store.Note{
			Path: filepath.Join(c.Vault, "alpha", "decisions", "alpha-결정-후회한선택-2026-08-05.md"),
			Stem: "alpha-결정-후회한선택-2026-08-05",
			Meta: store.Meta{
				Type: "decision", Date: "2026-08-05", Domain: []string{"alpha"},
				Summary: "후회한 선택", Status: "regretted", Outcome: "good",
			},
		},
	}}

	out := RenderInject(l, hits)
	if !strings.Contains(out, "(regretted/good)") {
		t.Fatalf("status/outcome 표기가 기대와 다르다:\n%s", out)
	}
	if !strings.Contains(out, warnLine) {
		t.Errorf("status: regretted 만으로는 경고가 붙지 않았다 (outcome 은 bad 가 아니다):\n%s", out)
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
	hits := mustRecall(t, l, c, "결정", Options{CrossProject: true, Limit: 10, MinScore: 1})
	if len(hits) != 0 {
		t.Errorf("불용어 '결정' 만으로 %d건 매칭 — stem 의 '-결정-' 표식이 새고 있다: %+v", len(hits), hits)
	}
}

// ★ 리뷰 대응 (수정 1): scoreAll 의 "head 히트가 없으면 점수 0" 조기 탈락
// (`if headHits == 0 { continue }`) 을 실제로 타는 데이터가 픽스처에 없었다
// — testdata/vault 의 노트들은 본문 키워드를 늘 summary/tags 에도 반복해서
// 이 경로를 건드리지 못했다(리뷰에서 확인: 이 블록을 통째로 지워도 전체
// 테스트가 통과했다). 픽스처 파일은 건드리지 않고, t.TempDir() 안에
// l.Write() 로 본문에만 있고 head(stem·summary·tags·domain)에는 없는
// 키워드를 가진 노트를 직접 만들어 그 경로를 덮는다.
func TestBodyOnlyKeywordExcluded(t *testing.T) {
	l, c := fixtureLayoutConfig(t)

	// stem/summary/tags/domain 어디에도 없고 다른 픽스처 노트에도 없는 조어.
	const bodyOnlyKeyword = "그림자백업진단로그"

	path, err := l.DecisionPath("alpha", "임시메모", "2026-08-05")
	if err != nil {
		t.Fatal(err)
	}
	n := store.Note{
		Path: path,
		Meta: store.Meta{
			Type:    "decision",
			Date:    "2026-08-05",
			Domain:  []string{"alpha"},
			Summary: "임시로 남기는 메모",
			Status:  "active",
			Outcome: "pending",
			Tags:    []string{"decision"},
		},
		Body: []byte("## 메모\n\n" + bodyOnlyKeyword + " 에 대한 상세 기록.\n"),
	}
	if err := l.Write(n); err != nil {
		t.Fatal(err)
	}

	hits := mustRecall(t, l, c, bodyOnlyKeyword, Options{CrossProject: true, Limit: 10, MinScore: 1})
	if len(hits) != 0 {
		t.Errorf("본문에만 있는 키워드로 검색했는데 %d건 매칭 — headHits==0 조기 탈락이 안 됐다: %+v", len(hits), hits)
	}
}

// TestRecallReportsVaultReadFailure 는 볼트를 읽지 못했을 때 Recall 이
// 침묵하지 않는지 본다. 예전에는 l.List() 에러를 nil 로 버려서, 결정 폴더가
// 읽기 불가여도 "관련 결정 없음" 과 똑같이 보였다 — 훅 주입 경로에서 에이전트가
// 과거 결정을 못 본 채 "없다" 로 읽는다. cb index 는 같은 List() 에서 죽는데
// cb recall 만 rc=0 으로 조용히 넘어가던 비대칭도 여기서 사라진다.
func TestRecallReportsVaultReadFailure(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root 는 디렉토리 퍼미션을 무시하므로 이 테스트가 성립하지 않는다")
	}
	l, c := fixtureLayoutConfig(t)

	// 픽스처의 alpha 결정 폴더에서 읽기 권한을 뺏는다.
	dir := filepath.Join(c.Vault, "alpha", "decisions")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	hits, err := Recall(l, c, "저장 엔진을 무엇으로 골랐지", Options{CrossProject: true, Limit: 3, MinScore: 1})
	if err == nil {
		t.Fatalf("읽을 수 없는 볼트인데 에러가 없다 (hits=%d) — 실패가 빈 결과로 뭉개졌다", len(hits))
	}
	if !strings.Contains(err.Error(), "결정 폴더를 읽을 수 없다") {
		t.Errorf("에러가 원인을 알려주지 않는다: %v", err)
	}
}

// ★ 리뷰 대응 (수정 2): 예전 TestTrimFillsLimitAfterMinScoreFilter 는
// trim() 의 "정렬 → MinScore 필터 → Limit 절단" 순서를 검증한다고 주장했지만
// 실제로는 아무것도 검증하지 못했다. 정렬 키(Score)와 필터 임계값(MinScore)이
// 같은 필드라, 내림차순 정렬된 배열에서 MinScore 이상인 항목은 항상 배열
// 앞쪽의 연속 구간(prefix)을 이룬다 — 그래서 "절단 후 필터"와 "필터 후
// 절단"은 수학적으로 항상 같은 최종 결과를 낸다. trim() 안의 필터·절단
// 순서를 실제로 바꿔서 돌려도 이 테스트는 여전히 통과했다(리뷰에서 확인).
// 즉 이 테스트는 순서가 바뀌어도 절대 실패할 수 없는 테스트였다.
//
// 그래서 "순서"를 주장하는 대신, trim() 이 실제로 보장하는 세 가지 계약을
// 각각 직접 검증한다: (1) MinScore 미만 항목이 제거되는지 (2) Limit 이
// 지켜지는지 (3) 동점(Score 같음) 정렬이 결정적인지(Path 내림차순).
func TestTrim(t *testing.T) {
	hits := []Hit{
		{Note: store.Note{Path: "a-low"}, Score: 2}, // MinScore 미만
		{Note: store.Note{Path: "b-hi"}, Score: 10},
		{Note: store.Note{Path: "c-hi"}, Score: 9},
		{Note: store.Note{Path: "d-hi"}, Score: 8},
		{Note: store.Note{Path: "z-tie"}, Score: 8}, // d-hi 와 동점, Path 로만 갈린다
		{Note: store.Note{Path: "e-low"}, Score: 1}, // MinScore 미만
	}
	out := trim(hits, Options{Limit: 3, MinScore: 5})

	if len(out) != 3 {
		t.Fatalf("Limit=3 인데 결과가 %d건: %+v", len(out), out)
	}
	for _, h := range out {
		if h.Score < 5 {
			t.Errorf("MinScore(5) 미만 항목이 결과에 섞였다: %+v", h)
		}
	}
	// 정렬 결과는 b-hi(10), c-hi(9), z-tie(8, Path>"d-hi"라 우선), d-hi(8) 순이다.
	// Limit=3 이 앞의 세 개만 남긴다 — 동점 정렬이 결정적이지 않으면 이 순서가 흔들린다.
	wantPaths := []string{"b-hi", "c-hi", "z-tie"}
	for i, want := range wantPaths {
		if out[i].Note.Path != want {
			t.Errorf("%d번째 결과가 %q 가 아니다(동점 정렬이 결정적이지 않다): %s", i, want, out[i].Note.Path)
		}
	}
}
