package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/config"
)

// root 는 --config 를 물려주는 최소 루트다 (cmd/cb 와 같은 모양).
func root(t *testing.T, args ...string) (*cobra.Command, *strings.Builder) {
	t.Helper()
	r := &cobra.Command{Use: "cb", SilenceUsage: true, SilenceErrors: true}
	r.PersistentFlags().String("config", "", "")
	r.AddCommand(NewInitCommand())
	var out strings.Builder
	r.SetOut(&out)
	r.SetErr(&out)
	r.SetArgs(args)
	return r, &out
}

// ★ **생성한 설정 파일이 실제로 읽히는지.**
//
// 이게 깨지면 새 사용자의 첫 경험이 "설치했는데 안 됨" 이다. TOML 을 문자열로 만들고
// 있어서 오타 하나면 조용히 무효한 파일이 생기는데, 그 조합은 우리가 직접 검사하지
// 않으면 아무도 안 본다 — config.Load 가 DisallowUnknownFields 라 더 예민하다.
func TestStarterConfigIsLoadable(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	vault := filepath.Join(dir, "볼트")

	if err := writeStarterConfig(cfgPath, vault); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(cfgPath)
	if err != nil {
		b, _ := os.ReadFile(cfgPath)
		t.Fatalf("만든 설정을 스스로 읽지 못한다: %v\n---\n%s", err, b)
	}
	if c.Vault != vault {
		t.Errorf("Vault = %q, want %q", c.Vault, vault)
	}
	if len(c.Capture.Signals) == 0 {
		t.Error("signals 가 비었다 — 데몬이 아무것도 표시하지 않는다")
	}
	if c.Naming.DecisionFile == "" || c.Naming.DecisionsDir == "" ||
		c.Naming.Worklog == "" || c.Naming.Index == "" {
		t.Errorf("[naming] 네 키가 다 있어야 한다: %+v", c.Naming)
	}
}

// 이미 있는 설정은 **절대 안 건드린다.** 사용자의 도메인 매핑을 덮어쓰면 되돌리기 어렵다.
func TestStarterConfigNeverOverwrites(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	const mine = "vault = \"/내가/쓴/볼트\"\n"
	if err := os.WriteFile(p, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeStarterConfig(p, "/다른/볼트"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != mine {
		t.Errorf("기존 설정을 덮어썼다:\n%s", got)
	}
}

// --apply 없이는 아무것도 안 바뀐다. cb init 의 가장 중요한 안전장치다.
func TestInitCommandWithoutApplyChangesNothing(t *testing.T) {
	sp := writeSettings(t, realisticSettings)
	before, _ := os.ReadFile(sp)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	r, out := root(t, "init", "--settings", sp, "--config", cfgPath, "--binary", "/usr/local/bin/cb")
	if err := r.Execute(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(sp)
	if string(after) != string(before) {
		t.Error("--apply 가 없는데 설정이 바뀌었다")
	}
	if _, err := os.Stat(cfgPath); err == nil {
		t.Error("--apply 가 없는데 설정 파일을 만들었다")
	}
	if !strings.Contains(out.String(), "--apply") {
		t.Errorf("어떻게 적용하는지 안 알려 준다:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "걷어낼 훅") {
		t.Errorf("계획을 안 보여 준다:\n%s", out.String())
	}
}

func TestInitCommandApplyAndRevert(t *testing.T) {
	sp := writeSettings(t, realisticSettings)
	before, _ := os.ReadFile(sp)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	r, out := root(t, "init", "--settings", sp, "--config", cfgPath,
		"--binary", "/usr/local/bin/cb", "--apply")
	if err := r.Execute(); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if after, _ := os.ReadFile(sp); string(after) == string(before) {
		t.Fatal("--apply 인데 안 바뀌었다")
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("설정 파일을 안 만들었다: %v", err)
	}

	r2, out2 := root(t, "init", "--settings", sp, "--revert")
	if err := r2.Execute(); err != nil {
		t.Fatalf("%v\n%s", err, out2.String())
	}
	if after, _ := os.ReadFile(sp); string(after) != string(before) {
		t.Error("revert 후 원본과 다르다")
	}
}

// 백업이 없는데 revert 하면 시끄럽게 실패해야 한다 — 조용히 성공하면
// 사용자는 되돌아간 줄 안다.
func TestRevertWithoutBackupFails(t *testing.T) {
	sp := writeSettings(t, realisticSettings)
	r, _ := root(t, "init", "--settings", sp, "--revert")
	if err := r.Execute(); err == nil {
		t.Error("백업이 없는데 성공했다고 한다")
	}
}
