package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// stubJudge 는 정해진 답만 내놓는 판별기다.
func stubJudge(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "judge")
	sh := "#!/bin/sh\ncat >/dev/null\ncat <<'J'\n" + body + "\nJ\n"
	if err := os.WriteFile(p, []byte(sh), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func promoteFixture(t *testing.T, judgeBody string) (*config.Config, *store.Layout, string) {
	t.Helper()
	c := testutil.VaultConfig(t)
	c.Capture.JudgePath = stubJudge(t, judgeBody)
	sd := t.TempDir()
	s := NewStore(sd)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPending(Pending{
		Path: "/t.jsonl", From: 0, Domain: "alpha", SessionID: "S1",
		Days: []string{"2026-08-09"}, Excerpt: "저장 엔진을 Postgres 로 정했다",
		At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	return c, store.NewLayout(c), sd
}

// ★★ **데몬이 승격할 수 있어야 한다.**
//
// 스펙 §9 는 훅 없는 호스트(Codex·Cursor 등)에서도 "놓친 기록 줍기 | 데몬 | 동일"
// 이라고 약속한다. 그런데 승격을 부르는 곳이 훅 하나뿐이었다 — 데몬의 drain 은
// Scan 만 하고 판별기를 안 불렀다. 그 호스트에서는 표시만 쌓이고 아무것도 안 남는다.
func TestDaemonCanPromoteWithoutHook(t *testing.T) {
	c, l, sd := promoteFixture(t,
		`{"record":true,"slug":"저장엔진","summary":"Postgres 로 간다","body":"## 결정\n\nx\n"}`)

	var out strings.Builder
	Promote(context.Background(), PromoteOptions{
		StateDir: sd, Config: c, Layout: l, Err: &out, Label: "prior watch"})

	m, _ := filepath.Glob(filepath.Join(c.Vault, "alpha", "decisions", "*저장엔진*"))
	if len(m) == 0 {
		t.Fatalf("데몬이 승격하지 못했다:\n%s", out.String())
	}
	if items, _ := ReadPending(sd); len(items) != 0 {
		t.Errorf("승격했는데 표시가 %d건 남았다", len(items))
	}
	// 원장이 남아야 한다 — 훅 경로와 같은 계약이다.
	proms, err := ReadPromotions(sd, time.Time{})
	if err != nil || len(proms) != 1 || !proms[0].Recorded {
		t.Errorf("원장이 안 남았다: %+v %v", proms, err)
	}
	if !strings.Contains(out.String(), "prior watch:") {
		t.Errorf("진행 보고에 라벨이 없다:\n%s", out.String())
	}
}

// 기각도 원장에 남아야 한다. "봤는데 기록할 게 없었다" 와 "아예 안 돌았다" 는 다르다.
func TestDaemonPromoteRecordsRejection(t *testing.T) {
	c, l, sd := promoteFixture(t, `{"record":false,"reason":"진행 보고일 뿐이다"}`)
	Promote(context.Background(), PromoteOptions{StateDir: sd, Config: c, Layout: l})

	proms, err := ReadPromotions(sd, time.Time{})
	if err != nil || len(proms) != 1 {
		t.Fatalf("원장 %+v %v", proms, err)
	}
	if proms[0].Recorded || proms[0].Reason == "" {
		t.Errorf("기각이 이유와 함께 안 남았다: %+v", proms[0])
	}
	if items, _ := ReadPending(sd); len(items) != 0 {
		t.Error("기각한 구간을 안 지웠다 — 매 세션 다시 묻게 된다")
	}
}

// 판별기가 없으면 아무것도 하지 않는다. 그게 판별기 없는 설치의 정상 동작이다.
func TestPromoteWithoutJudgeIsSilent(t *testing.T) {
	c := testutil.VaultConfig(t)
	c.Capture.JudgePath = "/nonexistent"
	sd := t.TempDir()
	var out strings.Builder
	Promote(context.Background(), PromoteOptions{
		StateDir: sd, Config: c, Layout: store.NewLayout(c), Err: &out})
	if out.String() != "" {
		t.Errorf("판별기가 없는데 뭔가 말했다: %q", out.String())
	}
}

// 재귀 차단 — 판별기가 띄운 세션에서는 승격하지 않는다.
func TestPromoteStopsInJudgeSession(t *testing.T) {
	t.Setenv("PRIORCASE_JUDGE", "1")
	c, l, sd := promoteFixture(t, `{"record":true,"slug":"x","summary":"y","body":"z"}`)
	Promote(context.Background(), PromoteOptions{StateDir: sd, Config: c, Layout: l})
	if proms, _ := ReadPromotions(sd, time.Time{}); len(proms) != 0 {
		t.Error("판별기 세션에서 승격했다 — 판별기가 판별기를 부른다")
	}
}

// First 는 순서만 바꾸고 버리지 않는다.
func TestOwnFirstReordersWithoutLoss(t *testing.T) {
	mine := Pending{Path: "/mine.jsonl"}
	other := Pending{Path: "/other.jsonl"}
	got := ownFirst([]Pending{other, mine, other}, "/mine.jsonl")
	if len(got) != 3 || got[0].Path != "/mine.jsonl" {
		t.Errorf("순서가 틀렸거나 잃었다: %v", got)
	}
	if got := ownFirst([]Pending{{Path: "/a"}, {Path: "/b"}}, ""); got[0].Path != "/a" {
		t.Errorf("빈 First 인데 순서가 바뀌었다: %v", got)
	}
}

// ★★ `prior watch` 가 실제로 승격해야 한다. Promote 함수가 있는 것과 데몬이 그걸
// 부르는 것은 다르다 — 함수만 테스트하면 호출부를 떼어내도 안 잡힌다.
func TestWatchPromotesInSteadyState(t *testing.T) {
	c := testutil.VaultConfig(t)
	c.Capture = config.Capture{Signals: []string{"결정"}, MinTurns: 2, QuiesceSeconds: 1}
	c.Capture.JudgePath = stubJudge(t,
		`{"record":true,"slug":"데몬승격","summary":"데몬이 만들었다","body":"## 결정\n\nx\n"}`)

	root := t.TempDir()
	proj := filepath.Join(root, "p")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	tp := filepath.Join(proj, "s.jsonl")
	var lines []string
	for i := 0; i < 6; i++ {
		lines = append(lines, `{"type":"assistant","cwd":"/tmp/proj/alpha","sessionId":"S9","timestamp":"2026-08-09T01:00:0`+
			string(rune('0'+i))+`Z","message":{"role":"assistant","content":[{"type":"text","text":"여기서 결정했다"}]}}`)
	}
	if err := os.WriteFile(tp, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sd := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	done := make(chan error, 1)
	promoted := make(chan struct{}, 1)
	go func() {
		done <- Run(ctx, Options{
			StateDir: sd, TranscriptRoot: root, Config: c,
			// Backfill 은 끈다 — 켜면 기동 패스가 다 훑고 전진해서, 그 뒤에 덧붙인
			// 구간이 임계를 못 채운다. 여기서 보려는 것은 **정상 감시 루프**다.
			Quiesce: 200 * time.Millisecond,
			OnEvent: func(e Event) {
				if e.Kind == "promote" && strings.Contains(e.Note, "자동 기록") {
					select {
					case promoted <- struct{}{}:
					default:
					}
				}
			},
		})
	}()

	// 감시가 붙은 뒤에 파일을 건드려 정상 drain 을 유발한다.
	time.Sleep(700 * time.Millisecond)
	f, err := os.OpenFile(tp, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	// 임계를 넘길 만큼 덧붙인다. 기동 시 현재 지점으로 시딩됐으므로 여기부터가 새 구간이다.
	for i := 0; i < 6; i++ {
		f.WriteString(lines[i] + "\n")
	}
	f.Close()

	select {
	case <-promoted:
	case <-time.After(20 * time.Second):
		t.Fatal("데몬이 승격하지 않았다 — drain 이 Promote 를 안 부른다")
	}
	cancel()
	<-done

	m, _ := filepath.Glob(filepath.Join(c.Vault, "alpha", "decisions", "*데몬승격*"))
	if len(m) == 0 {
		t.Error("노트가 안 만들어졌다")
	}
	if proms, _ := ReadPromotions(sd, time.Time{}); len(proms) == 0 {
		t.Error("원장이 안 남았다")
	}
}
