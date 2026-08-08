package hook

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofrs/flock"

	"github.com/xian0310567/casebook/internal/daemon"
)

// ★★ `cb watch` 를 켜는 것이 자동 기록을 끄는 행위가 되면 안 된다.
//
// 승격이 소유권 게이트 뒤에 있으면, 데몬이 락을 쥔 순간 훅은 promote 이전에
// 반환한다. 그런데 데몬의 drain 은 판별기를 부르지 않고, 데몬은 세션이 끝난 것도
// 모른다 — 아무도 승격하지 않는 상태가 무기한 이어진다. 사용자에게 "데몬을 띄우면
// 더 촘촘해진다" 고 말하는 순간 정반대가 된다.
func TestPromoteRunsEvenWhenDaemonOwnsLock(t *testing.T) {
	sd := t.TempDir()
	tp := writeTranscript(t, t.TempDir(), 8)
	c := cfg(t)
	c.Capture.JudgePath = stubJudge(t,
		`{"record":true,"slug":"저장엔진","summary":"SQLite 로 간다","body":"## 결정\n\nx\n"}`)

	// 먼저 데몬 없이 한 번 훑어 표시를 남긴다.
	seed := runHook(t, c, sd, EventStop, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if seed.e != nil {
		t.Fatal(seed.e)
	}
	if items, _ := daemon.ReadPending(sd); len(items) != 1 {
		t.Fatalf("표시가 %d건 — 준비가 안 됐다", len(items))
	}

	// 이제 cb watch 가 돌고 있는 상태를 만든다.
	lk := flock.New(filepath.Join(sd, "watch.lock"))
	got, err := lk.TryLock()
	if err != nil || !got {
		t.Fatalf("락을 잡을 수 없다: %v", err)
	}
	defer func() { _ = lk.Unlock() }()

	r := runHook(t, c, sd, EventSessionEnd, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if r.e != nil {
		t.Fatal(r.e)
	}
	if !strings.Contains(r.err, "자동 기록") {
		t.Errorf("데몬이 락을 쥐었다고 승격을 건너뛰었다 — cb watch 가 자동 기록을 끈다:\n%s", r.err)
	}
	m, _ := filepath.Glob(filepath.Join(c.Vault, "alpha", "decisions", "*저장엔진*"))
	if len(m) == 0 {
		t.Error("노트가 안 만들어졌다")
	}
	if items, _ := daemon.ReadPending(sd); len(items) != 0 {
		t.Errorf("승격했는데 표시가 %d건 남았다", len(items))
	}
}

// 데몬이 락을 쥐고 있으면 훑기는 하지 않는다 — 주인이 하나여야 중복 처리가 없다.
func TestDaemonOwnedLockStillYieldsScanning(t *testing.T) {
	sd := t.TempDir()
	tp := writeTranscript(t, t.TempDir(), 8)
	c := cfg(t)
	c.Capture.JudgePath = "" // 판별기 없음 — 승격은 애초에 안 일어난다

	lk := flock.New(filepath.Join(sd, "watch.lock"))
	if got, err := lk.TryLock(); err != nil || !got {
		t.Fatalf("락을 잡을 수 없다: %v", err)
	}
	defer func() { _ = lk.Unlock() }()

	r := runHook(t, c, sd, EventStop, Input{
		Cwd: "/tmp/proj/alpha", SessionID: "S1", TranscriptPath: tp})
	if r.e != nil {
		t.Fatal(r.e)
	}
	if items, _ := daemon.ReadPending(sd); len(items) != 0 {
		t.Errorf("데몬이 주인인데 훅이 훑어서 표시를 남겼다 (%d건) — 중복 처리다", len(items))
	}
}
