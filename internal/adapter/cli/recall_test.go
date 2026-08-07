package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recall 서브커맨드용 최소 설정. index_test.go 의 indexCmdConfigTemplate 과
// 같은 방식으로 TOML 을 직접 쓴다 — testutil.VaultConfig 는 config.Config
// 구조체를 직접 주지 파일을 주지 않기 때문이다.
const recallCmdConfigTemplate = `
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

const recallNoteFixture = `---
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

// writeRecallCmdFixture 는 결정 노트 1건이 든 볼트와 그걸 가리키는 설정 파일을
// t.TempDir() 아래에 만들고 설정 파일 경로를 준다.
func writeRecallCmdFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	vault := filepath.Join(root, "vault")

	decisionsDir := filepath.Join(vault, "alpha", "decisions")
	if err := os.MkdirAll(decisionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(decisionsDir, "alpha-결정-저장엔진-2026-08-06.md")
	if err := os.WriteFile(notePath, []byte(recallNoteFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(root, "config.toml")
	body := strings.ReplaceAll(recallCmdConfigTemplate, "%q", `"`+vault+`"`)
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

// TestRecallCmdInject 는 `cb recall --format inject <query>` 가 매칭을 찾으면
// [과거 결정 참조] 헤더로 시작하는 출력을 내는지 확인한다.
func TestRecallCmdInject(t *testing.T) {
	cfgPath := writeRecallCmdFixture(t)

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

// TestRecallCmdNoMatch 는 무관한 프롬프트에 아무 출력도 없는지 확인한다.
func TestRecallCmdNoMatch(t *testing.T) {
	cfgPath := writeRecallCmdFixture(t)

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
