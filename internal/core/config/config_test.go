package config

import (
	"os"
	"path/filepath"
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
paths = ["/home/t/Documents/automation-dropshipping"]
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
	if c.Vault != "/tmp/vault" {
		t.Errorf("Vault = %q", c.Vault)
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
		{"/home/t/Documents/automation-dropshipping", "occ"},
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

// TestLoadVaultEnvOverride 는 CASEBOOK_VAULT 가 설정 파일의 vault 를
// 덮어쓰는지 확인한다 (테스트 볼트 격리용 오버라이드).
func TestLoadVaultEnvOverride(t *testing.T) {
	t.Setenv("CASEBOOK_VAULT", "/override")
	c, err := Load(write(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	if c.Vault != "/override" {
		t.Errorf("Vault = %q, want CASEBOOK_VAULT 오버라이드 값 %q", c.Vault, "/override")
	}
}

// TestLoadTestdataFixture 는 testdata/valid.toml 이 파서 스키마와 어긋나지
// 않는 유효한 설정으로 계속 유지되는지 지킨다.
func TestLoadTestdataFixture(t *testing.T) {
	c, err := Load(filepath.Join("testdata", "valid.toml"))
	if err != nil {
		t.Fatalf("Load(testdata/valid.toml) error = %v", err)
	}
	if c.Vault == "" {
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
