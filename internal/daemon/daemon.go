package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gofrs/flock"
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
)

const lockFile = "watch.lock"

// ErrAlreadyRunning 은 다른 인스턴스가 이미 돌고 있다는 뜻이다 (감사 결함 3).
var ErrAlreadyRunning = errors.New("prior watch 가 이미 돌고 있다")

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

	// JudgeAvailable 이 true 면 키워드 시그널을 건너뛴다. 판정은 판별기가 한다.
	JudgeAvailable bool

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
	got, err := acquireLock(ctx, lk)
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

	d0 := &watcher{o: o}
	// 시그널이 하나도 없으면 어떤 구간도 표시되지 않는다 — 데몬이 돌긴 도는데
	// **아무 일도 안 한다.** 설정에 [capture] 절을 안 쓰면 이 상태가 되는데, 겉으로는
	// 정상 기동으로 보여서 "안전망이 켜져 있다" 고 믿게 된다. 무동작을 조용히 두지 않는다.
	if len(o.Config.Capture.Signals) == 0 && !o.JudgeAvailable {
		d0.emit(Event{Kind: "error", Note: "설정에 [capture] signals 가 없고 판별기도 없다 — " +
			"어떤 구간도 표시되지 않는다. 데몬이 사실상 아무 일도 하지 않는다"})
	}

	d := &watcher{o: o, st: st, l: store.NewLayout(o.Config), dirty: map[string]bool{}}
	return d.run(ctx)
}

type watcher struct {
	o  Options
	st *Store
	l  *store.Layout
	// hosts 는 run 이 시작할 때 푼 호스트별 루트다. 스캔이 이걸로 파서를 고른다.
	hosts []hosts.Resolved
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

	// **호스트마다 루트가 따로다.** 하나만 감시하면 나머지 호스트의 대화는
	// 파서가 있어도 영영 안 읽힌다.
	rs, err := ResolveHosts(d.o.Config, d.o.TranscriptRoot)
	if err != nil {
		return err
	}
	d.hosts = rs
	var watched []string
	for _, r := range rs {
		if err := d.watchTree(w, r.Root); err != nil {
			// 필수 호스트가 아니면 자리가 없는 것이 정상이다 — 그 사람은 그 도구를
			// 안 쓴다. 필수인데 없으면 배선이 틀린 것이라 알린다.
			if r.Host.Required {
				return err
			}
			continue
		}
		watched = append(watched, r.Host.Name+"("+r.Root+")")
	}
	d.startupPass(ctx)
	d.emit(Event{Kind: "ready", Note: fmt.Sprintf("감시 시작 (%s)", strings.Join(watched, " · "))})

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
			d.drain(ctx, true)
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

// startupPass 는 기동 시 한 번 도는 정리다. 파일마다 둘 중 하나를 한다.
//
//   - **처음 보는 파일이면 끝으로 시딩한다.** 데몬이 켜지기 전의 대화는 안전망 대상이
//     아니다. 실측으로 기존 transcript 가 1173개였는데 그걸 다 훑으면 pending 이
//     쏟아지고, 안전망이 소음이 되면 에이전트가 무시하는 법을 배운다.
//     (--backfill 이면 시딩하지 않고 훑는다.)
//   - **이미 아는 파일이면 훑는다.** 데몬이 꺼져 있는 동안 자란 구간이 있을 수 있다.
//     이걸 안 하면 데몬이 죽어 있는 사이에 끝난 세션은 **영원히** 검토되지 않는다 —
//     그 파일은 다시 바뀌지 않으므로 fsnotify 이벤트가 두 번 다시 오지 않는다.
//
// 기동 후에 새로 생기는 파일은 여기 오지 않으므로 체크포인트가 없어도 0부터 읽힌다.
func (d *watcher) startupPass(ctx context.Context) {
	// **판단은 PlanSweep 하나에 있다.** 훅의 훑기도 같은 것을 쓴다 — 각자 구현하면
	// 시딩 규칙이 어긋나고, 한쪽에서 pending 이 쏟아진다.
	plan, err := PlanSweep(d.st, d.hosts, d.o.Backfill)
	if err != nil {
		d.emit(Event{Kind: "error", Err: err})
		return
	}
	if plan.Unreadable > 0 {
		d.emit(Event{Kind: "error", Note: fmt.Sprintf("디렉토리 %d개를 읽지 못했다", plan.Unreadable)})
	}

	seeded := SeedToEnd(d.st, plan.Seed)

	// **데몬도 정리한다.** 정리를 훑기에만 넣으면 데몬을 켠 사용자는 한 번도 안
	// 돈다 — 훑기는 락을 못 얻으면 통째로 건너뛰기 때문이다. 정리를 그쪽에 둔
	// 근거("훑기가 이미 파일 목록과 락을 쥐고 있다")가 데몬에도 그대로 적용된다.
	if n, err := PruneMissing(d.st); err != nil {
		d.emit(Event{Kind: "error", Err: err})
	} else if n > 0 {
		d.emit(Event{Kind: "seed", Note: fmt.Sprintf("체크포인트 %d개를 정리했다 "+
			"(사라진 파일 · 진행 없음)", n)})
	}
	queued := 0
	for _, p := range plan.Scan {
		d.mu.Lock()
		d.dirty[p] = true
		d.mu.Unlock()
		queued++
	}
	d.emit(Event{Kind: "seed", Note: fmt.Sprintf(
		"transcript %d개 — 현재 지점부터 감시 %d개 · 밀린 구간 확인 %d개",
		len(plan.Seed)+len(plan.Scan), seeded, queued)})
	if queued > 0 {
		d.drain(ctx, false)
	}
}

// drain 은 쌓인 파일을 훑는다.
func (d *watcher) drain(ctx context.Context, promote bool) {
	d.mu.Lock()
	paths := make([]string, 0, len(d.dirty))
	for p := range d.dirty {
		paths = append(paths, p)
	}
	d.dirty = map[string]bool{}
	d.mu.Unlock()

	flagged := false
	for _, p := range paths {
		r, err := Scan(d.st, d.o.Config, d.l, p, d.o.JudgeAvailable, d.hosts)
		if err != nil {
			d.emit(Event{Kind: "error", Path: p, Err: err})
			continue
		}
		if r.Flagged {
			flagged = true
		}
		d.emit(Event{Kind: "scan", Path: p, Result: r})
	}

	// **표시한 것을 여기서 승격한다.**
	//
	// 스펙 §9 는 훅 없는 호스트에서도 "놓친 기록 줍기 | 데몬 | 동일" 이라고 약속하는데,
	// 승격을 부르는 곳이 훅 하나뿐이었다 — Codex·Cursor 처럼 훅이 없는 호스트에서는
	// 표시만 쌓이고 아무것도 안 남았다. 약속과 코드가 어긋나 있었다.
	//
	// 표시가 새로 생겼을 때만 부른다. 매 drain 마다 부르면 판별기가 이미 기각한 것을
	// 계속 다시 물게 되는데, 기각된 구간은 곧 해소되므로 실제로는 빈 목록을 도는
	// 비용뿐이다 — 그래도 판별기 탐색(exec)이 매번 도는 것은 낭비다.
	// **기동 패스에서는 승격하지 않는다** (promote=false).
	//
	// 두 가지 때문이다. 하나, 판별기 호출은 초 단위인데 기동을 거기서 막으면
	// `prior watch` 가 한참 뜨지 않는다. 둘, `--backfill` 이면 밀린 구간이 수백 개일 수
	// 있고 그걸 한꺼번에 판별기에 밀어 넣으면 호스트 CLI 를 두들기게 된다.
	// 정상 감시 루프의 drain 이 곧 다시 와서 처리한다.
	if promote && flagged {
		Promote(ctx, PromoteOptions{
			StateDir: d.o.StateDir, Config: d.o.Config, Layout: d.l,
			Err: d.promoteWriter(), Label: "prior watch",
		})
	}

	// **취소된 세션 끝 훅을 메운다.**
	//
	// 결정 노트는 아크 판정에서만 나오고, 아크는 세션 경계에서만 볼 수 있다. 그런데
	// 그 경계의 훅이 안 돌 수 있다 — 실측으로 사용자가
	// `SessionEnd hook ... failed: Hook cancelled` 를 봤다. 호스트가 종료 과정에서
	// 훅을 취소하면 그 세션의 결정은 영영 없다.
	//
	// 그래서 데몬이 오래 잠잠한 transcript 를 대신 판정한다. 이 도구가 "안전망" 인
	// 이유가 원래 이것이다 — 협조하지 않는 경로가 있으면 그 뒤에 서는 것.
	if promote {
		d.arcStale(ctx)
	}
}

// arcQuietFor 는 "이 세션은 끝났다고 봐도 된다" 는 침묵의 길이다.
//
// 세션이 정상적으로 끝나면 훅이 즉시 아크를 판정하므로 이 경로는 **취소·크래시
// 전용**이다. 그래서 늦는 것이 거의 손해가 아니고, 반대로 짧게 잡으면 사람이 잠깐
// 자리를 비운 대화를 끝난 것으로 오해해 아직 진행 중인 흐름에서 결정 노트가 나온다.
//
// 20분으로 둔다. 대화 중이면 도구 호출만으로도 transcript 가 몇 분 안에 자란다 —
// 20분 완전 침묵은 활동 중인 세션에서 나오기 어렵다.
//
// 오판해도 손실은 아니다. Decided 표식이 전진하므로 남은 대화는 다음 아크가 본다.
const arcQuietFor = 20 * time.Minute

// arcStale 은 오래 잠잠하고 아직 결정 판정하지 않은 transcript 하나를 판정한다.
//
// **한 판에 하나만 한다.** 판별기 호출은 초 단위인데 여기서 여러 개를 돌리면
// 데몬의 감시 루프가 그만큼 멎고, 밀린 파일이 많을 때(--backfill 뒤) 호스트 CLI 를
// 두들긴다. 후보는 매 drain 마다 다시 보므로 결국 전부 처리된다.
func (d *watcher) arcStale(ctx context.Context) {
	st := NewStore(d.o.StateDir)
	if err := st.Load(); err != nil {
		return
	}
	now := time.Now()
	for path, cp := range st.CheckpointSnapshot() {
		if cp.At.IsZero() || now.Sub(cp.At) < arcQuietFor {
			continue // 아직 활동 중이거나 흔적이 없다
		}
		if cp.Decided >= cp.Offset {
			continue // 이미 판정했다
		}
		PromoteArc(ctx, ArcOptions{
			StateDir: d.o.StateDir, Config: d.o.Config, Layout: d.l,
			Path: path, Err: d.promoteWriter(), Label: "prior watch (잠잠한 세션)",
		})
		return
	}
}

// promoteWriter 는 승격 진행 보고가 나갈 자리다. 데몬은 이벤트로 말하므로
// 그 통로에 얹는다 — 별도 stderr 로 새면 `prior watch` 의 출력이 두 갈래가 된다.
func (d *watcher) promoteWriter() io.Writer { return eventWriter{d} }

type eventWriter struct{ d *watcher }

func (w eventWriter) Write(b []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if line != "" {
			w.d.emit(Event{Kind: "promote", Note: line})
		}
	}
	return len(b), nil
}

// ScanOnce 는 데몬이 돌고 있지 않을 때만 파일 하나를 훑는다.
//
// **이것이 데몬 없이도 안전망이 도는 이유다.** prior watch 는 상태 디렉토리에 flock 을
// 잡고 산다. 훅이 TryLock 을 해서 **얻으면 데몬이 없는 것**이므로 자기가 훑고 놓는다.
// 못 얻으면 데몬이 돌고 있으므로 건너뛴다. 소유자가 언제나 하나뿐이라 중복 처리가
// 구조적으로 불가능하고(감사 결함 3 과 같은 방어), 데몬 등록에 실패한 사용자도
// 턴 경계마다 안전망을 얻는다.
//
// owned 가 false 면 아무것도 안 한 것이다 — 실패가 아니라 "주인이 따로 있다" 는 뜻이다.
// 이걸 에러로 만들면 훅이 매번 시끄러워진다.
//
// **호스트는 Claude Code 로 고정이다.** 이 함수를 부르는 것은 Claude Code 훅뿐이고,
// 훅은 자기를 부른 호스트를 안다. 경로로 추측하면 호스트가 기록 자리를 옮기는 순간
// 훅이 자기 대화를 못 읽는다 — 그리고 그건 조용히 실패한다.
func ScanOnce(stateDir string, c *config.Config, l *store.Layout, path string, judgeAvailable bool) (r ScanResult, owned bool, err error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return r, false, err
	}
	lk := flock.New(filepath.Join(stateDir, lockFile))
	got, err := lk.TryLock()
	if err != nil {
		return r, false, fmt.Errorf("락을 잡을 수 없다: %w", err)
	}
	if !got {
		return r, false, nil // prior watch 가 돌고 있다
	}
	defer func() {
		if uerr := lk.Unlock(); uerr != nil && err == nil {
			err = uerr
		}
	}()

	st := NewStore(stateDir)
	if err := st.Load(); err != nil {
		return r, true, err
	}
	r, err = Scan(st, c, l, path, judgeAvailable, hosts.ClaudeCode())
	return r, true, err
}

// lockWait 는 "이미 돌고 있다" 고 단정하기 전에 기다리는 시간이다.
//
// 훅(prior hook stop 등)도 데몬이 없을 때 같은 락을 잡는다. 그 스캔은 밀리초 단위지만,
// 하필 그 순간 사용자가 prior watch 를 띄우면 **아무도 안 도는데 "이미 돌고 있다" 고
// 말한다.** 틀린 진단은 사용자를 엉뚱한 데로 보낸다. 잠깐 기다려 보고 판정한다.
//
// 진짜로 데몬이 돌고 있으면 이 시간만큼 에러가 늦어지는데, 장기 실행 프로세스의
// 기동에서 1.5초는 없는 것과 같다.
const lockWait = 1500 * time.Millisecond

func acquireLock(ctx context.Context, lk *flock.Flock) (bool, error) {
	deadline := time.Now().Add(lockWait)
	for {
		got, err := lk.TryLock()
		if err != nil || got {
			return got, err
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// IsRunning 은 prior watch 가 돌고 있는지 본다.
//
// 락 **파일의 존재**는 증거가 아니다 — flock 이 풀려도 파일은 남는다. 잡아 보고
// 바로 놓는 것만이 확실하다. 잡히면 아무도 안 잡고 있는 것이므로 false 다.
//
// 잡는 순간이 훅의 스캔과 겹치면 "돌고 있다" 로 잘못 나올 수 있는데, 진단 표시일
// 뿐이라 Run 처럼 기다리지 않는다 — 기다리면 prior doctor 가 1.5초 멎는다.
func IsRunning(stateDir string) bool {
	// **모르면 작업 디렉토리를 더럽히지 않는다.** filepath.Join("", lockFile) 은
	// 그냥 "watch.lock" 이라 flock 이 **지금 있는 곳에** 파일을 만든다 — 실측으로
	// 그 파일이 레포에 커밋될 뻔했다. 상태 디렉토리를 모르면 데몬도 모르는 것이다.
	if stateDir == "" {
		return false
	}
	lk := flock.New(filepath.Join(stateDir, lockFile))
	got, err := lk.TryLock()
	if err != nil {
		return false
	}
	if got {
		_ = lk.Unlock()
		return false
	}
	return true
}
