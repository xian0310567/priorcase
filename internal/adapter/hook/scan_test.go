package hook

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/daemon"
)

// writeTranscript 는 결정 시그널이 든 대화를 만든다.
func writeTranscript(t *testing.T, dir string, n int) string {
	t.Helper()
	p := filepath.Join(dir, "s.jsonl")
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `{"type":"assistant","cwd":"/tmp/proj/alpha","sessionId":"S1","timestamp":"2026-08-07T01:00:%02dZ","message":{"role":"assistant","content":[{"type":"text","text":"여기서 결정했다"}]}}`+"\n", i)
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ★ 데몬이 안 돌면 훅이 대신 훑는다. 데몬 등록에 실패한 사용자도 안전망을 얻는다.
//
// (판별기는 꺼져 있으므로 표시가 그대로 남는다. 켜져 있으면 session-end·pre-compact
// 에서 승격돼 사라진다 — 아래 TestSessionEndPromotes 참조.)
func TestSafetyNetScansWhenDaemonIsNotRunning(t *testing.T) {
	stateDir := t.TempDir()
	tp := writeTranscript(t, t.TempDir(), 8)

	for _, ev := range []Event{EventStop, EventPreCompact, EventSessionEnd} {
		t.Run(string(ev), func(t *testing.T) {
			sd := t.TempDir()
			r := runHook(t, cfg(t), sd, ev, Input{
				Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
			if r.e != nil {
				t.Fatal(r.e)
			}
			// 이 셋은 컨텍스트를 주입하지 않는다.
			if r.out != "" {
				t.Errorf("%s 가 컨텍스트를 주입했다: %q", ev, r.out)
			}
			items, err := daemon.ReadPending(sd)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 {
				t.Fatalf("pending %d건, 1건이어야 한다 — 데몬 없이는 안전망이 없다", len(items))
			}
		})
	}
	_ = stateDir
}

// ★ 데몬이 돌면 훅은 건너뛴다. 소유자가 언제나 하나여서 중복 처리가 구조적으로 없다.
func TestSafetyNetYieldsToRunningDaemon(t *testing.T) {
	stateDir := t.TempDir()
	root := filepath.Join(t.TempDir(), "projects", "proj-a")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	tp := writeTranscript(t, root, 8)

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	var once sync.Once
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = daemon.Run(ctx, daemon.Options{
			StateDir: stateDir, TranscriptRoot: filepath.Dir(root), Config: cfg(t),
			Quiesce: time.Hour, // 데몬이 스스로 훑지 못하게 해서 훅만 관찰한다
			OnEvent: func(e daemon.Event) {
				if e.Kind == "ready" {
					once.Do(func() { close(ready) })
				}
			},
		})
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("데몬이 뜨지 않았다")
	}
	t.Cleanup(func() { cancel(); <-done })

	r := runHook(t, cfg(t), stateDir, EventStop, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if r.e != nil {
		t.Fatal(r.e)
	}

	// **상태를 본다.** stderr 메시지만 보면 약하다 — 훅이 실제로 훑었는지는 pending 과
	// 체크포인트가 말한다. 데몬의 Quiesce 를 1시간으로 둬서 데몬은 스스로 훑지 않으므로,
	// 여기서 무언가 생겼다면 그건 훅이 한 것이다.
	items, err := daemon.ReadPending(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("데몬이 도는데 훅이 훑어 pending %d건을 만들었다 — 같은 구간을 두 번 처리한다", len(items))
	}
	if strings.Contains(r.err, "훑음") {
		t.Errorf("데몬이 도는데 훅이 훑었다:\n%s", r.err)
	}
}

// stop_hook_active 는 "이 Stop 이 훅 때문에 다시 발동했다" 는 뜻이다. 여기서 또 일하면 루프다.
func TestSafetyNetStopsOnStopHookActive(t *testing.T) {
	sd := t.TempDir()
	tp := writeTranscript(t, t.TempDir(), 8)
	r := runHook(t, cfg(t), sd, EventStop, Input{
		Cwd: "/tmp/proj/alpha", TranscriptPath: tp, StopHookActive: true})
	if r.e != nil {
		t.Fatal(r.e)
	}
	items, _ := daemon.ReadPending(sd)
	if len(items) != 0 {
		t.Errorf("stop_hook_active 인데 일했다 — 루프가 된다 (pending %d건)", len(items))
	}
}

// transcript_path 가 없으면 할 일이 없다. 조용히 끝낸다.
func TestSafetyNetWithoutTranscriptIsNoop(t *testing.T) {
	r := runHook(t, cfg(t), t.TempDir(), EventStop, Input{Cwd: "/tmp/proj/alpha"})
	if r.e != nil {
		t.Errorf("transcript 가 없다고 에러를 냈다: %v", r.e)
	}
	if r.out != "" {
		t.Errorf("컨텍스트를 주입했다: %q", r.out)
	}
}

// 훅이 훑었으면 그 사실을 stderr 로 알린다 — 조용히 훑고 끝나면 도는지 알 수 없다.
func TestSafetyNetReportsToStderrOnly(t *testing.T) {
	sd := t.TempDir()
	tp := writeTranscript(t, t.TempDir(), 8)
	r := runHook(t, cfg(t), sd, EventStop, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if !strings.Contains(r.err, "훑음") {
		t.Errorf("훑고도 아무 말이 없다:\n%s", r.err)
	}
	if r.out != "" {
		t.Errorf("stdout 을 건드렸다: %q", r.out)
	}
}

// ★★ **"훑음" 만으로는 판별기가 무엇을 봤는지 알 수 없다.**
//
// 발췌가 잘렸는지를 사후에 대조할 방법이 없어서 "판별기가 결정을 못 알아봤다" 와
// "판별기에게 근거를 안 보여 줬다" 가 밖에서 구별되지 않았다. 원장은 승격 성공 때만
// 발췌를 싣는데, 이 머신의 원장 32줄 중 excerpt 키가 있는 줄이 **0건**이었다 —
// 최근 7일 판정 23건에 자동 기록이 0건이라 그 경로를 한 번도 안 탔기 때문이다.
//
// 문구는 데몬의 watch 줄과 **공유한다**(daemon.ScanResult.ExcerptNote). 각자 포맷하면
// 같은 사실이 두 문장으로 갈리고, 그러면 두 경로를 나란히 놓고 대조할 수 없다.
func TestSafetyNetReportsWhatTheJudgeSaw(t *testing.T) {
	sd := t.TempDir()
	tp := writeTranscript(t, t.TempDir(), 8)
	r := runHook(t, cfg(t), sd, EventStop, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if r.e != nil {
		t.Fatal(r.e)
	}
	if !strings.Contains(r.err, "발췌") {
		t.Errorf("판별기가 무엇을 봤는지 안 알린다:\n%s", r.err)
	}
	// **"생략 0" 을 내면 안 된다.** 다 담았으면 그렇다고 못 박아야, 발췌를 아예
	// 안 만든 구간의 0과 구별된다 — 0을 "다 담았다" 로 읽는 순간 이 값을 뺀 이유가
	// 통째로 무너진다.
	if !strings.Contains(r.err, "전부") {
		t.Errorf("다 담은 것을 '전부' 로 못 박지 않는다:\n%s", r.err)
	}
}

// ── 자동 승격 (③) ────────────────────────────────────────────────────────

// 판별기가 없으면 **아무것도 하지 않는다.** 그때는 표시만 남고 에이전트가 판단한다 —
// 판별기 없는 설치의 정상 동작이고, 경고를 낼 일이 아니다.
func TestPromoteIsNoopWithoutJudge(t *testing.T) {
	sd := t.TempDir()
	tp := writeTranscript(t, t.TempDir(), 8)
	c := cfg(t)
	c.Capture.JudgePath = filepath.Join(t.TempDir(), "없는판별기")

	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	r := runHook(t, c, sd, EventSessionEnd, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if r.e != nil {
		t.Fatal(r.e)
	}
	items, _ := daemon.ReadPending(sd)
	if len(items) != 1 {
		t.Errorf("표시가 %d건 — 판별기가 없으면 표시만 남아야 한다", len(items))
	}
	if strings.Contains(r.err, "승격 실패") {
		t.Errorf("판별기가 없는 것을 실패로 알렸다:\n%s", r.err)
	}
}

// ★ **Stop 에서는 승격하지 않는다.** 대화가 이어지는 중이라 에이전트에게 먼저
// 기회를 준다(주입 ②). 여기서 바로 승격하면 에이전트가 더 나은 노트를 쓸 기회를
// 뺏는다 — 근거·대안까지 아는 것은 그 세션의 에이전트다.
func TestStopDoesNotPromote(t *testing.T) {
	sd := t.TempDir()
	tp := writeTranscript(t, t.TempDir(), 8)
	c := cfg(t)
	// **성공하는 판별기**를 준다. /bin/echo 처럼 실패하는 것을 쓰면 "승격 실패" 가
	// 나서 "승격 안 함" 과 구별되지 않는다 — 변이 테스트에서 실제로 안 잡혔다.
	c.Capture.JudgePath = stubJudge(t, `{"record":true,"slug":"저장엔진","summary":"SQLite 로 간다","body":"## 결정\n\nx\n"}`)

	r := runHook(t, c, sd, EventStop, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if r.e != nil {
		t.Fatal(r.e)
	}
	for _, forbidden := range []string{"자동 기록", "기록 안 함", "승격"} {
		if strings.Contains(r.err, forbidden) {
			t.Errorf("Stop 에서 승격을 시도했다 (%q) — 에이전트에게 먼저 기회를 줘야 한다:\n%s",
				forbidden, r.err)
		}
	}
	items, _ := daemon.ReadPending(sd)
	if len(items) != 1 {
		t.Errorf("표시가 %d건, 1건이어야 한다 — Stop 이 승격해 지웠다", len(items))
	}
	// 노트도 안 생겼어야 한다.
	m, _ := filepath.Glob(filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "*저장엔진-2026-08-07*"))
	if len(m) != 0 {
		t.Errorf("Stop 이 노트를 만들었다: %v", m)
	}
}

// 판별기가 띄운 세션에서는 훅이 아무것도 하지 않는다 — 안 그러면 판별기가 판별기를 부른다.
func TestJudgeSessionDoesNotRecurse(t *testing.T) {
	t.Setenv("PRIORCASE_JUDGE", "1")
	sd := t.TempDir()
	tp := writeTranscript(t, t.TempDir(), 8)
	c := cfg(t)
	c.Capture.JudgePath = "/bin/echo"

	r := runHook(t, c, sd, EventSessionEnd, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if r.e != nil {
		t.Fatal(r.e)
	}
	if strings.Contains(r.err, "승격") || strings.Contains(r.err, "자동 기록") {
		t.Errorf("판별기 세션에서 승격을 시도했다 — 재귀가 된다:\n%s", r.err)
	}
}

// ★ **세션이 끝날 때 판별기가 대신 기록한다.** 에이전트가 끝내 안 불러도 남는다 —
// "평소처럼 쓰는데 전부 기록된다" 가 성립하는 자리다.
func TestSessionEndPromotes(t *testing.T) {
	for _, ev := range []Event{EventSessionEnd, EventPreCompact} {
		t.Run(string(ev), func(t *testing.T) {
			sd := t.TempDir()
			tp := writeTranscript(t, t.TempDir(), 8)
			c := cfg(t)
			c.Capture.JudgePath = stubJudge(t, `{"record":true,"slug":"저장엔진","summary":"SQLite 로 간다","body":"## 결정\n\nSQLite.\n","tags":["저장"]}`)

			r := runHook(t, c, sd, ev, Input{
				Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
			if r.e != nil {
				t.Fatal(r.e)
			}
			// **아크 판정에서 나와야 한다.** 옛 판은 이 자리에서 pending 큐를
			// ScopeEnd 로 돌렸는데, 데몬이 대화 도중 그 큐를 비우므로 실제 사용에서는
			// 판정할 것이 없었다 — 실측으로 자동 기록 63건이 전부 작업 로그이고
			// 결정 노트 0건이었다. 그래서 대상을 transcript 아크로 바꿨다.
			// 문구까지 못 박는 이유: "결정 노트가 생겼다" 만 보면 pending 경로로
			// 되돌아가도 통과한다.
			if !strings.Contains(r.err, "아크 → 결정 노트") {
				t.Fatalf("아크가 결정 노트를 만들지 않았다:\n%s", r.err)
			}
			// 노트가 실제로 생겼나 — capture.Do 를 거쳤으므로 정본형이어야 한다.
			// writeTranscript 의 타임스탬프가 2026-08-07 이라 그 날짜로 만들어진다.
			got, err := os.ReadFile(filepath.Join(c.DefaultVaultPath(), "alpha", "decisions",
				"alpha-결정-저장엔진-2026-08-07.md"))
			if err != nil {
				t.Fatalf("노트가 없다: %v", err)
			}
			s := string(got)
			if !strings.Contains(s, "SQLite 로 간다") {
				t.Errorf("요약이 안 들어갔다:\n%s", s)
			}
			if !strings.Contains(s, `source_session: "S1"`) {
				t.Errorf("세션이 안 박혔다 — capture.Do 를 안 거쳤을 수 있다:\n%s", s)
			}
			if !strings.Contains(s, "tags: [decision, 저장]") {
				t.Errorf("태그가 정본형이 아니다:\n%s", s)
			}

			// 승격됐으면 표시는 사라져야 한다 — 안 그러면 매 세션 다시 묻는다.
			items, _ := daemon.ReadPending(sd)
			if len(items) != 0 {
				t.Errorf("승격했는데 표시가 %d건 남았다", len(items))
			}
		})
	}
}

// 판별기가 "결정이 아니다" 라고 하면 표시를 지운다 — 안 지우면 매 세션 다시 묻는다.
func TestNotRecordedClearsPending(t *testing.T) {
	sd := t.TempDir()
	tp := writeTranscript(t, t.TempDir(), 8)
	c := cfg(t)
	c.Capture.JudgePath = stubJudge(t, `{"record":false,"reason":"진행 보고다"}`)

	r := runHook(t, c, sd, EventSessionEnd, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if r.e != nil {
		t.Fatal(r.e)
	}
	// 문구가 "기록 안 함" 에서 "아크에 남길 것이 없다" 로 바뀌었다 — 세션 끝의 판정
	// 대상이 pending 큐에서 transcript 아크로 옮겨졌기 때문이다. 못 박는 것은 문구가
	// 아니라 **이유가 밖으로 나온다**는 사실이다: 안 남긴 것과 판별기가 안 돈 것이
	// 구별되지 않으면 조용한 무동작을 진단할 수 없다.
	if !strings.Contains(r.err, "남길 것이 없다") || !strings.Contains(r.err, "진행 보고다") {
		t.Errorf("이유를 안 알린다:\n%s", r.err)
	}
	if items, _ := daemon.ReadPending(sd); len(items) != 0 {
		t.Errorf("결정이 아니라고 했는데 표시가 %d건 남았다", len(items))
	}
}

// 판별기가 깨지면 **표시를 지우지 않는다** — 다음 기회에 다시 시도해야 한다.
func TestJudgeFailureKeepsPending(t *testing.T) {
	sd := t.TempDir()
	tp := writeTranscript(t, t.TempDir(), 8)
	c := cfg(t)
	c.Capture.JudgePath = stubJudge(t, "이건 JSON 이 아니다")

	r := runHook(t, c, sd, EventSessionEnd, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if r.e != nil {
		t.Fatal(r.e)
	}
	if !strings.Contains(r.err, "승격 실패") {
		t.Errorf("실패를 안 알린다:\n%s", r.err)
	}
	if items, _ := daemon.ReadPending(sd); len(items) != 1 {
		t.Errorf("판별기가 깨졌는데 표시를 지웠다 (%d건) — 그 구간을 잃는다", len(items))
	}
}

// ★★★ **판별기 세션에는 회수를 주입하지 않는다.**
//
// TestJudgeSessionDoesNotRecurse 가 "훅이 아무것도 하지 않는다" 를 이름으로 걸어
// 뒀는데 실제로는 SessionEnd 하나만 덮고 있었다. 회수 주입 경로가 빠져 있었고,
// 그 자리가 실제로 샜다.
//
// 실측(2026-08-31): 홈 디렉토리에 쌓인 판별기 세션 7개가 **전부** 회수 주입을
// 4,007~4,889자씩 받았고 **전부 API safeguard 로 차단됐다.** 이 머신의 차단 42회
// 중 16회가 그것이다 — 사람은 못 보는 자리에서 판별기가 계속 죽고 있었다.
//
// 주입이 차단의 원인이라는 증거는 없다(같은 노트가 실린 무차단 세션이 58건이다).
// 막는 이유는 다른 것이다: 판별기는 발췌 하나를 등급 매기는 일만 하므로 볼트
// 회수를 보여 줄 이유가 하나도 없고, 그 4,889자는 순수한 낭비다.
func TestJudgeSessionGetsNoRecallInjection(t *testing.T) {
	for _, ev := range []Event{EventUserPromptSubmit, EventSessionStart} {
		t.Run(string(ev), func(t *testing.T) {
			c := cfg(t)
			sd := t.TempDir()

			// 먼저 가드가 없을 때 실제로 주입이 나가는지 확인한다 — 안 나가면
			// 이 테스트가 아무것도 안 지키는 셈이다.
			base := runHook(t, c, sd, ev, Input{
				Cwd: "/tmp/proj/alpha", SessionID: "S1", Prompt: "저장 엔진을 무엇으로 골랐지"})
			if base.e != nil {
				t.Fatal(base.e)
			}
			if strings.TrimSpace(base.out) == "" {
				t.Fatalf("판별기 세션이 아닌데도 주입이 없다 — 이 테스트의 전제가 사라졌다")
			}

			t.Setenv("PRIORCASE_JUDGE", "1")
			got := runHook(t, c, sd, ev, Input{
				Cwd: "/tmp/proj/alpha", SessionID: "S2", Prompt: "저장 엔진을 무엇으로 골랐지"})
			if got.e != nil {
				t.Fatal(got.e)
			}
			if strings.TrimSpace(got.out) != "" {
				t.Errorf("판별기 세션에 주입이 나갔다 (%d자):\n%s", len(got.out), got.out)
			}
		})
	}
}
