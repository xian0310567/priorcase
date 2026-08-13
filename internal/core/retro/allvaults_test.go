package retro_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/retro"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// note 는 회고 방아쇠에 걸릴 노트 한 쌍을 만든다.
//
// 방아쇠는 "재회수 2회 이상" 이므로, 뒤에 오는 노트들이 앞 노트를 꺼내야 한다.
// 같은 어휘를 나눠 갖게 해서 그 상태를 만든다.
func seedVault(t *testing.T, root, project string) {
	t.Helper()
	dir := filepath.Join(root, project, "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(date, slug, summary string) {
		body := "---\ntype: decision\ndate: " + date + "\ndomain: [" + project + "]\n" +
			"summary: \"" + summary + "\"\nstatus: active\noutcome: pending\n---\n\n## 결정\n\nx\n"
		name := project + "-결정-" + slug + "-" + date + ".md"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 앞 노트를 뒤 노트 둘이 꺼내게 한다 (재회수 2회).
	write("2026-08-01", "저장엔진", "저장 엔진을 임베디드 DB 로 고른다")
	write("2026-08-02", "저장엔진확장", "저장 엔진 스키마를 단일 테이블로 유지한다")
	write("2026-08-03", "저장엔진백업", "저장 엔진 백업을 로컬 git 으로 한다")
}

func twoVaultConfig(t *testing.T) (*config.Config, string, string) {
	t.Helper()
	a, b := t.TempDir(), t.TempDir()
	seedVault(t, a, "alpha")
	seedVault(t, b, "beta")
	return &config.Config{
		Vaults:        []config.Vault{{Name: "personal", Path: a}, {Name: "work", Path: b}},
		DefaultDomain: "alpha",
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-작업-로그.md",
			Index:        "_meta/00-결정-색인.md",
			Rollup:       "98-{project}-작업-로그-요약.md",
		},
		Domain: []config.Domain{
			{Prefix: "alpha", Folder: "alpha", Vault: "personal"},
			{Prefix: "beta", Folder: "beta", Vault: "work"},
		},
	}, a, b
}

// ★★★ **회고 큐는 볼트를 전부 덮어야 한다.**
//
// Due 는 Layout 하나 = 볼트 하나를 본다. CLI·훅은 cwd 로 볼트를 고르므로 그것이
// 맞지만 **메뉴바 앱에는 cwd 가 없다** — 실측으로 같은 설정에서 cwd 만 바꿔 부르니
// 회고 큐가 0건과 43건으로 갈렸다.
func TestAllDueCoversEveryVault(t *testing.T) {
	c, a, b := twoVaultConfig(t)

	// 볼트 하나만 보면 그 볼트 것만 나온다 — 이것이 고치려는 상태다.
	onlyA, _, err := retro.Due(store.NewLayoutFor(c, c.Vaults[0]), c)
	if err != nil {
		t.Fatal(err)
	}
	onlyB, _, err := retro.Due(store.NewLayoutFor(c, c.Vaults[1]), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(onlyA) == 0 || len(onlyB) == 0 {
		t.Fatalf("전제가 깨졌다 — 볼트별 회고가 %d/%d 건이다 (둘 다 0보다 커야 한다)",
			len(onlyA), len(onlyB))
	}

	all, _, errs := retro.AllDue(c)
	if len(errs) != 0 {
		t.Fatalf("errs=%v", errs)
	}
	if len(all) != len(onlyA)+len(onlyB) {
		t.Errorf("전체 %d건, 볼트별 합 %d건 — 한 볼트가 빠졌다",
			len(all), len(onlyA)+len(onlyB))
	}

	// **줄마다 볼트가 붙어야 한다.** 앱이 "이게 어디 결정이지" 를 답할 유일한 재료다.
	seen := map[string]int{}
	for _, it := range all {
		if it.Vault == "" {
			t.Errorf("%s 에 볼트가 없다", it.Stem)
		}
		seen[it.Vault]++
	}
	if seen["personal"] == 0 || seen["work"] == 0 {
		t.Errorf("한쪽 볼트만 나왔다: %v", seen)
	}
	_ = a
	_ = b
}

// ★★ **한 볼트가 깨져도 나머지를 주되, 몇 개를 못 봤는지 알려야 한다.**
//
// 조용히 짧은 목록을 주면 "회고할 것이 없다" 로 보인다.
func TestAllDueReportsBrokenVault(t *testing.T) {
	c, _, _ := twoVaultConfig(t)
	c.Vaults[1].Path = filepath.Join(t.TempDir(), "없는볼트")

	all, _, errs := retro.AllDue(c)
	if len(all) == 0 {
		t.Error("멀쩡한 볼트의 회고까지 잃었다")
	}
	if len(errs) == 0 {
		t.Error("볼트 하나가 깨졌는데 조용하다 — '회고할 것이 없다' 로 보인다")
	}
}

// ★ 날짜순으로 세워야 한다 — 볼트별로 뭉치면 "오래된 것부터" 를 못 본다.
func TestAllDueSortsByDate(t *testing.T) {
	c, _, _ := twoVaultConfig(t)
	all, _, _ := retro.AllDue(c)
	for i := 1; i < len(all); i++ {
		if all[i-1].Date > all[i].Date {
			t.Fatalf("날짜순이 아니다: %s(%s) 뒤에 %s(%s)",
				all[i-1].Stem, all[i-1].Date, all[i].Stem, all[i].Date)
		}
	}
}
