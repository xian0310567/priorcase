package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ★ **같은 저장소가 URL 형태로 갈리면 안 된다.**
//
// 사람마다 https 와 ssh 가 다르고, 같은 사람도 기계마다 다르다. 그건 도메인을
// 가를 이유가 아니다 — 갈리면 한 저장소의 결정이 두 폴더로 흩어지고, 회수는
// 그중 한쪽만 본다.
func TestNormalizeRemote(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"https://github.com/o/r.git", "o/r"},
		{"https://github.com/o/r", "o/r"},
		{"git@github.com:o/r.git", "o/r"},
		{"git@github.com:o/r", "o/r"},
		{"ssh://git@github.com/o/r.git", "o/r"},
		{"ssh://git@github.com/o/r", "o/r"},
		{"https://gitlab.com/grp/sub/proj.git", "grp/sub/proj"}, // 하위 그룹을 살린다
		{"https://github.com/o/r/", "o/r"},
		{"  https://github.com/o/r.git  ", "o/r"},

		// 대소문자를 맞춘다 — GitHub 은 구별하지 않는데 사람은 섞어 적는다.
		{"https://github.com/Xian0310567/PriorCase.git", "xian0310567/priorcase"},
		{"git@github.com:Xian0310567/PriorCase.git", "xian0310567/priorcase"},

		// owner 가 없으면 저장소 이름으로 볼 수 없다.
		{"https://example.com/onlyrepo", ""},
		{"", ""},
		{"   ", ""},

		// 설정에 사람이 이미 owner/repo 로 적은 경우도 그대로 통과해야 한다.
		{"o/r", "o/r"},
		{"O/R", "o/r"},
	} {
		if got := NormalizeRemote(c.in); got != c.want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// gitRepo 는 origin 이 걸린 가짜 저장소를 만든다.
func gitRepo(t *testing.T, url string) string {
	t.Helper()
	root := t.TempDir()
	g := filepath.Join(root, ".git")
	if err := os.MkdirAll(g, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[core]\n\trepositoryformatversion = 0\n" +
		"[remote \"origin\"]\n\turl = " + url + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	if err := os.WriteFile(filepath.Join(g, "config"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRepoForFindsOriginFromSubdirectory(t *testing.T) {
	root := gitRepo(t, "git@github.com:o/r.git")
	deep := filepath.Join(root, "internal", "core", "config")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, deep} {
		if got := RepoFor(dir); got != "o/r" {
			t.Errorf("RepoFor(%q) = %q, want o/r", dir, got)
		}
	}
}

// git 저장소가 아니면 조용히 빈 문자열이다. 에러가 아니다 — 저장소 밖에서
// 일하는 것은 정상이고, 그때는 경로 판정과 폴백이 받는다.
func TestRepoForOutsideRepoIsEmpty(t *testing.T) {
	if got := RepoFor(t.TempDir()); got != "" {
		t.Errorf("RepoFor = %q, 빈 문자열이어야 한다", got)
	}
}

// ★ **`.git` 이 파일인 경우를 다뤄야 한다.**
//
// worktree 와 submodule 이 그렇다. 안 다루면 그 안에서 도메인이 통째로 안 잡히고,
// **그게 조용하다** — 기록이 폴백 도메인으로 새거나 아예 막힌다.
func TestRepoForHandlesWorktree(t *testing.T) {
	main := gitRepo(t, "https://github.com/o/r.git")
	mainGit := filepath.Join(main, ".git")

	// worktree 의 gitdir: <main>/.git/worktrees/<name>, 그 안의 commondir 이 본체를 가리킨다.
	wtGit := filepath.Join(mainGit, "worktrees", "feature")
	if err := os.MkdirAll(wtGit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtGit, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	wt := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+wtGit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := RepoFor(wt); got != "o/r" {
		t.Errorf("worktree 에서 RepoFor = %q, want o/r", got)
	}
}

// origin 이 없는 저장소(로컬 전용)는 빈 문자열이다. 다른 remote 를 origin 인 척
// 쓰면 안 된다 — upstream 을 origin 으로 읽으면 포크가 원본 도메인으로 샌다.
func TestRepoForIgnoresNonOriginRemotes(t *testing.T) {
	root := t.TempDir()
	g := filepath.Join(root, ".git")
	if err := os.MkdirAll(g, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[remote \"upstream\"]\n\turl = git@github.com:someone/else.git\n"
	if err := os.WriteFile(filepath.Join(g, "config"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := RepoFor(root); got != "" {
		t.Errorf("RepoFor = %q, origin 이 없으면 빈 문자열이어야 한다", got)
	}
}

// domainCfg 는 경로와 저장소를 둘 다 가진 설정을 만든다.
func domainCfg() *Config {
	return &Config{
		DefaultDomain: "common",
		Exclude:       []string{"/secret"},
		Domain: []Domain{
			{Prefix: "alpha", Folder: "alpha", Paths: []string{"/home/me/project/alpha"},
				Repos: []string{"org/alpha"}},
			{Prefix: "mono-a", Folder: "mono-a", Paths: []string{"/home/me/mono/apps/a"}},
			{Prefix: "mono-b", Folder: "mono-b", Paths: []string{"/home/me/mono/apps/b"},
				Repos: []string{"org/mono"}},
			{Prefix: "beta", Folder: "beta", Repos: []string{"https://github.com/Org/Beta.git"}},
		},
	}
}

// ★★ **새 팀원이 설정을 손대지 않고도 도메인이 잡혀야 한다.**
//
// 이게 이 기능의 존재 이유다. 설정의 paths 는 절대 경로라 사람마다 다르고,
// 안 맞으면 기록이 폴백 도메인으로 새거나 아예 막힌다 — 그리고 **조용하다.**
// `owner/repo` 는 누구 기계에서든 같다.
func TestDomainForRepoLetsTeammatesSkipConfigEditing(t *testing.T) {
	c := domainCfg()
	// 팀원은 alpha 를 전혀 다른 자리에 체크아웃했다. 경로는 하나도 안 맞는다.
	for _, r := range []string{
		"org/alpha",
		"git@github.com:org/alpha.git",     // ssh 로 클론했어도
		"https://github.com/ORG/Alpha.git", // 대소문자가 달라도
	} {
		if got := c.DomainForRepo(r); got != "alpha" {
			t.Errorf("DomainForRepo(%q) = %q, want alpha", r, got)
		}
	}
	// 설정에 전체 URL 로 적어 둔 도메인도 걸려야 한다.
	if got := c.DomainForRepo("org/beta"); got != "beta" {
		t.Errorf("설정에 URL 로 적힌 도메인이 안 걸린다: %q", got)
	}
	if got := c.DomainForRepo("org/없는것"); got != "" {
		t.Errorf("모르는 저장소에 %q 를 줬다", got)
	}
}

// ★ **경로가 저장소보다 앞선다.** 이유가 둘이다.
//
// 하나, 이미 경로로 설정한 사람의 동작이 그대로 유지된다.
// 둘, **모노레포는 경로만이 하위 프로젝트를 가른다.** 저장소를 먼저 보면
// `apps/a` 와 `apps/b` 가 한 도메인으로 뭉개진다.
func TestPathBeatsRepoSoMonoreposStaySplit(t *testing.T) {
	c := domainCfg()
	root := gitRepo(t, "git@github.com:org/mono.git")
	a := filepath.Join(root, "apps", "a")
	if err := os.MkdirAll(a, 0o755); err != nil {
		t.Fatal(err)
	}
	// 이 저장소는 mono-b 로 등록돼 있지만, 경로가 mono-a 를 가리키면 mono-a 다.
	c.Domain[1].Paths = []string{a}
	if got := c.DomainForCwd(a); got != "mono-a" {
		t.Errorf("DomainForCwd = %q, 경로가 이겨서 mono-a 여야 한다 — "+
			"저장소가 먼저면 모노레포의 하위 프로젝트 구분이 사라진다", got)
	}
	// 경로가 안 맞는 자리에서는 저장소가 받는다.
	other := filepath.Join(root, "apps", "c")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := c.DomainForCwd(other); got != "mono-b" {
		t.Errorf("DomainForCwd = %q, 경로가 안 맞으면 저장소로 mono-b 여야 한다", got)
	}
}

// ★ **제외는 저장소보다도 앞선다.** NOI 처럼 손대면 안 되는 구역이 있고,
// 그 구역이 git 저장소라는 이유로 기록이 뚫리면 안 된다.
func TestExcludeStillWinsOverRepo(t *testing.T) {
	c := domainCfg()
	root := gitRepo(t, "git@github.com:org/alpha.git")
	c.Exclude = []string{root}
	if got := c.DomainForCwd(root); got != "" {
		t.Errorf("DomainForCwd = %q, 제외 구역은 빈 문자열이어야 한다 — "+
			"저장소 판정이 제외를 뚫으면 안 된다", got)
	}
}

// 저장소도 경로도 안 맞으면 폴백이다. 여기가 무너지면 새 사용자가 아무것도 기록하지 못한다.
func TestUnknownRepoFallsBackToDefault(t *testing.T) {
	c := domainCfg()
	root := gitRepo(t, "git@github.com:someone/unrelated.git")
	if got := c.DomainForCwd(root); got != "common" {
		t.Errorf("DomainForCwd = %q, 폴백 common 이어야 한다", got)
	}
}
