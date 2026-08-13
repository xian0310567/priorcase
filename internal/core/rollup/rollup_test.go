package rollup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// now 는 테스트 기준 시각. 2026-08-08 은 2026-W32 다.
var now = time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

func layout(t *testing.T) (*store.Layout, string) {
	t.Helper()
	c := testutil.VaultConfig(t)
	c.Naming.Rollup = "98-{project}-작업-로그-요약.md"
	return store.NewLayout(c), c.DefaultVaultPath()
}

func writeLog(t *testing.T, vault, folder, body string) {
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

// 넉넉한 분량의 하루치 블록 (minBlockBytes 를 넘긴다).
func day(date, text string) string {
	return "## " + date + " — " + text + "\n\n**한 일**\n\n- " +
		strings.Repeat(text+" ", 12) + "\n\n**다음 액션**\n\n- [ ] 계속\n\n"
}

func TestScanFindsWeeks(t *testing.T) {
	l, vault := layout(t)
	writeLog(t, vault, "alpha",
		"# 작업 로그\n\n"+day("2026-07-20", "지난주 일")+day("2026-07-21", "이어서")+
			day("2026-07-27", "저번주 일")+day("2026-08-08", "이번주 일"))

	weeks, err := Scan(l, now)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Week{}
	for _, w := range weeks {
		got[w.Week] = w
	}
	if len(got) != 3 {
		t.Fatalf("주 %d개 (%v), 3개여야 한다", len(got), keys(got))
	}
	// 같은 주의 두 날짜가 한 블록으로 묶여야 한다.
	if !strings.Contains(mustBlock(t, l, "alpha", "2026-W30"), "2026-07-21") {
		t.Error("같은 주의 두 날짜가 한 블록에 안 들어갔다")
	}
	if !got["2026-W32"].Current {
		t.Error("이번 주를 진행 중으로 표시하지 않았다")
	}
	if got["2026-W32"].Todo() {
		t.Error("진행 중인 주를 요약 대상으로 잡았다 — 주가 끝나기 전에 요약하면 반쪽이 된다")
	}
	if !got["2026-W30"].Todo() {
		t.Error("지난 주가 요약 대상이 아니다")
	}
}

func keys(m map[string]Week) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func mustBlock(t *testing.T, l *store.Layout, prefix, week string) string {
	t.Helper()
	b, err := Block(l, prefix, week)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// 내용이 적은 주는 건너뛴다 — 한 줄짜리를 요약하면 원본보다 길어진다.
func TestShortWeekIsSkipped(t *testing.T) {
	l, vault := layout(t)
	writeLog(t, vault, "alpha", "## 2026-07-20 — 짧음\n\n- 한 줄\n")
	weeks, err := Scan(l, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(weeks) != 1 {
		t.Fatalf("주 %d개", len(weeks))
	}
	if !weeks[0].Short || weeks[0].Todo() {
		t.Errorf("짧은 주를 요약 대상으로 잡았다: %+v", weeks[0])
	}
}

// 이미 요약된 주는 다시 하지 않는다. 몇 번 돌려도 안전해야 한다.
func TestAlreadySummarizedIsSkipped(t *testing.T) {
	l, vault := layout(t)
	writeLog(t, vault, "alpha", day("2026-07-20", "지난주 일"))
	if _, err := Append(l, "alpha", "2026-W30", "**한 주 요약**: 뭔가 했다."); err != nil {
		t.Fatal(err)
	}
	weeks, err := Scan(l, now)
	if err != nil {
		t.Fatal(err)
	}
	if !weeks[0].Done || weeks[0].Todo() {
		t.Errorf("이미 요약된 주를 다시 잡았다: %+v", weeks[0])
	}
}

// **덮어쓰지 않는다.** 덮어쓰면 앞서 쓴 요약이 조용히 사라진다.
func TestAppendRefusesDuplicate(t *testing.T) {
	l, vault := layout(t)
	writeLog(t, vault, "alpha", day("2026-07-20", "지난주 일"))
	if _, err := Append(l, "alpha", "2026-W30", "첫 요약"); err != nil {
		t.Fatal(err)
	}
	_, err := Append(l, "alpha", "2026-W30", "두 번째 요약")
	if err == nil {
		t.Fatal("같은 주를 두 번 붙였다 — 앞의 요약이 사라진다")
	}
	body, _ := os.ReadFile(filepath.Join(vault, "alpha", "98-alpha-작업-로그-요약.md"))
	if !strings.Contains(string(body), "첫 요약") {
		t.Error("첫 요약이 사라졌다")
	}
	if strings.Contains(string(body), "두 번째") {
		t.Error("거부했는데 써졌다")
	}
}

// **원본 작업 로그는 손대지 않는다.**
func TestAppendNeverTouchesTheLog(t *testing.T) {
	l, vault := layout(t)
	body := day("2026-07-20", "지난주 일")
	writeLog(t, vault, "alpha", body)
	logPath := filepath.Join(vault, "alpha", "99-alpha-작업-로그.md")
	before, _ := os.ReadFile(logPath)

	if _, err := Append(l, "alpha", "2026-W30", "요약"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(logPath)
	if string(before) != string(after) {
		t.Error("원본 작업 로그가 바뀌었다")
	}
}

// naming.rollup 이 없으면 **조용히 기본값을 쓰지 않고** 무엇을 적을지 알려 준다.
func TestMissingRollupNamingIsLoud(t *testing.T) {
	c := testutil.VaultConfig(t) // Rollup 비어 있음
	l := store.NewLayout(c)
	_, err := Append(l, "alpha", "2026-W30", "요약")
	if err == nil {
		t.Fatal("naming.rollup 이 없는데 성공했다")
	}
	if !strings.Contains(err.Error(), "rollup") || !strings.Contains(err.Error(), "98-") {
		t.Errorf("무엇을 적어야 하는지 안 알려 준다: %v", err)
	}
}

// 실제 날짜가 아닌 헤딩은 조용히 넘기지 않는다.
func TestImpossibleDateIsReported(t *testing.T) {
	l, vault := layout(t)
	writeLog(t, vault, "alpha", "## 2026-02-31 — 없는 날\n\n- 뭔가\n")
	if _, err := Scan(l, now); err == nil {
		t.Error("2026-02-31 을 조용히 받아들였다")
	}
}

// 작업 로그가 없는 도메인은 정상이다 — 아직 아무 일도 안 한 프로젝트다.
func TestMissingLogIsNotAnError(t *testing.T) {
	l, _ := layout(t)
	weeks, err := Scan(l, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(weeks) != 0 {
		t.Errorf("주 %d개, 0개여야 한다", len(weeks))
	}
}

// ★ 마지막 날짜 뒤에 오는 절이 그 주 블록에 삼켜지면 안 된다.
//
// 실볼트의 mesh 작업 로그가 `## 관련 문서` 로 끝나는데, 날짜 헤딩만 경계로 쓰면
// 그 절이 통째로 마지막 주에 들어간다. 어느 주의 작업도 아니다.
func TestTrailingSectionIsNotSwallowed(t *testing.T) {
	l, vault := layout(t)
	writeLog(t, vault, "alpha",
		day("2026-07-20", "지난주 일")+"## 관련 문서\n\n- [[어딘가]]\n- [[또 어딘가]]\n")

	block := mustBlock(t, l, "alpha", "2026-W30")
	if strings.Contains(block, "관련 문서") {
		t.Errorf("날짜가 아닌 절이 주 블록에 들어갔다:\n%s", block)
	}
	if !strings.Contains(block, "지난주 일") {
		t.Errorf("정작 그 주의 내용이 없다:\n%s", block)
	}
}

// 날짜 사이에 낀 다른 절도 그 앞 날짜에 붙지 않는다.
func TestSectionBetweenDaysIsNotAttached(t *testing.T) {
	l, vault := layout(t)
	writeLog(t, vault, "alpha",
		day("2026-07-20", "월요일 일")+"## 메모\n\n끼어든 절\n\n"+day("2026-07-27", "다음주 일"))

	if b := mustBlock(t, l, "alpha", "2026-W30"); strings.Contains(b, "끼어든 절") {
		t.Errorf("끼어든 절이 앞 주에 붙었다:\n%s", b)
	}
	if b := mustBlock(t, l, "alpha", "2026-W31"); strings.Contains(b, "끼어든 절") {
		t.Errorf("끼어든 절이 뒷 주에 붙었다:\n%s", b)
	}
}

// 하위 절(###)은 날짜 블록 안에 남아야 한다.
func TestSubsectionsStayInTheDay(t *testing.T) {
	l, vault := layout(t)
	writeLog(t, vault, "alpha",
		"## 2026-07-20 — 월요일\n\n### 세부\n\n"+strings.Repeat("내용 ", 40)+"\n")
	if b := mustBlock(t, l, "alpha", "2026-W30"); !strings.Contains(b, "### 세부") {
		t.Errorf("하위 절이 잘려 나갔다:\n%s", b)
	}
}
