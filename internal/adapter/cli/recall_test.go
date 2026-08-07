package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/testutil"
)

// TestRecallCmdInject 는 `cb recall --format inject <query>` 가 매칭을 찾으면
// [과거 결정 참조] 헤더로 시작하는 출력을 내는지 확인한다.
func TestRecallCmdInject(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)

	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"recall", "--config", cfgPath, "--format", "inject", "저장", "엔진을", "무엇으로", "골랐지"})

	if err := root.Execute(); err != nil {
		t.Fatalf("cb recall 실행 실패: %v", err)
	}

	got := buf.String()
	if !strings.HasPrefix(got, "[과거 결정 참조]\n") {
		t.Errorf("헤더가 없다:\n%s", got)
	}
	if !strings.Contains(got, "저장 엔진을 임베디드 DB 로 고른다") {
		t.Errorf("summary 가 없다:\n%s", got)
	}
}

// TestRecallCmdReportsUnreadableVault 는 결정 폴더를 못 읽을 때 `cb recall` 이
// 조용히 rc=0 으로 끝나지 않고 에러를 내는지 확인한다. 같은 l.List() 를 부르는
// `cb index` 는 이미 에러로 죽는다 — 두 명령의 에러 정책이 같아야 한다.
func TestRecallCmdReportsUnreadableVault(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root 는 디렉토리 퍼미션을 무시하므로 이 테스트가 성립하지 않는다")
	}
	cfgPath, c := testutil.VaultConfigFile(t)
	dir := filepath.Join(c.Vault, "alpha", "decisions")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"recall", "--config", cfgPath, "저장", "엔진을", "무엇으로", "골랐지"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("읽을 수 없는 볼트인데 cb recall 이 성공했다 (출력=%q)", buf.String())
	}
	if !strings.Contains(err.Error(), "결정 폴더를 읽을 수 없다") {
		t.Errorf("에러가 원인을 알려주지 않는다: %v", err)
	}
}

// TestRecallCmdNoMatch 는 무관한 프롬프트에 아무 출력도 없는지 확인한다.
func TestRecallCmdNoMatch(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)

	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"recall", "--config", cfgPath, "--format", "inject", "완전히", "무관한", "주제", "짜장면"})

	if err := root.Execute(); err != nil {
		t.Fatalf("cb recall 실행 실패: %v", err)
	}

	if got := buf.String(); got != "" {
		t.Errorf("무매칭인데 출력이 있다: %q", got)
	}
}
