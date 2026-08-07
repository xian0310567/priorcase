package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// capture 서브커맨드용 최소 설정. index_test.go 의 indexCmdConfigTemplate,
// recall_test.go 의 recallCmdConfigTemplate 과 같은 방식으로 TOML 을 직접
// 쓴다 — testutil.VaultConfig 는 config.Config 구조체를 직접 주지 파일을
// 주지 않기 때문이다.
const captureCmdConfigTemplate = `
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

const captureNoteFixture = `---
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

// writeCaptureCmdFixture 는 결정 노트 1건이 든 볼트와 그걸 가리키는 설정
// 파일을 t.TempDir() 아래에 만들고 (설정 파일 경로, 볼트 경로) 를 준다.
// 노트 1건을 미리 심어두는 이유: capture 로 관련 슬러그를 기록했을 때
// 편승(관련 과거 결정)이 실제로 CLI 출력까지 이어지는지 확인하려면
// 회수될 과거 결정이 있어야 한다.
func writeCaptureCmdFixture(t *testing.T) (cfgPath, vault string) {
	t.Helper()
	root := t.TempDir()
	vault = filepath.Join(root, "vault")

	decisionsDir := filepath.Join(vault, "alpha", "decisions")
	if err := os.MkdirAll(decisionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(decisionsDir, "alpha-결정-저장엔진-2026-08-06.md")
	if err := os.WriteFile(notePath, []byte(captureNoteFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath = filepath.Join(root, "config.toml")
	body := strings.ReplaceAll(captureCmdConfigTemplate, "%q", `"`+vault+`"`)
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath, vault
}

// TestCaptureCmdWritesNoteAndShowsRelated 는 `cb capture` 가 결정 노트를 실제로
// 만들고, 출력에 "기록됨:" 과 편승된 관련 과거 결정이 나오는지 확인한다.
func TestCaptureCmdWritesNoteAndShowsRelated(t *testing.T) {
	cfgPath, vault := writeCaptureCmdFixture(t)

	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{
		"capture", "--config", cfgPath,
		"--domain", "alpha", "--slug", "저장 엔진 재검토",
		"--summary", "저장 엔진을 다시 본다", "--date", "2026-08-07",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("cb capture 실행 실패: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "기록됨:") {
		t.Errorf("출력에 '기록됨:' 이 없다:\n%s", got)
	}
	if !strings.Contains(got, "관련 과거 결정:") || !strings.Contains(got, "저장 엔진을 임베디드 DB 로 고른다") {
		t.Errorf("출력에 편승된 관련 과거 결정이 없다:\n%s", got)
	}

	notePath := filepath.Join(vault, "alpha", "decisions", "alpha-결정-저장-엔진-재검토-2026-08-07.md")
	data, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("결정 노트 파일이 생기지 않았다 (%s): %v", notePath, err)
	}
	if !strings.Contains(string(data), `summary: "저장 엔진을 다시 본다"`) {
		t.Errorf("노트 frontmatter 에 summary 가 없다:\n%s", data)
	}
}

// TestCaptureCmdRequiresFlags 는 필수 플래그(domain/slug/summary)가 빠지면
// 에러가 나는지 확인한다.
func TestCaptureCmdRequiresFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"domain 없음", []string{"--slug", "x", "--summary", "s"}},
		{"slug 없음", []string{"--domain", "alpha", "--summary", "s"}},
		{"summary 없음", []string{"--domain", "alpha", "--slug", "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfgPath, _ := writeCaptureCmdFixture(t)

			root := newRootCmd()
			buf := &bytes.Buffer{}
			root.SetOut(buf)
			args := append([]string{"capture", "--config", cfgPath}, tc.args...)
			root.SetArgs(args)

			if err := root.Execute(); err == nil {
				t.Fatalf("%s 인데도 cb capture 가 성공했다", tc.name)
			}
		})
	}
}
