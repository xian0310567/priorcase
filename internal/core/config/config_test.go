package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `
vault = "/tmp/vault"
exclude = ["/home/t/project/NOI"]

[naming]
decision_file = "{domain}-결정-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog = "99-{project}-작업-로그.md"
index = "_meta/00-결정-색인.md"

[capture]
signals = ["결정", "선택"]
min_turns = 6
quiesce_seconds = 3

[[domain]]
prefix = "omni"
folder = "omni"
paths = ["/home/t/project/omni"]

[[domain]]
prefix = "occ"
folder = "OCC"
paths = ["/home/t/Documents/shop-automation"]
`

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad(t *testing.T) {
	c, err := Load(write(t, sample))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.DefaultVaultPath() != "/tmp/vault" {
		t.Errorf("Vault = %q", c.DefaultVaultPath())
	}
	// exclude 가 top-level 로 붙었는지 — [[domain]] 뒤에 두면 조용히 domain 필드가 된다
	if len(c.Exclude) != 1 || c.Exclude[0] != "/home/t/project/NOI" {
		t.Errorf("Exclude = %v, want top-level 1건", c.Exclude)
	}
	if len(c.Domain) != 2 || c.Domain[1].Folder != "OCC" {
		t.Errorf("Domain = %+v", c.Domain)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	_, err := Load(write(t, sample+"\ntypo_key = 1\n"))
	if err == nil {
		t.Fatal("오타 키를 통과시켰다 — strict 모드가 꺼져 있다")
	}
}

func TestDomainForCwd(t *testing.T) {
	c, err := Load(write(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ cwd, want string }{
		{"/home/t/project/omni", "omni"},
		{"/home/t/project/omni/src/deep", "omni"},
		{"/home/t/Documents/shop-automation", "occ"},
		{"/home/t/unmapped", ""},
		{"/home/t/project/omni-other", ""}, // 접두 문자열 오탐 방지
	}
	for _, tt := range tests {
		if got := c.DomainForCwd(tt.cwd); got != tt.want {
			t.Errorf("DomainForCwd(%q) = %q, want %q", tt.cwd, got, tt.want)
		}
	}
}

// TestDomainForCwdExcludeWinsOverDomain 은 실제로 났던 회귀를 못 박는다:
// 도메인 목록에 노출된 프로젝트(NOI)가 exclude 에도 걸리면 도메인 매핑이
// 아니라 제외가 이겨야 한다. DomainForCwd 가 IsExcluded 를 먼저 보지 않으면
// exclude 로 막으려던 경로가 도메인 접두어를 달고 그대로 새어나간다.
func TestDomainForCwdExcludeWinsOverDomain(t *testing.T) {
	const conflict = `
vault = "/tmp/vault"
exclude = ["/home/t/project/NOI"]

[naming]
decision_file = "{domain}-결정-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog = "99-{project}-작업-로그.md"
index = "_meta/00-결정-색인.md"

[capture]
signals = ["결정"]
min_turns = 6
quiesce_seconds = 3

[[domain]]
prefix = "noi"
folder = "NOI"
paths = ["/home/t/project/NOI"]
`
	c, err := Load(write(t, conflict))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.DomainForCwd("/home/t/project/NOI"); got != "" {
		t.Errorf("DomainForCwd(exclude 와 domain 이 같은 경로) = %q, want %q", got, "")
	}
	if got := c.DomainForCwd("/home/t/project/NOI/docs"); got != "" {
		t.Errorf("DomainForCwd(exclude 하위이자 domain 하위) = %q, want %q", got, "")
	}
}

func TestIsExcluded(t *testing.T) {
	c, err := Load(write(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsExcluded("/home/t/project/NOI/docs") {
		t.Error("NOI 하위가 제외되지 않았다")
	}
	if c.IsExcluded("/home/t/project/omni") {
		t.Error("omni 가 제외됐다")
	}
}

// TestLoadVaultEnvOverride 는 PRIORCASE_VAULT 가 설정 파일의 vault 를
// 덮어쓰는지 확인한다 (테스트 볼트 격리용 오버라이드).
func TestLoadVaultEnvOverride(t *testing.T) {
	t.Setenv("PRIORCASE_VAULT", "/override")
	c, err := Load(write(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultVaultPath() != "/override" {
		t.Errorf("Vault = %q, want PRIORCASE_VAULT 오버라이드 값 %q", c.DefaultVaultPath(), "/override")
	}
}

// TestLoadUsesConfigEnv 는 --config 플래그 없이도 PRIORCASE_CONFIG 가 설정
// 경로를 정하는지 확인한다. 플래그를 붙일 수 없는 훅·데몬 어댑터가 이 통로만
// 쓴다 — 여기가 막히면 그쪽은 XDG 기본 경로 하나에 묶인다.
func TestLoadUsesConfigEnv(t *testing.T) {
	p := write(t, sample)
	t.Setenv(PathEnv, p)
	c, err := Load("")
	if err != nil {
		t.Fatalf("PRIORCASE_CONFIG 를 무시했다: %v", err)
	}
	if c.DefaultVaultPath() != "/tmp/vault" {
		t.Errorf("Vault = %q, want %q", c.DefaultVaultPath(), "/tmp/vault")
	}
}

// TestLoadFlagBeatsConfigEnv 는 --config 플래그가 환경변수를 이기는지 본다.
// 환경변수에는 존재하지 않는 경로를 넣어, 플래그를 무시하면 반드시 실패하게 한다.
func TestLoadFlagBeatsConfigEnv(t *testing.T) {
	p := write(t, sample)
	t.Setenv(PathEnv, filepath.Join(t.TempDir(), "없는-설정.toml"))
	c, err := Load(p)
	if err != nil {
		t.Fatalf("플래그가 환경변수에 밀렸다: %v", err)
	}
	if c.DefaultVaultPath() != "/tmp/vault" {
		t.Errorf("Vault = %q, want %q", c.DefaultVaultPath(), "/tmp/vault")
	}
}

// TestResolvePathFallsBackToDefault 는 플래그도 환경변수도 없으면 XDG 기본
// 경로로 떨어지는지 확인한다.
func TestResolvePathFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(PathEnv, "")

	got, err := ResolvePath("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "priorcase", "config.toml")
	if got != want {
		t.Errorf("ResolvePath(\"\") = %q, want %q", got, want)
	}
}

// TestResolvePathPriority 는 세 출처의 우선순위를 한자리에서 못 박는다.
func TestResolvePathPriority(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	envPath := filepath.Join(dir, "from-env.toml")
	flagPath := filepath.Join(dir, "from-flag.toml")

	t.Setenv(PathEnv, envPath)
	if got, _ := ResolvePath(flagPath); got != flagPath {
		t.Errorf("플래그가 있을 때 = %q, want %q", got, flagPath)
	}
	if got, _ := ResolvePath(""); got != envPath {
		t.Errorf("환경변수만 있을 때 = %q, want %q", got, envPath)
	}
	t.Setenv(PathEnv, "")
	if got, _ := ResolvePath(""); got != filepath.Join(dir, "priorcase", "config.toml") {
		t.Errorf("둘 다 없을 때 = %q, want XDG 기본 경로", got)
	}
}

// TestDecisionMarkerIsDerivedFromTemplate 는 결정 표식이 코드 상수가 아니라
// decision_file 템플릿에서 유도된다는 사실을 못박는다. 템플릿을 바꾸면 표식이
// 따라 바뀌어야 국제화가 열린다 (스펙 §5).
func TestDecisionMarkerIsDerivedFromTemplate(t *testing.T) {
	tests := []struct{ template, want string }{
		{"{domain}-결정-{slug}-{date}.md", "-결정-"},
		{"{domain}-decision-{slug}-{date}.md", "-decision-"},
		{"{domain}__{slug}-{date}.md", "__"},
		{"{domain}{slug}-{date}.md", ""},     // 표식이 없다
		{"{slug}-결정-{domain}-{date}.md", ""}, // 순서가 뒤집혔다
		{"decision-{slug}-{date}.md", ""},    // {domain} 이 없다
		{"{domain}-결정-{date}.md", ""},        // {slug} 가 없다
	}
	for _, tt := range tests {
		c := &Config{Naming: Naming{DecisionFile: tt.template}}
		if got := c.DecisionMarker(); got != tt.want {
			t.Errorf("DecisionMarker(%q) = %q, want %q", tt.template, got, tt.want)
		}
	}
}

// TestLoadRejectsBadNaming 은 [naming] 오류가 config 층에서 잡히는지 본다.
// 여기서 안 잡으면 볼트 디렉터리명이 stem 으로 새거나(schema 에러) 색인이
// 볼트 디렉터리를 덮어쓰려 든다(store 에러) — 진단이 불가능한 자리에서 터진다.
func TestLoadRejectsBadNaming(t *testing.T) {
	const head = "vault = \"/tmp/vault\"\n\n"
	tests := []struct{ name, naming, want string }{
		{"[naming] 절이 통째로 없다", "", "[naming]"},
		{"decision_file 이 없다", `
[naming]
decisions_dir = "{project}/decisions"
worklog = "w-{project}.md"
index = "i.md"
`, "decision_file"},
		{"decisions_dir 이 없다", `
[naming]
decision_file = "{domain}-d-{slug}-{date}.md"
worklog = "w-{project}.md"
index = "i.md"
`, "decisions_dir"},
		{"worklog 이 없다", `
[naming]
decision_file = "{domain}-d-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
index = "i.md"
`, "worklog"},
		{"index 가 없다", `
[naming]
decision_file = "{domain}-d-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog = "w-{project}.md"
`, "index"},
		{"decision_file 에 {date} 가 없다", `
[naming]
decision_file = "{domain}-d-{slug}.md"
decisions_dir = "{project}/decisions"
worklog = "w-{project}.md"
index = "i.md"
`, "{date}"},
		{"decision_file 에서 {slug} 가 {domain} 보다 앞이다", `
[naming]
decision_file = "{slug}-d-{domain}-{date}.md"
decisions_dir = "{project}/decisions"
worklog = "w-{project}.md"
index = "i.md"
`, "{slug}"},
		{"decision_file 에 표식이 없다", `
[naming]
decision_file = "{domain}{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog = "w-{project}.md"
index = "i.md"
`, "표식"},
		{"decision_file 이 -{date}.md 로 안 끝난다", `
[naming]
decision_file = "{date}-{domain}-d-{slug}.md"
decisions_dir = "{project}/decisions"
worklog = "w-{project}.md"
index = "i.md"
`, "{date}.md"},
		{"decisions_dir 에 {project} 가 없다", `
[naming]
decision_file = "{domain}-d-{slug}-{date}.md"
decisions_dir = "decisions"
worklog = "w-{project}.md"
index = "i.md"
`, "{project}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(write(t, head+tt.naming))
			if err == nil {
				t.Fatalf("잘못된 [naming] 을 통과시켰다:\n%s", tt.naming)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("에러가 무엇이 잘못됐는지 알려주지 않는다 (%q 를 기대): %v", tt.want, err)
			}
		})
	}
}

// TestLoadAcceptsEnglishNaming 은 영어 템플릿 설정이 config 층을 통과하는지
// 본다 — 국제화가 여기서 열린다.
func TestLoadAcceptsEnglishNaming(t *testing.T) {
	const english = `
vault = "/tmp/vault"

[naming]
decision_file = "{domain}-decision-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog = "99-{project}-worklog.md"
index = "_meta/00-decision-index.md"

[[domain]]
prefix = "alpha"
folder = "alpha"
`
	c, err := Load(write(t, english))
	if err != nil {
		t.Fatalf("영어 템플릿 설정을 거부했다: %v", err)
	}
	if got := c.DecisionMarker(); got != "-decision-" {
		t.Errorf("DecisionMarker() = %q, want %q", got, "-decision-")
	}
}

// TestLoadTestdataFixture 는 testdata/valid.toml 이 파서 스키마와 어긋나지
// 않는 유효한 설정으로 계속 유지되는지 지킨다.
func TestLoadTestdataFixture(t *testing.T) {
	c, err := Load(filepath.Join("testdata", "valid.toml"))
	if err != nil {
		t.Fatalf("Load(testdata/valid.toml) error = %v", err)
	}
	if c.DefaultVaultPath() == "" {
		t.Error("testdata/valid.toml 의 vault 가 비어 있다")
	}
}

// TestLoadExpandHomeError 는 홈 디렉토리를 못 구할 때 ~ 가 조용히 그대로
// 남는 대신 Load 가 에러를 반환하는지 확인한다.
func TestLoadExpandHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	const tildeConfig = `
vault = "~/vault"

[naming]
decision_file = "{domain}-결정-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog = "99-{project}-작업-로그.md"
index = "_meta/00-결정-색인.md"

[capture]
signals = ["결정"]
min_turns = 6
quiesce_seconds = 3
`
	_, err := Load(write(t, tildeConfig))
	if err == nil {
		t.Fatal("HOME 을 못 구하는데 ~ 확장이 조용히 성공했다")
	}
}

// TestLoadNoTildeSucceedsWithoutHome 은 설정에 ~ 가 하나도 없으면 $HOME 이
// 없는 환경(launchd·cron·컨테이너)에서도 Load 가 성공하는지 확인한다.
// expand() 가 홈 디렉토리 조회를 지연시켜, ~ 를 실제로 만나지 않는 한
// os.UserHomeDir 를 부르지 않아야 한다.
func TestLoadNoTildeSucceedsWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	c, err := Load(write(t, sample))
	if err != nil {
		t.Fatalf("HOME 이 없어도 ~ 없는 설정은 성공해야 하는데 실패했다: %v", err)
	}
	if c.DefaultVaultPath() != "/tmp/vault" {
		t.Errorf("Vault = %q", c.DefaultVaultPath())
	}
}
