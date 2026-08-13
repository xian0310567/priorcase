package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// ★★ 앱이 노트를 열려면 절대 경로가 필요하다.
//
// queue --json 의 review[].path 는 볼트 상대 경로다. 앱이 볼트 절대 경로를
// 따로 계산하면 **볼트 선택 규칙이 둘이 된다** — 다중 볼트에서 그 어긋남은
// 앱이 엉뚱한 파일을 열거나 못 여는 것으로 나타난다.
func TestPathCmdPrintsAbsolutePath(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)

	out, err := runPath(t, cfgPath, "alpha-결정-저장엔진-2026-08-01")
	if err != nil {
		t.Fatalf("실행 실패: %v", err)
	}
	got := strings.TrimSpace(out)
	if !filepath.IsAbs(got) {
		t.Errorf("절대 경로가 아니다: %q", got)
	}
	if !strings.HasPrefix(got, c.DefaultVaultPath()) {
		t.Errorf("볼트 밖을 가리킨다: %q (볼트 %q)", got, c.DefaultVaultPath())
	}
	if !strings.HasSuffix(got, ".md") {
		t.Errorf(".md 가 아니다: %q", got)
	}
}

// ★ 규약에 안 맞는 stem 은 시끄럽게 실패해야 한다.
func TestPathCmdFailsOnUnknownStem(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)
	if _, err := runPath(t, cfgPath, "없는-결정-x-2026-01-01"); err == nil {
		t.Fatal("없는 stem 인데 통과했다")
	}
}

// ★★★ **없는 노트는 실패해야 한다 — 경로를 조립해 주면 안 된다.**
//
// store.ResolveStem 은 경로를 만들 뿐 파일이 있는지 보지 않는다. 그것만
// 쓰면 접두어만 맞으면 무엇이든 그럴듯한 절대 경로가 나오고, 앱은 그걸
// 그대로 OS 에 넘긴다. 그러면 사람은 **아무 일도 안 일어나는 버튼**을 누르게
// 되고, 원인이 "노트가 없다" 인지 "앱이 못 연다" 인지 구별할 수 없다.
//
// 위 TestPathCmdFailsOnUnknownStem 은 이걸 못 잡는다 — 거기서는 접두어
// "없는" 이 화이트리스트에 없어서 다른 이유로 실패하기 때문이다.
func TestPathCmdFailsOnMissingNote(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)
	out, err := runPath(t, cfgPath, "alpha-결정-존재하지않는것-2026-01-01")
	if err == nil {
		t.Fatalf("없는 노트인데 통과했다 (출력 %q)", strings.TrimSpace(out))
	}
	if !strings.Contains(err.Error(), "없다") {
		t.Errorf("무엇이 문제인지 안 말한다: %v", err)
	}
}

// ★★★ **다중 볼트가 이 명령의 존재 이유다.**
//
// stem 의 도메인이 볼트를 정한다. cwd 나 기본 볼트로 풀면 다른 프로젝트의
// 노트를 못 열거나 **엉뚱한 볼트의 같은 이름 파일**을 연다. 그 어긋남은
// 조용하다 — 열리기는 열리니까.
func TestPathCmdPicksVaultFromStemDomain(t *testing.T) {
	cfgPath, vaults := twoVaultConfigFile(t)

	for _, tc := range []struct {
		stem  string
		vault string
	}{
		{"alpha-결정-저장엔진-2026-08-01", vaults["one"]},
		{"beta-결정-배포전략-2026-08-03", vaults["two"]},
	} {
		out, err := runPath(t, cfgPath, tc.stem)
		if err != nil {
			t.Fatalf("%s: 실행 실패: %v", tc.stem, err)
		}
		got := strings.TrimSpace(out)
		if !strings.HasPrefix(got, tc.vault) {
			t.Errorf("%s → %q, 볼트 %q 안이어야 한다", tc.stem, got, tc.vault)
		}
	}
}

// ★★ **이 명령은 새 외부 입구다.** stem 은 LLM 이나 앱에서 온다.
//
// 볼트 루트에는 CLAUDE.md 같은 지침 문서가 바로 있어서, 경로 조립을 검사 없이
// 하면 ../CLAUDE 류가 그것을 가리킨다 (감사에서 도달 가능함이 확인된 적 있다).
// store 층이 막고 있지만 **입구마다 확인한다** — 여기서 뚫리면 앱이 그 경로를
// 그대로 OS 에 넘긴다.
func TestPathCmdRejectsTraversal(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)
	for _, bad := range []string{
		"../CLAUDE",
		"alpha-결정-../../CLAUDE-2026-01-01",
		"/etc/passwd",
		"alpha-결정-x-2026-01-01/../../../CLAUDE",
	} {
		out, err := runPath(t, cfgPath, bad)
		if err == nil {
			t.Errorf("%q 가 통과했다 → %q", bad, strings.TrimSpace(out))
		}
	}
}

// ★ 출력은 **한 줄, 경로만** 이어야 한다. 앱이 그대로 OS 에 넘긴다 — 장식이
// 섞이면 파일을 못 연다.
func TestPathCmdOutputIsBarePath(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)
	out, err := runPath(t, cfgPath, "alpha-결정-저장엔진-2026-08-01")
	if err != nil {
		t.Fatalf("실행 실패: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("한 줄이 아니다 (%d줄): %q", len(lines), out)
	}
	if strings.TrimSpace(lines[0]) != lines[0] {
		t.Errorf("앞뒤 공백이 붙었다: %q", lines[0])
	}
}

// ★★ 인자 개수를 막아야 한다.
//
// 안 막으면 인자 0개일 때 args[0] 이 **패닉**한다. 그리고 인자가 둘일 때는
// 조용히 첫째만 쓴다 — 사람이 오타로 두 개를 넘기면 무엇이 무시됐는지 모른다.
func TestPathCmdRequiresExactlyOneArg(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)
	for _, args := range [][]string{
		{},
		{"alpha-결정-저장엔진-2026-08-01", "alpha-결정-스키마-2026-08-02"},
	} {
		cmd := newPathCmd()
		root := &cobra.Command{Use: "prior"}
		root.PersistentFlags().String("config", "", "")
		root.AddCommand(cmd)
		var out, errb strings.Builder
		root.SetOut(&out)
		root.SetErr(&errb)
		root.SetArgs(append(append([]string{"path"}, args...), "--config", cfgPath))
		if err := root.Execute(); err == nil {
			t.Errorf("인자 %d개인데 통과했다 → %q", len(args), strings.TrimSpace(out.String()))
		}
	}
}

// twoVaultConfigFile 은 볼트 둘짜리 설정 파일을 만든다.
//
// testutil.VaultConfigFile 은 볼트 하나짜리라 다중 볼트를 못 본다. 여기서만
// 쓰므로 지역에 둔다 — 공용으로 올리는 것은 두 번째 사용처가 생길 때다.
func twoVaultConfigFile(t *testing.T) (cfgPath string, vaults map[string]string) {
	t.Helper()
	one, two := t.TempDir(), t.TempDir()
	for _, d := range []struct{ root, prefix, stem string }{
		{one, "alpha", "alpha-결정-저장엔진-2026-08-01"},
		{two, "beta", "beta-결정-배포전략-2026-08-03"},
	} {
		dir := filepath.Join(d.root, d.prefix, "decisions")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\ntype: decision\ndate: 2026-08-01\ndomain: [" + d.prefix + "]\nsummary: \"x\"\n---\n\n## 결정\n"
		if err := os.WriteFile(filepath.Join(dir, d.stem+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &config.Config{
		Vaults:        []config.Vault{{Name: "one", Path: one}, {Name: "two", Path: two}},
		DefaultDomain: "alpha",
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-작업-로그.md",
			Index:        "_meta/00-결정-색인.md",
		},
		Domain: []config.Domain{
			{Prefix: "alpha", Folder: "alpha", Vault: "one"},
			{Prefix: "beta", Folder: "beta", Vault: "two"},
		},
	}
	body, err := toml.Marshal(c)
	if err != nil {
		t.Fatalf("설정을 TOML 로 쓸 수 없다: %v", err)
	}
	// **볼트는 맨 뒤에 손으로 붙인다** — Config.Vaults 는 `toml:"-"` 이고,
	// [[vault]] 를 연 뒤의 맨 키는 전부 그 테이블로 빨려 들어간다.
	var tail bytes.Buffer
	for _, v := range c.Vaults {
		fmt.Fprintf(&tail, "\n[[vault]]\nname = %q\npath = %q\n", v.Name, v.Path)
	}
	cfgPath = filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, append(body, tail.Bytes()...), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath, map[string]string{"one": one, "two": two}
}

func runPath(t *testing.T, cfgPath, stem string) (string, error) {
	t.Helper()
	cmd := newPathCmd()
	root := &cobra.Command{Use: "prior"}
	root.PersistentFlags().String("config", "", "")
	root.AddCommand(cmd)
	var out, errb strings.Builder
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs([]string{"path", stem, "--config", cfgPath})
	err := root.Execute()
	return out.String(), err
}
