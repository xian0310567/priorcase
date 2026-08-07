package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// index 서브커맨드용 최소 설정. testutil.VaultConfig 는 config.Config 구조체를
// 직접 주지 파일을 주지 않으므로, 그 구조체가 쓰는 것과 같은 naming 값을
// 참고해 TOML 로 직접 쓴다.
const indexCmdConfigTemplate = `
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

const noteFixture = `---
type: decision
date: 2026-08-06
domain: [alpha]
summary: "CLI 테스트용 결정"
status: active
outcome: pending
supersedes: ""
related: []
tags: [decision, alpha, cli]
source_session: ""
---

## 결정

cb index CLI 테스트.
`

// writeIndexCmdFixture 는 결정 노트 1건이 든 볼트와 그걸 가리키는 설정 파일을
// t.TempDir() 아래에 만들고 설정 파일 경로를 준다.
func writeIndexCmdFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	vault := filepath.Join(root, "vault")

	decisionsDir := filepath.Join(vault, "alpha", "decisions")
	if err := os.MkdirAll(decisionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(decisionsDir, "alpha-결정-cli테스트-2026-08-06.md")
	if err := os.WriteFile(notePath, []byte(noteFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(root, "config.toml")
	body := strings.ReplaceAll(indexCmdConfigTemplate, "%q", `"`+vault+`"`)
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

// TestIndexCmd 는 `cb index` 가 --config 로 지정된 설정을 읽어 색인을 실제로
// 만들고, "색인 N행 생성" 형식으로 행 수를 보고하는지 확인한다.
func TestIndexCmd(t *testing.T) {
	cfgPath := writeIndexCmdFixture(t)
	idxPath := filepath.Join(filepath.Dir(cfgPath), "vault", "_meta", "00-결정-색인.md")

	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"index", "--config", cfgPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("cb index 실행 실패: %v", err)
	}

	got := buf.String()
	if got != "색인 1행 생성\n" {
		t.Errorf("출력 = %q, want %q", got, "색인 1행 생성\n")
	}

	data, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("색인 파일이 생기지 않았다 (%s): %v", idxPath, err)
	}
	if !strings.Contains(string(data), "CLI 테스트용 결정") {
		t.Errorf("색인 내용에 summary 가 없다:\n%s", data)
	}
}
