package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ★★★ **명령을 실제로 돌리는 시험이어야 한다.**
//
// config.AddVault 를 직접 부르는 시험은 이미 core 에 있다. 여기서 보는 것은
// **조립**이다 — 플래그가 붙었는가, 설정 경로를 찾는가, 쓴 것이 다시 읽히는가.
// 이 프로젝트에서 다섯 번 난 사고가 전부 "함수는 옳은데 조립이 틀렸다" 였다.

const settingsFixture = `# 손으로 쓴 주석. 사라지면 안 된다.
vault = "%VAULT%"
default_domain = "common"

[naming]
decision_file = "{domain}-결정-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog       = "99-{project}-작업-로그.md"
index         = "_meta/00-결정-색인.md"

[[domain]]
prefix = "proj"
folder = "proj"
paths  = ["/tmp/proj"]
`

func settingsFixtureAt(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	vault := filepath.Join(dir, "vault")
	if err := os.MkdirAll(filepath.Join(vault, "proj", "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.toml")
	body := strings.ReplaceAll(settingsFixture, "%VAULT%", vault)
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func runSettingsCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := &cobra.Command{Use: "prior"}
	root.PersistentFlags().String("config", "", "")
	root.AddCommand(newSettingsCmd(), newVaultCmd(), newDomainCmd(), newHostsCmd())
	var out, errb strings.Builder
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errb.String(), err
}

// ★★★ 앱이 읽는 계약이다. 필드가 비면 화면이 빈다.
func TestSettingsJSONCarriesVaultsHostsDomains(t *testing.T) {
	cfg := settingsFixtureAt(t)
	out, errb, err := runSettingsCmd(t, "settings", "--json", "--config", cfg)
	if err != nil {
		t.Fatalf("%v (stderr=%s)", err, errb)
	}
	var s settingsOut
	if jerr := json.Unmarshal([]byte(out), &s); jerr != nil {
		t.Fatalf("JSON 이 아니다: %v\n%s", jerr, out)
	}
	if s.ConfigPath == "" {
		t.Error("설정 경로가 비었다 — 사람이 어느 파일을 고치는지 알 수 없다")
	}
	if len(s.Vaults) != 1 || !s.Vaults[0].Exists {
		t.Errorf("볼트가 틀렸다: %+v", s.Vaults)
	}
	if len(s.Vaults[0].Domains) != 1 || s.Vaults[0].Domains[0] != "proj" {
		t.Errorf("볼트를 쓰는 도메인을 안 달았다: %+v", s.Vaults[0])
	}
	if len(s.Domains) != 1 || s.Domains[0].Prefix != "proj" {
		t.Errorf("도메인이 틀렸다: %+v", s.Domains)
	}
	// **레지스트리가 목록의 정본이다** — 설정에 한 줄도 없어도 호스트가 보여야 한다.
	if len(s.Hosts) < 2 {
		t.Errorf("호스트 %d개 — 레지스트리의 전부가 나와야 한다: %+v", len(s.Hosts), s.Hosts)
	}
	for _, h := range s.Hosts {
		if !h.Enabled {
			t.Errorf("설정에 없는 호스트가 꺼져 있다: %s", h.Name)
		}
	}
}

// ★★★ **경로를 안 줘도 만들어져야 한다.**
//
// 사람에게 경로를 묻는 것은 답이 이미 정해져 있는 질문이다 — 지금 볼트 옆이다.
// 볼트를 여럿 두는 이유가 공유를 위한 분리라 사람이 Finder 에서 다룰 수 있는
// 자리여야 하고, 그 자리는 기본 볼트가 이미 알려 준다.
func TestVaultAddWithoutPathGoesBesideCurrent(t *testing.T) {
	cfg := settingsFixtureAt(t)
	if _, errb, err := runSettingsCmd(t, "vault", "add", "회사", "--config", cfg); err != nil {
		t.Fatalf("%v (stderr=%s)", err, errb)
	}
	out, _, err := runSettingsCmd(t, "settings", "--json", "--config", cfg)
	if err != nil {
		t.Fatal(err)
	}
	var s settingsOut
	if jerr := json.Unmarshal([]byte(out), &s); jerr != nil {
		t.Fatal(jerr)
	}
	if len(s.Vaults) != 2 {
		t.Fatalf("볼트 %d개 — 2개여야 한다: %+v", len(s.Vaults), s.Vaults)
	}
	made := s.Vaults[1]
	want := filepath.Join(filepath.Dir(s.Vaults[0].Path), "회사")
	if made.Path != want {
		t.Errorf("볼트 자리 %q — %q 여야 한다", made.Path, want)
	}
	if !made.Exists {
		t.Error("폴더를 안 만들었다")
	}
}

// ★★★ **어디에 만들었는지 말해야 한다.**
//
// 경로를 안 물어봤으므로 이 줄이 없으면 어디에 생겼는지 모르는 폴더가 남는다.
func TestVaultAddReportsWhereItWent(t *testing.T) {
	cfg := settingsFixtureAt(t)
	_, errb, err := runSettingsCmd(t, "vault", "add", "회사", "--config", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errb, "회사") || !strings.Contains(errb, string(filepath.Separator)) {
		t.Errorf("만든 자리를 안 말한다: %q", errb)
	}
}

// ★★★ 앱이 **누르기 전에** 어디 생길지 보여 줄 수 있어야 한다.
func TestSettingsCarriesVaultParent(t *testing.T) {
	cfg := settingsFixtureAt(t)
	out, _, err := runSettingsCmd(t, "settings", "--json", "--config", cfg)
	if err != nil {
		t.Fatal(err)
	}
	var s settingsOut
	if jerr := json.Unmarshal([]byte(out), &s); jerr != nil {
		t.Fatal(jerr)
	}
	if s.VaultParent == "" {
		t.Fatal("새 볼트가 어디 생기는지 안 알려 준다")
	}
	if s.VaultParent != filepath.Dir(s.Vaults[0].Path) {
		t.Errorf("부모 %q — 기본 볼트 옆(%q)이어야 한다", s.VaultParent, filepath.Dir(s.Vaults[0].Path))
	}
}

// ★★★ `vault add` 가 **폴더까지 만든다.**
//
// 설정만 고치고 자리가 없으면 그 볼트로 엮인 도메인이 첫 기록에서 실패하는데,
// 그때는 설정을 고친 지 한참 뒤라 원인을 못 잇는다.
func TestVaultAddCreatesDirAndPersists(t *testing.T) {
	cfg := settingsFixtureAt(t)
	dir := filepath.Join(filepath.Dir(cfg), "second")
	if _, errb, err := runSettingsCmd(t, "vault", "add", "work", dir, "--config", cfg); err != nil {
		t.Fatalf("%v (stderr=%s)", err, errb)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Errorf("볼트 자리를 안 만들었다: %v", err)
	}
	out, _, err := runSettingsCmd(t, "settings", "--json", "--config", cfg)
	if err != nil {
		t.Fatal(err)
	}
	var s settingsOut
	if jerr := json.Unmarshal([]byte(out), &s); jerr != nil {
		t.Fatal(jerr)
	}
	if len(s.Vaults) != 2 || s.Vaults[1].Name != "work" {
		t.Errorf("볼트가 안 남았다: %+v", s.Vaults)
	}
	// 주석과 직전 판이 살아 있어야 한다.
	raw, _ := os.ReadFile(cfg)
	if !strings.Contains(string(raw), "# 손으로 쓴 주석") {
		t.Errorf("주석이 사라졌다:\n%s", raw)
	}
	if _, err := os.Stat(cfg + ".bak"); err != nil {
		t.Errorf("직전 판을 안 남겼다: %v", err)
	}
}

// ★★★ 도메인을 볼트에 엮으면 **그 볼트로 쓰인다.**
func TestDomainBindPersists(t *testing.T) {
	cfg := settingsFixtureAt(t)
	dir := filepath.Join(filepath.Dir(cfg), "second")
	if _, _, err := runSettingsCmd(t, "vault", "add", "work", dir, "--config", cfg); err != nil {
		t.Fatal(err)
	}
	if _, errb, err := runSettingsCmd(t, "domain", "bind", "proj", "work", "--config", cfg); err != nil {
		t.Fatalf("%v (stderr=%s)", err, errb)
	}
	out, _, err := runSettingsCmd(t, "settings", "--json", "--config", cfg)
	if err != nil {
		t.Fatal(err)
	}
	var s settingsOut
	if jerr := json.Unmarshal([]byte(out), &s); jerr != nil {
		t.Fatal(jerr)
	}
	if s.Domains[0].Vault != "work" {
		t.Errorf("도메인이 안 엮였다: %+v", s.Domains[0])
	}
}

// ★★★ **오타 난 호스트 이름을 받아 주면 안 된다.**
//
// 설정에는 아무 이름이나 들어가고 그것은 어느 호스트와도 안 맞으므로 아무 일도
// 안 일어난다 — 사람은 껐다고 믿는다.
func TestHostsRejectsUnknownName(t *testing.T) {
	cfg := settingsFixtureAt(t)
	_, _, err := runSettingsCmd(t, "hosts", "disable", "Codex", "--config", cfg)
	if err == nil {
		t.Fatal("오타를 받아 줬다")
	}
	if !strings.Contains(err.Error(), "Codex CLI") {
		t.Errorf("있는 이름을 안 알려 준다: %v", err)
	}
}

// ★★★ 호스트를 끄면 설정에 남고 다시 읽힌다.
func TestHostsDisablePersists(t *testing.T) {
	cfg := settingsFixtureAt(t)
	if _, errb, err := runSettingsCmd(t, "hosts", "disable", "Codex CLI", "--config", cfg); err != nil {
		t.Fatalf("%v (stderr=%s)", err, errb)
	}
	out, _, err := runSettingsCmd(t, "settings", "--json", "--config", cfg)
	if err != nil {
		t.Fatal(err)
	}
	var s settingsOut
	if jerr := json.Unmarshal([]byte(out), &s); jerr != nil {
		t.Fatal(jerr)
	}
	var found bool
	for _, h := range s.Hosts {
		if h.Name == "Codex CLI" {
			found = true
			if h.Enabled {
				t.Error("껐는데 켜져 있다")
			}
		}
	}
	if !found {
		t.Error("끈 호스트가 목록에서 사라졌다 — 다시 켤 방법이 없어진다")
	}
}

// ★★★ **자리가 없는 볼트를 조용히 넘기지 않는다.** 그 볼트로 엮인 도메인의
// 기록이 통째로 안 써지는데 겉으로는 아무 일도 안 난다.
func TestSettingsWarnsOnMissingVaultDir(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	body := strings.ReplaceAll(settingsFixture, "%VAULT%", filepath.Join(dir, "없는자리"))
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, err := runSettingsCmd(t, "settings", "--json", "--config", cfg)
	if err != nil {
		t.Fatal(err)
	}
	var s settingsOut
	if jerr := json.Unmarshal([]byte(out), &s); jerr != nil {
		t.Fatal(jerr)
	}
	if s.Vaults[0].Exists {
		t.Error("없는 자리를 있다고 한다")
	}
	if len(s.Warnings) == 0 {
		t.Error("경고가 없다 — 조용히 넘어갔다")
	}
}
