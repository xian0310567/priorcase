package daemon

import (
	"fmt"
	"github.com/xian0310567/priorcase/internal/transcript/claudecode"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
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

// ★ 이미 기록된 결정이 있으면 표시하지 않는다.
//
// 원본 명세 4-B 의 "INDEX 대조로 이미 기록된 결정과의 중복을 방지한다" 다. 판별 LLM 을
// 걷어내면서 그 안에 있던 이 검사까지 같이 사라졌었다.
//
// 없으면 안전망이 소음이 된다 — 실측으로 실 transcript 1173개 중 발화 6개를 넘는
// 585개의 **99%(578개)** 가 시그널에 걸린다. 기본 시그널이 "변경"·"선택"·"대신" 처럼
// 흔한 낱말이라 사실상 모든 실질 세션이 표시된다.
func TestAlreadyRecordedIsNotFlagged(t *testing.T) {
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
	if !r.Recorded {
		t.Error("그날 그 도메인에 결정 노트가 있는데 Recorded 가 false 다")
	}
	if r.Flagged {
		t.Error("이미 기록된 날인데 표시했다 — 제 할 일을 한 세션까지 표시하면 무시하는 법을 배운다")
	}
	if !r.Advanced {
		t.Error("다 봤으면 전진해야 한다")
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
	if r.Recorded {
		t.Error("그날 결정 노트가 없는데 Recorded 가 true 다")
	}
	if !r.Flagged {
		t.Error("기록 없는 날인데 표시하지 않았다 — 안전망이 일하지 않는다")
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
	if r.Recorded {
		t.Error("다른 도메인(alpha)의 기록이 gamma 를 가렸다")
	}
	if !r.Flagged {
		t.Error("gamma 에는 기록이 없는데 표시하지 않았다")
	}
}

// 볼트를 못 읽으면 표시하는 쪽으로 기운다 — 대조 실패로 안전망이 조용히 꺼지면 안 된다.
func TestVaultReadFailureStillFlags(t *testing.T) {
	vc := testutil.VaultConfig(t)
	vc.Vault = filepath.Join(t.TempDir(), "없는볼트")
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
func TestSessionMatchSuppressesAcrossDays(t *testing.T) {
	vc := testutil.VaultConfig(t)
	vc.Capture = config.Capture{Signals: []string{"결정"}, MinTurns: 6}
	l := store.NewLayout(vc)

	// 픽스처에 없는 날짜(2026-08-07)의 대화인데, 그 세션으로 기록된 노트를 심는다.
	note := filepath.Join(vc.Vault, "alpha", "decisions", "alpha-결정-세션기록-2026-01-01.md")
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
	if !r.Recorded {
		t.Error("같은 세션으로 기록된 노트가 있는데 못 알아봤다")
	}
	if r.Flagged {
		t.Error("이미 기록된 세션인데 표시했다")
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
	if r.Recorded {
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
