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

	"github.com/xian0310567/casebook/internal/daemon"
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
