package daemon

import (
	"regexp"
	"strings"

	"github.com/xian0310567/priorcase/internal/testutil"
	"testing"
	"time"
)

// runPending 은 **격리된 설정**으로 명령을 돌린다.
//
// `--config` 를 안 주면 `config.Load("")` 가 **사용자의 진짜 설정**을 잡고, 볼트 대조가
// 진짜 볼트를 읽는다. 그러면 테스트 결과가 그 사람의 볼트 내용에 따라 달라진다 —
// 실제로 그 상태로 한 번 깨졌다.
func runPending(t *testing.T, stateDir string, args ...string) (string, error) {
	t.Helper()
	cfgPath, _ := testutil.VaultConfigFile(t)
	cmd := NewPendingCommand()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"--state-dir", stateDir, "--config", cfgPath}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func seed(t *testing.T, dir string) Pending {
	t.Helper()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	p := Pending{
		SessionID: "S1", Path: "/t/a.jsonl", Domain: "alpha", Turns: 9,
		Signals: []string{"결정"}, Days: []string{"2026-08-08"},
		At:      time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC),
		Excerpt: strings.Repeat("에이전트: SQLite 로 하기로 결정했다. ", 40),
	}
	if err := s.AddPending(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// 훅 주입이 "prior pending 으로 지워라" 라고 안내한다. 그 명령이 실제로 있고,
// **id 를 보여 줘야** 지울 수 있다.
func TestPendingListsIDAndExcerpt(t *testing.T) {
	dir := t.TempDir()
	p := seed(t, dir)
	got, err := runPending(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{p.ID(), "alpha", "2026-08-08", "SQLite", "prior capture", "--resolve"} {
		if !strings.Contains(got, want) {
			t.Errorf("목록에 %q 가 없다:\n%s", want, got)
		}
	}
	// 발췌는 기본으로 잘린다 — 목록이 화면을 덮으면 안 읽는다.
	if !strings.Contains(got, "…") {
		t.Errorf("긴 발췌를 안 잘랐다:\n%s", got)
	}
}

// --full 은 자르지 않는다.
func TestPendingFullShowsWholeExcerpt(t *testing.T) {
	dir := t.TempDir()
	seed(t, dir)
	got, err := runPending(t, dir, "--full")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "…") {
		t.Errorf("--full 인데 잘랐다:\n%s", got)
	}
}

// 없으면 없다고 말한다 — 빈 출력은 고장으로 읽힌다.
func TestPendingSaysWhenEmpty(t *testing.T) {
	got, err := runPending(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "없다") {
		t.Errorf("없다는 말이 없다: %q", got)
	}
}

func TestPendingResolveRemoves(t *testing.T) {
	dir := t.TempDir()
	p := seed(t, dir)
	if _, err := runPending(t, dir, "--resolve", p.ID()); err != nil {
		t.Fatal(err)
	}
	items, err := ReadPending(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("지웠는데 %d건 남았다", len(items))
	}
}

// 없는 id 는 조용히 성공하면 안 된다 — 지운 줄 알고 넘어간다.
func TestPendingResolveRejectsBadID(t *testing.T) {
	if _, err := runPending(t, t.TempDir(), "--resolve", "형식이아님"); err == nil {
		t.Error("형식이 아닌 id 를 받아들였다")
	}
}

// ★★ **판별기는 중복 대조를 받는데 사람은 못 받았다.**
//
// 자동 승격은 그 도메인의 기존 결정 요약을 판별기에게 넘겨 중복을 거른다. 그런데
// 확인 큐에서 사람이 판정할 때는 아무 도움도 없어서 볼트를 맨눈으로 뒤져야 했다 —
// 실측에서 큐 7건 중 4건이 이 대조만으로 판정됐다.
//
// **판정하지는 않는다.** 점수와 함께 후보만 보여 준다. 회수는 언제나 무언가를
// 돌려주므로, 도구가 "이미 기록됨" 이라고 단정하면 그 단정이 틀렸을 때 사람이
// 확인을 건너뛴다.
func TestPendingShowsVaultComparison(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	// 픽스처 볼트에 "저장 엔진을 임베디드 DB 로 고른다" 노트가 있다.
	if err := s.AddPending(Pending{
		SessionID: "S1", Path: "/t/b.jsonl", From: 7, Domain: "alpha", Turns: 9,
		Signals: []string{"결정"}, Days: []string{"2026-08-08"},
		At:      time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC),
		Excerpt: "에이전트: 저장 엔진을 어느 것으로 할지 정했다. 임베디드 DB 로 간다.",
	}); err != nil {
		t.Fatal(err)
	}

	out, err := runPending(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "비슷한 기존 결정") {
		t.Fatalf("볼트 대조가 안 보인다 — 사람이 맨눈으로 뒤져야 한다:\n%s", out)
	}
	if !strings.Contains(out, "저장엔진") {
		t.Errorf("맞는 노트가 안 나왔다:\n%s", out)
	}
	// **점수가 있어야 상대 비교가 된다.** 절대 점수로는 판정할 수 없다는 것이
	// 실측 결론이므로, 사람이 1위와 2위를 견줄 재료를 줘야 한다.
	if !regexp.MustCompile(`\n\s+\d+\s+alpha-결정-저장엔진`).MatchString(out) {
		t.Errorf("점수가 안 붙었다:\n%s", out)
	}
	// **판정하지 않는다.**
	for _, banned := range []string{"이미 기록", "중복이다", "기록됨"} {
		if strings.Contains(out, banned) {
			t.Errorf("도구가 단정했다(%q) — 틀리면 사람이 확인을 건너뛴다:\n%s", banned, out)
		}
	}
}
