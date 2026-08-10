package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/testutil"
)

// TestCaptureCmdWritesNoteAndShowsRelated 는 `prior capture` 가 결정 노트를 실제로
// 만들고, 출력에 "기록됨:" 과 편승된 관련 과거 결정이 나오는지 확인한다.
// 편승이 실제로 이어지는지 보려면 회수될 과거 결정이 볼트에 있어야 하는데,
// 픽스처 볼트의 alpha-결정-저장엔진-2026-08-01 이 그 역할을 한다.
func TestCaptureCmdWritesNoteAndShowsRelated(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{
		"capture", "--config", cfgPath,
		"--domain", "alpha", "--slug", "저장 엔진 재검토",
		"--summary", "저장 엔진을 다시 본다", "--date", "2026-08-07",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("prior capture 실행 실패: %v", err)
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

			root := NewRootCmd()
			buf := &bytes.Buffer{}
			root.SetOut(buf)
			args := append([]string{"capture", "--config", cfgPath}, tc.args...)
			root.SetArgs(args)

			if err := root.Execute(); err == nil {
				t.Fatalf("%s 인데도 prior capture 가 성공했다", tc.name)
			}
		})
	}
}

// TestCaptureCmdRevealsSkippedNotes 는 `prior capture` 도 색인이 불완전해졌다는
// 사실을 알리는지 본다. capture 는 노트를 쓴 뒤 내부적으로 색인을 갱신하므로,
// 여기서 침묵하면 사용자가 `prior index` 를 따로 돌리기 전까지는 6건이 빠진 색인을
// 완전한 것으로 믿게 된다. 기록 자체는 성공해야 한다 — 남의 노트가 깨졌다고
// 내 기록이 실패하면 안 된다.
func TestCaptureCmdRevealsSkippedNotes(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)
	rel := plantLegacyNote(t, c.Vault) // index_test.go 의 헬퍼

	root := NewRootCmd()
	buf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(errBuf)
	root.SetArgs([]string{
		"capture", "--config", cfgPath,
		"--domain", "alpha", "--slug", "저장 엔진 재검토",
		"--summary", "저장 엔진을 다시 본다", "--date", "2026-08-07",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("깨진 노트 한 건 때문에 prior capture 가 죽으면 안 된다: %v", err)
	}
	if !strings.Contains(buf.String(), "기록됨:") {
		t.Errorf("기록이 안 됐다:\n%s", buf.String())
	}
	warn := errBuf.String()
	if !strings.Contains(warn, "읽지 못해 건너뛰었다") || !strings.Contains(warn, rel) {
		t.Errorf("색인이 불완전해진 사실이 안 나왔다:\n%s", warn)
	}
	// 같은 경고가 두 번 나가면 안 된다 — capture 는 Recall 과 index.Write 에서
	// 같은 목록을 두 번 받는다.
	if n := strings.Count(warn, "읽지 못해 건너뛰었다"); n != 1 {
		t.Errorf("경고가 %d번 나왔다, want 1:\n%s", n, warn)
	}
}
