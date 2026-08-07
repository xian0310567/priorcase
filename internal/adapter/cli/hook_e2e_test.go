package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/testutil"
)

// **훅은 무슨 일이 있어도 exit 0 이다.** 실패해서 대화가 막히면 사용자는 casebook 을
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
	broken := filepath.Join(c.Vault, "alpha", "decisions", "alpha-결정-깨짐-2026-01-01.md")
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
	for _, forbidden := range []string{"경고", "깨짐", "cb hook"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("stdout 에 %q 가 섞였다:\n%s", forbidden, got)
		}
	}
	if !strings.Contains(errb.String(), "깨짐") {
		t.Errorf("건너뛴 노트를 stderr 로도 안 알렸다:\n%s", errb.String())
	}
}
