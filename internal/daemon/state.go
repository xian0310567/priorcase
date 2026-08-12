// Package daemon 은 놓친 기록을 줍는 안전망이다.
//
// transcript 를 읽어 "이 구간에 결정이 있었을 수 있다" 는 플래그(pending)를 남긴다.
// **이 패키지 자체는 판별기를 부르지 않는다** — 그건 어댑터가 SessionEnd·PreCompact
// 에서 한다(D12). 여기서 부르지 않는 이유는 세션 생명주기를 이 패키지가 모르기 때문이지,
// LLM 을 쓰지 않기로 해서가 아니다.
//
// ⚠️ 옛 D5("데몬은 LLM 을 부르지 않는다")는 2026-08-08 에 뒤집혔다 → D8.
// 규칙으로는 결정을 판정할 수 없다는 것이 실측으로 확인됐고(한 세션 후보 160개),
// 호스트 CLI 는 이미 인증돼 있어 추가 키가 0이다
// ([[priorcase-결정-자동기록-판별기복원-2026-08-08]]).
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"

	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/core/xdgpath"
)

const stateFile = "state.json"

// DefaultDir 은 상태 파일이 놓이는 자리다. **볼트 밖이다** (스펙 §5) — 볼트에는
// 사람이 읽을 문서만 두고, 기계 상태는 XDG state 로 보낸다.
func DefaultDir() (string, error) {
	state, err := xdgpath.StateHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "priorcase"), nil
}

// Checkpoint 는 transcript 파일 하나의 진행 지점이다.
type Checkpoint struct {
	// Offset 은 **성공적으로 처리를 끝낸** 바이트 수다.
	Offset int64 `json:"offset"`
	// Size 는 그때 본 파일 크기다. 다음에 파일이 이보다 작으면 잘렸거나 다른 파일로
	// 바뀐 것이므로 Offset 을 믿을 수 없다.
	Size int64 `json:"size"`

	// At 은 이 파일을 **마지막으로 훑은 시각**이다.
	//
	// 전진하지 않은 스캔도 기록한다. 이게 없으면 "안전망이 도는데 표시할 게 없다" 와
	// "안전망이 한 번도 안 돌았다" 가 똑같이 보인다 — 컷오버 1일차 회고에서 실제로
	// prior doctor 가 후자를 전자로 보고했다.
	//
	// omitzero 다 — time.Time 은 구조체라 omitempty 가 안 먹어서, 흔적이 없는
	// 체크포인트에 `"at": "0001-01-01T00:00:00Z"` 가 박힌다. 사람이 상태 파일을
	// 열어 보는데 그건 데이터처럼 보인다.
	At time.Time `json:"at,omitzero"`

	// SessionCredited 는 **세션 대조 축**에서 면제로 소모한 노트 수다.
	//
	// 이 축은 단조다 — 그 세션 id 를 단 노트는 늘기만 하고 날짜에 무관하다.
	// 그래서 개수 하나로 비교해도 안전하다.
	SessionCredited int `json:"session_credited,omitempty"`

	// DayCredited 는 **날짜별로** 면제에 소모한 노트 수다.
	//
	// 통짜 개수 하나로 두면 안 된다. 날짜 축의 계수는 *그 구간이 걸친 날짜*로
	// 걸러지므로 구간마다 창이 달라진다. 고점을 넓은 창으로 찍고 비교를 좁은
	// 창으로 하면, 바쁜 날을 한 번 지나간 세션은 **그 뒤로 영영 면제되지 않는다** —
	// 자정을 넘긴 세션, `claude --continue`, 데몬 정지 뒤 backfill, 볼트 아카이브가
	// 전부 그 상태를 만든다. 실제로 그렇게 만들었다가 리뷰에서 잡혔다.
	//
	// 날짜별로 나누면 각 날의 비교 창이 고정되어 그 고장이 사라진다.
	DayCredited map[string]int `json:"day_credited,omitempty"`

	// Suppressed 는 이 파일에서 면제로 넘긴 구간 수다. 진단용이다 — 억제가 그냥
	// 사라지면 안전망이 일을 안 하는 것과 구분되지 않는다.
	Suppressed int `json:"suppressed,omitempty"`
}

// Pending 은 "이 구간에 결정이 있었을 수 있다" 는 표시다. 결정 노트가 아니다.
type Pending struct {
	SessionID string   `json:"session_id"`
	Path      string   `json:"path"`
	Cwd       string   `json:"cwd"`
	Domain    string   `json:"domain"`
	Turns     int      `json:"turns"`
	Signals   []string `json:"signals"`
	From      int64    `json:"from"`
	To        int64    `json:"to"`
	// Days 는 **대화가 오간 날짜**다 (YYYY-MM-DD).
	//
	// At(표시한 시각)과 다르다. 에이전트는 이 값으로 대화를 찾아가므로, 표시 시각을
	// 보여 주면 엉뚱한 날을 뒤진다 — 데몬이 며칠 뒤에 켜졌거나 훅이 밀린 구간을
	// 뒤늦게 훑으면 둘이 크게 벌어진다.
	Days []string  `json:"days,omitempty"`
	At   time.Time `json:"at"`

	// Excerpt 는 그 구간의 **발화 원문**이다 (앞부분 일부).
	//
	// 오프셋만 들고 있으면 나중에 그 구간을 보려면 transcript 를 다시 읽어야 하는데,
	// 그 파일은 호스트 것이라 지워질 수 있고 경로도 바뀔 수 있다. 표시가 남아 있는데
	// 내용을 못 보면 표시가 무의미하다.
	//
	// 여기 담아 두면 ② 훅 주입(에이전트에게 들이밀기)과 ③ 자동 승격(판별기에 넘기기)이
	// 둘 다 transcript 없이 된다.
	Excerpt string `json:"excerpt,omitempty"`

	// Fails 는 판별기를 부르다 실패한 횟수다 (판정을 못 받은 것만 — "결정이 아니다"
	// 라는 판정은 성공이고 구간이 해소된다).
	//
	// **이게 없으면 실패한 구간이 영원히 돈다.** 실패해도 구간은 남고, 다음 세션이
	// 끝날 때 같은 발췌를 같은 판별기에 다시 넘긴다. 실측으로 확인했다 — 구간 하나가
	// 상한을 6번 연속으로 넘겼고, 매번 세션 끝에서 사람을 그만큼 기다리게 했다.
	// 판별기는 비결정적이라 재시도에 값이 있지만, **무한히 값이 있지는 않다.**
	Fails int `json:"fails,omitempty"`

	// ClaimedAt 은 어떤 프로세스가 이 구간을 승격하려고 집어 간 시각이다.
	//
	// 승격이 스캔 소유권 게이트 밖으로 나오면서(D12) 여러 훅이 동시에 같은 pending 을
	// 집을 수 있게 됐다 — 판별기는 비결정적이라 같은 대화에 다른 slug 의 노트가 둘 생긴다.
	ClaimedAt time.Time `json:"claimed_at,omitempty"`
}

// When 은 사람에게 보여 줄 날짜다. 대화 날짜를 알면 그것을, 모르면 표시 시각을 준다.
func (p Pending) When() string {
	if len(p.Days) > 0 {
		if len(p.Days) == 1 {
			return p.Days[0]
		}
		return p.Days[0] + "~" + p.Days[len(p.Days)-1]
	}
	return p.At.Format("2006-01-02")
}

// DecidedOn 은 이 구간의 결정에 적을 날짜다. **마지막 날이다.**
//
// When 이 주는 범위("08-08~08-10")를 그대로 쓸 수 없어서 하루를 골라야 하는데,
// 첫날은 언제나 틀린 쪽이다. 결정은 대화가 흘러간 **끝**에서 내려진다 — 앞쪽은
// 아직 논의 중인 구간이다.
//
// 실제로 그랬다. 2026-08-10 에 내린 개명 결정이 08-08 로 기록됐다. 그 세션이
// 08-08 에 시작했기 때문이다. 세션이 길수록 더 벌어진다.
func (p Pending) DecidedOn() string {
	if n := len(p.Days); n > 0 {
		return p.Days[n-1]
	}
	return p.At.Format("2006-01-02")
}

// key 는 중복 판정 기준이다. **구간의 시작점**으로 잡는다 — 체크포인트가 전진하지
// 못한 구간은 매 스캔마다 같은 From 으로 다시 오는데, 그때 새 레코드를 쌓으면
// pending 이 무한히 늘어난다. 안전망이 소음이 되면 에이전트가 무시하는 법을 배운다.
func (p Pending) key() string { return p.Path + "\x00" + fmt.Sprint(p.From) }

type state struct {
	Checkpoints map[string]Checkpoint `json:"checkpoints"`
	Pending     []Pending             `json:"pending"`
}

// stateLock 은 상태 파일을 고칠 때 잡는 잠금이다. **watch.lock 과 다른 파일이다.**
//
// watch.lock 은 "누가 스캔의 주인인가" 를 정하고 데몬이 살아 있는 내내 쥔다.
// 이건 상태 파일 한 번 고치는 동안만 쥔다.
//
// **교착이 없는 근거는 '데몬만 겹쳐 잡는다' 가 아니다** — 훅도 ScanOnce 안에서
// watch.lock 을 쥔 채 state.lock 을 잡는다. 근거는 **순서가 한 방향뿐**이라는 것이다:
// watch.lock → state.lock. 반대 순서로 잡는 코드가 하나도 없다.
//
// ⚠️ 새 경로를 넣을 때 이 순서를 뒤집지 마라. state.lock 을 쥔 채 watch.lock 을
// 기다리는 코드를 하나 넣는 순간 교착이 생긴다.
const stateLock = "state.lock"

// stateLockWait 는 상태 파일 잠금을 기다리는 상한이다. 임계 구역이 파일 하나를
// 읽고 쓰는 것뿐이라 밀리초면 끝난다 — 2초는 사실상 무한이고, 그래도 안 잡히면
// 무언가 잘못된 것이므로 조용히 진행하지 말고 알린다.
const stateLockWait = 2 * time.Second

// Store 는 상태 파일 하나를 다룬다.
//
// **디스크가 정본이다.** 메모리에 든 것은 캐시일 뿐이고, 고칠 때마다 잠금을 잡고
// 다시 읽는다. 데몬이 메모리 상태를 정본으로 쥐고 있으면, 그 사이 훅이 pending 을
// 해소해도 데몬의 다음 저장이 그것을 되살린다 — 승격을 락 밖으로 빼는 순간
// 그 경합이 실제로 생긴다.
//
// 데몬은 단일 인스턴스지만 fsnotify 콜백이 여러 고루틴에서 오므로 뮤텍스도 둔다.
type Store struct {
	dir string
	mu  sync.Mutex
	st  state
}

// mutate 는 잠금 안에서 디스크를 다시 읽고, 고치고, 쓴다.
func (s *Store) mutate(fn func(*state)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	lk := flock.New(filepath.Join(s.dir, stateLock))
	ctx, cancel := context.WithTimeout(context.Background(), stateLockWait)
	defer cancel()
	got, err := lk.TryLockContext(ctx, 20*time.Millisecond)
	if err != nil || !got {
		return fmt.Errorf("상태 파일 잠금을 잡을 수 없다 (%s): %v — "+
			"다른 priorcase 프로세스가 멈춰 있는지 확인해라", filepath.Join(s.dir, stateLock), err)
	}
	defer func() { _ = lk.Unlock() }()

	if err := s.loadLocked(); err != nil {
		return err
	}
	fn(&s.st)
	return s.save()
}

// reload 는 읽기 전에 디스크를 다시 본다.
//
// 잠금을 잡지 않는다 — save 가 원자적 교체(WriteFileAtomic)라 읽는 쪽은 언제나
// 이전 판 아니면 다음 판을 보고, 찢어진 파일을 보지 않는다.
func (s *Store) reload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.loadLocked()
}

func NewStore(dir string) *Store {
	return &Store{dir: dir, st: state{Checkpoints: map[string]Checkpoint{}}}
}

func (s *Store) path() string { return filepath.Join(s.dir, stateFile) }

// Load 는 상태 파일을 읽는다. 파일이 없으면 빈 상태로 시작한다(첫 실행).
//
// **깨져 있으면 시끄럽게 실패한다.** 조용히 빈 상태로 갈아치우면 체크포인트를 전부
// 잃고, 다음 스캔이 모든 transcript 를 처음부터 훑어 pending 을 쏟아낸다. 자동
// 복구가 더 나빠지는 경우다 — 사람이 보고 지우게 한다.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

// loadLocked 는 뮤텍스를 이미 잡은 상태에서 부른다.
func (s *Store) loadLocked() error {
	b, err := os.ReadFile(s.path())
	if os.IsNotExist(err) {
		s.st = state{Checkpoints: map[string]Checkpoint{}}
		return nil
	}
	if err != nil {
		return fmt.Errorf("상태 파일을 읽을 수 없다 (%s): %w", s.path(), err)
	}
	var st state
	if err := json.Unmarshal(b, &st); err != nil {
		return fmt.Errorf("상태 파일이 깨졌다 (%s): %w — "+
			"지우면 처음부터 다시 시작한다(이미 기록된 결정은 그대로다)", s.path(), err)
	}
	if st.Checkpoints == nil {
		st.Checkpoints = map[string]Checkpoint{}
	}
	s.st = st
	return nil
}

// save 는 뮤텍스를 이미 잡은 상태에서 부른다.
//
// 원자적으로 쓴다. 데몬은 SIGKILL 로 죽을 수 있고, 잘린 상태 파일은 위의 Load 가
// 거부하므로 다음 기동이 통째로 막힌다.
func (s *Store) save() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	return store.WriteFileAtomic(s.path(), append(b, '\n'), 0o600)
}

// Checkpoint 는 파일의 진행 지점을 준다. 파일 크기를 모를 때 쓴다.
func (s *Store) Checkpoint(path string) int64 {
	s.reload()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Checkpoints[path].Offset
}

// CheckpointFor 는 현재 파일 크기를 함께 보고 진행 지점을 준다.
//
// 파일이 지난번보다 작아졌으면 0 을 준다 — 잘렸거나 다른 파일로 바뀐 것이라,
// 옛 오프셋부터 읽으면 레코드 한복판에서 시작한다. 크기가 같거나 커진 경우만
// 이어서 읽는다. (inode 를 보면 더 정확하지만 이식성이 떨어지고, transcript 는
// 세션 UUID 로 이름이 갈려 교체가 애초에 드물다.)
func (s *Store) CheckpointFor(path string, size int64) int64 {
	s.reload()
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := s.st.Checkpoints[path]
	if size < cp.Size || size < cp.Offset {
		return 0
	}
	return cp.Offset
}

// Advance 는 진행 지점을 옮긴다. **구간을 끝까지 성공 처리했을 때만 부른다**
// (스펙 §7.2 단일 규칙). 파싱 실패가 하나라도 있으면 부르지 않는다.
//
// 나머지 필드는 **보존한다.** 통째로 갈아치우면 Credited 가 매번 0 으로 돌아가
// 면제가 다시 무한해진다 — 소모성 크레딧이 조용히 무력화된다.
func (s *Store) Advance(path string, offset, size int64) error {
	return s.mutate(func(st *state) {
		cp := st.Checkpoints[path]
		cp.Offset, cp.Size = offset, size
		st.Checkpoints[path] = cp
	})
}

// Credit 은 소모성 면제를 판정하고 그 결과를 체크포인트에 새긴다.
//
// **마지막 확인 이후 새 노트가 생겼으면 면제한다.** 노트가 새로 생겼다는 것은 그
// 사이에 에이전트가 기록을 했다는 직접 증거이고, 안 늘었다면 이 구간은 아직 아무도
// 안 본 것이다. 그때 걸린 노트들은 거기서 **소모된다** — 다음 구간을 또 면제하지 못한다.
//
// 옛 구현은 "노트가 하나라도 있는가" 였고, 그래서 세션 첫머리에서 노트 하나를 남긴
// 뒤로는 그 세션이 영원히 안전망 밖이었다. 컷오버 1일차에 이 세션의 노트 11건이
// 정확히 그 상태를 만들어 판별기가 하루 종일 한 번도 못 돌았다.
//
// **두 축을 따로 본다.** 하나로 합치면 날짜 축의 창이 바뀔 때 세션 축까지 같이
// 망가진다(위 DayCredited 주석).
func (s *Store) Credit(path string, sessionN int, perDay map[string]int) (bool, error) {
	suppress := false
	err := s.mutate(func(st *state) {
		cp := st.Checkpoints[path]
		fresh := sessionN > cp.SessionCredited
		for d, n := range perDay {
			if n > cp.DayCredited[d] {
				fresh = true
			}
		}
		if !fresh {
			return
		}
		if sessionN > cp.SessionCredited {
			cp.SessionCredited = sessionN
		}
		for d, n := range perDay {
			if cp.DayCredited == nil {
				cp.DayCredited = map[string]int{}
			}
			if n > cp.DayCredited[d] {
				cp.DayCredited[d] = n
			}
		}
		cp.Suppressed++
		st.Checkpoints[path] = cp
		suppress = true
	})
	return suppress, err
}

// CreditNote 는 **안전망이 방금 만든 노트**를 미리 소모 처리한다.
//
// 이게 없으면 안전망이 자기 출력으로 자기를 억제한다. 승격이 만든 노트는 그 구간의
// 스캔 *이후* 에 생기므로, 다음 스캔에서 "새 노트가 생겼다" 로 세어져 **아직 아무도
// 안 본 다음 구간**을 면제한다. 크레딧의 전제("노트 = 에이전트가 기록했다는 증거")가
// 자동 경로에서만 깨지는 구조적 off-by-one 이다.
//
// 노트에 출처 필드가 없어 사후에는 구분할 수 없다(스펙: 자동/수기를 구분하지 않는다).
// 그래서 만든 그 자리에서 소모시킨다.
func (s *Store) CreditNote(path, date, sessionID string) error {
	return s.mutate(func(st *state) {
		cp := st.Checkpoints[path]
		if sessionID != "" {
			cp.SessionCredited++
		}
		if date != "" {
			if cp.DayCredited == nil {
				cp.DayCredited = map[string]int{}
			}
			cp.DayCredited[date]++
		}
		st.Checkpoints[path] = cp
	})
}

// CreditNoteFor 는 프로세스 밖에서 부르는 CreditNote 다 (훅의 승격 경로).
func CreditNoteFor(dir, path, date, sessionID string) error {
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		return err
	}
	return s.CreditNote(path, date, sessionID)
}

// NoteScan 은 훑은 흔적을 남긴다. **아무 일도 안 한 스캔도 남긴다** — 그것이
// "돌고 있다" 는 유일한 증거다.
func (s *Store) NoteScan(path string, at time.Time) error {
	return s.mutate(func(st *state) {
		cp := st.Checkpoints[path]
		cp.At = at
		st.Checkpoints[path] = cp
	})
}

// LastScan 은 어느 파일이든 마지막으로 훑은 시각이다. 하나도 없으면 제로값이다.
func (s *Store) LastScan() time.Time {
	s.reload()
	s.mu.Lock()
	defer s.mu.Unlock()
	var last time.Time
	for _, cp := range s.st.Checkpoints {
		if cp.At.After(last) {
			last = cp.At
		}
	}
	return last
}

// Suppressed 는 전 파일의 누적 억제 횟수다.
func (s *Store) Suppressed() int {
	s.reload()
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, cp := range s.st.Checkpoints {
		n += cp.Suppressed
	}
	return n
}

// AddPending 은 구간을 표시한다. 같은 구간이면 새로 쌓지 않고 갱신한다.
func (s *Store) AddPending(p Pending) error {
	return s.mutate(func(st *state) {
		for i := range st.Pending {
			if st.Pending[i].key() == p.key() {
				st.Pending[i] = p
				return
			}
		}
		st.Pending = append(st.Pending, p)
	})
}

// Pending 은 표시된 구간을 오래된 순으로 준다.
func (s *Store) Pending() []Pending {
	s.reload()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Pending, len(s.st.Pending))
	copy(out, s.st.Pending)
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// Resolve 는 확인이 끝난 구간을 지운다. 에이전트가 판단을 마쳤을 때 부른다.
func (s *Store) Resolve(path string, from int64) error {
	return s.mutate(func(st *state) {
		k := Pending{Path: path, From: from}.key()
		kept := st.Pending[:0]
		for _, p := range st.Pending {
			if p.key() != k {
				kept = append(kept, p)
			}
		}
		st.Pending = kept
	})
}

// ID 는 pending 하나를 가리키는 문자열이다. 에이전트가 해소할 때 되돌려 준다.
func (p Pending) ID() string { return fmt.Sprintf("%s@%d", p.Path, p.From) }

// ParseID 는 ID 를 되돌린다. 경로에 '@' 가 있을 수 있으므로 **마지막** 것에서 자른다.
func ParseID(id string) (path string, from int64, err error) {
	i := strings.LastIndex(id, "@")
	if i < 0 {
		return "", 0, fmt.Errorf("pending id 형식이 아니다: %q", id)
	}
	from, err = strconv.ParseInt(id[i+1:], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("pending id 의 오프셋을 읽을 수 없다 (%q): %w", id, err)
	}
	return id[:i], from, nil
}

// ReadPending 은 데몬이 표시한 구간을 읽기만 한다. 데몬이 돌고 있지 않아도 안전하다.
//
// 상태 파일이 없으면 빈 목록이다(데몬을 한 번도 안 켰다는 뜻). 깨져 있으면 에러다 —
// **"미확인 0건" 과 "확인할 수 없다" 는 다른 사실이고**, 후자를 전자로 보여 주면
// 안전망이 죽은 것을 안전망이 할 일이 없는 것으로 오해하게 만든다.
func ReadPending(dir string) ([]Pending, error) {
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		return nil, err
	}
	return s.Pending(), nil
}

// ResolvePending 은 확인이 끝난 구간을 지운다.
func ResolvePending(dir, id string) error {
	path, from, err := ParseID(id)
	if err != nil {
		return err
	}
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		return err
	}
	return s.Resolve(path, from)
}

// MaxJudgeFails 는 한 구간을 판별기에 넘겨 볼 횟수다.
//
// 판별기는 비결정적이다 — 같은 발췌가 한 번은 상한을 넘고 다음엔 통과한다.
// 그래서 재시도에 값이 있다. 그런데 **값이 무한하지는 않다.** 세 번 연속으로
// 판정을 못 받은 발췌는 다음에도 대개 못 받는다.
//
// 3 인 이유는 실측이다. 상한(75초)이 실측 중앙값의 5배가 넘으므로 한 번의 실패는
// 대개 일시적인 부하다. 세 번이면 그 설명이 더는 안 통한다.
//
// 넘어서면 구간을 **지우지 않는다.** 확인 큐에 남겨 사람에게 넘긴다 — 자동으로
// 처리 못 한 것이지 결정이 없는 것이 아니다. 지워 버리면 그 자리에 있었을지 모를
// 결정이 조용히 사라진다.
const MaxJudgeFails = 3

// GaveUp 은 이 구간을 더는 자동으로 처리하지 않는다는 뜻이다.
func (p Pending) GaveUp() bool { return p.Fails >= MaxJudgeFails }

// FailPending 은 판별 실패를 한 번 새긴다. 새긴 뒤의 횟수를 준다.
//
// **선점 표시를 같이 지운다.** 실패한 구간의 선점이 claimTTL(5분) 동안 남아 있으면,
// 바로 뒤에 도는 승격이 그 구간을 "처리 중" 으로 보고 건너뛴다 — 실패를 선점으로
// 오해하는 것이다. 실패는 처리가 끝난 것이므로 자리를 비워 준다.
func FailPending(dir, id string) (int, error) {
	path, from, err := ParseID(id)
	if err != nil {
		return 0, err
	}
	n := 0
	s := NewStore(dir)
	err = s.mutate(func(st *state) {
		k := Pending{Path: path, From: from}.key()
		for i := range st.Pending {
			if st.Pending[i].key() != k {
				continue
			}
			st.Pending[i].Fails++
			st.Pending[i].ClaimedAt = time.Time{}
			n = st.Pending[i].Fails
			return
		}
	})
	return n, err
}

// claimTTL 은 승격 선점이 유효한 시간이다.
//
// 승격은 판별기(`claude --print`) 호출이라 수 초가 걸리고, 그동안 다른 프로세스가
// 같은 구간을 집으면 같은 대화에 결정 노트가 둘 생긴다. 선점 표시로 막는다.
// **프로세스가 중간에 죽어도 이 시간이 지나면 자동으로 풀린다** — 잠금 파일을
// 남기는 방식과 달리 사람이 치울 것이 없다.
const claimTTL = 5 * time.Minute

// ClaimPending 은 구간 하나를 승격 대상으로 선점한다. 이미 다른 쪽이 최근에 선점했으면
// false 다. 선점은 상태 잠금 안에서 일어나므로 둘이 동시에 성공할 수 없다.
func ClaimPending(dir, id string, now time.Time) (bool, error) {
	path, from, err := ParseID(id)
	if err != nil {
		return false, err
	}
	got := false
	s := NewStore(dir)
	err = s.mutate(func(st *state) {
		k := Pending{Path: path, From: from}.key()
		for i := range st.Pending {
			if st.Pending[i].key() != k {
				continue
			}
			if !st.Pending[i].ClaimedAt.IsZero() && now.Sub(st.Pending[i].ClaimedAt) < claimTTL {
				return // 다른 쪽이 처리 중이다
			}
			st.Pending[i].ClaimedAt = now
			got = true
			return
		}
	})
	return got, err
}
