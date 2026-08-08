// Package daemon 은 놓친 기록을 줍는 안전망이다.
//
// **LLM 을 부르지 않는다.** transcript 를 읽어 "이 구간에 결정이 있었을 수 있다" 는
// 플래그만 남기고, 판별은 다음 세션의 에이전트가 한다 — 그 모델이 이미 전체 맥락을
// 갖고 있고, 키 등록이 오픈소스 진입 장벽이 되기 때문이다
// ([[casebook-결정-기록회수모델-에이전트주도-2026-08-07]]).
package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/core/xdgpath"
)

const stateFile = "state.json"

// DefaultDir 은 상태 파일이 놓이는 자리다. **볼트 밖이다** (스펙 §5) — 볼트에는
// 사람이 읽을 문서만 두고, 기계 상태는 XDG state 로 보낸다.
func DefaultDir() (string, error) {
	state, err := xdgpath.StateHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "casebook"), nil
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
	// cb doctor 가 후자를 전자로 보고했다.
	At time.Time `json:"at,omitempty"`

	// Credited 는 **면제로 이미 소모한 결정 노트 수**다.
	//
	// 결정 노트 하나는 구간 하나를 면제한다. 노트가 새로 생기지 않으면 다음 구간은
	// 면제되지 않는다. 옛 구현은 노트가 하나라도 있으면 그 세션을 영구 면제했는데,
	// 그러면 세션 앞부분에서 한 번 기록한 뒤로는 무엇을 놓쳐도 안전망이 안 본다.
	Credited int `json:"credited,omitempty"`

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

// key 는 중복 판정 기준이다. **구간의 시작점**으로 잡는다 — 체크포인트가 전진하지
// 못한 구간은 매 스캔마다 같은 From 으로 다시 오는데, 그때 새 레코드를 쌓으면
// pending 이 무한히 늘어난다. 안전망이 소음이 되면 에이전트가 무시하는 법을 배운다.
func (p Pending) key() string { return p.Path + "\x00" + fmt.Sprint(p.From) }

type state struct {
	Checkpoints map[string]Checkpoint `json:"checkpoints"`
	Pending     []Pending             `json:"pending"`
}

// Store 는 상태 파일 하나를 소유한다. 데몬은 단일 인스턴스지만 fsnotify 콜백이
// 여러 고루틴에서 오므로 뮤텍스를 둔다.
type Store struct {
	dir string
	mu  sync.Mutex
	st  state
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
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := s.st.Checkpoints[path]
	cp.Offset, cp.Size = offset, size
	s.st.Checkpoints[path] = cp
	return s.save()
}

// Credit 은 소모성 면제를 판정하고 그 결과를 체크포인트에 새긴다.
//
// count 는 지금 볼트에서 "이 구간을 가려 줄 수 있는" 결정 노트 수다(세션 대조 또는
// 날짜+도메인 대조에 걸린 수). **지난번 소모분보다 늘었을 때만** 면제한다 — 노트가
// 새로 생겼다는 것은 그 사이에 에이전트가 기록을 했다는 직접 증거이고, 안 늘었다면
// 이 구간은 아직 아무도 안 본 것이다.
//
// 이 한 줄이 옛 구현과 갈린다. 옛 구현은 `count > 0` 이면 면제였고, 그래서 세션
// 첫머리에서 노트 하나를 남긴 뒤로는 그 세션이 영원히 안전망 밖이었다. 컷오버
// 1일차에 이 세션의 노트 11건이 정확히 그 상태를 만들어 판별기가 한 번도 못 돌았다.
func (s *Store) Credit(path string, count int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := s.st.Checkpoints[path]
	if count <= cp.Credited {
		return false, nil
	}
	cp.Credited = count
	cp.Suppressed++
	s.st.Checkpoints[path] = cp
	return true, s.save()
}

// NoteScan 은 훑은 흔적을 남긴다. **아무 일도 안 한 스캔도 남긴다** — 그것이
// "돌고 있다" 는 유일한 증거다.
func (s *Store) NoteScan(path string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := s.st.Checkpoints[path]
	cp.At = at
	s.st.Checkpoints[path] = cp
	return s.save()
}

// LastScan 은 어느 파일이든 마지막으로 훑은 시각이다. 하나도 없으면 제로값이다.
func (s *Store) LastScan() time.Time {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Pending {
		if s.st.Pending[i].key() == p.key() {
			s.st.Pending[i] = p
			return s.save()
		}
	}
	s.st.Pending = append(s.st.Pending, p)
	return s.save()
}

// Pending 은 표시된 구간을 오래된 순으로 준다.
func (s *Store) Pending() []Pending {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Pending, len(s.st.Pending))
	copy(out, s.st.Pending)
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// Resolve 는 확인이 끝난 구간을 지운다. 에이전트가 판단을 마쳤을 때 부른다.
func (s *Store) Resolve(path string, from int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := Pending{Path: path, From: from}.key()
	kept := s.st.Pending[:0]
	for _, p := range s.st.Pending {
		if p.key() != k {
			kept = append(kept, p)
		}
	}
	s.st.Pending = kept
	return s.save()
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
