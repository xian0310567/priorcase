package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/config"
)

// root 는 --config 를 물려주는 최소 루트다 (cmd/prior 와 같은 모양).
func root(t *testing.T, args ...string) (*cobra.Command, *strings.Builder) {
	t.Helper()
	r := &cobra.Command{Use: "prior", SilenceUsage: true, SilenceErrors: true}
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
	if c.DefaultVaultPath() != vault {
		t.Errorf("Vault = %q, want %q", c.DefaultVaultPath(), vault)
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

// --apply 없이는 아무것도 안 바뀐다. prior init 의 가장 중요한 안전장치다.
func TestInitCommandWithoutApplyChangesNothing(t *testing.T) {
	sp := writeSettings(t, realisticSettings)
	before, _ := os.ReadFile(sp)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	r, out := root(t, "init", "--settings", sp, "--config", cfgPath, "--binary", "/usr/local/bin/prior")
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
		"--binary", "/usr/local/bin/prior", "--apply")
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

// ★ **새 사용자가 바로 쓸 수 있어야 한다.**
//
// 예전에는 [[domain]] 을 전부 주석으로 두었다. 그러면 도메인이 0개인 채로 시작하고
// 아무것도 기록되지 않는데 — 훅은 돌고 안전망은 표시까지 해서 **정상으로 보인다.**
// 실제로 그 상태를 재현해 확인했다.
func TestStarterConfigIsImmediatelyUsable(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	vault := filepath.Join(dir, "볼트")

	if err := writeStarterConfig(cfgPath, vault); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("만든 설정을 스스로 읽지 못한다: %v", err)
	}

	if len(c.Domain) == 0 {
		t.Fatal("도메인이 0개다 — 아무것도 기록되지 않는다")
	}
	if c.DefaultDomain == "" {
		t.Fatal("default_domain 이 없다 — 프로젝트 밖에서는 기록되지 않는다")
	}
	// 폴백이 실제로 동작해야 한다: 아무 경로에서나 도메인이 나와야 한다.
	if got := c.DomainForCwd("/어디에도/없는/경로"); got != c.DefaultDomain {
		t.Errorf("DomainForCwd = %q, 폴백 %q 여야 한다", got, c.DefaultDomain)
	}
	// 볼트가 실제로 있어야 한다 — 없는 경로를 가리키면 첫 실행부터 빨간불이다.
	if fi, err := os.Stat(c.DefaultVaultPath()); err != nil || !fi.IsDir() {
		t.Errorf("볼트를 안 만들었다: %v", err)
	}
	// rollup·judge 키가 있어야 prior rollup 과 자동 기록이 바로 된다.
	if c.Naming.Rollup == "" {
		t.Error("naming.rollup 이 없다 — prior rollup 이 실패한다")
	}
	if c.Capture.JudgeModel == "" {
		t.Error("judge_model 이 없다")
	}
}

// 영어 로케일이면 영어 설정을 만든다. **새 영어 사용자가 손댈 게 없어야 한다** —
// 한국어 템플릿을 주면 첫 결정부터 `{domain}-결정-...` 파일이 생긴다.
func TestStarterConfigFollowsLocale(t *testing.T) {
	for _, tc := range []struct {
		lang, wantLang, wantMarker string
	}{
		{"en_US.UTF-8", "en", "-decision-"},
		{"ko_KR.UTF-8", "ko", "-결정-"},
		{"", "en", "-decision-"}, // 로케일이 없으면 영어 — OSS 기본값
	} {
		t.Run(tc.lang, func(t *testing.T) {
			t.Setenv("LC_ALL", tc.lang)
			t.Setenv("LC_MESSAGES", "")
			t.Setenv("LANG", "")
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.toml")
			if err := writeStarterConfig(cfgPath, filepath.Join(dir, "v")); err != nil {
				t.Fatal(err)
			}
			c, err := config.Load(cfgPath)
			if err != nil {
				t.Fatalf("만든 설정을 못 읽는다: %v", err)
			}
			if c.Lang != tc.wantLang {
				t.Errorf("lang = %q, want %q", c.Lang, tc.wantLang)
			}
			if !strings.Contains(c.Naming.DecisionFile, tc.wantMarker) {
				t.Errorf("decision_file = %q, %q 를 담아야 한다", c.Naming.DecisionFile, tc.wantMarker)
			}
			// 어느 쪽이든 바로 쓸 수 있어야 한다.
			if c.DefaultDomain == "" || len(c.Domain) == 0 || len(c.Capture.Signals) == 0 {
				t.Errorf("바로 쓸 수 없는 설정이다: %+v", c)
			}
		})
	}
}
