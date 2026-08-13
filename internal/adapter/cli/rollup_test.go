package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// listWeeks 는 손으로만 확인하고 있었다. 여기 로직은 네 갈래(요약 필요·이미 요약됨·
// 진행 중·내용 부족)를 가르고 각각 **이유를 보여 준다** — 목록에서 조용히 빠지면
// 왜 요약이 안 되는지 알 수 없다.
func listOut(t *testing.T, body string, now time.Time) string {
	t.Helper()
	c := testutil.VaultConfig(t)
	c.Naming.Rollup = "98-{project}-요약.md"
	writeWorklog(t, c.DefaultVaultPath(), "alpha", body)

	var b strings.Builder
	if err := listWeeks(&b, store.NewLayout(c), now); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestListWeeksShowsEachReason(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC) // 2026-W32
	body := day(t, "2026-07-20", "지난주") +              // W30 — 요약 필요
		"## 2026-07-27 — 짧음\n\n- 한 줄\n\n" + // W31 — 내용 부족
		day(t, "2026-08-08", "이번주") // W32 — 진행 중

	got := listOut(t, body, now)
	for _, want := range []string{
		"2026-W30", "요약 필요",
		"2026-W31", "내용 부족",
		"2026-W32", "진행 중인 주",
		"prior rollup <프로젝트> <주>", // 다음에 무엇을 하라는지
		"에이전트가 한다",                // 요약문은 priorcase 가 안 만든다
	} {
		if !strings.Contains(got, want) {
			t.Errorf("목록에 %q 가 없다:\n%s", want, got)
		}
	}
}

// 요약할 주가 없으면 그렇다고 말한다 — 빈 출력은 "고장" 으로 읽힌다.
func TestListWeeksSaysWhenNothingToDo(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	got := listOut(t, day(t, "2026-08-08", "이번주만"), now)
	if !strings.Contains(got, "요약할 주가 없다") {
		t.Errorf("할 일이 없다는 말이 없다:\n%s", got)
	}
	if strings.Contains(got, "요약 필요") {
		t.Errorf("진행 중인 주를 요약 대상으로 셌다:\n%s", got)
	}
}

// 날짜 헤딩이 없는 로그는 요약할 것이 없다.
func TestListWeeksOnLogWithoutDates(t *testing.T) {
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	got := listOut(t, "# 작업 로그\n\n아직 아무것도 없다.\n", now)
	if !strings.Contains(got, "날짜 헤딩이 없다") {
		t.Errorf("이유를 안 알려 준다:\n%s", got)
	}
}

// day 는 minBlockBytes(100) 를 넘는 하루치 블록을 만든다.
func day(t *testing.T, date, text string) string {
	t.Helper()
	return "## " + date + " — " + text + "\n\n**한 일**\n\n- " +
		strings.Repeat(text+" 자세한 내용 ", 10) + "\n\n"
}

func writeWorklog(t *testing.T, vault, folder, body string) {
	t.Helper()
	dir := filepath.Join(vault, folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "99-"+folder+"-작업-로그.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 요약문은 에이전트가 파이프로 넣는다 — `-` 가 표준입력이라는 규약이 깨지면
// prior 가 "-" 라는 이름의 파일을 찾다 실패한다.
func TestReadBodyFromStdinAndFile(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("파이프로 들어온 요약문"))
	got, err := readBody(cmd, "-")
	if err != nil || got != "파이프로 들어온 요약문" {
		t.Fatalf("표준입력: %q, %v", got, err)
	}

	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte("파일에서 온 요약문"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := readBody(&cobra.Command{}, path); err != nil || got != "파일에서 온 요약문" {
		t.Fatalf("파일: %q, %v", got, err)
	}

	if _, err := readBody(&cobra.Command{}, filepath.Join(t.TempDir(), "없다")); err == nil {
		t.Error("없는 파일을 빈 요약문으로 삼켰다 — 빈 주가 붙는다")
	}
}
