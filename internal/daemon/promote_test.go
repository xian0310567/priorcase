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

	m, _ := filepath.Glob(filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "*저장엔진*"))
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

	m, _ := filepath.Glob(filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "*데몬승격*"))
	if len(m) == 0 {
		t.Error("노트가 안 만들어졌다")
	}
	if proms, _ := ReadPromotions(sd, time.Time{}); len(proms) == 0 {
		t.Error("원장이 안 남았다")
	}
}

// slowJudge 는 답하기 전에 오래 자는 판별기다. 호출 **도중** 취소를 재현한다.
func slowJudge(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "judge")
	sh := "#!/bin/sh\ncat >/dev/null\nsleep 5\ncat <<'J'\n" + body + "\nJ\n"
	if err := os.WriteFile(p, []byte(sh), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// addPendings 는 구간을 n 개 심는다. 하나만 있으면 "무더기" 가 재현되지 않는다.
func addPendings(t *testing.T, sd string, n int) int {
	t.Helper()
	s := NewStore(sd)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		if err := s.AddPending(Pending{
			Path: "/t.jsonl", From: int64(i * 1000), Domain: "alpha", SessionID: "S1",
			Days: []string{"2026-08-09"}, Excerpt: "결정했다", At: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReadPending(sd)
	if err != nil {
		t.Fatal(err)
	}
	return len(got)
}

// ★★ **이미 취소된 채로 들어오면 한 건도 건드리지 않는다.**
//
// 루프 첫머리의 ctx 검사가 이걸 한다. 없으면 첫 구간을 붙잡아 실패로 기록한다 —
// 그리고 실패 **전에** ClaimPending 이 도장을 찍으므로, 아무 일도 안 한 구간이
// claimTTL(5분) 동안 건너뛰어진다. 미확인 구간이 안 줄어드는 이유가 그것이었다.
func TestAlreadyCancelledTouchesNothing(t *testing.T) {
	c, l, sd := promoteFixture(t, `{"record":false,"reason":"아니다"}`)
	n := addPendings(t, sd, 5)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var errBuf strings.Builder
	Promote(ctx, PromoteOptions{StateDir: sd, Config: c, Layout: l,
		Budget: 5 * time.Minute, Err: &errBuf})

	recs, err := ReadPromotions(sd, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Errorf("원장에 %d건이 남았다 — 이미 취소됐으면 한 건도 건드리면 안 된다", len(recs))
		for _, r := range recs {
			t.Logf("  %s err=%q", r.ID, r.Err)
		}
	}
	if got := errBuf.String(); !strings.Contains(got, "중단") {
		t.Errorf("보고가 중단을 말하지 않는다: %q", got)
	}
	after, err := ReadPending(sd)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != n {
		t.Errorf("구간이 %d → %d 로 줄었다 — 취소는 '결정이 아니다' 가 아니다", n, len(after))
	}

	// ★ **도장도 찍히면 안 된다.** 원장이 비어 있어도 ClaimPending 이 돌았으면
	// 그 구간은 claimTTL(5분) 동안 건너뛰어진다 — 아무 일도 안 했는데.
	// 이게 미확인 구간이 영원히 안 줄어들던 진짜 원인이다. 원장만 보면 안 보인다.
	for _, p := range after {
		if !p.ClaimedAt.IsZero() {
			t.Errorf("%s 에 도장이 찍혔다 (%s) — 이미 취소됐는데 집어 갔다. "+
				"그 구간은 5분간 아무도 못 본다", p.ID(), p.ClaimedAt.Format(time.RFC3339))
		}
	}
}

// ★★ **호출 도중에 취소되면 그건 판별기 실패가 아니다.**
//
// 그렇게 남기면 원장이 거짓말을 한다. 실측에서 이 구별이 없어 원장 62건 중 52건이
// "판별기 실행 실패" 로 보였고, 판별기가 고장 났다고 오진했다 — 실제로는 9.3초에
// 멀쩡히 답하고 있었다(상한 45초). 원장의 존재 이유가 바로 이 구별이다.
//
// 그리고 남은 구간으로 넘어가면 안 된다. 이미 취소됐으니 전부 같은 에러를 낸다.
func TestCancelDuringJudgeIsNotRecordedAsFailure(t *testing.T) {
	c, l, sd := promoteFixture(t, `{"record":false,"reason":"아니다"}`)
	c.Capture.JudgePath = slowJudge(t, `{"record":false,"reason":"아니다"}`)
	addPendings(t, sd, 5)

	// 루프 첫 검사는 통과시키고, 판별기가 자는 동안 취소한다.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	var errBuf strings.Builder
	Promote(ctx, PromoteOptions{StateDir: sd, Config: c, Layout: l,
		Budget: 5 * time.Minute, Err: &errBuf})

	recs, err := ReadPromotions(sd, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.Err != "" {
			t.Errorf("취소를 판별기 실패로 남겼다 (%s): %q — 판별기는 멀쩡했다", r.ID, r.Err)
		}
	}
	if len(recs) > 1 {
		t.Errorf("원장에 %d건이 남았다 — 취소 뒤 남은 구간까지 태웠다", len(recs))
	}
	if got := errBuf.String(); !strings.Contains(got, "중단") {
		t.Errorf("보고가 중단을 말하지 않는다: %q", got)
	}
}

// ★★ **Only 는 그 구간 하나만 건드려야 한다.**
//
// 앱의 [결정이다] 가 이걸 부른다. 필터가 안 먹으면 버튼 한 번에 큐 전체가 판별기로
// 넘어가고, 사람은 하나만 승인했다고 믿는다.
//
// 필터로 구현한 이유는 쓰기 경로를 하나로 유지하기 위해서다 — 집기·원장·크레딧·
// 해소가 전부 같은 루프 안에 있고, 한 건짜리 함수를 따로 만들면 그 부기가 두 벌이 된다.
func TestPromoteOnlyTouchesOneSegment(t *testing.T) {
	c, l, sd := promoteFixture(t, `{"record":false,"reason":"아니다"}`)
	addPendings(t, sd, 4)
	before, err := ReadPending(sd)
	if err != nil {
		t.Fatal(err)
	}
	target := before[1].ID()

	var seen []Promotion
	Promote(context.Background(), PromoteOptions{
		StateDir: sd, Config: c, Layout: l, Only: target,
		Budget: 5 * time.Minute, OnResult: func(p Promotion) { seen = append(seen, p) },
	})

	if len(seen) != 1 {
		t.Fatalf("결과가 %d건이다 — Only 인데 하나만 처리해야 한다", len(seen))
	}
	if seen[0].ID != target {
		t.Errorf("다른 구간을 처리했다: %s (지목: %s)", seen[0].ID, target)
	}
	// 나머지는 도장도 안 찍혀야 한다. 찍히면 claimTTL 동안 아무도 못 본다.
	after, err := ReadPending(sd)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range after {
		if p.ID() != target && !p.ClaimedAt.IsZero() {
			t.Errorf("%s 에 도장이 찍혔다 — 지목하지 않은 구간이다", p.ID())
		}
	}
}

// ★★ **없는 id 를 주면 조용히 끝내면 안 된다.**
//
// 앱이 버튼을 눌러 이걸 부르는데, 그 구간이 이미 해소됐거나 id 가 틀렸을 때
// 아무 일도 안 일어나면 사람은 승인이 된 줄 안다. 그리고 그 구간은 큐에 계속 남는다.
func TestPromoteUnknownIDReportsInsteadOfSilence(t *testing.T) {
	c, l, sd := promoteFixture(t, `{"record":false,"reason":"아니다"}`)
	addPendings(t, sd, 2)

	var errBuf strings.Builder
	var seen []Promotion
	Promote(context.Background(), PromoteOptions{
		StateDir: sd, Config: c, Layout: l, Only: "/없는/구간@999",
		Budget: 5 * time.Minute, Err: &errBuf,
		OnResult: func(p Promotion) { seen = append(seen, p) },
	})

	if len(seen) != 0 {
		t.Errorf("없는 id 인데 %d건을 처리했다", len(seen))
	}
	if got := errBuf.String(); !strings.Contains(got, "그런 구간이 없다") {
		t.Errorf("보고가 없다: %q — 앱이 버튼을 눌렀는데 아무 말도 없으면 된 줄 안다", got)
	}
}

// OnResult 는 세 갈래를 전부 준다 — 기록함·기록 안 함·실패. 호출자가 "결정이
// 아니라고 판정했다" 와 "판정을 못 했다" 를 구별할 수 있어야 한다.
func TestPromoteOnResultCarriesAllBranches(t *testing.T) {
	for _, c := range []struct {
		name, judge string
		check       func(*testing.T, Promotion)
	}{
		{"기록함", `{"record":true,"slug":"x","summary":"요약","body":"## 결정\n\nx"}`,
			func(t *testing.T, p Promotion) {
				if !p.Recorded || p.Path == "" {
					t.Errorf("기록했는데 %+v", p)
				}
			}},
		{"기록 안 함", `{"record":false,"reason":"진행 보고다"}`,
			func(t *testing.T, p Promotion) {
				if p.Recorded || p.Reason == "" {
					t.Errorf("판정 결과가 안 실렸다: %+v", p)
				}
			}},
		{"실패", `이건 JSON 이 아니다`,
			func(t *testing.T, p Promotion) {
				if p.Recorded || p.Err == "" {
					t.Errorf("실패가 안 실렸다: %+v", p)
				}
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg, l, sd := promoteFixture(t, c.judge)
			var seen []Promotion
			Promote(context.Background(), PromoteOptions{
				StateDir: sd, Config: cfg, Layout: l,
				Budget: 5 * time.Minute, OnResult: func(p Promotion) { seen = append(seen, p) },
			})
			if len(seen) == 0 {
				t.Fatal("OnResult 가 안 불렸다")
			}
			c.check(t, seen[0])
		})
	}
}

// ★★ **설정에 없는 도메인은 판별기를 부르기 전에 걸러야 한다.**
//
// 안 걸러내면 판별기를 부르고 나서 capture 가 "알 수 없는 도메인 접두어" 로
// 거부한다. 호출 한 번이 실측 10~45초이고 **예산이 있으므로 그 낭비가 다른 구간의
// 기회를 먹는다.** 그리고 그 구간은 다음에도 같은 일을 반복한다.
//
// 실제로 그 상태였다. 2026-08-12 개명(casebook → priorcase) 때 이미 표시돼 있던
// 구간의 도메인을 안 옮겨서, 옛 이름을 단 구간 7건이 큐에 남았다.
func TestPromoteSkipsUnknownDomainBeforeCallingJudge(t *testing.T) {
	c, l, sd := promoteFixture(t, `{"record":true,"slug":"x","summary":"요약","body":"## 결정\n\nx"}`)

	// 판별기가 불리면 흔적을 남기게 한다 — 안 불렸다는 것을 증명하려면 필요하다.
	mark := filepath.Join(t.TempDir(), "called")
	judge := filepath.Join(t.TempDir(), "judge")
	sh := "#!/bin/sh\ncat >/dev/null\ntouch " + mark + "\necho '{\"record\":false,\"reason\":\"x\"}'\n"
	if err := os.WriteFile(judge, []byte(sh), 0o755); err != nil {
		t.Fatal(err)
	}
	c.Capture.JudgePath = judge

	s := NewStore(sd)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPending(Pending{
		Path: "/t.jsonl", From: 777, Domain: "없는도메인", SessionID: "S1",
		Days: []string{"2026-08-09"}, Excerpt: "결정했다", At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	var errBuf strings.Builder
	var seen []Promotion
	Promote(context.Background(), PromoteOptions{
		StateDir: sd, Config: c, Layout: l, Only: "/t.jsonl@777",
		Budget: 5 * time.Minute, Err: &errBuf,
		OnResult: func(p Promotion) { seen = append(seen, p) },
	})

	if _, err := os.Stat(mark); err == nil {
		t.Error("판별기를 불렀다 — 설정에 없는 도메인이면 부르기 전에 걸러야 한다")
	}
	if len(seen) != 0 {
		t.Errorf("원장에 %d건이 남았다 — 판정한 적이 없다", len(seen))
	}
	got := errBuf.String()
	if !strings.Contains(got, "설정에 없는 도메인") {
		t.Errorf("왜 건너뛰었는지 안 알려 준다: %q", got)
	}
	if !strings.Contains(got, "없는도메인") {
		t.Errorf("어느 도메인인지 안 알려 준다: %q", got)
	}
	// **구간은 남아 있어야 한다.** 설정을 고치면 다시 처리할 수 있어야 하고,
	// 지우는 것은 사람의 판단이다.
	after, _ := ReadPending(sd)
	found := false
	for _, p := range after {
		if p.ID() == "/t.jsonl@777" {
			found = true
			if !p.ClaimedAt.IsZero() {
				t.Error("도장이 찍혔다 — 아무 일도 안 했는데 5분간 건너뛰어진다")
			}
		}
	}
	if !found {
		t.Error("구간이 사라졌다 — 건너뛴 것은 해소가 아니다")
	}
}

// ★★ **승격된 구간의 발췌는 원장에 남아야 한다.**
//
// 승격되면 ResolvePending 이 구간을 지운다. 그러면 판별기가 만든 노트를 **무엇을
// 보고 썼는지와 대조할 방법이 없다** — 감독 앱의 검토 화면이 바로 그 대조이고,
// 스펙이 그것을 "앱의 존재 이유에 가장 가까운 화면" 이라고 적었다.
//
// 2026-08-12 에 21건을 승격시키고 만들어진 노트를 검증하려다 실제로 막혔다.
//
// id 에 경로+오프셋이 있어 트랜스크립트를 다시 읽을 수는 있지만, 그건 호스트의
// 파일이라 지워질 수 있다. pending 에 발췌를 통째로 실은 이유가 그것이었는데
// 승격 뒤에는 그 자리가 비어 있었다.
func TestPromotionCarriesExcerptWhenSegmentDisappears(t *testing.T) {
	for _, c := range []struct {
		name, judge string
		wantExcerpt bool
		why         string
	}{
		{"기록함", `{"record":true,"slug":"x","summary":"요약","body":"## 결정\n\nx"}`,
			true, "노트를 대조하려면 필요하다"},
		{"기록 안 함", `{"record":false,"reason":"진행 보고다"}`,
			true, "구간이 해소되므로 발췌가 사라진다 — 판정이 옳았는지 나중에 못 본다"},
		{"판별기 실패", `이건 JSON 이 아니다`,
			false, "구간이 안 지워지고 다시 시도한다 — state.json 에 그대로 있다"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg, l, sd := promoteFixture(t, c.judge)
			var seen []Promotion
			Promote(context.Background(), PromoteOptions{
				StateDir: sd, Config: cfg, Layout: l,
				Budget: 5 * time.Minute, OnResult: func(p Promotion) { seen = append(seen, p) },
			})
			if len(seen) == 0 {
				t.Fatal("OnResult 가 안 불렸다")
			}
			got := seen[0].Excerpt != ""
			if got != c.wantExcerpt {
				t.Errorf("발췌 있음=%v, %v 여야 한다 — %s", got, c.wantExcerpt, c.why)
			}
			// 원장에도 실제로 들어갔는지 본다. OnResult 만 맞고 파일이 다르면
			// 앱은 못 읽는다.
			recs, err := ReadPromotions(sd, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if len(recs) == 0 {
				t.Fatal("원장이 비었다")
			}
			if (recs[0].Excerpt != "") != c.wantExcerpt {
				t.Errorf("원장의 발췌가 OnResult 와 다르다: %q", recs[0].Excerpt)
			}
		})
	}
}

// ★ **긴 발췌는 잘라야 한다.** 한 줄이 스캐너 상한을 넘으면 그 줄만이 아니라
// **그 뒤가 통째로 안 읽힌다** — 그리고 하필 그 순간이 진단이 가장 필요한 순간이다.
func TestPromotionExcerptIsTrimmed(t *testing.T) {
	c, l, sd := promoteFixture(t, `{"record":false,"reason":"x"}`)
	s := NewStore(sd)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("가", 9000)
	if err := s.AddPending(Pending{
		Path: "/t.jsonl", From: 424242, Domain: "alpha", SessionID: "S1",
		Days: []string{"2026-08-09"}, Excerpt: huge, At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	var seen []Promotion
	Promote(context.Background(), PromoteOptions{
		StateDir: sd, Config: c, Layout: l, Only: "/t.jsonl@424242",
		Budget: 5 * time.Minute, OnResult: func(p Promotion) { seen = append(seen, p) },
	})
	if len(seen) != 1 {
		t.Fatalf("결과가 %d건", len(seen))
	}
	if n := len([]rune(seen[0].Excerpt)); n > maxLedgerText+10 {
		t.Errorf("발췌가 %d 자다 — 잘리지 않았다", n)
	}
	// 잘렸다는 것이 보여야 한다. 조용히 자르면 사람이 "판별기가 이만큼만 봤나" 를
	// 오해한다.
	if !strings.Contains(seen[0].Excerpt, "잘림") {
		t.Error("잘렸다는 표시가 없다")
	}
	// 그리고 그 줄이 여전히 읽혀야 한다 — 자르기의 목적이 그것이다.
	if _, err := ReadPromotions(sd, time.Time{}); err != nil {
		t.Fatalf("원장을 못 읽는다: %v", err)
	}
}

// ★ **지목한 구간이 이미 선점돼 있으면 그렇다고 말해야 한다.**
//
// 전체를 도는 중이라면 남이 집어 간 구간을 조용히 건너뛰는 것이 맞다. 그런데
// Only 로 하나를 지목했다면 호출자(앱의 [결정이다] 버튼)는 그것이 처리되기를
// 기다린다 — 조용히 끝나면 "그런 구간이 없다" 로 보이고 실제 원인이 안 드러난다.
//
// 실측으로 만났다: 판별기가 죽은 뒤 바로 재시도하니 5분간 "구간이 없다" 고 나왔다.
// 방금 실패한 시도의 도장이 claimTTL 동안 남아 있던 것이다.
func TestPromoteReportsClaimContentionWhenTargeted(t *testing.T) {
	c, l, sd := promoteFixture(t, `{"record":false,"reason":"x"}`)
	items, err := ReadPending(sd)
	if err != nil || len(items) == 0 {
		t.Fatal("픽스처에 구간이 없다")
	}
	id := items[0].ID()

	// 남이 방금 집어 간 상태를 만든다.
	if ok, err := ClaimPending(sd, id, time.Now().UTC()); err != nil || !ok {
		t.Fatalf("선점 준비 실패: ok=%v err=%v", ok, err)
	}

	var errBuf strings.Builder
	var seen []Promotion
	Promote(context.Background(), PromoteOptions{
		StateDir: sd, Config: c, Layout: l, Only: id,
		Budget: 5 * time.Minute, Err: &errBuf,
		OnResult: func(p Promotion) { seen = append(seen, p) },
	})

	if len(seen) != 0 {
		t.Errorf("선점된 구간을 처리했다 — 두 프로세스가 같은 대화에 노트를 둘 만든다")
	}
	got := errBuf.String()
	if !strings.Contains(got, "이미 처리 중") {
		t.Errorf("선점을 안 알린다: %q — 호출자는 '구간이 없다' 로 오해한다", got)
	}

	// **전체를 도는 경우에는 조용해야 한다.** 남이 집어 간 것을 건너뛰는 것은
	// 정상 동작이고, 매번 보고하면 소음이 된다.
	var quiet strings.Builder
	Promote(context.Background(), PromoteOptions{
		StateDir: sd, Config: c, Layout: l,
		Budget: 5 * time.Minute, Err: &quiet,
	})
	if strings.Contains(quiet.String(), "이미 처리 중") {
		t.Errorf("전체를 도는데 선점을 보고했다 (소음): %q", quiet.String())
	}
}

// ★★ **실패한 구간이 영원히 돌면 안 된다.**
//
// 실패해도 구간은 남는다 — 그건 옳다. 판별기는 비결정적이라 다음엔 될 수 있고,
// 지워 버리면 거기 있었을지 모를 결정이 조용히 사라진다. 그런데 **횟수를 안 세면
// 영원히 반복된다.** 실측에서 구간 하나가 상한을 6번 연속으로 넘겼고, 매번 세션
// 끝에서 사람을 그 시간만큼 붙잡았다. 예산이 있으므로 그 낭비는 다른 구간의
// 기회를 먹는다.
func TestPromoteGivesUpAfterRepeatedJudgeFailures(t *testing.T) {
	c, l, sd := promoteFixture(t, `{"record":true,"slug":"x","summary":"요약","body":"## 결정\n\nx"}`)

	// 부를 때마다 흔적을 하나씩 쌓고 실패하는 판별기.
	countDir := t.TempDir()
	jp := filepath.Join(t.TempDir(), "judge")
	sh := "#!/bin/sh\ncat >/dev/null\nmktemp " + countDir + "/c.XXXXXX >/dev/null\nexit 1\n"
	if err := os.WriteFile(jp, []byte(sh), 0o755); err != nil {
		t.Fatal(err)
	}
	c.Capture.JudgePath = jp

	s := NewStore(sd)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPending(Pending{
		Path: "/t.jsonl", From: 42, Domain: "alpha", SessionID: "S1",
		Days: []string{"2026-08-09"}, Excerpt: "결정했다", At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	calls := func() int {
		e, _ := os.ReadDir(countDir)
		return len(e)
	}
	// 판별기를 여유 있게 여러 판 돌린다. 포기가 없으면 매 판 부른다.
	var last string
	for i := 0; i < MaxJudgeFails+3; i++ {
		var errBuf strings.Builder
		Promote(context.Background(), PromoteOptions{
			StateDir: sd, Config: c, Layout: l, Only: "/t.jsonl@42",
			Budget: 5 * time.Minute, Err: &errBuf,
		})
		last = errBuf.String()
	}

	if got := calls(); got != MaxJudgeFails {
		t.Errorf("판별기를 %d번 불렀다 — %d번에서 그만뒀어야 한다", got, MaxJudgeFails)
	}

	// **구간은 남아 있어야 한다.** 자동으로 처리 못 한 것이지 결정이 없는 것이 아니다.
	after, _ := ReadPending(sd)
	var p *Pending
	for i := range after {
		if after[i].ID() == "/t.jsonl@42" {
			p = &after[i]
		}
	}
	if p == nil {
		t.Fatal("구간이 사라졌다 — 포기는 해소가 아니다. 사람이 봐야 한다")
	}
	if !p.GaveUp() {
		t.Errorf("포기 표시가 안 됐다 (fails=%d)", p.Fails)
	}
	// 지목해서 불렀는데 조용하면 호출자는 "그런 구간이 없다" 로 오해한다.
	if !strings.Contains(last, "그만뒀다") {
		t.Errorf("왜 아무 일도 안 났는지 안 알려 준다: %q", last)
	}
}

// ★ **실패는 선점 도장을 남기면 안 된다.**
//
// 실패 뒤에 도장이 남아 있으면 바로 뒤에 도는 승격이 그 구간을 "처리 중" 으로 보고
// claimTTL(5분) 동안 건너뛴다 — 실패를 선점으로 오해하는 것이다. 실측에서 판별기가
// 죽은 뒤 재시도하니 5분간 "구간이 없다" 고 나왔다.
func TestPromoteFailureClearsClaim(t *testing.T) {
	c, l, sd := promoteFixture(t, `{"record":true,"slug":"x","summary":"요약","body":"## 결정\n\nx"}`)
	jp := filepath.Join(t.TempDir(), "judge")
	if err := os.WriteFile(jp, []byte("#!/bin/sh\ncat >/dev/null\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c.Capture.JudgePath = jp

	s := NewStore(sd)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPending(Pending{
		Path: "/t.jsonl", From: 9, Domain: "alpha", SessionID: "S1",
		Days: []string{"2026-08-09"}, Excerpt: "결정했다", At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	var errBuf strings.Builder
	Promote(context.Background(), PromoteOptions{
		StateDir: sd, Config: c, Layout: l, Only: "/t.jsonl@9",
		Budget: 5 * time.Minute, Err: &errBuf,
	})

	after, _ := ReadPending(sd)
	for _, p := range after {
		if p.ID() != "/t.jsonl@9" {
			continue
		}
		if p.Fails != 1 {
			t.Errorf("실패를 %d번으로 셌다, 1이어야 한다", p.Fails)
		}
		if !p.ClaimedAt.IsZero() {
			t.Error("실패했는데 도장이 남았다 — 다음 시도가 5분간 이 구간을 건너뛴다")
		}
		return
	}
	t.Fatal("구간이 사라졌다")
}

// ★ **예산 마감 직전에 새 구간을 시작하면 안 된다.**
//
// "마감 전이면 시작" 으로 두면 마감 직전에 집어 든 구간이 판별기 상한만큼 예산을
// 넘겨서 돈다. 그러면 훅 상한을 넘겨 호스트가 훅을 통째로 죽이고, 원장도 못 쓴 채
// 선점 도장만 남는다. 상한을 75초로 올리면서 이 틈이 커졌다.
func TestPromoteDoesNotStartWhatItCannotFinish(t *testing.T) {
	c, l, sd := promoteFixture(t, `{"record":true,"slug":"x","summary":"요약","body":"## 결정\n\nx"}`)
	mark := filepath.Join(t.TempDir(), "called")
	jp := filepath.Join(t.TempDir(), "judge")
	sh := "#!/bin/sh\ncat >/dev/null\ntouch " + mark + "\necho '{\"record\":false,\"reason\":\"x\"}'\n"
	if err := os.WriteFile(jp, []byte(sh), 0o755); err != nil {
		t.Fatal(err)
	}
	c.Capture.JudgePath = jp

	s := NewStore(sd)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPending(Pending{
		Path: "/t.jsonl", From: 5, Domain: "alpha", SessionID: "S1",
		Days: []string{"2026-08-09"}, Excerpt: "결정했다", At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	// 판별기 상한보다 짧은 예산 — 시작하면 끝낼 수 없다.
	var errBuf strings.Builder
	Promote(context.Background(), PromoteOptions{
		StateDir: sd, Config: c, Layout: l, Only: "/t.jsonl@5",
		Budget: time.Second, Err: &errBuf,
	})

	if _, err := os.Stat(mark); err == nil {
		t.Error("판별기를 불렀다 — 끝낼 시간이 없으면 시작하지 말아야 한다")
	}
	// 아무 일도 안 했으므로 도장도 실패 횟수도 남으면 안 된다.
	after, _ := ReadPending(sd)
	for _, p := range after {
		if p.ID() != "/t.jsonl@5" {
			continue
		}
		if p.Fails != 0 {
			t.Errorf("시작도 안 했는데 실패를 %d번 셌다", p.Fails)
		}
		if !p.ClaimedAt.IsZero() {
			t.Error("시작도 안 했는데 도장이 찍혔다")
		}
		return
	}
	t.Fatal("구간이 사라졌다")
}
