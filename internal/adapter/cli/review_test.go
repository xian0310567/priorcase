package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// review 서브커맨드용 최소 설정. capture_test.go 의 captureCmdConfigTemplate 과
// 같은 방식으로 TOML 을 직접 쓴다 — testutil.VaultConfig 는 config.Config
// 구조체를 직접 주지 파일을 주지 않기 때문이다.
const reviewCmdConfigTemplate = `
vault = %q

[naming]
decision_file = "{domain}-결정-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog = "99-{project}-작업-로그.md"
index = "_meta/00-결정-색인.md"

[[domain]]
prefix = "alpha"
folder = "alpha"
`

const reviewNoteFixture = `---
type: decision
date: 2026-08-06
domain: [alpha]
summary: "저장 엔진을 임베디드 DB 로 고른다"
status: active
outcome: pending
supersedes: ""
related: []
tags: [decision, alpha, 저장엔진]
source_session: ""
---

## 결정

임베디드 DB 를 쓴다.
`

// writeReviewCmdFixture 는 결정 노트 1건이 든 볼트와 그걸 가리키는 설정
// 파일을 t.TempDir() 아래에 만들고 (설정 파일 경로, 노트 stem) 을 준다.
func writeReviewCmdFixture(t *testing.T) (cfgPath, vault, stem string) {
	t.Helper()
	root := t.TempDir()
	vault = filepath.Join(root, "vault")
	stem = "alpha-결정-저장엔진-2026-08-06"

	decisionsDir := filepath.Join(vault, "alpha", "decisions")
	if err := os.MkdirAll(decisionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(decisionsDir, stem+".md")
	if err := os.WriteFile(notePath, []byte(reviewNoteFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath = filepath.Join(root, "config.toml")
	body := strings.ReplaceAll(reviewCmdConfigTemplate, "%q", `"`+vault+`"`)
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath, vault, stem
}

// TestReviewCmdUpdatesOutcomeAndRetro 는 `cb review <stem> --outcome --retro`
// 가 노트를 실제로 갱신하고, 출력에 "갱신됨:" 이 나오는지 확인한다.
func TestReviewCmdUpdatesOutcomeAndRetro(t *testing.T) {
	cfgPath, vault, stem := writeReviewCmdFixture(t)

	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{
		"review", "--config", cfgPath, stem,
		"--outcome", "good", "--retro", "잘 됐다.",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("cb review 실행 실패: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "갱신됨:") {
		t.Errorf("출력에 '갱신됨:' 이 없다:\n%s", got)
	}
	if !strings.Contains(got, stem) {
		t.Errorf("출력에 대상 stem 이 없다:\n%s", got)
	}

	notePath := filepath.Join(vault, "alpha", "decisions", stem+".md")
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
	cfgPath, _, _ := writeReviewCmdFixture(t)

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
