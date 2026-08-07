package daemon

import (
	"context"
	"errors"

	"github.com/gofrs/flock"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// collector 는 데몬이 흘리는 이벤트를 모은다.
type collector struct {
	mu sync.Mutex
	ev []Event
}

func (c *collector) on(e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ev = append(c.ev, e)
}

func (c *collector) of(kind string) []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Event
	for _, e := range c.ev {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// waitFor 는 조건이 참이 될 때까지 기다린다. 시간에 기대는 테스트를 sleep 으로
// 쓰면 느린 CI 에서 깨진다 — 조건을 폴링한다.
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s: %v 안에 일어나지 않았다", what, d)
}

type harness struct {
	root     string
	stateDir string
	col      *collector
	cancel   context.CancelFunc
	done     chan error
}

func start(t *testing.T, o Options) *harness {
	t.Helper()
	h := &harness{root: o.TranscriptRoot, stateDir: o.StateDir, col: &collector{}, done: make(chan error, 1)}
	o.OnEvent = h.col.on
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() { h.done <- Run(ctx, o) }()
	waitFor(t, 3*time.Second, "데몬 기동", func() bool { return len(h.col.of("ready")) > 0 })
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.done:
		case <-time.After(3 * time.Second):
			t.Error("데몬이 종료되지 않았다")
		}
	})
	return h
}

func baseOpts(t *testing.T) Options {
	t.Helper()
	root := filepath.Join(t.TempDir(), "projects", "proj-a")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return Options{
		StateDir:       t.TempDir(),
		TranscriptRoot: filepath.Dir(root),
		Config:         scanCfg(),
		Quiesce:        60 * time.Millisecond,
	}
}

// 감사 결함 3 — 서기 동시 실행 락이 없어 같은 구간을 중복 처리했다.
func TestSecondInstanceExitsImmediately(t *testing.T) {
	o := baseOpts(t)
	start(t, o)

	err := Run(context.Background(), o)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("두 번째 인스턴스의 에러 = %v, ErrAlreadyRunning 이어야 한다", err)
	}
}

// 락은 상태 디렉토리별이다 — 볼트가 다르면 같이 돌 수 있어야 한다.
func TestDifferentStateDirsCanCoexist(t *testing.T) {
	start(t, baseOpts(t))
	h2 := start(t, baseOpts(t))
	if len(h2.col.of("ready")) == 0 {
		t.Error("상태 디렉토리가 다른데 두 번째가 못 떴다")
	}
}

// 기동 전에 있던 파일은 훑지 않는다. 실측 1173개를 다 훑으면 pending 이 쏟아지고
// 안전망이 소음이 된다 — 데몬은 '지금부터' 놓친 것을 줍는다.
func TestExistingFilesAreSeededNotScanned(t *testing.T) {
	o := baseOpts(t)
	old := filepath.Join(o.TranscriptRoot, "proj-a", "old.jsonl")
	writeLines(t, old, turns(t, 20, "여기서 결정했다", "/tmp/proj/alpha")...)

	h := start(t, o)
	seeds := h.col.of("seed")
	if len(seeds) == 0 {
		t.Fatal("시딩 이벤트가 없다")
	}

	s := NewStore(o.StateDir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if got := len(s.Pending()); got != 0 {
		t.Errorf("기동 전 파일에서 pending %d건이 생겼다 — 시딩되지 않았다", got)
	}
	fi, _ := os.Stat(old)
	if got := s.Checkpoint(old); got != fi.Size() {
		t.Errorf("체크포인트 = %d, 파일 끝(%d)이어야 한다", got, fi.Size())
	}
}

// --backfill 은 그 반대다. **기동만으로** 훑어야 한다 — 뒤에 파일을 건드려서
// 훑히는 것은 backfill 이 아니라 그냥 평소 동작이다.
func TestBackfillScansExistingFilesAtStartup(t *testing.T) {
	o := baseOpts(t)
	o.Backfill = true
	old := filepath.Join(o.TranscriptRoot, "proj-a", "old.jsonl")
	writeLines(t, old, turns(t, 20, "여기서 결정했다", "/tmp/proj/alpha")...)

	start(t, o) // 기동만 하고 아무것도 건드리지 않는다

	s := NewStore(o.StateDir)
	waitFor(t, 3*time.Second, "backfill 스캔", func() bool {
		if err := s.Load(); err != nil {
			return false
		}
		return len(s.Pending()) > 0
	})
}

// ★ 데몬이 꺼져 있는 동안 자란 구간을 기동 시 훑어야 한다.
//
// 안 하면 **데몬이 죽어 있는 사이에 끝난 세션은 영원히 검토되지 않는다** — 그 파일은
// 다시 바뀌지 않으므로 fsnotify 이벤트가 두 번 다시 오지 않는다. 안전망이 조용히
// 구멍을 내는 자리다.
func TestGrowthDuringDowntimeIsScannedAtStartup(t *testing.T) {
	o := baseOpts(t)
	tp := filepath.Join(o.TranscriptRoot, "proj-a", "s.jsonl")

	// 지난 기동에서 앞부분까지 봤다.
	writeLines(t, tp, turns(t, 3, "잡담", "/tmp/proj/alpha")...)
	fi, err := os.Stat(tp)
	if err != nil {
		t.Fatal(err)
	}
	pre := NewStore(o.StateDir)
	if err := pre.Load(); err != nil {
		t.Fatal(err)
	}
	if err := pre.Advance(tp, fi.Size(), fi.Size()); err != nil {
		t.Fatal(err)
	}

	// 데몬이 꺼진 사이에 세션이 이어지고 끝났다.
	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)

	start(t, o) // 다시 켠다. 파일은 이제 아무도 건드리지 않는다.

	s := NewStore(o.StateDir)
	waitFor(t, 3*time.Second, "밀린 구간 스캔", func() bool {
		if err := s.Load(); err != nil {
			return false
		}
		return len(s.Pending()) > 0
	})
	p := s.Pending()
	if p[0].From != fi.Size() {
		t.Errorf("From = %d, 지난 체크포인트(%d)부터여야 한다", p[0].From, fi.Size())
	}
}

// 기동 후에 생긴 파일은 처음부터 읽어야 한다. 시딩이 여기까지 적용되면 새 세션이
// 통째로 안 보인다.
func TestFileCreatedAfterStartIsScannedFromZero(t *testing.T) {
	o := baseOpts(t)
	h := start(t, o)

	fresh := filepath.Join(o.TranscriptRoot, "proj-a", "new.jsonl")
	writeLines(t, fresh, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)

	waitFor(t, 3*time.Second, "새 파일 스캔", func() bool { return len(h.col.of("scan")) > 0 })

	s := NewStore(o.StateDir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	p := s.Pending()
	if len(p) != 1 {
		t.Fatalf("pending %d건, 1건이어야 한다 — 새 세션을 놓쳤다", len(p))
	}
	if p[0].From != 0 {
		t.Errorf("From = %d, 0이어야 한다 (처음부터 읽어야 한다)", p[0].From)
	}
}

// 쓰기가 이어지는 동안에는 훑지 않는다. 반쯤 쓰인 파일을 훑으면 잘린 줄만 만난다.
func TestQuiesceDefersScanWhileWriting(t *testing.T) {
	o := baseOpts(t)
	o.Quiesce = 250 * time.Millisecond
	h := start(t, o)

	fresh := filepath.Join(o.TranscriptRoot, "proj-a", "live.jsonl")
	// 조용해질 틈을 주지 않고 계속 쓴다.
	for i := 0; i < 6; i++ {
		writeLines(t, fresh, turns(t, 2, "여기서 결정했다", "/tmp/proj/alpha")...)
		time.Sleep(60 * time.Millisecond)
	}
	if n := len(h.col.of("scan")); n != 0 {
		t.Errorf("쓰는 도중에 %d번 훑었다 — quiesce 가 동작하지 않는다", n)
	}
	waitFor(t, 3*time.Second, "조용해진 뒤 스캔", func() bool { return len(h.col.of("scan")) > 0 })
}

// 새 프로젝트 디렉토리가 생겨도 감시한다. fsnotify 는 재귀 감시를 안 하므로
// 새 디렉토리를 직접 추가해 주지 않으면 그 세션만 통째로 안 보인다.
func TestNewProjectDirectoryIsWatched(t *testing.T) {
	o := baseOpts(t)
	h := start(t, o)

	newDir := filepath.Join(o.TranscriptRoot, "proj-b")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 디렉토리 생성 이벤트가 처리될 틈을 준다.
	time.Sleep(150 * time.Millisecond)
	writeLines(t, filepath.Join(newDir, "s.jsonl"), turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)

	waitFor(t, 3*time.Second, "새 프로젝트 스캔", func() bool { return len(h.col.of("scan")) > 0 })
}

// 시그널이 없으면 데몬은 돌긴 도는데 아무것도 표시하지 않는다. 겉으로는 정상
// 기동으로 보이므로 사용자는 안전망이 켜져 있다고 믿는다 — 조용한 무동작이다.
func TestNoSignalsWarnsLoudly(t *testing.T) {
	o := baseOpts(t)
	c := *o.Config
	c.Capture.Signals = nil
	o.Config = &c

	h := start(t, o)
	var found bool
	for _, e := range h.col.of("error") {
		if strings.Contains(e.Note, "signals") {
			found = true
		}
	}
	if !found {
		t.Error("시그널이 없는데 아무 경고도 없다 — 데몬이 조용히 무동작한다")
	}
}

// 훅이 잠깐 잡은 락 때문에 "이미 돌고 있다" 고 잘못 말하면 안 된다.
// 훅 스캔은 밀리초 단위인데, 하필 그때 cb watch 를 띄운 사용자는 아무도 안 도는데
// 도는 줄 알고 엉뚱한 데를 뒤진다.
//
// 락을 **명시적으로 붙잡아** 결정적으로 만든다 — ScanOnce 를 반복 호출하는 식으로는
// 창이 너무 좁아 재현되지 않는다(실제로 그렇게 썼다가 변이가 안 잡혔다).
func TestTransientLockDoesNotLookLikeRunningDaemon(t *testing.T) {
	o := baseOpts(t)
	if err := os.MkdirAll(o.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}

	lk := flock.New(filepath.Join(o.StateDir, lockFile))
	got, err := lk.TryLock()
	if err != nil || !got {
		t.Fatalf("테스트가 락을 못 잡았다: got=%v err=%v", got, err)
	}
	// 훅 한 번이 잡고 있는 시간을 흉내 낸다. lockWait(1.5초)보다 훨씬 짧다.
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = lk.Unlock()
	}()

	start(t, o) // 락이 잡힌 채로 띄운다 — 기다렸다가 떠야 한다
}

// 반대로 **진짜로** 돌고 있으면 기다린 뒤 정확히 알려야 한다.
func TestHeldLockStillReportsAlreadyRunning(t *testing.T) {
	o := baseOpts(t)
	if err := os.MkdirAll(o.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	lk := flock.New(filepath.Join(o.StateDir, lockFile))
	if got, err := lk.TryLock(); err != nil || !got {
		t.Fatalf("테스트가 락을 못 잡았다: got=%v err=%v", got, err)
	}
	t.Cleanup(func() { _ = lk.Unlock() })

	if err := Run(context.Background(), o); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("락이 계속 잡혀 있는데 %v 를 냈다", err)
	}
}
