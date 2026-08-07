package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/testutil"
)

// TestCaptureCmdWritesNoteAndShowsRelated 는 `cb capture` 가 결정 노트를 실제로
// 만들고, 출력에 "기록됨:" 과 편승된 관련 과거 결정이 나오는지 확인한다.
// 편승이 실제로 이어지는지 보려면 회수될 과거 결정이 볼트에 있어야 하는데,
// 픽스처 볼트의 alpha-결정-저장엔진-2026-08-01 이 그 역할을 한다.
func TestCaptureCmdWritesNoteAndShowsRelated(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)

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

	notePath := filepath.Join(c.Vault, "alpha", "decisions", "alpha-결정-저장-엔진-재검토-2026-08-07.md")
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
			cfgPath, _ := testutil.VaultConfigFile(t)

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
