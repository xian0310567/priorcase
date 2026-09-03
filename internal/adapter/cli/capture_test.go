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

	notePath := filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-저장-엔진-재검토-2026-08-07.md")
	data, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("결정 노트 파일이 생기지 않았다 (%s): %v", notePath, err)
	}
	if !strings.Contains(string(data), `summary: "저장 엔진을 다시 본다"`) {
		t.Errorf("노트 frontmatter 에 summary 가 없다:\n%s", data)
	}
}

// **--supersedes 만으로는 "왜" 가 안 남는다.** 옛 노트에는 status 와 링크만 찍히고,
// 실볼트 18노트 중 번복 사유가 기록된 것은 0건이었다. --reason 은 그 이유가 뒤집히는
// **옛 노트**에 붙는지까지 본다 — 사유는 옛 결정의 성질이지 새 결정의 성질이 아니다.
func TestCaptureCmdWritesSupersedeReasonOntoOldNote(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{
		"capture", "--config", cfgPath,
		"--domain", "alpha", "--slug", "저장 엔진 교체",
		"--summary", "저장 엔진을 서버 DB 로 바꾼다", "--date", "2026-08-07",
		"--supersedes", "alpha-결정-저장엔진-2026-08-01",
		"--reason", "동시 쓰기 3프로세스에서 락 경합으로 막혔다",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("prior capture 실행 실패: %v", err)
	}

	old, err := os.ReadFile(filepath.Join(c.DefaultVaultPath(), "alpha", "decisions",
		"alpha-결정-저장엔진-2026-08-01.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(old), "동시 쓰기 3프로세스에서 락 경합으로 막혔다") {
		t.Errorf("번복 사유가 뒤집힌 옛 노트에 안 남았다:\n%s", old)
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
	rel := plantLegacyNote(t, c.DefaultVaultPath()) // index_test.go 의 헬퍼

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

// ── 기록 뒤에 링크를 걸게 만든다 ──────────────────────────────────────
//
// # 고치려는 고장 (2026-09-02 실측)
//
// 볼트 결정 668건 중 **291건(43.6%)이 고아**다 — 어느 노트와도 안 이어져 있다.
// 그리고 링크 작성률이 **떨어지고 있다**: 2026-08 53.8% → 2026-09 29.8%.
//
// 원인은 이 자리다. `capture` 는 관련 과거 결정을 **이미 찾는다**(요약+슬러그로
// 회수 top-3). 그런데 **화면에 출력만 하고 아무 일도 시키지 않는다.** `related` 를
// 채우려면 사람이 `--related` 를 손으로 넣어야 하는데, 후보를 본 그 순간에
// 무엇을 하라는 말이 없으니 아무도 안 넣는다.
//
// 고칠 수단은 이미 있었다 — `prior review <stem> --related` 가 기존 값을 지우지 않고
// 덧붙인다. 없던 것은 **그것을 쓰라는 말 한 줄**이다.
//
// # 왜 자동으로 안 박는가
//
// 회수 품질이 중간이다(2026-09-02 실측: 사람이 이어 둔 노트를 어휘 회수가 상위3에
// 넣는 것이 32.3%). 자동으로 박으면 틀린 링크가 굳고, 그건 나중에 링크를 회수에
// 쓰기 시작할 때 그대로 오염이 된다. **후보를 주고 호스트가 판정한다.**
func TestCaptureTellsHostToLinkWhenRelatedEmpty(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{
		"capture", "--config", cfgPath,
		"--domain", "alpha", "--slug", "저장 엔진 재검토",
		"--summary", "저장 엔진을 다시 본다", "--date", "2026-08-07",
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	// 무엇을 하라는 말이 있어야 한다. 후보 목록만으로는 아무도 안 움직였다.
	if !strings.Contains(got, "prior review") || !strings.Contains(got, "--related") {
		t.Errorf("링크를 거는 방법을 안 알려준다:\n%s", got)
	}
	// **그 노트의 stem 이 명령에 박혀 있어야 한다.** 사람이 조립하게 두면 안 한다.
	if !strings.Contains(got, "alpha-결정-저장-엔진-재검토-2026-08-07") {
		t.Errorf("명령에 방금 쓴 노트의 stem 이 없다:\n%s", got)
	}
	// 후보를 판정하라고 해야 한다 — 자동으로 박는 것이 아니다.
	if !strings.Contains(got, "읽고") {
		t.Errorf("후보를 판정하라는 말이 없다:\n%s", got)
	}
}

// **이미 걸었으면 잔소리하지 않는다.** 매번 뜨는 안내는 며칠이면 안 읽힌다.
func TestCaptureStaysQuietWhenRelatedGiven(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{
		"capture", "--config", cfgPath,
		"--domain", "alpha", "--slug", "저장 엔진 재검토",
		"--summary", "저장 엔진을 다시 본다", "--date", "2026-08-07",
		"--related", "alpha-결정-저장엔진-2026-08-01",
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); strings.Contains(got, "prior review") {
		t.Errorf("이미 related 를 걸었는데 링크를 걸라고 한다:\n%s", got)
	}
}
