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
		Options{Limit: 5, MinScore: 1, CrossProject: true, IncludeReferences: true})
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
