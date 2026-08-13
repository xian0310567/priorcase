package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const namingBlock = `
[naming]
decision_file = "{domain}-결정-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog       = "99-{project}-작업-로그.md"
index         = "_meta/00-결정-색인.md"
rollup        = "98-{project}-작업-로그-요약.md"
`

// cfg 는 맨 키를 먼저, 테이블을 뒤에 놓는다.
//
// **TOML 은 테이블을 연 뒤의 맨 키를 전부 그 테이블의 것으로 읽는다** — 순서가 곧
// 의미다. 이 함정에 실제로 두 번 빠졌다 (여기와 testutil 의 설정 파일 생성).
func cfg(bare, tables string) string {
	return bare + "\n" + namingBlock + tables
}

// ★★ **옛 설정(`vault = "경로"`)이 그대로 돌아야 한다.**
//
// TOML 은 같은 키를 문자열과 테이블 배열로 겸할 수 없어서 Load 가 2패스로 읽는다.
// 그 처리가 깨지면 기존 사용자의 설정이 "vault 가 비어 있다" 로 죽는다.
func TestLoadLegacySingleVault(t *testing.T) {
	dir := t.TempDir()
	c, err := config.Load(writeCfg(t, cfg(
		"vault = \""+dir+"\"\ndefault_domain = \"common\"", "")))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Vaults) != 1 {
		t.Fatalf("볼트 %d개", len(c.Vaults))
	}
	if c.Vaults[0].Path != dir {
		t.Errorf("Path=%q", c.Vaults[0].Path)
	}
	// 이름이 없으면 도메인이 가리킬 수 없으므로 기본 이름을 붙여야 한다.
	if c.Vaults[0].Name != config.DefaultVaultName {
		t.Errorf("Name=%q, %q 여야 한다", c.Vaults[0].Name, config.DefaultVaultName)
	}
}

// ★★★ **[[vault]] 여러 개와 도메인의 vault 지정이 이어져야 한다.**
//
// 이게 이 기능의 전부다 — A 프로젝트에서 일하면 A 가 지정한 볼트에 쓴다.
func TestLoadMultipleVaultsAndDomainMapping(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	c, err := config.Load(writeCfg(t, cfg(`default_domain = "common"`, `
[[domain]]
prefix = "alpha"
folder = "alpha"
vault  = "work"
paths  = ["/tmp/proj/alpha"]

[[domain]]
prefix = "beta"
folder = "beta"
vault  = "personal"
paths  = ["/tmp/proj/beta"]

[[domain]]
prefix = "common"
folder = "common"

[[vault]]
name = "personal"
path = "`+a+`"

[[vault]]
name = "work"
path = "`+b+`"
`)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		prefix, want, why string
	}{
		{"alpha", b, "work 을 지정했다"},
		{"beta", a, "personal 을 지정했다"},
		{"common", a, "지정이 없으면 기본(첫 번째)"},
		{"", a, "도메인을 몰라도 쓸 자리는 있어야 한다"},
	} {
		v, err := c.VaultFor(tc.prefix)
		if err != nil {
			t.Errorf("VaultFor(%q): %v", tc.prefix, err)
			continue
		}
		if v.Path != tc.want {
			t.Errorf("VaultFor(%q) = %q, want %q (%s)", tc.prefix, v.Path, tc.want, tc.why)
		}
	}

	// cwd → 도메인 → 볼트가 이어져야 한다. 이것이 실사용 경로다.
	if v, err := c.VaultForCwd("/tmp/proj/alpha/sub"); err != nil || v.Path != b {
		t.Errorf("VaultForCwd(alpha 하위) = %q (%v), work 볼트여야 한다", v.Path, err)
	}
	if v, err := c.VaultForCwd("/tmp/proj/beta"); err != nil || v.Path != a {
		t.Errorf("VaultForCwd(beta) = %q (%v), personal 볼트여야 한다", v.Path, err)
	}
}

// ★★ **없는 볼트를 가리키면 시끄럽게 실패해야 한다.**
//
// 오타를 기본 볼트로 뭉개면 그 프로젝트의 결정이 사람이 찾을 수 없는 자리에 쌓인다.
func TestUnknownVaultNameIsRejected(t *testing.T) {
	dir := t.TempDir()
	_, err := config.Load(writeCfg(t, cfg(`default_domain = "common"`, `
[[domain]]
prefix = "alpha"
folder = "alpha"
vault  = "오타"

[[vault]]
name = "personal"
path = "`+dir+`"
`)))
	if err == nil {
		t.Fatal("없는 볼트를 가리키는데 통과했다")
	}
	if !strings.Contains(err.Error(), "오타") {
		t.Errorf("어느 이름이 문제인지 안 알려 준다: %v", err)
	}
}

// ★★ 볼트가 여럿인데 이름이 없으면 도메인이 가리킬 수 없다.
func TestMultipleVaultsRequireNames(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	_, err := config.Load(writeCfg(t, cfg(`default_domain = "common"`, `
[[vault]]
path = "`+a+`"

[[vault]]
name = "work"
path = "`+b+`"
`)))
	if err == nil {
		t.Fatal("이름 없는 볼트가 통과했다")
	}
}

// ★ 이름이 겹치면 어느 쪽인지 알 수 없다.
func TestDuplicateVaultNameIsRejected(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	_, err := config.Load(writeCfg(t, cfg(`default_domain = "common"`, `
[[vault]]
name = "v"
path = "`+a+`"

[[vault]]
name = "v"
path = "`+b+`"
`)))
	if err == nil {
		t.Fatal("이름이 겹치는데 통과했다")
	}
}

// ★★ **오타 키 검사가 두 형태 모두에서 살아 있어야 한다.**
//
// 2패스로 읽으면서 한쪽 변형만 엄격 검사를 하면, 그 형태를 쓰는 사용자는 오타를
// 조용히 무시당한다.
func TestUnknownKeyRejectedInBothForms(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"레거시": cfg("vault = \""+dir+"\"\nlanguage = \"ko\"", ""),
		"새형태": cfg(`language = "ko"`, "\n[[vault]]\nname = \"v\"\npath = \""+dir+"\"\n"),
	} {
		if _, err := config.Load(writeCfg(t, body)); err == nil {
			t.Errorf("%s: 오타 키(language)가 통과했다", name)
		}
	}
}
