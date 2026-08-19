package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
	"github.com/xian0310567/priorcase/internal/transcript"
	"github.com/xian0310567/priorcase/internal/transcript/claudecode"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
)

func scanCfg() *config.Config {
	return &config.Config{
		Vaults:  []config.Vault{{Name: config.DefaultVaultName, Path: "/tmp/vault"}},
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
	r1, err := Scan(s, c, nil, tp, false, anyHost(tp))
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
	r2, err := Scan(s, c, nil, tp, false, anyHost(tp))
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
	r, err := Scan(s, scanCfg(), nil, tp, false, anyHost(tp))
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

	r, err := Scan(s, scanCfg(), nil, tp, false, anyHost(tp))
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
		if _, err := Scan(s, c, nil, tp, false, anyHost(tp)); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(s.Pending()); got != 1 {
		t.Errorf("3번 스캔 후 pending %d건, 1건이어야 한다 — 무한 증식한다", got)
	}
}

// **제외된 프로젝트는 표시하지 않는다.** NOI 처럼 priorcase 가 손대면 안 되는 구역이
// 있고, pending 은 다음 세션 에이전트에게 "여기 기록해라" 라고 말하는 것이다.
func TestExcludedCwdIsNotFlagged(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)

	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/secret")...)
	r, err := Scan(s, scanCfg(), nil, tp, false, anyHost(tp))
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
	if _, err := Scan(s, scanCfg(), nil, tp, false, anyHost(tp)); err != nil {
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
	if _, err := Scan(s, c, nil, tp, false, anyHost(tp)); err != nil {
		t.Fatal(err)
	}
	before := len(s.Pending())

	r, err := Scan(s, c, nil, tp, false, anyHost(tp))
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

// ★★ **이미 기록된 날이어도 표시는 한다 — 조용히 할 뿐이다.**
//
// 이 테스트는 정확히 거꾸로였다. `!r.Flagged` 를 요구했고, 근거는 *"에이전트가 제 할
// 일을 다 한 세션까지 표시하면 무시하는 법을 배운다"* 였다. 실측도 그 편이었다 —
// 실 transcript 1173개 중 발화 6개를 넘는 585개의 **99%(578개)** 가 시그널에 걸린다.
//
// **그 위험은 사라진 게 아니라 감수하기로 한 것이다.** 뒤집은 근거는 표시가 없다는
// 것의 뜻이 바뀌었다는 것이다: pending 은 이제 판별기의 **유일한** 입력이고
// (Promote 는 ReadPending 으로만 대상을 찾는다), 깨진 줄이 없으면 Scan 이 그대로
// Advance 를 부른다 — 표시하지 않은 구간은 판정에 닿지도 못한 채 **영구 소멸**한다.
//
// 실측이 그 값을 매겼다. 최근 7일 판정 23건 / 자동 기록 0건 / 같은 기간 면제 6회,
// 원장 23건 **전부 미기록**. 볼트에 노트가 하나 생길 때마다 다음 구간 하나가
// 사라졌다는 뜻이고, 사람이 손으로 기록할수록 자동 경로가 더 눈감는 되먹임이다.
//
// 두 위험은 **틀렸을 때의 비용이 다르다.** 소음은 회복된다 — 구간이 큐에 남아 다음
// 세션에도 뜬다. 소멸은 회복되지 않는다 — 발췌째 사라진다. 그래서 어림짐작을
// 회복 가능한 쪽으로 옮겼다: 표시는 남기고 Quiet 로 눌러, 묻지 않았는데 들이미는
// 자리(ForNudge)에서만 뺀다.
func TestAlreadyRecordedIsFlaggedButQuiet(t *testing.T) {
	vc := testutil.VaultConfig(t)
	vc.Capture = config.Capture{Signals: []string{"결정"}, MinTurns: 6}
	l := store.NewLayout(vc)

	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)

	// 픽스처 볼트의 alpha 도메인에는 2026-08-01·08-02 결정이 있다.
	// 그날 대화라면 에이전트가 제 할 일을 한 것이다.
	line := func(i int, day string) string {
		return fmt.Sprintf(
			`{"type":"assistant","cwd":"/tmp/proj/alpha","sessionId":"S1","timestamp":"%sT01:00:%02dZ","message":{"role":"assistant","content":[{"type":"text","text":"여기서 결정했다"}]}}`+"\n",
			day, i)
	}
	var recorded []string
	for i := 0; i < 8; i++ {
		recorded = append(recorded, line(i, "2026-08-01"))
	}
	writeLines(t, tp, recorded...)

	r, err := Scan(s, vc, l, tp, false, anyHost(tp))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Quiet {
		t.Error("그날 그 도메인에 결정 노트가 있는데 면제가 안 걸렸다")
	}
	if !r.Flagged {
		t.Fatal("면제됐다고 표시를 건너뛰었다 — 그 구간은 판별기에 닿지 못한 채 " +
			"체크포인트와 함께 소멸한다 (실측: 면제 6회 / 최근 7일 자동 기록 0건)")
	}
	if !r.Advanced {
		t.Error("다 봤으면 전진해야 한다")
	}

	// **그러나 조용해야 한다.** 소음 위험은 없어진 게 아니라 여기로 옮겨졌다 —
	// 표시는 남기되 묻지 않았는데 들이미는 자리에서는 빠진다.
	items := s.Pending()
	if len(items) != 1 {
		t.Fatalf("표시가 %d건이다", len(items))
	}
	if !items[0].Quiet {
		t.Error("표시가 조용하지 않다 — 제 할 일을 한 세션까지 들이밀면 무시하는 법을 배운다")
	}
	if n := len(ForNudge(items)); n != 0 {
		t.Errorf("들이밀 목록에 %d건이 남았다 — 면제가 아무 일도 안 한 셈이다", n)
	}
}

// 기록이 없는 날이면 표시한다 — 안전망이 실제로 일하는 경우다.
func TestUnrecordedDayIsFlagged(t *testing.T) {
	vc := testutil.VaultConfig(t)
	vc.Capture = config.Capture{Signals: []string{"결정"}, MinTurns: 6}
	l := store.NewLayout(vc)

	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)
	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...) // 2026-08-07

	r, err := Scan(s, vc, l, tp, false, anyHost(tp))
	if err != nil {
		t.Fatal(err)
	}
	if r.Quiet {
		t.Error("그날 결정 노트가 없는데 면제가 걸렸다")
	}
	if !r.Flagged {
		t.Error("기록 없는 날인데 표시하지 않았다 — 안전망이 일하지 않는다")
	}
	// 조용하지 않아야 한다 — 이건 실제로 들이밀어야 하는 구간이다.
	if n := len(ForNudge(s.Pending())); n != 1 {
		t.Errorf("들이밀 목록이 %d건이다 — 기록 없는 날의 표시를 감췄다", n)
	}
}

// 다른 도메인의 기록은 이 도메인을 가려 주지 않는다.
func TestOtherDomainRecordDoesNotSuppress(t *testing.T) {
	vc := testutil.VaultConfig(t)
	vc.Capture = config.Capture{Signals: []string{"결정"}, MinTurns: 6}
	vc.Domain = append(vc.Domain, config.Domain{Prefix: "gamma", Folder: "gamma", Paths: []string{"/tmp/proj/gamma"}})
	l := store.NewLayout(vc)

	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)
	// 08-01 은 alpha 에 기록이 있는 날이지만 이 대화는 gamma 다.
	var lines []string
	for i := 0; i < 8; i++ {
		lines = append(lines, fmt.Sprintf(
			`{"type":"assistant","cwd":"/tmp/proj/gamma","sessionId":"S1","timestamp":"2026-08-01T01:00:%02dZ","message":{"role":"assistant","content":[{"type":"text","text":"여기서 결정했다"}]}}`+"\n", i))
	}
	writeLines(t, tp, lines...)

	r, err := Scan(s, vc, l, tp, false, anyHost(tp))
	if err != nil {
		t.Fatal(err)
	}
	if r.Quiet {
		t.Error("다른 도메인(alpha)의 기록이 gamma 를 가렸다")
	}
	if !r.Flagged {
		t.Error("gamma 에는 기록이 없는데 표시하지 않았다")
	}
}

// 볼트를 못 읽으면 표시하는 쪽으로 기운다 — 대조 실패로 안전망이 조용히 꺼지면 안 된다.
func TestVaultReadFailureStillFlags(t *testing.T) {
	vc := testutil.VaultConfig(t)
	vc.Vaults = []config.Vault{{Name: config.DefaultVaultName, Path: filepath.Join(t.TempDir(), "없는볼트")}}
	vc.Capture = config.Capture{Signals: []string{"결정"}, MinTurns: 6}
	l := store.NewLayout(vc)

	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)
	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)

	r, err := Scan(s, vc, l, tp, false, anyHost(tp))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Flagged {
		t.Error("볼트 대조에 실패했다고 표시를 건너뛰었다 — 안전망이 조용히 꺼진다")
	}
}

// 세션 대조는 날짜·도메인과 무관하게 성립한다 — 자정을 넘긴 세션이나 도메인이 바뀐
// 경우도 잡는다. 날짜 폴백만 있으면 이런 건 놓친다.
//
// **다만 그 대조가 하는 일이 바뀌었다.** 옛 이름은 Suppresses 였고 단언은
// `!r.Flagged` 였다 — 세션이 대조되면 표시 자체가 없었다. 그러면 그 구간은 판별기의
// 유일한 입력(pending)이 되지 못한 채 체크포인트가 지나가 **영구 소멸**한다
// (TestAlreadyRecordedIsFlaggedButQuiet 의 실측). 지금 대조가 사는 곳은 Quiet 이고,
// 그것을 보는 곳은 묻지 않았는데 들이미는 자리뿐이다.
func TestSessionMatchQuietsButStillFlags(t *testing.T) {
	vc := testutil.VaultConfig(t)
	vc.Capture = config.Capture{Signals: []string{"결정"}, MinTurns: 6}
	l := store.NewLayout(vc)

	// 픽스처에 없는 날짜(2026-08-07)의 대화인데, 그 세션으로 기록된 노트를 심는다.
	note := filepath.Join(vc.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-세션기록-2026-01-01.md")
	body := "---\ntype: decision\ndate: 2026-01-01\ndomain: [alpha]\nsummary: \"x\"\n" +
		"status: active\noutcome: pending\nsupersedes: \"\"\nrelated: []\ntags: []\n" +
		"source_session: \"S1\"\n---\n\n## 결정\n\nx\n"
	if err := os.WriteFile(note, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)
	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...) // sessionId=S1, 2026-08-07

	r, err := Scan(s, vc, l, tp, false, anyHost(tp))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Quiet {
		t.Error("같은 세션으로 기록된 노트가 있는데 못 알아봤다 — 날짜·도메인이 다르다고 놓쳤다")
	}
	if !r.Flagged {
		t.Fatal("세션이 대조됐다고 표시를 건너뛰었다 — 그 구간은 판별기에 닿지 못한 채 소멸한다")
	}
	items := s.Pending()
	if len(items) != 1 || !items[0].Quiet {
		t.Errorf("표시가 조용하지 않다: %+v", items)
	}
	if n := len(ForNudge(items)); n != 0 {
		t.Errorf("들이밀 목록에 %d건이 남았다 — 세션 대조가 아무 일도 안 한 셈이다", n)
	}
}

// 다른 세션의 기록은 이 세션을 가려 주지 않는다 (날짜·도메인이 다를 때).
func TestDifferentSessionDoesNotSuppress(t *testing.T) {
	vc := testutil.VaultConfig(t)
	vc.Capture = config.Capture{Signals: []string{"결정"}, MinTurns: 6}
	vc.Domain = append(vc.Domain, config.Domain{Prefix: "gamma", Folder: "gamma", Paths: []string{"/tmp/proj/gamma"}})
	l := store.NewLayout(vc)

	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)
	var lines []string
	for i := 0; i < 8; i++ {
		lines = append(lines, fmt.Sprintf(
			`{"type":"assistant","cwd":"/tmp/proj/gamma","sessionId":"다른세션","timestamp":"2026-12-31T01:00:%02dZ","message":{"role":"assistant","content":[{"type":"text","text":"여기서 결정했다"}]}}`+"\n", i))
	}
	writeLines(t, tp, lines...)

	r, err := Scan(s, vc, l, tp, false, anyHost(tp))
	if err != nil {
		t.Fatal(err)
	}
	if r.Quiet {
		t.Error("다른 세션·다른 날·다른 도메인인데 억제됐다")
	}
	if !r.Flagged {
		t.Error("표시했어야 한다")
	}
}

// pending 은 **대화가 오간 날짜**를 담아야 한다. 표시 시각(At)을 보여 주면 에이전트가
// 엉뚱한 날을 뒤진다 — 데몬이 며칠 뒤에 켜졌거나 훅이 밀린 구간을 뒤늦게 훑으면
// 둘이 크게 벌어진다.
func TestPendingCarriesConversationDate(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)
	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...) // 2026-08-07

	if _, err := Scan(s, scanCfg(), nil, tp, false, anyHost(tp)); err != nil {
		t.Fatal(err)
	}
	p := s.Pending()[0]
	if len(p.Days) == 0 || p.Days[0] != "2026-08-07" {
		t.Errorf("Days = %v, 대화 날짜 2026-08-07 이어야 한다", p.Days)
	}
	if got := p.When(); got != "2026-08-07" {
		t.Errorf("When() = %q, 대화 날짜여야 한다 (표시 시각이 아니라)", got)
	}
}

// 여러 날에 걸친 구간의 결정 날짜는 **마지막 날**이다.
//
// 옛 구현은 첫날로 잘랐고("여러 날에 걸쳤으면 첫날로"), 그래서 2026-08-10 에 내린
// 개명 결정이 08-08 로 기록됐다 — 그 세션이 08-08 에 시작했기 때문이다. 결정은
// 대화가 흘러간 끝에서 내려지므로 첫날은 언제나 틀린 쪽이고, 세션이 길수록 더
// 벌어진다. 날짜는 파일명과 회수 정렬에 둘 다 들어가서 조용히 틀린다.
func TestDecidedOnTakesLastDay(t *testing.T) {
	for _, c := range []struct {
		name string
		days []string
		want string
	}{
		{"하루", []string{"2026-08-08"}, "2026-08-08"},
		{"사흘", []string{"2026-08-08", "2026-08-09", "2026-08-10"}, "2026-08-10"},
		{"이틀", []string{"2026-08-08", "2026-08-10"}, "2026-08-10"},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := Pending{Days: c.days}
			if got := p.DecidedOn(); got != c.want {
				t.Errorf("DecidedOn() = %q, %q 여야 한다 (구간 %v)", got, c.want, c.days)
			}
		})
	}

	// Days 를 모르면 표시 시각으로 떨어진다 — When 과 같은 규칙이다.
	at := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)
	if got := (Pending{At: at}).DecidedOn(); got != "2026-08-11" {
		t.Errorf("Days 가 없을 때 DecidedOn() = %q, 표시 시각이어야 한다", got)
	}
}

// pending 은 **발췌를 담아야** 한다. 오프셋만 들고 있으면 나중에 그 구간을 보려면
// transcript 를 다시 읽어야 하는데, 그 파일은 호스트 것이라 지워질 수 있다.
// 표시가 남아 있는데 내용을 못 보면 표시가 무의미하다.
func TestPendingCarriesExcerpt(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)
	writeLines(t, tp, turns(t, 8, "저장 엔진을 임베디드 DB 로 하기로 결정했다", "/tmp/proj/alpha")...)

	if _, err := Scan(s, scanCfg(), nil, tp, false, anyHost(tp)); err != nil {
		t.Fatal(err)
	}
	p := s.Pending()[0]
	if p.Excerpt == "" {
		t.Fatal("발췌가 비었다 — transcript 가 사라지면 이 표시는 쓸모가 없다")
	}
	if !strings.Contains(p.Excerpt, "임베디드 DB") {
		t.Errorf("발췌에 내용이 없다: %q", p.Excerpt)
	}
	if !strings.Contains(p.Excerpt, "에이전트:") {
		t.Errorf("누가 한 말인지 없다: %q", p.Excerpt)
	}
	if len(p.Excerpt) > maxExcerpt+200 {
		t.Errorf("발췌가 상한을 넘었다: %d", len(p.Excerpt))
	}
}

// transcript 가 사라져도 발췌는 남는다.
func TestExcerptSurvivesTranscriptDeletion(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)
	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)
	if _, err := Scan(s, scanCfg(), nil, tp, false, anyHost(tp)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tp); err != nil {
		t.Fatal(err)
	}
	items, err := ReadPending(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Excerpt == "" {
		t.Error("transcript 를 지웠더니 표시의 내용이 사라졌다")
	}
}

// filler 는 상한을 넘기려고 채우는 발화다. 줄 상한(maxExcerptLine)보다 짧게 만든다 —
// 잘림까지 섞이면 무엇 때문에 실패했는지 알 수 없다.
func filler(i int) transcript.Turn {
	return transcript.Turn{
		Kind: transcript.KindAssistant,
		Text: fmt.Sprintf("채움%03d ", i) + strings.Repeat("가나다라마바사아자차", 100),
	}
}

// omittedCount 는 생략 표시에 적힌 발화 수를 꺼낸다. 표시가 없으면 -1.
var omitPattern = regexp.MustCompile(`… \((\d+) 발화 생략\) …`)

func omittedCount(t *testing.T, s string) int {
	t.Helper()
	m := omitPattern.FindStringSubmatch(s)
	if m == nil {
		return -1
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("생략 표시의 수를 읽을 수 없다: %q", m[0])
	}
	return n
}

// ★★ **발췌는 앞뒤 양쪽에서 채운다.**
//
// 옛 구현은 뒤에서만 담았다. "결정은 구간 끝쪽에서 내려진다" 는 관찰은 맞았는데
// 거기서 **결론과 근거를 같은 것으로 다뤘다.** 실측(메인 세션 십분위)으로는 갈린다 —
// 결론이 든 발화는 앞 30%에 **0건**이고, 근거·대안이 든 발화는 앞 절반에 58.1%다.
//
// 뒤만 담으면 판별기는 "결론 같은 것" 만 보고 왜 그렇게 정했는지는 못 본다. 그리고
// judge.go 의 프롬프트는 근거가 발췌에 없으면 "근거가 대화에 남지 않았다" 라고 적게
// 시킨다 — 사용자가 가장 원하는 "왜" 가 매번 그 문장으로 대체된다.
func TestExcerptKeepsBothEnds(t *testing.T) {
	var turns []transcript.Turn
	turns = append(turns, transcript.Turn{
		Kind: transcript.KindUser,
		Text: "왜냐하면 벤치마크에서 임베디드DB가 3배 빨랐다 — 이게 근거다",
	})
	for i := 0; i < 200; i++ {
		turns = append(turns, filler(i))
	}
	turns = append(turns, transcript.Turn{
		Kind: transcript.KindAssistant,
		Text: "그래서 임베디드DB로 결정했다",
	})

	got := excerpt(turns)
	if !strings.Contains(got, "벤치마크에서 임베디드DB가 3배 빨랐다") {
		t.Errorf("**앞을 통째로 버렸다** — 근거가 사라졌다. 발췌 %dB:\n%.400s", len(got), got)
	}
	if !strings.Contains(got, "그래서 임베디드DB로 결정했다") {
		t.Errorf("뒤를 잃었다 — 결론이 사라졌다. 발췌 %dB:\n%.400s", len(got), got)
	}
	// 앞이 먼저 오고 뒤가 나중이어야 한다. 순서가 뒤집히면 판별기가 인과를 거꾸로 읽는다.
	if i, j := strings.Index(got, "벤치마크"), strings.Index(got, "그래서 임베디드DB"); i > j {
		t.Errorf("시간순이 아니다 (근거 %d, 결론 %d)", i, j)
	}
}

// ★ **버린 자리에 표시를 남긴다.**
//
// 표시가 없으면 판별기는 앞줄과 뒷줄이 잇달아 나온 것으로 읽는다 — 없는 인과를
// 만들어 낸다. "여기 뭔가 더 있었다" 를 알려 주는 것이 이 표시의 전부다.
func TestExcerptMarksOmission(t *testing.T) {
	var turns []transcript.Turn
	turns = append(turns, transcript.Turn{Kind: transcript.KindUser, Text: "처음"})
	for i := 0; i < 200; i++ {
		turns = append(turns, filler(i))
	}
	turns = append(turns, transcript.Turn{Kind: transcript.KindAssistant, Text: "마지막"})

	got, st := buildExcerpt(turns)
	n := omittedCount(t, got)
	if n < 0 {
		t.Fatalf("생략했는데 표시가 없다 — 판별기가 앞뒤를 이어 붙여 읽는다:\n%.400s", got)
	}
	if n != st.Omitted {
		t.Errorf("표시의 수(%d)와 통계(%d)가 다르다", n, st.Omitted)
	}
	if want := len(turns) - st.Turns; n != want {
		t.Errorf("생략 %d발화라는데 실린 것(%d)과 합치면 %d, 원래 %d발화다",
			n, st.Turns, n+st.Turns, len(turns))
	}
	if st.Bytes > maxExcerpt+200 {
		t.Errorf("상한을 넘었다: %dB (상한 %d)", st.Bytes, maxExcerpt)
	}

	// 다 담기는 구간에는 표시를 붙이지 않는다 — 없는 손실을 알리면 표시를 못 믿는다.
	short, sst := buildExcerpt([]transcript.Turn{
		{Kind: transcript.KindUser, Text: "짧다"},
		{Kind: transcript.KindAssistant, Text: "그렇다"},
	})
	if omittedCount(t, short) >= 0 {
		t.Errorf("버린 게 없는데 생략 표시를 붙였다:\n%s", short)
	}
	if sst.Omitted != 0 || sst.Turns != 2 {
		t.Errorf("통계가 틀렸다: %+v", sst)
	}
}

// ★★ **발화 하나가 발췌를 통째로 죽이면 안 된다.**
//
// 옛 구현은 예산을 넘는 줄을 만나면 `break` 했다. 그래서 구간 끝에 거대한 발화가
// 하나 있으면 거기서 발췌가 끝났다 — 2.68MB·40발화짜리 구간의 발췌가 **단 1줄**이었던
// 것이 이 경로다. 실측으로 발화 줄의 상위 1%가 전체 바이트의 25.4%를 먹는다.
func TestExcerptSurvivesGiantTurn(t *testing.T) {
	giant := strings.Repeat("붙여넣은 로그 한 줄. ", 20000) // 수십만 바이트
	turns := []transcript.Turn{
		{Kind: transcript.KindUser, Text: "이 로그를 보고 정하자"},
		{Kind: transcript.KindAssistant, Text: giant},
		{Kind: transcript.KindAssistant, Text: "그래서 캐시를 끄기로 했다"},
	}
	got, st := buildExcerpt(turns)

	if !strings.Contains(got, "이 로그를 보고 정하자") || !strings.Contains(got, "그래서 캐시를 끄기로 했다") {
		t.Fatalf("거대한 발화 하나가 이웃을 통째로 밀어냈다 (%dB, 발화 %d):\n%.300s",
			st.Bytes, st.Turns, got)
	}
	if st.Turns != 3 {
		t.Errorf("발화 %d개가 실렸다, 3개여야 한다", st.Turns)
	}
	if st.Clipped != 1 {
		t.Errorf("잘린 줄 %d개, 거대한 발화 하나여야 한다", st.Clipped)
	}
	if st.Bytes > maxExcerpt+200 {
		t.Errorf("상한을 넘었다: %dB", st.Bytes)
	}
	// **자른 자리가 보여야 한다.** 통째로 있는 것처럼 보이면 판별기가 뒤를 못 본 채
	// 다 봤다고 여긴다.
	if !strings.Contains(got, "…(중략)…") {
		t.Errorf("긴 발화를 조용히 잘랐다:\n%.300s", got)
	}
	// 한글이 3바이트라 바이트로 자르면 깨진다. 깨진 발췌는 JSON 으로도 프롬프트로도
	// 지저분해지고, 하필 그 자리가 판별기가 읽어야 할 자리다.
	if !utf8.ValidString(got) {
		t.Error("발췌에 깨진 바이트가 있다 — 룬 경계를 안 지켰다")
	}
}

// 반복은 예산을 재기 **전에** 접어야 한다. 예산을 세면서 접으면 예산 밖으로 밀려난
// 반복은 접히지도 세어지지도 않아서, 같은 명령을 20번 돌린 구간이 (×3) 으로 보인다.
func TestExcerptFoldsBeforeBudgeting(t *testing.T) {
	var turns []transcript.Turn
	turns = append(turns, transcript.Turn{Kind: transcript.KindUser, Text: "돌려 봐"})
	for i := 0; i < 20; i++ {
		turns = append(turns, transcript.Turn{Kind: transcript.KindTool, Text: "Bash go test ./..."})
	}
	turns = append(turns, transcript.Turn{Kind: transcript.KindAssistant, Text: "통과했다"})

	got, st := buildExcerpt(turns)
	if !strings.Contains(got, "(×20)") {
		t.Errorf("20번 돌린 것이 안 보인다:\n%s", got)
	}
	if strings.Count(got, "Bash go test") != 1 {
		t.Errorf("반복이 안 접혔다:\n%s", got)
	}
	if st.Turns != len(turns) {
		t.Errorf("접힌 발화가 안 세어졌다: %d, %d여야 한다", st.Turns, len(turns))
	}
}

// ★ **발췌가 얼마나 잘렸는지 밖에서 보여야 한다.**
//
// 이게 없어서 "판별기가 결정을 못 알아봤다" 와 "판별기에게 근거를 안 보여 줬다" 가
// 구별되지 않았다. 원장은 승격 성공 때만 발췌를 싣는데, 이 머신의 원장 32줄 중
// excerpt 키가 있는 줄이 **0건**이라 사후 대조가 통째로 불가능했다.
func TestScanReportsExcerptSize(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)

	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)
	r, err := Scan(s, scanCfg(), nil, tp, false, anyHost(tp))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Flagged {
		t.Fatal("표시했어야 한다 — 테스트 전제가 틀렸다")
	}
	if r.ExcerptBytes != len(s.Pending()[0].Excerpt) {
		t.Errorf("ExcerptBytes = %d, 실제 발췌는 %dB", r.ExcerptBytes, len(s.Pending()[0].Excerpt))
	}
	if r.ExcerptTurns != 8 {
		t.Errorf("ExcerptTurns = %d, 8이어야 한다", r.ExcerptTurns)
	}
	if r.ExcerptOmitted != 0 {
		t.Errorf("짧은 구간인데 %d발화를 버렸다고 한다", r.ExcerptOmitted)
	}

	// 표시하지 않은 구간에는 발췌를 만들지 않으므로 0이다 — 0을 "다 담았다" 로
	// 읽으면 안 된다는 뜻이라, 이 구별이 로그에 나갈 때 함께 다뤄져야 한다.
	s2 := newStore(t)
	tp2 := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, tp2, turns(t, 8, "그냥 잡담이다", "/tmp/proj/alpha")...)
	r2, err := Scan(s2, scanCfg(), nil, tp2, false, anyHost(tp2))
	if err != nil {
		t.Fatal(err)
	}
	if r2.Flagged || r2.ExcerptBytes != 0 {
		t.Errorf("표시 안 한 구간에 발췌가 있다: %+v", r2)
	}
}

// ★★ **"생략 0" 과 "발췌 없음" 이 둘 다 0 으로 보이면 안 된다.**
//
// 표시하지 않은 구간은 발췌를 아예 만들지 않아 셋 다 0이다. 거기에 "생략 0" 이라
// 적으면 사람은 "다 담았다" 로 읽는다 — 그건 이 필드들을 만든 이유(발췌가 잘렸는지를
// 사후에 대조한다)를 정확히 무너뜨린다. 침묵 실패를 진단 문구 쪽에서 되살리는 셈이다.
//
// 문구를 헬퍼에 둔 이유는 읽는 곳이 둘이기 때문이다 — 훅의 "훑음" 줄과 데몬 watch 의
// "scan" 이벤트. 각자 포맷하면 같은 사실이 두 문장으로 갈리고, 그러면 두 경로를
// 나란히 놓고 대조하는 일이 안 된다.
func TestExcerptNoteSeparatesNothingFromEverything(t *testing.T) {
	// 표시하지 않았다 → 아무 말도 하지 않는다. 여기서 "생략 0" 을 내면 안 된다.
	if got := (ScanResult{}).ExcerptNote(); got != "" {
		t.Errorf("발췌가 없는데 %q 라고 한다 — 0을 '다 담았다' 로 읽게 만든다", got)
	}
	// 다 담았으면 그렇다고 **말한다.** 침묵과 구별되어야 한다.
	full := ScanResult{Flagged: true, ExcerptBytes: 11636, ExcerptTurns: 119}.ExcerptNote()
	if !strings.Contains(full, "전부") {
		t.Errorf("다 담은 것을 안 알린다: %q", full)
	}
	if strings.Contains(full, "생략") {
		t.Errorf("버린 게 없는데 생략을 말한다: %q", full)
	}
	// 버렸으면 몇 발화인지 나와야 한다.
	cut := ScanResult{Flagged: true, ExcerptBytes: 24001, ExcerptTurns: 157, ExcerptOmitted: 193}.ExcerptNote()
	if !strings.Contains(cut, "193") {
		t.Errorf("버린 발화 수가 없다: %q", cut)
	}
	// 표시는 했는데 발췌가 비었다 — 고장이다. 조용하면 "판별기가 결정을 못
	// 알아봤다" 로 오진된다.
	if got := (ScanResult{Flagged: true}).ExcerptNote(); !strings.Contains(got, "비었다") {
		t.Errorf("표시했는데 발췌가 빈 상태를 조용히 넘긴다: %q", got)
	}
}

// ★ **판별기가 있으면 시그널을 건너뛴다.**
//
// 시그널은 설정에 적힌 낱말이라 언어에 묶인다. 한국어 기본 시그널로 영어 대화를 훑으면
// 아무것도 안 걸리는데 로그에는 "훑음 — 발화 8" 이라 정상으로 보인다 — 실제로 그 상태를
// 재현해 확인했다. 판별기가 있으면 그 지뢰가 사라진다.
//
// 잃는 것도 없다. 실측으로 발화 6개를 넘는 세션 585개 중 578개(98.8%)가 어차피
// 시그널에 걸린다 — 건너뛰어도 판별기 호출은 ~1.2% 늘 뿐이다.
func TestJudgeAvailableSkipsSignalFilter(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	c := scanCfg() // signals = ["결정", "채택"] — 한국어

	// 영어 대화. 한국어 시그널에는 하나도 안 걸린다.
	var lines []string
	for i := 0; i < 8; i++ {
		lines = append(lines, fmt.Sprintf(
			`{"type":"assistant","cwd":"/tmp/proj/alpha","sessionId":"S1","timestamp":"2026-08-08T01:00:%02dZ","message":{"role":"assistant","content":[{"type":"text","text":"We chose SQLite instead of Postgres."}]}}`+"\n", i))
	}
	writeLines(t, tp, lines...)

	// 판별기 없음 — 지금까지의 동작. 아무것도 안 걸린다.
	s1 := newStore(t)
	r1, err := Scan(s1, c, nil, tp, false, anyHost(tp))
	if err != nil {
		t.Fatal(err)
	}
	if r1.Flagged {
		t.Error("한국어 시그널에 영어 대화가 걸렸다 — 테스트 전제가 틀렸다")
	}
	if len(s1.Pending()) != 0 {
		t.Fatal("판별기 없이 표시됐다")
	}

	// 판별기 있음 — 시그널을 건너뛰고 표시한다.
	s2 := newStore(t)
	r2, err := Scan(s2, c, nil, tp, true, anyHost(tp))
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Flagged {
		t.Fatal("판별기가 있는데 표시하지 않았다 — 언어 지뢰가 그대로다")
	}
	if !r2.NoFilter {
		t.Error("시그널을 건너뛴 사실을 안 알린다")
	}
	if len(r2.Signals) != 0 {
		t.Errorf("걸린 시그널이 있다고 한다: %v", r2.Signals)
	}
	if len(s2.Pending()) != 1 {
		t.Errorf("표시 %d건, 1건이어야 한다", len(s2.Pending()))
	}
}

// 제외 구역은 판별기가 있어도 표시하지 않는다.
func TestJudgeDoesNotOverrideExclusion(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	s := newStore(t)
	writeLines(t, tp, turns(t, 8, "그냥 잡담", "/tmp/proj/secret")...)

	r, err := Scan(s, scanCfg(), nil, tp, true, anyHost(tp))
	if err != nil {
		t.Fatal(err)
	}
	if r.Flagged {
		t.Error("제외 구역인데 판별기가 있다고 표시했다")
	}
}

// ★★ **어느 호스트의 파일인지 모르면 읽지 않는다.**
//
// 아무 파서나 대면 발화가 0개로 나온다. 그건 "그 세션에 결정이 없었다" 와 구별되지
// 않아서, 안전망이 도는 것처럼 보이면서 아무것도 안 하는 상태가 된다 — 이 도구가
// 가장 피해야 하는 실패 모양이다. 조용히 0을 주느니 시끄럽게 실패한다.
func TestScanRefusesFileFromUnknownHost(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(tp, []byte(`{"type":"user","message":{"content":"결정했다"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}

	// 루트가 전혀 다른 호스트 목록 — 이 파일은 어디에도 속하지 않는다.
	other := []hosts.Resolved{{
		Host: hosts.Host{Name: "다른곳", Parse: claudecode.Parse, List: claudecode.List,
			DefaultRoot: func() (string, error) { return "/전혀/다른/곳", nil }, Required: true},
		Root: "/전혀/다른/곳",
	}}

	_, err := Scan(s, scanCfg(), nil, tp, false, other)
	if err == nil {
		t.Fatal("모르는 경로를 조용히 훑었다 — 발화 0개가 '결정 없음' 으로 보인다")
	}
	if !strings.Contains(err.Error(), "어느 호스트") {
		t.Errorf("이유가 안 드러난다: %v", err)
	}

	// **훑지 않았으므로 생존 흔적도 남기면 안 된다.** 남기면 doctor 에게
	// "방금 훑음" 으로 보여서, 생존 증거로 세운 값이 거짓말을 한다.
	if !s.LastScan().IsZero() {
		t.Error("실패한 스캔이 '방금 훑음' 흔적을 남겼다")
	}
}
