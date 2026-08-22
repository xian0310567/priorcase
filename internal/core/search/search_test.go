package search

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
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
			Path: filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-후회한선택-2026-08-05.md"),
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
// 과거 결정을 못 본 채 "없다" 로 읽는다. prior index 는 같은 List() 에서 죽는데
// prior recall 만 rc=0 으로 조용히 넘어가던 비대칭도 여기서 사라진다.
func TestRecallReportsVaultReadFailure(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root 는 디렉토리 퍼미션을 무시하므로 이 테스트가 성립하지 않는다")
	}
	l, c := fixtureLayoutConfig(t)

	// 픽스처의 alpha 결정 폴더에서 읽기 권한을 뺏는다.
	dir := filepath.Join(c.DefaultVaultPath(), "alpha", "decisions")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	hits, _, err := Recall(l, c, "저장 엔진을 무엇으로 골랐지", Options{CrossProject: true, Limit: 3, MinScore: 1})
	if err == nil {
		t.Fatalf("읽을 수 없는 볼트인데 에러가 없다 (hits=%d) — 실패가 빈 결과로 뭉개졌다", len(hits))
	}
	if !strings.Contains(err.Error(), "결정 폴더를 읽을 수 없다") {
		t.Errorf("에러가 원인을 알려주지 않는다: %v", err)
	}
}

// TestRecallReportsSkippedNotes 는 회수 대상에서 빠진 노트를 Recall 이
// 호출자에게 넘기는지 본다. 폴더 전체를 못 읽는 것(에러)과 노트 몇 건을 못 읽는
// 것(건너뜀)은 훅 주입 경로에서 같은 위험이다 — 어느 쪽이든 에이전트는 있었던
// 과거 결정을 못 본 채 "없다" 로 읽는다. 다만 후자로는 죽지 않고, 읽힌 것만
// 회수한 뒤 빠진 목록을 함께 준다.
func TestRecallReportsSkippedNotes(t *testing.T) {
	l, c := fixtureLayoutConfig(t)

	// 검색어에 걸릴 법한 이름을 가진 구 스키마 노트를 심는다.
	broken := filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-저장엔진구형-2026-01-02.md")
	body := "---\ntitle: 구 스키마\nproject: alpha\ncreated: 2026-01-02\n---\n\n## 결정\n\n저장 엔진.\n"
	if err := os.WriteFile(broken, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	hits, skipped, err := Recall(l, c, "저장 엔진을 무엇으로 골랐지",
		Options{CrossProject: true, Limit: 3, MinScore: 1})
	if err != nil {
		t.Fatalf("깨진 노트 한 건 때문에 Recall 이 죽으면 안 된다: %v", err)
	}
	if len(hits) == 0 {
		t.Error("정상 노트 회수까지 멈췄다")
	}
	if len(skipped) != 1 || skipped[0].Path != broken {
		t.Fatalf("건너뛴 노트가 보고되지 않았다: %+v", skipped)
	}
	if skipped[0].Reason == nil {
		t.Error("건너뛴 원인이 비었다")
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

// ★ 이 파일의 진짜 위험은 **순위 성질을 단언하는 테스트가 없다는 것**이었다.
// 가중치는 상수 다섯 개인데 그 상수들 사이의 관계를 못 박은 자리가 하나도 없어서,
// 숫자를 바꿔도 아무 시험이 안 깨진다.
//
// 못 박는 성질: **질의어를 하나 더 맞히는 것이 "그 폴더에 산다" 는 사실보다 세다.**
// 실측(2026-08-15)으로 이게 깨져 있었다 — weightCwdDomain(4) > weightHead(3) 이라
// 교차 프로젝트 질의 12개 중 8개에서 상위 3이 바뀌었고, nova 결정 3건이 통째로
// 탈락하고 무관한 priorcase 노트가 그 자리를 먹은 판이 있었다.
func TestKeywordHitOutweighsCwdDomain(t *testing.T) {
	if weightHead <= weightCwdDomain {
		t.Fatalf("weightHead(%d) 가 weightCwdDomain(%d) 이하다 — "+
			"어느 폴더에서 물었나가 무엇을 물었나를 이긴다", weightHead, weightCwdDomain)
	}
}

// 위 성질이 실제 회수에서 관측되는지 본다. 상수 비교만으로는 점수식이 그 상수를
// 어떻게 쓰는지 못 잡는다.
//
// 점수를 정확히 통제하려고 노트 두 건을 심는다. 공용 픽스처로는 격차를 1점밖에
// 못 만들어서 이 성질을 가르지 못한다.
//
//	cwd 도메인 노트 : head 히트 1개(케이크)        → 3 + (cwd 보너스)
//	타 도메인 노트  : head 히트 2개(케이크·딸기)    → 6
//
// 낱말은 조사 제거기(keywords.go 의 josa)에 안 걸리는 것으로 골랐다 — "파이" 는
// 끝의 "이" 가 조사로 잘려 "파" 가 되고, 두 글자 미만이라 질의에서 통째로 빠진다.
//
// 보너스가 3 이상이면 **덜 맞은 노트가 이긴다.** 그게 실볼트에서 관측된 것이다.
func TestCwdDomainDoesNotOutrankBetterMatch(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	plant(t, c, "alpha", "alpha-결정-보너스만-2026-08-10", "케이크를 고른다")
	plant(t, c, "beta", "beta-결정-내용우위-2026-08-10", "케이크와 딸기를 고른다")

	hits := mustRecall(t, l, c, "케이크 딸기",
		Options{Cwd: "/tmp/proj/alpha", CrossProject: true, Limit: 5, MinScore: 1})
	if len(hits) < 2 {
		t.Fatalf("비교할 만큼 안 걸렸다: %+v", hits)
	}
	if !strings.Contains(hits[0].Note.Stem, "내용우위") {
		var got []string
		for _, h := range hits {
			got = append(got, fmt.Sprintf("%s=%d", h.Note.Stem, h.Score))
		}
		t.Errorf("1위가 내용우위가 아니다 — cwd 보너스가 더 나은 매칭을 눌렀다: %v", got)
	}
}

// plant 는 점수를 통제한 노트를 심는다. 본문은 질의어를 담지 않는다 —
// bodyHits 가 섞이면 head 격차만 재려는 의도가 흐려진다.
func plant(t *testing.T, c *config.Config, dir, stem, summary string) {
	t.Helper()
	plantDated(t, c, dir, stem, "2026-08-10", summary)
}

func plantDated(t *testing.T, c *config.Config, dir, stem, date, summary string) {
	t.Helper()
	p := filepath.Join(c.DefaultVaultPath(), dir, "decisions", stem+".md")
	src := "---\ntype: decision\ndate: " + date + "\ndomain: [" + dir + "]\n" +
		"summary: \"" + summary + "\"\nstatus: active\noutcome: pending\n" +
		"supersedes: \"\"\nrelated: []\ntags: [decision]\nsource_session: \"\"\n---\n\n## 결정\n\n내용.\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 점수가 같으면 **최근 결정이 위**여야 한다.
//
// 예전 동점 처리는 경로 문자열 내림차순이었다(셸 `sort -rn` 동작 보존). 그건
// 사실상 무작위다 — 파일명이 `<도메인>-결정-<slug>-<날짜>` 라 slug 의 가나다순이
// 이기고 날짜는 맨 뒤에 있어 거의 영향을 못 준다. 점수식에 시간 항이 하나도 없는
// 시스템에서 **동점 처리가 시간을 볼 유일한 자리**인데 그걸 안 쓰고 있었다.
//
// 주입은 상위 3줄뿐이므로 동점 하나가 곧 탈락이다.
func TestTieBreakPrefersNewerDecision(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	// 같은 도메인·같은 summary → head 히트가 같아 점수가 동점이다.
	// 새 노트의 slug 가 가나다순으로 앞이라, 경로 내림차순이면 옛 노트가 이긴다.
	plantDated(t, c, "alpha", "alpha-결정-가나다-2026-08-20", "2026-08-20", "참외를 고른다")
	plantDated(t, c, "alpha", "alpha-결정-하하하-2026-08-01", "2026-08-01", "참외를 고른다")

	hits := mustRecall(t, l, c, "참외", Options{Limit: 5, MinScore: 1})
	if len(hits) < 2 {
		t.Fatalf("비교할 만큼 안 걸렸다: %+v", hits)
	}
	if hits[0].Score != hits[1].Score {
		t.Fatalf("동점이 아니라 이 테스트가 무의미하다: %d vs %d", hits[0].Score, hits[1].Score)
	}
	if !strings.Contains(hits[0].Note.Stem, "가나다") {
		t.Errorf("동점인데 옛 결정이 위다: 1위=%s(%s) 2위=%s(%s)",
			hits[0].Note.Stem, hits[0].Note.Meta.Date, hits[1].Note.Stem, hits[1].Note.Meta.Date)
	}
}

// ★ **철회된 노트는 회수에서 통째로 빠진다.**
//
// 이 시스템에는 "잘못 기록된 노트를 걷어낼 경로" 가 없었다. 실측으로
// `--status regretted` 를 걸어도 회수 점수가 1점도 안 깎이고, superseded 로
// 내려도 -5 뿐이라 MinScore:1 인 훅 주입에서 절대 안 빠졌다.
//
// **regretted 와 다른 것이다.** regretted 는 "했는데 나빴다" 라서 계속 떠야
// 한다 — 같은 실수를 되풀이하지 않으려면 눈앞에 있어야 하고, search 가
// 경고 줄을 붙이는 것이 그 설계다. retracted 는 "애초에 결정이 아니었다 ·
// 판별기가 잘못 만들었다" 라서 떠 있을 이유가 없다.
//
// 파일은 지우지 않는다 — "사용자가 볼트에 둔 것을 지우지 않는다" 는 규칙이
// 여기에도 적용된다. 회수에서만 뺀다.
func TestRetractedNoteIsExcludedFromRecall(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	plant(t, c, "alpha", "alpha-결정-잘못기록-2026-08-10", "참외를 고른다")
	if hits := mustRecall(t, l, c, "참외", Options{Limit: 5, MinScore: 1}); len(hits) == 0 {
		t.Fatal("심은 노트가 애초에 안 걸린다 — 이 테스트가 무의미하다")
	}

	retract(t, c, "alpha", "alpha-결정-잘못기록-2026-08-10")

	if hits := mustRecall(t, l, c, "참외", Options{Limit: 5, MinScore: 1}); len(hits) != 0 {
		t.Errorf("철회했는데 여전히 걸린다: %+v", hits[0].Note.Stem)
	}
}

// regretted 는 반대다 — 계속 떠야 한다. 둘을 같은 것으로 다루면 안 된다.
func TestRegrettedNoteStillSurfaces(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	plant(t, c, "alpha", "alpha-결정-후회-2026-08-10", "참외를 고른다")
	setStatus(t, c, "alpha", "alpha-결정-후회-2026-08-10", "regretted")

	if hits := mustRecall(t, l, c, "참외", Options{Limit: 5, MinScore: 1}); len(hits) == 0 {
		t.Error("regretted 가 회수에서 빠졌다 — 같은 실수를 되풀이하지 말라는 장치가 죽는다")
	}
}

func retract(t *testing.T, c *config.Config, dir, stem string) {
	t.Helper()
	setStatus(t, c, dir, stem, "retracted")
}

func setStatus(t *testing.T, c *config.Config, dir, stem, status string) {
	t.Helper()
	p := filepath.Join(c.DefaultVaultPath(), dir, "decisions", stem+".md")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Replace(string(b), "status: active", "status: "+status, 1)
	if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ★★ **뒤집힌 결정이 회수에서 사실상 사라지던 자리.**
//
// 감점(penaltySuperseded=5)이 head 히트 하나(weightHead=3)보다 커서, 히트가 하나뿐인
// 질의에서는 점수가 음수가 되고 `score > 0` 이 노트를 통째로 버렸다. 즉 뒤집힌
// 결정은 head 히트가 **둘은 있어야** 회수에 떴다.
//
// 그게 왜 치명적인가 — capture 가 번복 이유를 summary 꼬리표로 붙이는 이유가
// "head 밖에 두면 검색이 안 된다" 인데(markOverturned 주석), 정작 그 이유에만
// 나오는 낱말은 대개 한 개짜리 질의로 들어온다. 실볼트 측정에서 "osxkeychain",
// "403" 같은 질의가 전부 0건이었다 — 이유를 head 에 올려 놓고도 못 찾은 것이다.
//
// 픽스처의 alpha-결정-스키마 는 superseded 이고 "테이블" 은 그 노트에만 있다.
// head 1건 + body 1건 = 4점, 감점 후 -1 — 바닥이 없으면 결과가 통째로 빈다.
func TestSupersededSurvivesSingleHeadHit(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	hits := mustRecall(t, l, c, "테이블", Options{CrossProject: true, Limit: 3, MinScore: 1})
	if len(hits) != 1 {
		t.Fatalf("뒤집힌 결정이 히트 하나짜리 질의에서 사라졌다 (%d건): %+v", len(hits), hits)
	}
	if hits[0].Note.Meta.Status != "superseded" {
		t.Fatalf("엉뚱한 노트가 걸렸다: %s (%s)", hits[0].Note.Stem, hits[0].Note.Meta.Status)
	}
	if hits[0].Score != supersededFloor {
		t.Errorf("바닥값이 아니다: %d, want %d — 감점을 건너뛰었으면 순위가 오른 것이다",
			hits[0].Score, supersededFloor)
	}
}

// TestSupersededFloorNeverOutranksActive 는 **바닥이 순위를 건드리지 않는지** 본다.
//
// 이게 이 안(바닥)을 감점 완화(5→2) 대신 고른 이유다. 실볼트 49질의 측정에서
// 감점을 2로 낮추면 뒤집힌 노트가 살아 있는 결정을 제치고 1위가 되는 질의가 생겼고
// (역전 10→12건), 슬롯 3개짜리 회수에서 active 노트 하나를 밀어냈다. 바닥은 순위를
// 그대로 두고(역전 10건 유지·밀려남 0건) 빈 슬롯만 채운다.
func TestSupersededFloorNeverOutranksActive(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	hits := mustRecall(t, l, c, "테이블 저장 엔진", Options{CrossProject: true, Limit: 3, MinScore: 1})
	if len(hits) < 2 {
		t.Fatalf("비교할 결과가 부족하다: %+v", hits)
	}
	last := hits[len(hits)-1]
	if last.Note.Meta.Status != "superseded" {
		t.Fatalf("뒤집힌 노트가 맨 끝이 아니다: %+v", hits)
	}
	for _, h := range hits[:len(hits)-1] {
		if h.Note.Meta.Status == "superseded" {
			continue
		}
		if h.Score <= last.Score {
			t.Errorf("살아 있는 결정(%s, %d)이 뒤집힌 것(%d)보다 높지 않다",
				h.Note.Stem, h.Score, last.Score)
		}
	}
}

// TestSupersededFloorStaysBelowAnyRealHit 은 "언제나 맨 끝" 을 상수 수준에서 못 박는다.
//
// 감점 없는 노트의 최소 점수는 weightHead 다 (headHits==0 이면 위에서 이미 탈락한다).
// 바닥이 그보다 크거나 같아지는 순간 뒤집힌 결정이 살아 있는 결정을 밀어내기 시작하고,
// 그건 이 변경이 하지 않기로 한 바로 그 일이다. 실볼트 측정의 "밀려남 0건" 은 이
// 부등식이 성립할 때만 참이다.
func TestSupersededFloorStaysBelowAnyRealHit(t *testing.T) {
	if supersededFloor >= weightHead {
		t.Fatalf("supersededFloor(%d) 가 weightHead(%d) 이상이다 — 뒤집힌 결정이 슬롯을 빼앗는다",
			supersededFloor, weightHead)
	}
}

// ★★ **딱지만으로는 부족하다.**
//
// 예전 주입은 `(superseded/bad)` 만 찍었다. 그건 "쓰지 마라" 는 말이지 "무엇을
// 대신 하라" 는 말이 아니라서, 읽는 에이전트는 왜 버렸는지를 처음부터 다시 판다 —
// 번복 이유를 기록하기로 한 목적이 정확히 그걸 막는 것이었다.
func TestRenderInjectShowsOverturnReason(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	const reason = "osxkeychain 이 helper 목록에 누적돼 push 가 403 이 됐다"
	hits := []Hit{{
		Score: supersededFloor,
		Note: store.Note{
			Path: filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-저장엔진-2026-08-01.md"),
			Stem: "alpha-결정-저장엔진-2026-08-01",
			Meta: store.Meta{
				Type: "decision", Date: "2026-08-01", Domain: []string{"alpha"},
				Summary: "저장 엔진을 임베디드 DB 로 고른다",
				Status:  "superseded", Outcome: "pending", SupersededReason: reason,
			},
		},
	}}

	out := RenderInject(l, hits)
	if !strings.Contains(out, "(superseded/pending)") {
		t.Fatalf("status/outcome 표기가 사라졌다:\n%s", out)
	}
	if !strings.Contains(out, reason) {
		t.Errorf("번복 이유가 안 실렸다 — 딱지만 보고는 무엇을 대신할지 알 수 없다:\n%s", out)
	}
}

// TestRenderInjectSkipsReasonAlreadyInSummary 는 같은 문장이 두 번 나가지 않는지 본다.
//
// markOverturned 는 이유를 frontmatter 와 **summary 꼬리표** 양쪽에 쓴다(회수 head 에
// 실으려면 summary 밖에 자리가 없다). 그래서 아무 조건 없이 한 줄 더 찍으면 주입
// 블록에 같은 이유가 두 번 나간다. 회수는 매 프롬프트마다 실려 나가는 예산이라
// 중복 한 줄이 그냥 낭비가 아니다.
//
// 꼬리표는 80자로 잘려 있으므로(summaryReasonRunes) 전문 대조로는 못 잡는다 —
// 앞머리로 본다. 여기서는 잘림까지 재현해서 그 경로를 실제로 태운다.
func TestRenderInjectSkipsReasonAlreadyInSummary(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	reason := "helper 목록에 osxkeychain 이 누적됐다 " + strings.Repeat("가", 200)
	clipped := string([]rune(reason)[:80]) + "…"

	hits := []Hit{{
		Score: supersededFloor,
		Note: store.Note{
			Path: filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-저장엔진-2026-08-01.md"),
			Stem: "alpha-결정-저장엔진-2026-08-01",
			Meta: store.Meta{
				Type: "decision", Date: "2026-08-01", Domain: []string{"alpha"},
				Summary:          "저장 엔진을 임베디드 DB 로 고른다 — 번복: " + clipped,
				Status:           "superseded",
				Outcome:          "pending",
				SupersededReason: reason,
			},
		},
	}}

	out := RenderInject(l, hits)
	if strings.Contains(out, "번복 이유:") {
		t.Errorf("summary 가 이미 이유를 담고 있는데 한 줄 더 찍었다:\n%s", out)
	}
	if strings.Count(out, "osxkeychain") != 1 {
		t.Errorf("이유가 %d번 나갔다 — 한 번이어야 한다:\n%s", strings.Count(out, "osxkeychain"), out)
	}
}

// TestRenderInjectReasonSurvivesSelfOverturn 은 **status 가 아니라 키의 유무로**
// 판단하는지 본다.
//
// 대체할 새 결정 없는 번복(capture/review.go 의 자기 번복)은 status 를 regretted 로
// 두는 것이 정상이고, 측정으로 가정이 깨져 "그냥 그만둔다" 로 끝나는 그쪽이 실제로
// 더 흔하다. `status == "superseded"` 로 좁히면 그 경우가 통째로 빠진다.
func TestRenderInjectReasonSurvivesSelfOverturn(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	const reason = "임베디드 DB 로는 다중 프로세스 쓰기가 안 된다는 것이 실측으로 드러났다"
	hits := []Hit{{
		Score: 9,
		Note: store.Note{
			Path: filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-저장엔진-2026-08-01.md"),
			Stem: "alpha-결정-저장엔진-2026-08-01",
			Meta: store.Meta{
				Type: "decision", Date: "2026-08-01", Domain: []string{"alpha"},
				Summary: "저장 엔진을 임베디드 DB 로 고른다",
				Status:  "regretted", Outcome: "bad", SupersededReason: reason,
			},
		},
	}}
	if out := RenderInject(l, hits); !strings.Contains(out, reason) {
		t.Errorf("자기 번복(status=regretted)의 이유가 빠졌다:\n%s", out)
	}
}

// TestRenderInjectQuietWithoutReason 은 이유가 없으면 줄을 안 만드는지 본다.
// 실볼트 18노트 전부가 이 상태다 — 여기서 빈 줄이 붙으면 모든 주입이 부풀어 오른다.
func TestRenderInjectQuietWithoutReason(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	hits := []Hit{{
		Score: 4,
		Note: store.Note{
			Path: filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-스키마-2026-08-02.md"),
			Stem: "alpha-결정-스키마-2026-08-02",
			Meta: store.Meta{
				Type: "decision", Date: "2026-08-02", Domain: []string{"alpha"},
				Summary: "스키마를 단일 테이블로 유지한다", Status: "superseded", Outcome: "pending",
			},
		},
	}}
	out := RenderInject(l, hits)
	if n := strings.Count(out, "\n"); n != 2 { // 헤더 + 노트 한 줄
		t.Errorf("이유가 없는데 줄이 늘었다 (%d줄):\n%s", n, out)
	}
}
