package search

import (
	"os"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// ★ **코퍼스를 넘겨도 결과가 같아야 한다.**
//
// Corpus 는 순수한 성능 장치다 — 볼트를 한 번만 읽고 head·body 소문자판을 재사용할
// 뿐, 무엇이 걸리는지는 바뀌면 안 된다. 여기가 갈리면 `retro.Due` 의 회고 큐가
// 조용히 다른 것을 내고, 그건 사람이 알아챌 방법이 없다.
func TestCorpusGivesSameResults(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	corpus, err := LoadCorpus(l)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"저장 엔진을 무엇으로 골랐지", "스키마 단일 저장 엔진",
		"배포 정적 바이너리", "로케일 바이트", "전혀 무관한 짜장면",
	} {
		o := Options{CrossProject: true, Limit: 10, MinScore: 1,
			IncludeReferences: true, ReferenceLimit: 3, RuleLimit: 2}
		want := mustRecall(t, l, c, q, o)

		o.Corpus = corpus
		got, _, err := Recall(l, c, q, o)
		if err != nil {
			t.Fatalf("Recall(%q, corpus): %v", q, err)
		}
		if len(got) != len(want) {
			t.Fatalf("%q: 코퍼스로 %d건, 없이 %d건", q, len(got), len(want))
		}
		for i := range want {
			if got[i].Note.Stem != want[i].Note.Stem || got[i].Score != want[i].Score {
				t.Errorf("%q [%d]: 코퍼스 %s(%d) ≠ 직접 %s(%d)",
					q, i, got[i].Note.Stem, got[i].Score, want[i].Note.Stem, want[i].Score)
			}
		}
	}
}

// ★ **회고 큐는 노트 수의 제곱으로 자랐다.**
//
// `retro.Due` 가 노트마다 Recall 을 부르는데 Recall 이 매번 볼트를 다시 읽고,
// 동의어 형제를 노트마다 다시 풀고, 버릴 노트의 본문까지 훑었다. 실측:
//
//	결정 156건 → prior queue  2.6초  (2026-08-14)
//	결정 558건 → prior queue 31.8초  (2026-08-31, 고치기 전)
//	결정 558건 → prior queue  6.3초  (고친 뒤, 출력은 바이트 단위로 동일)
//
// 앱의 읽기 상한이 10초라 그 순간 화면이 통째로 "prior 가 너무 오래 걸린다" 로 찼다.
// **기능이 늘어서가 아니라 볼트가 자라서** 죽는 종류라 아무 예고가 없다.
//
// 이 테스트는 절대 시간을 재지 않는다(기계마다 다르다). **같은 볼트를 두 번 훑는
// 것보다 코퍼스 재사용이 빨라야 한다**는 관계만 잠근다.
func TestCorpusIsFasterThanRereading(t *testing.T) {
	if os.Getenv("PRIORCASE_TEST_VAULT") == "" && testing.Short() {
		t.Skip("짧은 모드에서는 건너뛴다")
	}
	l, c := fixtureLayoutConfig(t)
	corpus, err := LoadCorpus(l)
	if err != nil {
		t.Fatal(err)
	}
	const n = 30
	o := Options{CrossProject: true, Limit: 5, MinScore: 1}

	start := time.Now()
	for i := 0; i < n; i++ {
		if _, _, err := Recall(l, c, "저장 엔진 스키마 배포 로케일", o); err != nil {
			t.Fatal(err)
		}
	}
	direct := time.Since(start)

	o.Corpus = corpus
	start = time.Now()
	for i := 0; i < n; i++ {
		if _, _, err := Recall(l, c, "저장 엔진 스키마 배포 로케일", o); err != nil {
			t.Fatal(err)
		}
	}
	cached := time.Since(start)

	t.Logf("%d회: 직접 %v · 코퍼스 %v", n, direct.Round(time.Microsecond), cached.Round(time.Microsecond))
	if cached >= direct {
		t.Errorf("코퍼스가 더 느리다 (%v ≥ %v) — 볼트를 다시 읽고 있다", cached, direct)
	}
}

// 코퍼스는 참고·규칙도 담는다. 안 담으면 훅 경로가 그것들을 잃는다.
func TestCorpusCarriesReferencesAndRules(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	corpus, err := LoadCorpus(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus.Notes) == 0 {
		t.Fatal("결정이 하나도 안 읽혔다")
	}
	if len(corpus.pNotes) != len(corpus.Notes) {
		t.Errorf("접어 둔 판이 %d건, 원본이 %d건", len(corpus.pNotes), len(corpus.Notes))
	}
	// 접어 둔 head 가 실제 점수 계산이 보는 것과 같아야 한다.
	for i, p := range corpus.pNotes {
		if want := headText(corpus.Notes[i], corpus.marker); p.head != want {
			t.Fatalf("%s: 접어 둔 head 가 다르다", corpus.Notes[i].Stem)
		}
	}
	_ = store.Note{}
}
