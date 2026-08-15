package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/index"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// find 는 이름으로 검사 하나를 꺼낸다.
func find(t *testing.T, r *Report, name string) Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("검사 %q 가 없다. 있는 것: %v", name, names(r))
	return Check{}
}

func names(r *Report) []string {
	var out []string
	for _, c := range r.Checks {
		out = append(out, c.Name)
	}
	return out
}

// 픽스처 볼트는 건강해야 한다. 이게 안 되면 나머지 테스트의 기준선이 없다.
func TestHealthyVault(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	if _, err := index.Write(l); err != nil {
		t.Fatal(err)
	}
	r := Vault(c, l)
	for _, ck := range r.Checks {
		if ck.Level != OK {
			t.Errorf("[%s] %s — %s", ck.Level.Mark(), ck.Name, ck.Detail)
		}
	}
	if r.Worst() != OK {
		t.Errorf("Worst = %v, OK 여야 한다", r.Worst())
	}
}

// ★ 이 패키지의 핵심 검사.
//
// 볼트에 결정 폴더가 있는데 설정에 없으면 그 프로젝트의 결정이 **전부** 색인과
// 회수에서 빠진다. 그런데 색인은 정상 생성되고 회수도 에러를 안 낸다 — 없는 것처럼 군다.
// 그래서 이건 Warn 이 아니라 Fail 이다.
func TestUndeclaredDomainIsFail(t *testing.T) {
	c := testutil.VaultConfig(t)
	dir := filepath.Join(c.DefaultVaultPath(), "감마", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	note := "---\ntype: decision\ndate: 2026-08-08\ndomain: [감마]\nsummary: \"x\"\n" +
		"status: active\noutcome: pending\nsupersedes: \"\"\nrelated: []\ntags: []\n" +
		"source_session: \"\"\n---\n\n## 결정\n\nx\n"
	if err := os.WriteFile(filepath.Join(dir, "감마-결정-x-2026-08-08.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}

	got := find(t, Vault(c, store.NewLayout(c)), "미선언 도메인")
	if got.Level != Fail {
		t.Errorf("Level = %v, Fail 이어야 한다 — 결정이 통째로 빠지는데 경고로 그치면 안 된다", got.Level)
	}
	if !strings.Contains(got.Detail, "감마") {
		t.Errorf("어느 폴더인지 안 알려 준다: %s", got.Detail)
	}
	if got.Fix == "" {
		t.Error("고치는 법을 안 알려 준다 — 모르면 못 고친다")
	}
}

// 빈 폴더는 알리지 않는다. 결정이 없으면 잃을 것도 없고, 소음만 는다.
func TestEmptyUndeclaredFolderIsNotReported(t *testing.T) {
	c := testutil.VaultConfig(t)
	if err := os.MkdirAll(filepath.Join(c.DefaultVaultPath(), "감마", "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := find(t, Vault(c, store.NewLayout(c)), "미선언 도메인"); got.Level != OK {
		t.Errorf("빈 폴더를 알렸다: %s", got.Detail)
	}
}

// 색인이 낡았으면 알아야 한다. **색인이 결정적이라서 가능한 검사다.**
func TestStaleIndexIsDetected(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	if _, err := index.Write(l); err != nil {
		t.Fatal(err)
	}
	// 노트를 하나 더 넣는다 — 색인은 그대로다.
	dir := filepath.Join(c.DefaultVaultPath(), "alpha", "decisions")
	note := "---\ntype: decision\ndate: 2026-08-08\ndomain: [alpha]\nsummary: \"새 결정\"\n" +
		"status: active\noutcome: pending\nsupersedes: \"\"\nrelated: []\ntags: []\n" +
		"source_session: \"\"\n---\n\n## 결정\n\nx\n"
	if err := os.WriteFile(filepath.Join(dir, "alpha-결정-새것-2026-08-08.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}

	got := find(t, Vault(c, l), "색인")
	if got.Level != Warn {
		t.Errorf("Level = %v, Warn 이어야 한다 (%s)", got.Level, got.Detail)
	}
	if got.Fix != "prior index" {
		t.Errorf("Fix = %q, \"prior index\" 여야 한다", got.Fix)
	}
}

// 읽지 못한 노트는 Fail 이다 — 회수에서 빠지는 것은 조용한 손실이다.
func TestUnreadableNoteIsFail(t *testing.T) {
	c := testutil.VaultConfig(t)
	broken := filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-깨짐-2026-01-01.md")
	if err := os.WriteFile(broken, []byte("---\ntitle: 구 스키마\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := find(t, Vault(c, store.NewLayout(c)), "결정 노트")
	if got.Level != Fail {
		t.Errorf("Level = %v, Fail 이어야 한다", got.Level)
	}
	if !strings.Contains(got.Detail, "깨짐") {
		t.Errorf("어느 파일인지 안 알려 준다: %s", got.Detail)
	}
}

// 볼트가 없으면 나머지를 볼 것도 없다.
func TestMissingVaultIsFail(t *testing.T) {
	c := &config.Config{
		Vaults: []config.Vault{{Name: config.DefaultVaultName, Path: filepath.Join(t.TempDir(), "없는볼트")}},
		Naming: testutil.VaultConfig(t).Naming,
		Domain: []config.Domain{{Prefix: "alpha", Folder: "alpha"}},
	}
	r := Vault(c, store.NewLayout(c))
	if got := find(t, r, "볼트"); got.Level != Fail {
		t.Errorf("Level = %v, Fail 이어야 한다", got.Level)
	}
	if r.Worst() != Fail {
		t.Errorf("Worst = %v, Fail 이어야 한다", r.Worst())
	}
}

// ★ **파싱과 검증은 다르다.** List() 는 frontmatter 가 10키인지만 보고, 접두어와
// domain 첫 값이 같은지·status 가 허용값인지는 안 본다. `prior capture` 는 이걸 검증하지만
// **손으로 쓰면 통째로 우회된다** — 여기가 그 그물이다.
func TestSchemaViolationIsCaught(t *testing.T) {
	c := testutil.VaultConfig(t)
	// 접두어(alpha)와 domain 첫 값(beta)이 어긋난 노트를 손으로 심는다.
	bad := filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-어긋남-2026-08-08.md")
	body := "---\ntype: decision\ndate: 2026-08-08\ndomain: [beta]\nsummary: \"x\"\n" +
		"status: active\noutcome: pending\nsupersedes: \"\"\nrelated: []\ntags: []\n" +
		"source_session: \"\"\n---\n\n## 결정\n\nx\n"
	if err := os.WriteFile(bad, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	r := Vault(c, store.NewLayout(c))
	if got := find(t, r, "결정 노트"); got.Level != OK {
		t.Errorf("파싱은 됐어야 한다 (검증과 다르다): %s", got.Detail)
	}
	got := find(t, r, "스키마")
	if got.Level != Fail {
		t.Fatalf("Level = %v, Fail 이어야 한다 — 파싱만 보면 이 노트는 정상으로 보인다", got.Level)
	}
	if !strings.Contains(got.Detail, "어긋남") {
		t.Errorf("어느 노트인지 안 알려 준다: %s", got.Detail)
	}
}

// 감사 결함 4 — 하이픈·대소문자만 다른 중복. prior capture 는 거부하지만 손으로 쓰면 우회된다.
func TestSimilarSlugIsCaught(t *testing.T) {
	c := testutil.VaultConfig(t)
	dup := filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-저장-엔진-2026-08-01.md")
	body := "---\ntype: decision\ndate: 2026-08-01\ndomain: [alpha]\nsummary: \"x\"\n" +
		"status: active\noutcome: pending\nsupersedes: \"\"\nrelated: []\ntags: []\n" +
		"source_session: \"\"\n---\n\n## 결정\n\nx\n"
	if err := os.WriteFile(dup, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// 픽스처의 alpha-결정-저장엔진-2026-08-01 과 하이픈 하나 차이, 같은 날짜다.
	// (날짜가 다르면 capture 도 안 잡는다 — 후속 결정일 수 있기 때문이다.)
	got := find(t, Vault(c, store.NewLayout(c)), "유사 slug")
	if got.Level != Warn {
		t.Fatalf("Level = %v, Warn 이어야 한다 (%s)", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "↔") {
		t.Errorf("무엇과 무엇이 겹치는지 안 보여 준다: %s", got.Detail)
	}
}

func TestRecentDecisionsCounts(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	// 픽스처는 2026-08-01~04 다.
	if got := RecentDecisions(l, time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), 7); got != 4 {
		t.Errorf("RecentDecisions = %d, 4여야 한다", got)
	}
	if got := RecentDecisions(l, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), 7); got != 0 {
		t.Errorf("RecentDecisions = %d, 0이어야 한다 (전부 오래됐다)", got)
	}
}

// ★ **이 경고는 볼트가 공유될 때만 나와야 한다.**
//
// paths 만 쓰는 것은 혼자 쓰는 볼트에서 아무 문제가 아니다. 그런데도 매번 경고하면
// 그건 소음이고, 이 프로젝트는 소음을 죄목으로 삼는다 — 사람이 무시하는 법을 배우면
// 정작 진짜 경고도 같이 묻힌다.
//
// 반대로 **공유되는데 침묵하면** 새 팀원의 기록이 조용히 폴백 도메인으로 샌다.
// 그래서 두 방향을 다 검사한다.
func TestTeamPortabilityWarnsOnlyWhenVaultIsShared(t *testing.T) {
	pathOnly := func(vault string) *config.Config {
		return &config.Config{
			Vaults: []config.Vault{{Name: config.DefaultVaultName, Path: vault}}, DefaultDomain: "common",
			Domain: []config.Domain{
				// **폴백에도 paths 를 준다.** 없으면 "폴백은 지적하지 않는다" 는
				// 분기를 테스트가 아예 안 타서, 그 규칙이 사라져도 통과한다.
				{Prefix: "common", Folder: "common", Paths: []string{"/home/me/misc"}},
				{Prefix: "alpha", Folder: "alpha", Paths: []string{"/home/me/alpha"}},
			},
		}
	}
	find := func(r *Report) *Check {
		for i := range r.Checks {
			if r.Checks[i].Name == "팀 이식성" {
				return &r.Checks[i]
			}
		}
		return nil
	}

	// 혼자 쓰는 볼트 — 조용해야 한다.
	solo := t.TempDir()
	r := &Report{}
	checkTeamPortability(r, pathOnly(solo), store.NewLayout(pathOnly(solo)))
	if c := find(r); c != nil {
		t.Errorf("혼자 쓰는 볼트에 경고를 냈다 (소음): %s", c.Detail)
	}

	// git 아래 있는 볼트 — 공유되고 있다는 신호다. 말해야 한다.
	shared := t.TempDir()
	if err := os.MkdirAll(filepath.Join(shared, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	r = &Report{}
	checkTeamPortability(r, pathOnly(shared), store.NewLayout(pathOnly(shared)))
	c := find(r)
	if c == nil {
		t.Fatal("공유되는 볼트인데 침묵했다 — 새 팀원의 기록이 조용히 폴백으로 샌다")
	}
	if c.Level != Warn {
		t.Errorf("Level = %v, Warn 이어야 한다 (동작은 하므로 Fail 이 아니다)", c.Level)
	}
	if !strings.Contains(c.Detail, "alpha") {
		t.Errorf("어느 도메인이 문제인지 안 알려 준다: %s", c.Detail)
	}
	if strings.Contains(c.Detail, "common") {
		t.Errorf("폴백 도메인까지 지적했다 — 그건 원래 경로가 없어도 되는 자리다: %s", c.Detail)
	}
	if c.Fix == "" {
		t.Error("고칠 방법을 안 준다")
	}

	// repos 를 채우면 조용해진다 — 경고가 실제로 해소돼야 한다.
	withRepos := pathOnly(shared)
	withRepos.Domain[1].Repos = []string{"org/alpha"}
	r = &Report{}
	checkTeamPortability(r, withRepos, store.NewLayout(withRepos))
	if c := find(r); c == nil || c.Level != OK {
		t.Errorf("repos 를 채웠는데 해소되지 않았다: %+v", c)
	}
}

// ★★ **파생물이 git 에 들어 있으면 팀이 매번 충돌한다.**
//
// 색인은 결정 노트에서 다시 만들 수 있는데 `prior capture` 가 매번 통째로 다시 쓴다.
// 두 사람이 각자 하나씩 기록하면 각자 옳은 표를 만들 뿐인데 git 은 충돌로 본다.
// 실측으로 재현했다 — 결정 노트는 깨끗이 병합되고 색인만 충돌한다.
//
// 그 충돌을 손으로 잘못 풀면 **남의 결정이 색인에서 사라지고, 회수가 그걸 못 본다.**
//
// 반대로 git 이 아닌 볼트에서 이 경고가 뜨면 그건 소음이다 — 혼자 쓰는 사람에게
// 색인이 디스크에 있는 것은 아무 문제가 아니다.
func TestIndexInGitWarnsOnlyWhenTrackedAndShared(t *testing.T) {
	find := func(r *Report) *Check {
		for i := range r.Checks {
			if r.Checks[i].Name == "색인/git" {
				return &r.Checks[i]
			}
		}
		return nil
	}
	setup := func(t *testing.T, git bool, ignore string) (*config.Config, *store.Layout) {
		t.Helper()
		c := testutil.VaultConfig(t)
		if git {
			if err := os.MkdirAll(filepath.Join(c.DefaultVaultPath(), ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if ignore != "" {
			if err := os.WriteFile(filepath.Join(c.DefaultVaultPath(), ".gitignore"), []byte(ignore), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return c, store.NewLayout(c)
	}

	// git 이 아니면 조용해야 한다.
	c, l := setup(t, false, "")
	r := &Report{}
	checkIndexInGit(r, l)
	if ck := find(r); ck != nil {
		t.Errorf("git 이 아닌 볼트에 경고를 냈다 (소음): %s", ck.Detail)
	}

	// git 인데 무시 목록에 없으면 경고 + 고치는 법.
	c, l = setup(t, true, "")
	r = &Report{}
	checkIndexInGit(r, l)
	ck := find(r)
	if ck == nil || ck.Level != Warn {
		t.Fatalf("공유되는 볼트에서 색인이 추적 중인데 경고가 없다: %+v", ck)
	}
	rel := l.RelPath(l.IndexPath())
	if !strings.Contains(ck.Fix, ".gitignore") || !strings.Contains(ck.Fix, rel) {
		t.Errorf("고치는 법에 실제 경로가 없다: %q", ck.Fix)
	}

	// 무시 목록에 있으면 해소된다. 앞의 `/` 도 같은 뜻으로 봐야 한다.
	for _, line := range []string{rel, "/" + rel, "# 주석\n" + rel + "\n"} {
		c, l = setup(t, true, line)
		r = &Report{}
		checkIndexInGit(r, l)
		if ck := find(r); ck == nil || ck.Level != OK {
			t.Errorf("무시 목록 %q 인데 해소되지 않았다: %+v", line, ck)
		}
	}

	// .git/info/exclude 도 무시 목록이다.
	c, l = setup(t, true, "")
	if err := os.MkdirAll(filepath.Join(c.DefaultVaultPath(), ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.DefaultVaultPath(), ".git", "info", "exclude"),
		[]byte(rel+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r = &Report{}
	checkIndexInGit(r, l)
	if ck := find(r); ck == nil || ck.Level != OK {
		t.Errorf(".git/info/exclude 를 안 본다: %+v", ck)
	}
}

// ★★★ **고칠 수 없는 것을 권하면 그 경고는 영영 떠 있는다.**
//
// 실측(2026-08-14): 도메인 여덟에 repos 를 채우자 둘이 남았는데, 하나는 git
// 저장소가 아니고(`create`) 하나는 origin 이 없었다(`synth`). 둘 다 `repos` 를
// 적을 방법이 없다 — 그런데 doctor 는 계속 "repos 를 더해라" 고 말했다.
//
// **늘 뜨는 경고는 무시하는 법을 가르친다.** 그러면 정작 고칠 수 있는 도메인이
// 하나 늘어도 같이 묻힌다.
//
// 다만 **모르는 것을 침묵으로 바꾸지는 않는다.** 경로가 이 기계에 없으면
// (팀원의 체크아웃 자리) 판정할 수 없으므로 경고를 유지한다 — 침묵하면 새
// 팀원의 기록이 조용히 폴백으로 샌다. 침묵은 **불가능함을 증명했을 때만**이다.
func TestTeamPortabilitySkipsWhatCannotBeFixed(t *testing.T) {
	shared := t.TempDir()
	if err := os.MkdirAll(filepath.Join(shared, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 이 기계에 있고 git 이 아닌 프로젝트 — repos 를 적을 수 없다.
	local := t.TempDir()

	cfg := &config.Config{
		Vaults: []config.Vault{{Name: config.DefaultVaultName, Path: shared}}, DefaultDomain: "common",
		Domain: []config.Domain{
			{Prefix: "common", Folder: "common", Paths: []string{"/home/me/misc"}},
			{Prefix: "nogit", Folder: "nogit", Paths: []string{local}},
		},
	}
	r := &Report{}
	checkTeamPortability(r, cfg, store.NewLayout(cfg))
	for i := range r.Checks {
		if r.Checks[i].Name != "팀 이식성" {
			continue
		}
		if r.Checks[i].Level == Warn {
			t.Errorf("고칠 수 없는 도메인에 경고했다: %s", r.Checks[i].Detail)
		}
	}
}

// ★★ **고칠 수 있으면 무엇을 적을지까지 말한다.**
//
// "repos 를 더해라" 만으로는 사람이 `git remote -v` 를 치고 형식을 맞춰야 한다.
// 우리가 이미 아는 값이면 그대로 준다 — 진단은 다음 행동을 짧게 만드는 것이 일이다.
func TestTeamPortabilityNamesTheRepo(t *testing.T) {
	shared := t.TempDir()
	if err := os.MkdirAll(filepath.Join(shared, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitCfg := "[remote \"origin\"]\n\turl = https://github.com/acme/widget.git\n"
	if err := os.WriteFile(filepath.Join(proj, ".git", "config"), []byte(gitCfg), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Vaults: []config.Vault{{Name: config.DefaultVaultName, Path: shared}}, DefaultDomain: "common",
		Domain: []config.Domain{
			{Prefix: "common", Folder: "common", Paths: []string{"/home/me/misc"}},
			{Prefix: "widget", Folder: "widget", Paths: []string{proj}},
		},
	}
	r := &Report{}
	checkTeamPortability(r, cfg, store.NewLayout(cfg))
	for i := range r.Checks {
		if r.Checks[i].Name != "팀 이식성" {
			continue
		}
		if r.Checks[i].Level != Warn {
			t.Fatalf("경고해야 한다 (등급 %v)", r.Checks[i].Level)
		}
		if !strings.Contains(r.Checks[i].Fix, "acme/widget") {
			t.Errorf("무엇을 적을지 안 알려 준다: %s", r.Checks[i].Fix)
		}
		return
	}
	t.Fatal("검사가 아예 없다")
}

// writeNote 는 결정 노트 하나를 심는다. related·supersedes·본문을 골라 넣는다.
func writeNote(t *testing.T, c *config.Config, dir, stem, related, supersedes, body string) {
	t.Helper()
	writeNoteStatus(t, c, dir, stem, "active", related, supersedes, body)
}

func writeNoteStatus(t *testing.T, c *config.Config, dir, stem, status, related, supersedes, body string) {
	t.Helper()
	p := filepath.Join(c.DefaultVaultPath(), dir, "decisions", stem+".md")
	src := "---\ntype: decision\ndate: 2026-08-09\ndomain: [" + dir + "]\nsummary: \"x\"\n" +
		"status: " + status + "\noutcome: pending\nsupersedes: " + supersedes + "\n" +
		"related: [" + related + "]\ntags: [decision]\nsource_session: \"\"\n---\n\n" + body
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 개명·삭제로 끊어진 링크는 아무 신호 없이 그래프만 끊는다 — 회수는 Related 를
// 아예 안 읽으므로 점수도 안 변하고, doctor 는 지금까지 초록불이었다.
func TestBrokenFrontmatterLinkIsWarned(t *testing.T) {
	c := testutil.VaultConfig(t)
	writeNote(t, c, "alpha", "alpha-결정-끊긴링크-2026-08-09",
		`"[[alpha-결정-사라진것-2026-08-01]]"`, `""`, "## 결정\n\nx\n")

	got := find(t, Vault(c, store.NewLayout(c)), "링크")
	if got.Level != Warn {
		t.Fatalf("Level = %v, Warn 이어야 한다 (%s)", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "alpha-결정-끊긴링크-2026-08-09") ||
		!strings.Contains(got.Detail, "alpha-결정-사라진것-2026-08-01") {
		t.Errorf("어느 노트가 무엇을 가리키는지 안 알려 준다: %s", got.Detail)
	}
}

// ★ ResolveStem 으로 판정하면 실볼트 링크의 23%(214개 중 49개)가 오탐이 된다.
// related 는 결정이 아닌 문서도 가리킨다 — 프로젝트 개요·볼트 규약 문서.
func TestLinkToNonDecisionDocIsNotBroken(t *testing.T) {
	c := testutil.VaultConfig(t)
	meta := filepath.Join(c.DefaultVaultPath(), "_meta")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "00-볼트-네이밍-규약.md"), []byte("# 규약\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeNote(t, c, "alpha", "alpha-결정-규약참조-2026-08-09",
		`"[[00-볼트-네이밍-규약]]"`, `""`, "## 결정\n\nx\n")

	if got := find(t, Vault(c, store.NewLayout(c)), "링크"); got.Level != OK {
		t.Fatalf("결정이 아닌 문서를 가리키는 것은 정상이다: [%v] %s", got.Level, got.Detail)
	}
}

// ★ 본문을 검사하면 오탐률이 93% 다 (실볼트 15건 중 진짜 1건).
// ```toml 펜스 안의 [[domain]]·[[vault]] 는 TOML array-of-tables 문법이지 링크가 아니다.
func TestBodyWikilinksAreNotChecked(t *testing.T) {
	c := testutil.VaultConfig(t)
	body := "## 결정\n\n```toml\n[[domain]]\nprefix = \"x\"\n```\n\n" +
		"자리표시자 [[옛이름]] → [[새이름]] 과 인용 [[벧전 5:7]] 도 링크가 아니다.\n"
	writeNote(t, c, "alpha", "alpha-결정-본문링크-2026-08-09", "", `""`, body)

	if got := find(t, Vault(c, store.NewLayout(c)), "링크"); got.Level != OK {
		t.Fatalf("본문은 사람의 산문이다 — 우리 규약의 대상이 아니다: [%v] %s", got.Level, got.Detail)
	}
}

// (b) 뒤집힌 대상이 active 로 남으면 회수 감점(penaltySuperseded)이 안 걸려
// 이미 죽은 결정이 만점으로 계속 올라온다.
func TestSupersedeTargetLeftActiveIsWarned(t *testing.T) {
	c := testutil.VaultConfig(t)
	writeNoteStatus(t, c, "alpha", "alpha-결정-옛것-2026-08-09", "active",
		`"[[alpha-결정-새것-2026-08-09]]"`, `""`, "## 결정\n\nx\n")
	writeNote(t, c, "alpha", "alpha-결정-새것-2026-08-09",
		"", `"[[alpha-결정-옛것-2026-08-09]]"`, "## 결정\n\nx\n")

	got := find(t, Vault(c, store.NewLayout(c)), "뒤집기")
	if got.Level != Warn {
		t.Fatalf("Level = %v, Warn 이어야 한다 (%s)", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "alpha-결정-옛것-2026-08-09") {
		t.Errorf("어느 노트인지 안 알려 준다: %s", got.Detail)
	}
}

// (c) 옛 노트가 후속을 안 가리키면 그 결정을 열었을 때 "무엇이 이걸 대체했나" 로
// 갈 길이 없다. supersede 가 심는 역링크가 사람 손에 지워진 경우다.
func TestSupersedeTargetMissingBackLinkIsWarned(t *testing.T) {
	c := testutil.VaultConfig(t)
	writeNoteStatus(t, c, "alpha", "alpha-결정-옛것-2026-08-09", "superseded",
		"", `""`, "## 결정\n\nx\n") // related 가 비었다 — 역링크 없음
	writeNote(t, c, "alpha", "alpha-결정-새것-2026-08-09",
		"", `"[[alpha-결정-옛것-2026-08-09]]"`, "## 결정\n\nx\n")

	got := find(t, Vault(c, store.NewLayout(c)), "뒤집기")
	if got.Level != Warn {
		t.Fatalf("Level = %v, Warn 이어야 한다 (%s)", got.Level, got.Detail)
	}
}

// ★ (d) 실볼트에 실재하는 사례. status 만 손으로 superseded 로 바꾸고 frontmatter 를
// 안 건드리면, 그 노트를 뒤집은 결정이 무엇인지 아무 데도 안 적힌다.
// 근본 원인은 Supersedes 가 한 칸뿐이라 방향전환 하나가 셋을 뒤집을 수 없었던 것이다.
func TestOrphanSupersededNoteIsWarned(t *testing.T) {
	c := testutil.VaultConfig(t)
	writeNoteStatus(t, c, "alpha", "alpha-결정-고아-2026-08-09", "superseded",
		"", `""`, "## 결정\n\nx\n")

	got := find(t, Vault(c, store.NewLayout(c)), "뒤집기")
	if got.Level != Warn {
		t.Fatalf("Level = %v, Warn 이어야 한다 (%s)", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "alpha-결정-고아-2026-08-09") {
		t.Errorf("어느 노트인지 안 알려 준다: %s", got.Detail)
	}
}
