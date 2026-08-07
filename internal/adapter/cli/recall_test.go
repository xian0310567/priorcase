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

	root := NewRootCmd()
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

	root := NewRootCmd()
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

	root := NewRootCmd()
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

// TestRecallCmdInjectStaysCleanWhenNotesSkipped 는 이번 수정의 가장 좁은
// 요구사항을 못 박는다: 건너뛴 노트가 있어도 `--format inject` 의 **stdout 은
// 오염되지 않는다**. 그 출력은 훅이 그대로 에이전트 컨텍스트에 밀어넣는 순수
// 데이터라, 경고가 한 줄이라도 섞이면 "[과거 결정 참조]" 블록이 망가진다.
//
// 동시에 침묵도 아니어야 한다 — 회수 대상에서 빠진 결정이 있다는 사실은
// stderr 로 나가야 한다. 두 요구를 한 테스트에서 같이 본다: 한쪽만 검사하면
// "경고를 아예 안 낸다" 와 "stdout 을 깨끗이 지킨다" 가 구별되지 않는다.
func TestRecallCmdInjectStaysCleanWhenNotesSkipped(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)
	rel := plantLegacyNote(t, c.Vault) // index_test.go 의 헬퍼

	root := NewRootCmd()
	buf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(errBuf)
	root.SetArgs([]string{"recall", "--config", cfgPath, "--format", "inject", "저장", "엔진을", "무엇으로", "골랐지"})

	if err := root.Execute(); err != nil {
		t.Fatalf("cb recall 실행 실패: %v", err)
	}

	out := buf.String()
	if !strings.HasPrefix(out, "[과거 결정 참조]\n") {
		t.Fatalf("inject 출력이 헤더로 시작하지 않는다:\n%s", out)
	}
	// 주입 블록은 "- <날짜> ..." 줄만 있어야 한다. 경고 문구가 새어 들어오면
	// 에이전트가 그걸 과거 결정으로 읽는다.
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if line == "[과거 결정 참조]" || strings.HasPrefix(line, "- ") {
			continue
		}
		t.Errorf("inject stdout 에 정체불명의 줄이 섞였다: %q\n전체:\n%s", line, out)
	}
	if strings.Contains(out, "건너뛰었다") || strings.Contains(out, "경고") || strings.Contains(out, rel) {
		t.Errorf("경고가 inject stdout 을 오염시켰다:\n%s", out)
	}

	// 그렇다고 침묵하지도 않는다.
	warn := errBuf.String()
	if !strings.Contains(warn, "읽지 못해 건너뛰었다") || !strings.Contains(warn, rel) {
		t.Errorf("회수에서 빠진 노트가 stderr 로도 안 나왔다:\n%s", warn)
	}
}

// TestRecallCmdHumanRevealsSkippedNotes 는 사람이 보는 기본 포맷에서도 같은
// 경고가 나오는지 본다. inject 만 챙기고 human 을 빠뜨리면 대화형으로 쓰는
// 사용자가 오히려 못 보게 된다.
func TestRecallCmdHumanRevealsSkippedNotes(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)
	rel := plantLegacyNote(t, c.Vault)

	root := NewRootCmd()
	buf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(errBuf)
	root.SetArgs([]string{"recall", "--config", cfgPath, "저장", "엔진을", "무엇으로", "골랐지"})

	if err := root.Execute(); err != nil {
		t.Fatalf("cb recall 실행 실패: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("정상 노트 회수까지 멈췄다")
	}
	if warn := errBuf.String(); !strings.Contains(warn, rel) {
		t.Errorf("human 포맷에서 건너뜀이 안 보인다:\n%s", warn)
	}
}
