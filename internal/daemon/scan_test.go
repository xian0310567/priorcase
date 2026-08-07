package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/xian0310567/casebook/internal/core/config"
)

func scanCfg() *config.Config {
	return &config.Config{
		Vault:   "/tmp/vault",
		Exclude: []string{"/tmp/proj/secret"},
		Capture: config.Capture{
			Signals:        []string{"결정", "채택"},
			MinTurns:       6,
			QuiesceSeconds: 1,
		},
		Domain: []config.Domain{
			{Prefix: "alpha", Folder: "alpha", Paths: []string{"/tmp/proj/alpha"}},
			{Prefix: "secret", Folder: "secret", Paths: []string{"/tmp/proj/secret"}},
		},
	}
}

// turnLine 은 발화 한 줄을 만든다. cwd 를 바꿔 제외 규칙을 시험할 수 있다.
func turnLine(i int, text, cwd string) string {
	return fmt.Sprintf(
		`{"type":"assistant","cwd":%q,"sessionId":"S1","timestamp":"2026-08-07T01:00:%02dZ","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`+"\n",
		cwd, i, text)
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l); err != nil {
			t.Fatal(err)
		}
	}
}

func turns(t *testing.T, n int, text, cwd string) []string {
	t.Helper()
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, turnLine(i, text, cwd))
	}
	return out
}

// ★ 턴 수가 임계에 못 미치면 **체크포인트를 전진시키면 안 된다.**
//
// 전진시키면 임계가 영원히 안 찬다: 4턴 보고 전진 → 다음에 또 4턴 보고 전진 →
// 매번 4 < 6 이라 아무것도 표시되지 않는다. 대화가 아무리 길어져도 안전망이
// 한 번도 발동하지 않는 침묵 실패다.
func TestUnderThresholdDoesNotAdvance(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)
	c := scanCfg()

	writeLines(t, tp, turns(t, 4, "여기서 결정했다", "/tmp/proj/alpha")...)
	r1, err := Scan(s, c, tp)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Flagged {
		t.Error("4턴인데 표시했다 (임계 6)")
	}
	if r1.Advanced {
		t.Fatal("임계 미달인데 체크포인트를 전진시켰다 — 임계가 영원히 안 찬다")
	}

	// 4턴 더. 누적 8턴이 보여야 한다.
	writeLines(t, tp, turns(t, 4, "여기서 결정했다", "/tmp/proj/alpha")...)
	r2, err := Scan(s, c, tp)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Turns != 8 {
		t.Errorf("누적 턴 %d, 8이어야 한다 — 앞 4턴을 잃었다", r2.Turns)
	}
	if !r2.Flagged {
		t.Error("8턴 + 시그널인데 표시하지 않았다")
	}
	if !r2.Advanced {
		t.Error("처리를 끝냈는데 전진하지 않았다")
	}
}

// 임계를 넘겼는데 시그널이 없으면 표시하지 않되 전진한다 — 다 보고 결정이 없다고
// 판단한 것이므로 다시 볼 이유가 없다.
func TestOverThresholdWithoutSignalAdvancesWithoutFlag(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)

	writeLines(t, tp, turns(t, 8, "그냥 잡담이다", "/tmp/proj/alpha")...)
	r, err := Scan(s, scanCfg(), tp)
	if err != nil {
		t.Fatal(err)
	}
	if r.Flagged {
		t.Error("시그널이 없는데 표시했다")
	}
	if !r.Advanced {
		t.Error("다 보고 결정이 없다고 판단했으면 전진해야 한다")
	}
}

// 감사 결함 1·2 — 깨진 줄이 하나라도 있으면 전진하지 않는다.
func TestCorruptLineBlocksAdvance(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)

	lines := turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")
	lines = append(lines, "{이건 JSON 이 아니다\n")
	writeLines(t, tp, lines...)

	r, err := Scan(s, scanCfg(), tp)
	if err != nil {
		t.Fatal(err)
	}
	if r.Bad != 1 {
		t.Errorf("Bad = %d, 1이어야 한다", r.Bad)
	}
	if r.Advanced {
		t.Fatal("깨진 줄이 있는데 전진했다 — 그 구간은 영원히 검토되지 않는다")
	}
	if !r.Flagged {
		t.Error("전진은 못 해도 표시는 해야 한다")
	}
	if got := s.Checkpoint(tp); got != 0 {
		t.Errorf("체크포인트 = %d, 0이어야 한다", got)
	}
}

// 전진 못 한 구간을 반복 스캔해도 pending 이 늘지 않는다.
func TestRepeatedScanOfBlockedSegmentDoesNotPileUp(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)
	c := scanCfg()

	lines := append(turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha"), "{깨짐\n")
	writeLines(t, tp, lines...)

	for i := 0; i < 3; i++ {
		if _, err := Scan(s, c, tp); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(s.Pending()); got != 1 {
		t.Errorf("3번 스캔 후 pending %d건, 1건이어야 한다 — 무한 증식한다", got)
	}
}

// **제외된 프로젝트는 표시하지 않는다.** NOI 처럼 casebook 이 손대면 안 되는 구역이
// 있고, pending 은 다음 세션 에이전트에게 "여기 기록해라" 라고 말하는 것이다.
func TestExcludedCwdIsNotFlagged(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)

	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/secret")...)
	r, err := Scan(s, scanCfg(), tp)
	if err != nil {
		t.Fatal(err)
	}
	if r.Flagged {
		t.Error("제외된 경로의 구간을 표시했다")
	}
	if !r.Advanced {
		t.Error("제외 구역도 다 읽었으면 전진해야 한다 — 안 그러면 매번 다시 읽는다")
	}
}

func TestPendingCarriesDomainAndSignals(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)

	writeLines(t, tp, turns(t, 8, "이 방식을 채택하기로 결정했다", "/tmp/proj/alpha")...)
	if _, err := Scan(s, scanCfg(), tp); err != nil {
		t.Fatal(err)
	}
	p := s.Pending()
	if len(p) != 1 {
		t.Fatalf("pending %d건", len(p))
	}
	if p[0].Domain != "alpha" {
		t.Errorf("Domain = %q, want alpha", p[0].Domain)
	}
	if p[0].SessionID != "S1" {
		t.Errorf("SessionID = %q, want S1", p[0].SessionID)
	}
	if len(p[0].Signals) != 2 {
		t.Errorf("Signals = %v, 결정·채택 둘 다 걸려야 한다", p[0].Signals)
	}
}

// 새로 읽을 것이 없으면 아무 일도 하지 않는다 (fsnotify 가 무관한 이벤트를 줄 수 있다).
func TestNothingNewIsNoop(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)
	c := scanCfg()

	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)
	if _, err := Scan(s, c, tp); err != nil {
		t.Fatal(err)
	}
	before := len(s.Pending())

	r, err := Scan(s, c, tp)
	if err != nil {
		t.Fatal(err)
	}
	if r.Turns != 0 {
		t.Errorf("새 내용이 없는데 턴 %d개를 읽었다", r.Turns)
	}
	if len(s.Pending()) != before {
		t.Error("새 내용이 없는데 pending 이 바뀌었다")
	}
}
