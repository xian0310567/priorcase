package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gofrs/flock"
	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/transcript/claudecode"
)

const lockFile = "watch.lock"

// ErrAlreadyRunning 은 다른 인스턴스가 이미 돌고 있다는 뜻이다 (감사 결함 3).
var ErrAlreadyRunning = errors.New("cb watch 가 이미 돌고 있다")

// Options 는 데몬 설정이다. 시간 값을 config 가 아니라 여기서 받는 이유는 테스트가
// 밀리초 단위로 돌아야 하기 때문이다 — config 의 quiesce_seconds 는 초 단위다.
type Options struct {
	StateDir       string
	TranscriptRoot string
	Config         *config.Config

	// Quiesce 는 쓰기가 멎고 이만큼 지나야 스캔한다. 0 이면 설정값에서 온다.
	Quiesce time.Duration

	// Backfill 이 true 면 첫 기동에서 기존 파일도 처음부터 훑는다.
	//
	// 기본은 false 다. 실측으로 기존 transcript 가 1173개였는데 그걸 전부 처음부터
	// 훑으면 pending 이 쏟아지고, 안전망이 소음이 되면 에이전트가 무시하는 법을
	// 배운다. 데몬은 **지금부터** 놓친 것을 줍는 도구다.
	Backfill bool

	// OnEvent 는 진행 상황을 알린다. nil 이면 아무 데도 안 알린다.
	OnEvent func(Event)
}

// Event 는 데몬이 밖에 알리는 사건이다. 데몬은 백그라운드라 조용히 실패하면
// 아무도 모른다 — 그래서 성공·실패를 다 흘려보내고 호출자가 어디로 낼지 정한다.
type Event struct {
	Kind   string // "seed" | "scan" | "error" | "ready"
	Path   string
	Result ScanResult
	Err    error
	Note   string
}

// Run 은 데몬을 돌린다. ctx 가 끝나거나 치명적 에러가 날 때까지 돌아온다.
func Run(ctx context.Context, o Options) error {
	if o.Config == nil {
		return errors.New("설정이 없다")
	}
	if o.Quiesce <= 0 {
		q := o.Config.Capture.QuiesceSeconds
		if q <= 0 {
			q = 3
		}
		o.Quiesce = time.Duration(q) * time.Second
	}
	if err := os.MkdirAll(o.StateDir, 0o700); err != nil {
		return err
	}

	// 감사 결함 3 — 단일 인스턴스. flock 은 프로세스가 죽으면 커널이 알아서 놓아
	// 주므로 pid 파일처럼 죽은 락이 남지 않는다.
	lk := flock.New(filepath.Join(o.StateDir, lockFile))
	got, err := lk.TryLock()
	if err != nil {
		return fmt.Errorf("락을 잡을 수 없다: %w", err)
	}
	if !got {
		return ErrAlreadyRunning
	}
	defer func() { _ = lk.Unlock() }()

	st := NewStore(o.StateDir)
	if err := st.Load(); err != nil {
		return err
	}

	d := &watcher{o: o, st: st, dirty: map[string]bool{}}
	return d.run(ctx)
}

type watcher struct {
	o     Options
	st    *Store
	mu    sync.Mutex
	dirty map[string]bool
}

func (d *watcher) emit(e Event) {
	if d.o.OnEvent != nil {
		d.o.OnEvent(e)
	}
}

func (d *watcher) run(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	if err := d.watchTree(w, d.o.TranscriptRoot); err != nil {
		return err
	}
	d.seed()
	d.emit(Event{Kind: "ready", Note: fmt.Sprintf("감시 시작 (%s)", d.o.TranscriptRoot)})

	// 디바운스 타이머. 처음에는 멈춰 있다.
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	armed := false

	for {
		select {
		case <-ctx.Done():
			return nil

		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			// 새 프로젝트 디렉토리가 생기면 그 아래도 감시한다. 안 그러면 데몬을
			// 켜 둔 채 새 프로젝트를 시작했을 때 그 세션만 통째로 안 보인다.
			if ev.Has(fsnotify.Create) {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					if err := d.watchTree(w, ev.Name); err != nil {
						d.emit(Event{Kind: "error", Path: ev.Name, Err: err})
					}
					continue
				}
			}
			if !strings.HasSuffix(ev.Name, ".jsonl") {
				continue
			}
			if !ev.Has(fsnotify.Write) && !ev.Has(fsnotify.Create) {
				continue
			}
			d.mu.Lock()
			d.dirty[ev.Name] = true
			d.mu.Unlock()

			// 쓰기가 이어지는 동안 계속 미룬다 — 대화 도중에 반쯤 쓰인 파일을
			// 훑어 봐야 잘린 줄만 만난다.
			if armed && !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(d.o.Quiesce)
			armed = true

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			d.emit(Event{Kind: "error", Err: err})

		case <-timer.C:
			armed = false
			d.drain()
		}
	}
}

// watchTree 는 root 와 그 아래 모든 디렉토리를 감시 목록에 넣는다.
// fsnotify 는 재귀 감시를 지원하지 않는다.
func (d *watcher) watchTree(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			d.emit(Event{Kind: "error", Path: p, Err: err})
			return fs.SkipDir
		}
		if !e.IsDir() {
			return nil
		}
		if err := w.Add(p); err != nil {
			d.emit(Event{Kind: "error", Path: p, Err: err})
		}
		return nil
	})
}

// seed 는 첫 기동에서 **이미 있던** 파일의 체크포인트를 현재 끝으로 옮긴다.
//
// 데몬이 켜지기 전의 대화는 안전망 대상이 아니다. 실측으로 기존 transcript 가
// 1173개였는데 그걸 다 훑으면 pending 이 쏟아진다. 기동 후에 새로 생기는 파일은
// 체크포인트가 없어도 시딩되지 않으므로 처음부터 읽힌다 — seed 는 여기서 한 번만 돈다.
func (d *watcher) seed() {
	if d.o.Backfill {
		d.emit(Event{Kind: "seed", Note: "--backfill — 기존 파일도 처음부터 훑는다"})
		return
	}
	paths, unreadable, err := claudecode.List(d.o.TranscriptRoot)
	if err != nil {
		d.emit(Event{Kind: "error", Err: err})
		return
	}
	if unreadable > 0 {
		d.emit(Event{Kind: "error", Note: fmt.Sprintf("디렉토리 %d개를 읽지 못했다", unreadable)})
	}
	n := 0
	for _, p := range paths {
		if d.st.Checkpoint(p) != 0 {
			continue // 이전 기동에서 이미 본 파일
		}
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if fi.Size() == 0 {
			continue
		}
		if err := d.st.Advance(p, fi.Size(), fi.Size()); err != nil {
			d.emit(Event{Kind: "error", Path: p, Err: err})
			continue
		}
		n++
	}
	d.emit(Event{Kind: "seed", Note: fmt.Sprintf("기존 transcript %d개를 현재 지점부터 감시한다 (%d개 중)", n, len(paths))})
}

// drain 은 쌓인 파일을 훑는다.
func (d *watcher) drain() {
	d.mu.Lock()
	paths := make([]string, 0, len(d.dirty))
	for p := range d.dirty {
		paths = append(paths, p)
	}
	d.dirty = map[string]bool{}
	d.mu.Unlock()

	for _, p := range paths {
		r, err := Scan(d.st, d.o.Config, p)
		if err != nil {
			d.emit(Event{Kind: "error", Path: p, Err: err})
			continue
		}
		d.emit(Event{Kind: "scan", Path: p, Result: r})
	}
}
