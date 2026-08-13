package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// ★★★ **참고를 켜야만 들어온다.** 기본은 지금 그대로다 — 켜는 것은 호출부의
// 선택이고, capture 의 중복 대조처럼 결정만 봐야 하는 자리가 있다.
func TestRecallIncludesReferencesOnlyWhenAsked(t *testing.T) {
	c, l := refSearchFixture(t)

	off, _, err := Recall(l, c, "캐릭터 컨셉 페르소나", Options{Limit: 5, MinScore: 1, CrossProject: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range off {
		if h.Note.IsReference() {
			t.Errorf("안 켰는데 참고가 들어왔다: %s", h.Note.Stem)
		}
	}

	on, _, err := Recall(l, c, "캐릭터 컨셉 페르소나",
		Options{Limit: 5, ReferenceLimit: 3, MinScore: 1, CrossProject: true, IncludeReferences: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range on {
		if h.Note.IsReference() {
			found = true
		}
	}
	if !found {
		t.Errorf("켰는데 참고가 안 들어왔다 (히트 %d건)", len(on))
	}
}

// ★★★ **참고를 결정처럼 그리면 안 된다.**
//
// 참고에는 status·outcome 이 없다. 지금 형식은 빈 값을 active/pending 으로
// 채우므로, 그대로 내면 **기획 초안이 확정된 결정으로 보인다.** 에이전트는 그걸
// 근거로 삼는다.
func TestRenderInjectMarksReferences(t *testing.T) {
	_, l := refSearchFixture(t)
	notes, _, err := l.ListReferences()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) == 0 {
		t.Fatal("픽스처에 참고가 없다")
	}
	s := RenderInject(l, []Hit{{Note: notes[0], Score: 9}})
	if !strings.Contains(s, "참고") {
		t.Errorf("참고 표시가 없다:\n%s", s)
	}
	if strings.Contains(s, "active/pending") {
		t.Errorf("참고에 결정 상태를 붙였다 — 확정된 결정으로 읽힌다:\n%s", s)
	}
}

func refSearchFixture(t *testing.T) (*config.Config, *store.Layout) {
	t.Helper()
	vault := t.TempDir()
	c := &config.Config{
		Vaults:        []config.Vault{{Name: config.DefaultVaultName, Path: vault}},
		DefaultDomain: "proj",
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-작업-로그.md",
			Index:        "_meta/00-결정-색인.md",
		},
		Domain: []config.Domain{{Prefix: "proj", Folder: "proj"}},
	}
	w := func(p, body string) {
		full := filepath.Join(vault, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("proj/decisions/proj-결정-저장엔진-2026-08-01.md",
		"---\ntype: decision\ndate: 2026-08-01\ndomain: [proj]\nsummary: \"저장 엔진을 고른다\"\nstatus: active\noutcome: pending\n---\n\n## 결정\n")
	w("proj/01-캐릭터컨셉-설계.md",
		"---\ntype: spec\nsummary: \"캐릭터 컨셉 설계 — 페르소나 3안\"\n---\n\n# 캐릭터 컨셉\n페르소나를 셋 둔다\n")
	return c, store.NewLayout(c)
}

// ★★★ **참고가 결정을 밀어내면 안 된다.**
//
// 처음엔 한 목록에 섞어 상위 N 을 잘랐다. H1 제목을 head 로 가정하고 측정했을
// 때는 밀려남이 2%(51건 중 1건)라 괜찮아 보였는데, **실제 요약을 달고 다시 재니
// 15%(53건 중 8건)였다** — 요약이 제목보다 키워드가 촘촘해 훨씬 센 경쟁자다.
//
// 그중에는 잃으면 안 되는 것이 있었다: "승격 준비해줘" 질의에서
// `승격전-적대감사를-기본절차로` 결정이 밀렸다. 이 시스템이 존재하는 이유가
// 바로 그런 결정을 그 순간에 들이미는 것이다.
//
// 그래서 **자리를 나눈다.** 결정은 결정끼리 Limit 만큼, 참고는 참고끼리
// ReferenceLimit 만큼. 섞어서 자르지 않는다.
func TestReferencesNeverDisplaceDecisions(t *testing.T) {
	c, l := refDisplaceFixture(t)

	hits, _, err := Recall(l, c, "저장 엔진", Options{
		Limit: 2, ReferenceLimit: 2, MinScore: 1, CrossProject: true, IncludeReferences: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var dec, ref int
	for _, h := range hits {
		if h.Note.IsReference() {
			ref++
		} else {
			dec++
		}
	}
	if dec != 2 {
		t.Errorf("결정 %d건 — 2건이어야 한다 (참고가 밀어냈다). 결과: %v", dec, stemsOf(hits))
	}
	if ref != 2 {
		t.Errorf("참고 %d건 — 2건이어야 한다. 결과: %v", ref, stemsOf(hits))
	}
	// 결정이 먼저 나온다 — 사람과 에이전트가 위에서부터 읽는다.
	if len(hits) > 0 && hits[0].Note.IsReference() {
		t.Errorf("참고가 맨 위다: %v", stemsOf(hits))
	}
}

// ★★ ReferenceLimit 이 0 이면 참고를 안 준다 — 켰다고 무조건 붙지 않는다.
func TestReferenceLimitZeroMeansNone(t *testing.T) {
	c, l := refDisplaceFixture(t)
	hits, _, err := Recall(l, c, "저장 엔진", Options{
		Limit: 3, MinScore: 1, CrossProject: true, IncludeReferences: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Note.IsReference() {
			t.Errorf("ReferenceLimit 0 인데 참고가 왔다: %s", h.Note.Stem)
		}
	}
}

func stemsOf(hs []Hit) []string {
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		mark := ""
		if h.Note.IsReference() {
			mark = "[참고]"
		}
		out = append(out, mark+h.Note.Stem)
	}
	return out
}

// refDisplaceFixture 는 **참고가 결정보다 높은 점수를 받도록** 만든다.
// 그래야 섞어 자르는 구현에서 실제로 밀려난다.
func refDisplaceFixture(t *testing.T) (*config.Config, *store.Layout) {
	t.Helper()
	vault := t.TempDir()
	c := &config.Config{
		Vaults:        []config.Vault{{Name: config.DefaultVaultName, Path: vault}},
		DefaultDomain: "proj",
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-작업-로그.md",
			Index:        "_meta/00-결정-색인.md",
		},
		Domain: []config.Domain{{Prefix: "proj", Folder: "proj"}},
	}
	w := func(p, body string) {
		full := filepath.Join(vault, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 결정 둘 — 요약이 짧다 (실제 결정 노트의 모양)
	w("proj/decisions/proj-결정-저장-2026-08-01.md",
		"---\ntype: decision\ndate: 2026-08-01\ndomain: [proj]\nsummary: \"저장 방식을 고른다\"\nstatus: active\noutcome: pending\n---\n\n## 결정\n저장 엔진\n")
	w("proj/decisions/proj-결정-엔진-2026-08-02.md",
		"---\ntype: decision\ndate: 2026-08-02\ndomain: [proj]\nsummary: \"엔진을 바꾼다\"\nstatus: active\noutcome: pending\n---\n\n## 결정\n저장 엔진\n")
	// 참고 셋 — 요약이 키워드로 촘촘하다 (방금 단 요약의 모양)
	for i, n := range []string{"01", "02", "03"} {
		_ = i
		w("proj/"+n+"-조사.md",
			"---\ntype: spec\nsummary: \"저장 엔진 조사 — 저장 엔진 비교와 저장 엔진 선택 기준\"\n---\n\n# 조사\n저장 엔진\n")
	}
	return c, store.NewLayout(c)
}
