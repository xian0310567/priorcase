package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

func TestCheckpointRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if got := s.Checkpoint("/t/a.jsonl"); got != 0 {
		t.Errorf("처음 보는 파일의 체크포인트 = %d, 0이어야 한다", got)
	}
	if err := s.Advance("/t/a.jsonl", 120, 120); err != nil {
		t.Fatal(err)
	}

	// 새 Store 로 다시 읽어도 남아 있어야 한다 — 데몬이 재시작해도 구간을 다시 훑지 않는다.
	s2 := NewStore(dir)
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	if got := s2.Checkpoint("/t/a.jsonl"); got != 120 {
		t.Errorf("재기동 후 체크포인트 = %d, 120이어야 한다", got)
	}
}

// 파일이 체크포인트보다 작아졌다 = 잘렸거나 다른 파일로 바뀌었다. 그 오프셋부터
// 읽으면 엉뚱한 바이트 한복판에서 시작한다. 0 으로 되돌린다.
func TestShrunkFileResetsCheckpoint(t *testing.T) {
	s := newStore(t)
	if err := s.Advance("/t/a.jsonl", 500, 500); err != nil {
		t.Fatal(err)
	}
	if got := s.CheckpointFor("/t/a.jsonl", 500); got != 500 {
		t.Errorf("크기가 같은데 %d 로 되돌렸다", got)
	}
	if got := s.CheckpointFor("/t/a.jsonl", 499); got != 0 {
		t.Errorf("파일이 줄었는데 체크포인트 = %d, 0이어야 한다", got)
	}
	if got := s.CheckpointFor("/t/a.jsonl", 900); got != 500 {
		t.Errorf("파일이 늘었는데 체크포인트 = %d, 500이어야 한다", got)
	}
}

// **전진 못 한 구간을 다시 스캔해도 pending 이 늘어나면 안 된다.**
//
// 깨진 줄이 있으면 체크포인트가 전진하지 않는데(감사 결함 1의 규칙), 그 구간을
// 매 스캔마다 새 레코드로 쌓으면 pending 이 무한 증식한다. 안전망이 소음이 되면
// 에이전트가 무시하는 법을 배우고, 그러면 없는 것과 같다.
func TestPendingIsDedupedBySegment(t *testing.T) {
	s := newStore(t)
	p := Pending{SessionID: "S1", Path: "/t/a.jsonl", From: 0, To: 400, Turns: 8}

	for i := 0; i < 3; i++ {
		if err := s.AddPending(p); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(s.Pending()); got != 1 {
		t.Fatalf("같은 구간을 3번 표시했더니 pending %d건, 1건이어야 한다", got)
	}

	// 구간이 다르면 별개다.
	p2 := p
	p2.From, p2.To = 400, 900
	if err := s.AddPending(p2); err != nil {
		t.Fatal(err)
	}
	if got := len(s.Pending()); got != 2 {
		t.Errorf("다른 구간인데 pending %d건, 2건이어야 한다", got)
	}
}

// 같은 구간을 다시 표시하면 최신 정보로 갱신된다 (턴이 더 쌓였을 수 있다).
func TestPendingUpdatesInPlace(t *testing.T) {
	s := newStore(t)
	base := Pending{SessionID: "S1", Path: "/t/a.jsonl", From: 0, To: 400, Turns: 8}
	if err := s.AddPending(base); err != nil {
		t.Fatal(err)
	}
	grown := base
	grown.To, grown.Turns = 900, 20
	if err := s.AddPending(grown); err != nil {
		t.Fatal(err)
	}
	got := s.Pending()
	if len(got) != 1 {
		t.Fatalf("pending %d건, 1건이어야 한다", len(got))
	}
	if got[0].Turns != 20 || got[0].To != 900 {
		t.Errorf("갱신이 안 됐다: %+v", got[0])
	}
}

// ★★ **면제된 구간은 표시에서만 빠진다. 목록에서 빠지는 것이 아니다.**
//
// 면제가 pending 을 아예 안 만들던 시절에는 이 구분이 없었고, 그래서 판별기가 붙은
// 뒤로 면제 한 번이 곧 기록 하나의 소멸이었다(실측: 면제 6회 / 자동 기록 0건).
// 이제 면제는 Quiet 로만 남고, **묻지 않았는데 들이미는 자리**에서만 걸러진다.
func TestForNudgeDropsQuietOnly(t *testing.T) {
	items := []Pending{
		{Path: "/t/a.jsonl", From: 0, Quiet: true},
		{Path: "/t/a.jsonl", From: 400},
		{Path: "/t/b.jsonl", From: 0, Quiet: true},
	}
	got := ForNudge(items)
	if len(got) != 1 || got[0].From != 400 {
		t.Fatalf("ForNudge = %+v — 면제된 것만 빠져야 한다", got)
	}
	// **원본은 그대로다.** 승격은 같은 목록을 거르지 않고 받아야 한다 — 거기서 빠지면
	// 판별기가 그 대화를 못 보고, 체크포인트는 이미 지나갔다.
	if len(items) != 3 || !items[0].Quiet {
		t.Errorf("원본이 변했다: %+v", items)
	}
	// 전부 면제여도 nil 이 아니라 빈 목록이다 — 호출부가 len 으로만 판정한다.
	if got := ForNudge([]Pending{{Quiet: true}}); len(got) != 0 {
		t.Errorf("ForNudge = %+v, 비어야 한다", got)
	}
}

// 면제 표시가 상태 파일을 건너 살아남아야 한다. 안 그러면 데몬이 재시작할 때마다
// 조용하던 구간이 전부 다시 들이밀어져 안전망이 소음이 된다.
func TestQuietSurvivesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPending(Pending{Path: "/t/a.jsonl", From: 0, Quiet: true, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPending(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("pending %d건, 1건이어야 한다", len(got))
	}
	if !got[0].Quiet {
		t.Error("면제 표시가 저장을 못 넘겼다 — 재시작마다 다시 들이민다")
	}
}

func TestPendingSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPending(Pending{SessionID: "S1", Path: "/t/a.jsonl", To: 400, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	s2 := NewStore(dir)
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	if got := len(s2.Pending()); got != 1 {
		t.Errorf("재기동 후 pending %d건, 1건이어야 한다", got)
	}
}

func TestLoadOnMissingFileIsEmptyNotError(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "아직없는디렉토리"))
	if err := s.Load(); err != nil {
		t.Fatalf("첫 실행에서 에러가 났다: %v", err)
	}
	if len(s.Pending()) != 0 {
		t.Error("빈 상태여야 한다")
	}
}

// 깨진 상태 파일을 조용히 빈 상태로 갈아치우면 **체크포인트를 전부 잃는다.**
// 그러면 다음 스캔이 모든 transcript 를 처음부터 훑어 pending 을 쏟아낸다.
// 시끄럽게 실패해서 사람이 보게 한다.
func TestLoadOnCorruptFileFailsLoudly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, stateFile), []byte("{이건 JSON 이 아니다"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir)
	err := s.Load()
	if err == nil {
		t.Fatal("깨진 상태 파일을 조용히 받아들였다 — 체크포인트가 전부 사라진다")
	}
	if !strings.Contains(err.Error(), stateFile) {
		t.Errorf("에러가 어느 파일인지 말하지 않는다: %v", err)
	}
}

// 상태 파일은 볼트가 아니라 XDG state 아래에 있어야 한다 (스펙 §5).
func TestDefaultDirIsOutsideVault(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdgstate")
	got, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/xdgstate", "priorcase"); got != want {
		t.Errorf("DefaultDir() = %q, want %q", got, want)
	}
}
