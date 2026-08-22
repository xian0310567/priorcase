package cli_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/testutil"
)

// **훅은 무슨 일이 있어도 exit 0 이다.** 실패해서 대화가 막히면 사용자는 priorcase 을
// 고치는 게 아니라 지운다.
//
// 이건 프로세스 수준으로만 검증할 수 있다 — cobra 가 RunE 의 에러를 종료 코드로
// 옮기므로, in-process 테스트는 그 배선을 못 본다.
func TestHookAlwaysExitsZero(t *testing.T) {
	bin := buildCB(t)
	cfgPath, _ := testutil.VaultConfigFile(t)
	missing := filepath.Join(t.TempDir(), "없는설정.toml")

	cases := []struct {
		name  string
		cfg   string
		event string
		stdin string
	}{
		{"정상 회수", cfgPath, "user-prompt-submit", `{"cwd":"/tmp/proj/alpha","prompt":"저장 엔진을 무엇으로 할지"}`},
		{"세션 진입", cfgPath, "session-start", `{"cwd":"/tmp/proj/alpha","source":"startup"}`},
		{"설정 파일 없음", missing, "session-start", `{"cwd":"/tmp/proj/alpha"}`},
		{"깨진 JSON", cfgPath, "user-prompt-submit", `이건 JSON 이 아니다`},
		{"빈 입력", cfgPath, "user-prompt-submit", ``},
		{"알 수 없는 이벤트", cfgPath, "존재하지않는이벤트", `{}`},
		{"transcript 없음", cfgPath, "stop", `{"cwd":"/tmp/proj/alpha"}`},
		{"이상한 필드 타입", cfgPath, "stop", `{"cwd":123,"stop_hook_active":"네"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin, "--config", tc.cfg, "hook", tc.event)
			cmd.Stdin = strings.NewReader(tc.stdin)
			var out, errb strings.Builder
			cmd.Stdout, cmd.Stderr = &out, &errb
			err := cmd.Run()
			if err != nil {
				t.Errorf("종료 코드가 0이 아니다 (%v)\nstdout: %q\nstderr: %q", err, out.String(), errb.String())
			}
		})
	}
}

// 실패했을 때 **stdout 은 반드시 비어 있어야 한다.** user-prompt-submit 의 stdout 은
// 그대로 컨텍스트로 주입되므로, 에러 메시지가 새면 모델이 그걸 과거 결정으로 읽는다.
func TestHookNeverLeaksErrorsToStdout(t *testing.T) {
	bin := buildCB(t)
	missing := filepath.Join(t.TempDir(), "없는설정.toml")

	for _, ev := range []string{"user-prompt-submit", "session-start", "stop"} {
		t.Run(ev, func(t *testing.T) {
			cmd := exec.Command(bin, "--config", missing, "hook", ev)
			cmd.Stdin = strings.NewReader(`{"cwd":"/tmp/proj/alpha","prompt":"저장 엔진 이야기"}`)
			var out, errb strings.Builder
			cmd.Stdout, cmd.Stderr = &out, &errb
			if err := cmd.Run(); err != nil {
				t.Fatalf("exit 0 이어야 한다: %v", err)
			}
			if out.String() != "" {
				t.Errorf("설정이 없는데 stdout 에 무언가 나왔다 — 컨텍스트가 오염된다: %q", out.String())
			}
			if errb.String() == "" {
				t.Error("실패했는데 stderr 도 조용하다 — 진단할 방법이 없다")
			}
		})
	}
}

// 정상 회수의 stdout 은 주입 블록 **그것뿐**이어야 한다.
func TestHookRecallStdoutIsOnlyTheBlock(t *testing.T) {
	bin := buildCB(t)
	cfgPath, c := testutil.VaultConfigFile(t)
	broken := filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-깨짐-2026-01-01.md")
	if err := os.WriteFile(broken, []byte("---\ntitle: 구 스키마\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "--config", cfgPath, "hook", "user-prompt-submit")
	cmd.Stdin = strings.NewReader(`{"cwd":"/tmp/proj/alpha","prompt":"저장 엔진을 무엇으로 할지 다시 보자"}`)
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("exit 0 이어야 한다: %v\n%s", err, errb.String())
	}

	got := out.String()
	if !strings.HasPrefix(got, "[과거 결정 참조]\n") {
		t.Errorf("주입 블록으로 시작하지 않는다:\n%q", got)
	}
	for _, forbidden := range []string{"경고", "깨짐", "prior hook"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("stdout 에 %q 가 섞였다:\n%s", forbidden, got)
		}
	}
	if !strings.Contains(errb.String(), "깨짐") {
		t.Errorf("건너뛴 노트를 stderr 로도 안 알렸다:\n%s", errb.String())
	}
}

// **`--host` 는 사람이 타이핑하는 플래그가 아니다** — `prior init` 이 설정 파일에
// 써 넣는다. 그래도 프로세스 수준으로 검증한다: 이 배선이 끊기면 Codex 에서 주입이
// 통째로 사라지는데, 훅은 exit 0 이라 아무 표시도 안 난다.
func TestHookHostFlagSelectsCodexEnvelope(t *testing.T) {
	bin := buildCB(t)
	cfgPath, _ := testutil.VaultConfigFile(t)

	run := func(args ...string) (string, string, error) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Stdin = strings.NewReader(`{"cwd":"/tmp/proj/alpha","source":"startup"}`)
		var out, errb strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		return out.String(), errb.String(), err
	}

	codexOut, _, err := run("--config", cfgPath, "hook", "--host", "codex", "session-start")
	if err != nil {
		t.Fatalf("종료 코드가 0이 아니다: %v", err)
	}
	if !strings.Contains(codexOut, `"additionalContext"`) {
		t.Errorf("Codex 봉투가 아니다:\n%s", codexOut)
	}

	plainOut, _, err := run("--config", cfgPath, "hook", "session-start")
	if err != nil {
		t.Fatalf("종료 코드가 0이 아니다: %v", err)
	}
	if strings.Contains(plainOut, `"additionalContext"`) {
		t.Errorf("기본값이 Claude Code 가 아니다:\n%s", plainOut)
	}

	// 오타는 조용히 기본값으로 떨어지지 않는다. 대화는 안 막되(exit 0) 이유는 남긴다.
	out, errOut, err := run("--config", cfgPath, "hook", "--host", "codx", "session-start")
	if err != nil {
		t.Errorf("모르는 호스트에서 종료 코드가 0이 아니다: %v", err)
	}
	if !strings.Contains(errOut, "호스트") {
		t.Errorf("모르는 호스트인데 stderr 에 아무 말이 없다:\n%s", errOut)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("모르는 호스트인데 컨텍스트를 냈다 — 어느 형식인지 모르는 채로 내면 안 된다:\n%s", out)
	}
}

// splitArgs 는 따옴표만 아는 최소 분해기다 — **셸이 아니다.**
// Codex 가 셸 없이 spawn 할 때 argv 가 어떻게 되는지를 흉내 낸다.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQ = !inQ
		case r == ' ' && !inQ:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// ★ **훅 명령은 셸 없이도 돌아야 한다.**
//
// Codex 가 훅 명령을 `sh -c` 로 돌리는지, 따옴표만 쪼개서 직접 spawn 하는지는
// 바이너리 문자열로 확정할 수 없었다. 후자라면 `PRIORCASE_HOOK=1 ...` 의 환경변수
// 접두어가 **argv[0] 로 잡혀 훅 4개가 통째로 죽는다** — 그리고 조용하다.
//
// 그래서 어느 쪽이든 도는 형태여야 한다. 이 테스트는 셸이 없는 쪽을 시험한다.
func TestCodexHookCommandRunsWithoutShell(t *testing.T) {
	bin := buildCB(t)
	cfgPath, _ := testutil.VaultConfigFile(t)
	hooksPath := filepath.Join(t.TempDir(), "hooks.json")

	initCmd := exec.Command(bin, "--config", cfgPath, "init", "--host", "codex",
		"--settings", hooksPath, "--binary", bin, "--apply")
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("init 실패: %v\n%s", err, out)
	}

	raw, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string } `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	var command string
	for _, g := range root.Hooks["UserPromptSubmit"] {
		for _, h := range g.Hooks {
			if strings.Contains(h.Command, "PRIORCASE_HOOK") {
				command = h.Command
			}
		}
	}
	if command == "" {
		t.Fatalf("UserPromptSubmit 훅을 못 찾았다:\n%s", raw)
	}

	argv := splitArgs(command)
	c := exec.Command(argv[0], argv[1:]...)
	c.Env = append(os.Environ(), "PRIORCASE_CONFIG="+cfgPath)
	c.Stdin = strings.NewReader(`{"cwd":"/tmp/proj/alpha","prompt":"저장 엔진을 무엇으로 할지"}`)
	var out, errb strings.Builder
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		t.Fatalf("셸 없이 실행하니 죽었다 (%v)\n명령: %s\nargv: %q\nstderr: %s",
			err, command, argv, errb.String())
	}
	if !strings.Contains(out.String(), `"additionalContext"`) {
		t.Errorf("셸 없이 돌았지만 주입이 안 나왔다:\nstdout: %s\nstderr: %s", out.String(), errb.String())
	}
}
