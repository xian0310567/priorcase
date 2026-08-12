package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/daemon"
)

// fakeHome 은 진짜 홈 대신 쓸 자리를 만든다.
//
// **격리가 필수다.** 훑기는 hosts.Resolve("") 로 호스트의 기본 자리를 찾고, 그건
// os.UserHomeDir() → $HOME 이다. 격리하지 않으면 테스트가 **사용자의 진짜 대화
// 3,350개**를 훑는다 — 느린 것은 둘째 치고 남의 데이터를 읽는 것이 문제다.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, d := range []string{
		filepath.Join(home, ".claude", "projects", "p"),
		filepath.Join(home, ".codex", "sessions", "2026", "01", "01"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// ★★ **훅이 훑기를 실제로 부르는지 본다.**
//
// 함수만 시험하면 호출부를 떼어내도 안 잡힌다. 그리고 그 상태는 조용하다 — 훑기
// 함수에는 통과하는 테스트가 잔뜩 있는데 아무도 부르지 않는다. 사용자에게는
// "Codex 를 써도 아무것도 안 잡힌다" 로 보이고, 그건 "결정이 없었다" 와 구별되지 않는다.
//
// 실제로 이 구멍에 두 번 빠졌다 (similarFor 도 같은 부류였다). 변형 테스트가 잡았다.
func TestSessionEndSweepsOtherHosts(t *testing.T) {
	home := fakeHome(t)
	sd := t.TempDir()
	c := cfg(t)

	// Codex 세션 하나를 심는다. 훅은 이 파일을 인자로 받지 않는다 —
	// 훑기가 스스로 찾아야 한다.
	codex := filepath.Join(home, ".codex", "sessions", "2026", "01", "01", "rollout-x.jsonl")
	if err := os.WriteFile(codex, []byte(
		`{"type":"session_meta","timestamp":"2026-01-01T00:00:00Z","payload":{"id":"s1","cwd":"/x"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 훅이 받을 자기 transcript.
	tp := writeTranscript(t, t.TempDir(), 8)

	r := runHook(t, c, sd, EventSessionEnd, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if r.e != nil {
		t.Fatal(r.e)
	}
	if !strings.Contains(r.err, "다른 호스트 훑기") {
		t.Fatalf("훅이 훑기를 안 불렀다 — Codex 는 파서가 있어도 영영 안 읽힌다:\n%s", r.err)
	}

	// **체크포인트가 실제로 생겨야 한다.** 메시지만 보고 통과하면 껍데기다.
	st := daemon.NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	cps := st.CheckpointSnapshot()
	if _, ok := cps[codex]; !ok {
		var seen []string
		for k := range cps {
			seen = append(seen, filepath.Base(k))
		}
		t.Errorf("Codex 파일이 상태에 안 들어왔다 (지금: %v)", seen)
	}
}

// ★★ **Stop 에서는 훑지 않는다.**
//
// Stop 은 대화 도중이라 자주 온다. 거기서 수천 개 파일을 stat 하면 그 지연을 사람이
// **매 턴** 겪는다. 세션이 끝날 때만 하는 것이 이 기능의 값과 비용을 맞추는 지점이다.
func TestStopDoesNotSweep(t *testing.T) {
	home := fakeHome(t)
	sd := t.TempDir()
	c := cfg(t)
	tp := writeTranscript(t, t.TempDir(), 8)

	// **훑을 것이 있어야 안 훑은 것을 볼 수 있다.** 홈이 비어 있으면 훑기가 조용히
	// 끝나서, 불렸는지 안 불렸는지 구별되지 않는다 — 변형 테스트가 그 공허함을 잡았다.
	codex := filepath.Join(home, ".codex", "sessions", "2026", "01", "01", "rollout-y.jsonl")
	if err := os.WriteFile(codex, []byte(
		`{"type":"session_meta","timestamp":"2026-01-01T00:00:00Z","payload":{"id":"s2","cwd":"/x"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := runHook(t, c, sd, EventStop, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if r.e != nil {
		t.Fatal(r.e)
	}
	if strings.Contains(r.err, "다른 호스트 훑기") {
		t.Errorf("Stop 에서 훑었다 — 그 지연을 사람이 매 턴 겪는다:\n%s", r.err)
	}
	// 체크포인트도 안 생겨야 한다.
	st := daemon.NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.CheckpointSnapshot()[codex]; ok {
		t.Error("Stop 에서 다른 호스트 파일에 체크포인트를 찍었다")
	}
}
