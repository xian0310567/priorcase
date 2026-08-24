package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/testutil"
)

// fixtureStem 은 픽스처 볼트에 있는 결정 노트 하나의 stem 이다. review 대상.
const fixtureStem = "alpha-결정-저장엔진-2026-08-01"

// TestReviewCmdUpdatesOutcomeAndRetro 는 `prior review <stem> --outcome --retro`
// 가 노트를 실제로 갱신하고, 출력에 "갱신됨:" 이 나오는지 확인한다.
func TestReviewCmdUpdatesOutcomeAndRetro(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{
		"review", "--config", cfgPath, fixtureStem,
		"--outcome", "good", "--retro", "잘 됐다.",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("prior review 실행 실패: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "갱신됨:") {
		t.Errorf("출력에 '갱신됨:' 이 없다:\n%s", got)
	}
	if !strings.Contains(got, fixtureStem) {
		t.Errorf("출력에 대상 stem 이 없다:\n%s", got)
	}

	notePath := filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", fixtureStem+".md")
	data, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("노트 파일을 읽을 수 없다: %v", err)
	}
	if !strings.Contains(string(data), "outcome: good") {
		t.Errorf("파일에 outcome 갱신이 반영되지 않았다:\n%s", data)
	}
	if !strings.Contains(string(data), "잘 됐다.") {
		t.Errorf("파일에 회고가 반영되지 않았다:\n%s", data)
	}
}

// **review 는 --supersedes 없이 번복 이유를 남길 수 있는 유일한 경로다.** 측정으로
// 가정이 깨져 대체안 없이 그만두는 번복이 실제로 더 흔한데, capture 에는 그 자리가 없다.
// 실볼트 18노트 중 번복 사유가 기록된 것이 0건이었던 이유가 이것이다.
func TestReviewCmdRecordsSupersedeReasonWithoutSupersedes(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{
		"review", "--config", cfgPath, fixtureStem,
		"--outcome", "bad", "--status", "regretted",
		"--reason", "실측 23건 중 자동 기록이 0건이었다",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("prior review 실행 실패: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", fixtureStem+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "실측 23건 중 자동 기록이 0건이었다") {
		t.Errorf("번복 사유가 노트에 안 남았다:\n%s", data)
	}
}

// 사유만 오고 status 를 안 바꾸면 거부돼야 한다. 그냥 두면 "사유는 있는데 여전히
// active" 인 노트가 되어 회수가 감점 없이 만점으로 계속 올려 보낸다.
func TestReviewCmdRejectsReasonWithoutStatusChange(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{
		"review", "--config", cfgPath, fixtureStem, "--reason", "측정으로 깨졌다",
	})
	if err := root.Execute(); err == nil {
		t.Fatal("status 를 안 바꿨는데 --reason 만으로 통과했다")
	}
}

// summary 는 **회수가 주입하는 유일한 한 줄**이다. 결론이 뒤집혔는데 이 줄이 그대로면
// 회수가 계속 거짓말을 한다 — 볼트의 codecommit 노트가 실제로 그 상태였다.
// 옛 줄은 버리지 않고 summary_history 로 보존된다.
func TestReviewCmdCorrectsSummaryAndKeepsHistory(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{
		"review", "--config", cfgPath, fixtureStem,
		"--outcome", "bad", "--summary", "임베디드 DB 는 동시 쓰기에서 막혔다",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("prior review 실행 실패: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", fixtureStem+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "임베디드 DB 는 동시 쓰기에서 막혔다") {
		t.Errorf("새 summary 가 반영되지 않았다:\n%s", data)
	}
	if !strings.Contains(string(data), "summary_history") {
		t.Errorf("옛 summary 가 보존되지 않았다 — 폐기한 판단도 번복 기록의 절반이다:\n%s", data)
	}
}

// TestReviewCmdRejectsMissingStem 은 존재하지 않는 stem 을 주면 에러로
// 실패하는지 확인한다.
func TestReviewCmdRejectsMissingStem(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{
		"review", "--config", cfgPath, "alpha-결정-없는것-2026-01-01",
		"--outcome", "good",
	})

	if err := root.Execute(); err == nil {
		t.Fatal("없는 stem 인데도 prior review 가 성공했다")
	}
}
