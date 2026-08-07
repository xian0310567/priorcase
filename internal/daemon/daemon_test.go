package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

// --backfill 은 그 반대다.
func TestBackfillScansExistingFiles(t *testing.T) {
	o := baseOpts(t)
	o.Backfill = true
	old := filepath.Join(o.TranscriptRoot, "proj-a", "old.jsonl")
	writeLines(t, old, turns(t, 20, "여기서 결정했다", "/tmp/proj/alpha")...)

	h := start(t, o)
	// backfill 은 시딩을 건너뛸 뿐이므로, 파일이 건드려질 때 훑힌다.
	writeLines(t, old, turns(t, 1, "덧붙임", "/tmp/proj/alpha")...)
	waitFor(t, 3*time.Second, "backfill 스캔", func() bool { return len(h.col.of("scan")) > 0 })

	s := NewStore(o.StateDir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if len(s.Pending()) == 0 {
		t.Error("--backfill 인데 기존 내용이 표시되지 않았다")
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
