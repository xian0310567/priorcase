package daemon

import (
	"strings"
	"testing"
	"time"
)

func runPending(t *testing.T, stateDir string, args ...string) (string, error) {
	t.Helper()
	cmd := NewPendingCommand()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"--state-dir", stateDir}, args...))
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
