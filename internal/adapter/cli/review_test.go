package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/testutil"
)

// fixtureStem 은 픽스처 볼트에 있는 결정 노트 하나의 stem 이다. review 대상.
const fixtureStem = "alpha-결정-저장엔진-2026-08-01"

// TestReviewCmdUpdatesOutcomeAndRetro 는 `cb review <stem> --outcome --retro`
// 가 노트를 실제로 갱신하고, 출력에 "갱신됨:" 이 나오는지 확인한다.
func TestReviewCmdUpdatesOutcomeAndRetro(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)

	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{
		"review", "--config", cfgPath, fixtureStem,
		"--outcome", "good", "--retro", "잘 됐다.",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("cb review 실행 실패: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "갱신됨:") {
		t.Errorf("출력에 '갱신됨:' 이 없다:\n%s", got)
	}
	if !strings.Contains(got, fixtureStem) {
		t.Errorf("출력에 대상 stem 이 없다:\n%s", got)
	}

	notePath := filepath.Join(c.Vault, "alpha", "decisions", fixtureStem+".md")
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

// TestReviewCmdRejectsMissingStem 은 존재하지 않는 stem 을 주면 에러로
// 실패하는지 확인한다.
func TestReviewCmdRejectsMissingStem(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)

	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{
		"review", "--config", cfgPath, "alpha-결정-없는것-2026-01-01",
		"--outcome", "good",
	})

	if err := root.Execute(); err == nil {
		t.Fatal("없는 stem 인데도 cb review 가 성공했다")
	}
}

// TestReviewCmdRevealsSkippedNotes 는 `cb review` 도 색인이 불완전해졌다는 사실을
// 알리는지 본다 — review 역시 갱신 뒤 내부적으로 색인을 다시 쓴다.
func TestReviewCmdRevealsSkippedNotes(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)
	rel := plantLegacyNote(t, c.Vault) // index_test.go 의 헬퍼

	root := newRootCmd()
	buf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(errBuf)
	root.SetArgs([]string{"review", "--config", cfgPath, fixtureStem, "--outcome", "good"})

	if err := root.Execute(); err != nil {
		t.Fatalf("깨진 노트 한 건 때문에 cb review 가 죽으면 안 된다: %v", err)
	}
	if !strings.Contains(buf.String(), "갱신됨:") {
		t.Errorf("갱신이 안 됐다:\n%s", buf.String())
	}
	if warn := errBuf.String(); !strings.Contains(warn, rel) {
		t.Errorf("색인이 불완전해진 사실이 안 나왔다:\n%s", warn)
	}
}
