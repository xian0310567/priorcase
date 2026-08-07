package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/xian0310567/casebook/internal/testutil"
)

// syncBuf 는 exec.Cmd 의 출력 고루틴과 테스트 고루틴이 함께 쓰는 버퍼다.
// strings.Builder 를 그냥 주면 Write 와 String 이 겹쳐 -race 가 잡는다 —
// 실제로 이 테스트를 그렇게 처음 썼다가 걸렸다.
type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// waitOutput 은 출력에 문자열이 나타날 때까지 기다린다.
func waitOutput(buf *syncBuf, want string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), want) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// cb watch 는 장기 실행 프로세스다. **Ctrl-C 에 스스로 빠져나와야 한다.**
//
// root.Execute() (ExecuteContext 가 아닌) 를 쓰면 cmd.Context() 가
// context.Background() 라서 데몬의 ctx.Done() 이 영원히 오지 않는다. 그러면 신호가
// 프로세스를 정리 없이 죽이고, 종료 코드도 정상 종료가 아니게 된다.
//
// 이건 in-process 로는 검증할 수 없다 — 신호와 종료 코드가 걸린 문제라 실제 프로세스여야 한다.
func TestWatchExitsCleanlyOnSignal(t *testing.T) {
	bin := buildCB(t)
	cfgPath, _ := testutil.VaultConfigFile(t)

	root := filepath.Join(t.TempDir(), "projects", "proj-a")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()

	cmd := exec.Command(bin, "--config", cfgPath, "watch",
		"--state-dir", stateDir, "--transcript-root", filepath.Dir(root))
	var out syncBuf
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if !waitOutput(&out, "감시 시작", 5*time.Second) {
		_ = cmd.Process.Kill()
		t.Fatalf("데몬이 뜨지 않았다:\n%s", out.String())
	}

	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("SIGINT 후 종료 코드가 정상이 아니다: %v\n%s", err, out.String())
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("SIGINT 를 받고도 5초 안에 끝나지 않았다 — ctx 가 닫히지 않는다")
	}
}

// 두 번째 인스턴스는 락에서 즉시 죽되, **왜 죽었는지 말해야 한다** (감사 결함 3).
func TestSecondWatchProcessSaysWhy(t *testing.T) {
	bin := buildCB(t)
	cfgPath, _ := testutil.VaultConfigFile(t)
	root := filepath.Join(t.TempDir(), "projects", "proj-a")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	args := []string{"--config", cfgPath, "watch", "--state-dir", stateDir,
		"--transcript-root", filepath.Dir(root)}

	first := exec.Command(bin, args...)
	var out syncBuf
	first.Stdout, first.Stderr = &out, &out
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = first.Process.Signal(syscall.SIGINT)
		_ = first.Wait()
	})
	if !waitOutput(&out, "감시 시작", 5*time.Second) {
		_ = first.Process.Kill()
		t.Fatalf("첫 인스턴스가 뜨지 않았다:\n%s", out.String())
	}

	second, err := exec.Command(bin, args...).CombinedOutput()
	if err == nil {
		t.Fatal("두 번째 인스턴스가 그냥 떴다 — 같은 구간을 중복 처리한다")
	}
	if !strings.Contains(string(second), "이미 돌고 있다") {
		t.Errorf("왜 죽었는지 말하지 않는다:\n%s", second)
	}
	if !strings.Contains(string(second), stateDir) {
		t.Errorf("어느 상태 디렉토리인지 말하지 않는다:\n%s", second)
	}
}
