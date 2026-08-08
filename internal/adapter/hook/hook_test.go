package hook

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/daemon"
	"github.com/xian0310567/casebook/internal/testutil"
)

type run struct {
	out, err string
	e        error
}

func runHook(t *testing.T, c *config.Config, stateDir string, ev Event, in Input) run {
	t.Helper()
	var out, errb strings.Builder
	e := Run(context.Background(), Options{
		Event: ev, Input: in, Config: c, Layout: store.NewLayout(c),
		StateDir: stateDir, Out: &out, Err: &errb,
	})
	return run{out: out.String(), err: errb.String(), e: e}
}

func cfg(t *testing.T) *config.Config {
	t.Helper()
	c := testutil.VaultConfig(t)
	c.Exclude = []string{"/tmp/proj/secret"}
	c.Capture = config.Capture{Signals: []string{"결정"}, MinTurns: 6}
	// **판별기를 끈다.** 안 그러면 테스트가 진짜 LLM 을 부른다 — 느리고 결정적이지
	// 않다. 실제로 이 줄이 없었을 때 훅 테스트가 13초 걸렸다.
	// 승격을 시험하는 테스트는 stubJudge 로 켠다.
	c.Capture.JudgePath = "/casebook-test-판별기없음"
	return c
}

// stubJudge 는 정해진 JSON 을 뱉는 가짜 판별기다. 실제 LLM 없이 승격 경로 전체를
// 검증한다 — 판별기 호출은 exec 라 인터페이스로 갈아 끼울 수 없다.
func stubJudge(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "judge")
	script := "#!/bin/sh\ncat >/dev/null\ncat <<'JSON'\n" + body + "\nJSON\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// ── 입력 파싱 ────────────────────────────────────────────────────────────

// 훅은 대화를 막으면 안 된다. 깨진 입력도 조용히 넘긴다 — 다만 **왜 그런지는 알린다.**
func TestParseInputTolerates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"빈 입력", "", false},
		{"공백만", "  \n ", false},
		{"정상", `{"cwd":"/x","prompt":"p"}`, false},
		{"모르는 필드", `{"cwd":"/x","새필드":1}`, false},
		{"JSON 아님", `이건 JSON 이 아니다`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseInput(strings.NewReader(tc.body))
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestUnknownEventIsReportedNotSilent(t *testing.T) {
	r := runHook(t, cfg(t), t.TempDir(), Event("존재하지않는이벤트"), Input{})
	if r.e == nil {
		t.Error("알 수 없는 이벤트를 조용히 넘겼다 — 배선이 틀려도 정상으로 보인다")
	}
	if r.out != "" {
		t.Errorf("실패했는데 컨텍스트를 주입했다: %q", r.out)
	}
}

// ── user-prompt-submit — 이 어댑터의 존재 이유 ────────────────────────────

func TestRecallInjectsRelatedDecisions(t *testing.T) {
	r := runHook(t, cfg(t), t.TempDir(), EventUserPromptSubmit,
		Input{Cwd: "/tmp/proj/alpha", Prompt: "저장 엔진을 무엇으로 할지 다시 보자"})
	if r.e != nil {
		t.Fatal(r.e)
	}
	if !strings.Contains(r.out, "과거 결정 참조") {
		t.Errorf("주입 블록이 없다:\n%s", r.out)
	}
	if !strings.Contains(r.out, "저장 엔진을 임베디드 DB 로 고른다") {
		t.Errorf("관련 결정이 없다:\n%s", r.out)
	}
}

// **stdout 은 그대로 컨텍스트가 된다.** 경고가 한 줄이라도 섞이면 주입 블록이 오염된다.
func TestRecallKeepsStdoutPure(t *testing.T) {
	c := cfg(t)
	broken := filepath.Join(c.Vault, "alpha", "decisions", "alpha-결정-깨짐-2026-01-01.md")
	if err := os.WriteFile(broken, []byte("---\ntitle: 구 스키마\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := runHook(t, c, t.TempDir(), EventUserPromptSubmit,
		Input{Cwd: "/tmp/proj/alpha", Prompt: "저장 엔진을 무엇으로 할지 다시 보자"})

	if strings.Contains(r.out, "경고") || strings.Contains(r.out, "깨짐") {
		t.Errorf("stdout 에 경고가 섞였다 — 주입 블록이 오염된다:\n%s", r.out)
	}
	if !strings.Contains(r.err, "깨짐") {
		t.Errorf("건너뛴 노트를 어디에도 안 알렸다 — 침묵이다:\n%s", r.err)
	}
}

// "고마워" 같은 프롬프트에도 발동하던 것이 옛 구현의 소음원이었다.
func TestRecallSkipsShortPrompts(t *testing.T) {
	r := runHook(t, cfg(t), t.TempDir(), EventUserPromptSubmit,
		Input{Cwd: "/tmp/proj/alpha", Prompt: "고마워"})
	if r.out != "" {
		t.Errorf("짧은 프롬프트에 주입했다: %q", r.out)
	}
}

func TestRecallSilentWhenNoMatch(t *testing.T) {
	r := runHook(t, cfg(t), t.TempDir(), EventUserPromptSubmit,
		Input{Cwd: "/tmp/proj/alpha", Prompt: "zzzz전혀관계없는주제zzzz 에 대해 알려줘"})
	if r.out != "" {
		t.Errorf("맞는 결정이 없는데 주입했다 — 컨텍스트를 낭비한다: %q", r.out)
	}
}

// **제외 구역에서도 회수한다.** 쓰기만 막는 것이지 읽기까지 막을 이유가 없다.
// 자체 스키마를 쓰는 저장소에서도 common 교훈은 꺼내 써야 한다.
func TestRecallWorksInExcludedDir(t *testing.T) {
	r := runHook(t, cfg(t), t.TempDir(), EventUserPromptSubmit,
		Input{Cwd: "/tmp/proj/secret", Prompt: "저장 엔진을 무엇으로 할지 다시 보자"})
	if !strings.Contains(r.out, "저장 엔진") {
		t.Errorf("제외 구역이라고 회수까지 막았다:\n%s", r.out)
	}
}

// ── session-start ────────────────────────────────────────────────────────

func TestSessionStartCarriesDomainAndContract(t *testing.T) {
	r := runHook(t, cfg(t), t.TempDir(), EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})
	for _, want := range []string{"alpha", "cb capture", "최근 결정"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("세션 진입에 %q 가 없다:\n%s", want, r.out)
		}
	}
}

// 제외 구역에서는 기록 계약을 심으면 안 된다 — 그 저장소의 규약을 깨라고 시키는 것이다.
func TestSessionStartInExcludedDirForbidsCapture(t *testing.T) {
	r := runHook(t, cfg(t), t.TempDir(), EventSessionStart, Input{Cwd: "/tmp/proj/secret"})
	if !strings.Contains(r.out, "제외 구역") {
		t.Errorf("제외 구역임을 안 알린다:\n%s", r.out)
	}
	if strings.Contains(r.out, "cb capture` 를 부른다") {
		t.Errorf("제외 구역인데 기록하라고 한다:\n%s", r.out)
	}
	if !strings.Contains(r.out, "회수는 계속") {
		t.Errorf("회수는 되는데 안 알린다:\n%s", r.out)
	}
}

func TestSessionStartShowsPending(t *testing.T) {
	stateDir := t.TempDir()
	s := daemon.NewStore(stateDir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPending(daemon.Pending{
		Path: "/t/a.jsonl", Domain: "alpha", Turns: 9, Signals: []string{"결정"}}); err != nil {
		t.Fatal(err)
	}
	r := runHook(t, cfg(t), stateDir, EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})
	if !strings.Contains(r.out, "미확인 구간이 1건") {
		t.Errorf("미확인 구간을 안 알린다:\n%s", r.out)
	}
}

// "0건" 과 "확인할 수 없다" 는 다른 사실이다.
func TestSessionStartDistinguishesBrokenStateFromZero(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "state.json"), []byte("{깨짐"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := runHook(t, cfg(t), stateDir, EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})
	if !strings.Contains(r.out, "확인할 수 없다") {
		t.Errorf("상태 파일이 깨졌는데 조용하다:\n%s", r.out)
	}
	if !strings.Contains(r.out, "꺼져 있다") {
		t.Errorf("안전망이 꺼졌다는 사실을 안 알린다:\n%s", r.out)
	}
}

// ── ② 훅 주입 강화 ───────────────────────────────────────────────────────

// ★ **매 프롬프트마다 들이민다.** 세션 진입 안내는 세션당 한 번뿐이라 그 뒤에 생긴
// 구간을 못 알린다. 회수 주입이 매 프롬프트마다 도는 유일한 통로다.
func TestNudgeRidesOnEveryPrompt(t *testing.T) {
	c := cfg(t)
	sd := t.TempDir()
	s := daemon.NewStore(sd)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPending(daemon.Pending{
		Path: "/t/a.jsonl", Domain: "alpha", Turns: 9, Days: []string{"2026-08-08"},
		Signals: []string{"결정"},
		Excerpt: "에이전트: SQLite 로 하기로 결정했다"}); err != nil {
		t.Fatal(err)
	}

	r := runHook(t, c, sd, EventUserPromptSubmit,
		Input{Cwd: "/tmp/proj/alpha", Prompt: "이제 인덱스 전략을 정하자"})

	if !strings.Contains(r.out, "기록되지 않은 결정") {
		t.Fatalf("주입되지 않았다:\n%s", r.out)
	}
	// ★ 발췌가 같이 실려야 한다. "1건 있다" 만 알리면 확인 비용이 커서 그냥 넘어간다.
	if !strings.Contains(r.out, "SQLite 로 하기로") {
		t.Errorf("발췌가 없다 — 무엇을 기록할지 모르면 안 부른다:\n%s", r.out)
	}
	if !strings.Contains(r.out, "cb capture") {
		t.Errorf("무엇을 하라는지 없다:\n%s", r.out)
	}
}

// 다른 프로젝트의 미확인 구간은 들이밀지 않는다 — 맥락에 안 맞는 경고가 무시를 학습시킨다.
func TestNudgeOnlyForCurrentProject(t *testing.T) {
	c := cfg(t)
	sd := t.TempDir()
	s := daemon.NewStore(sd)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPending(daemon.Pending{
		Path: "/t/b.jsonl", Domain: "beta", Turns: 9, Excerpt: "베타 이야기"}); err != nil {
		t.Fatal(err)
	}
	r := runHook(t, c, sd, EventUserPromptSubmit,
		Input{Cwd: "/tmp/proj/alpha", Prompt: "알파 작업을 계속하자"})
	if strings.Contains(r.out, "기록되지 않은 결정") {
		t.Errorf("다른 프로젝트 것을 들이밀었다:\n%s", r.out)
	}
}

// 표시가 없으면 조용하다.
func TestNoNudgeWhenNothingPending(t *testing.T) {
	r := runHook(t, cfg(t), t.TempDir(), EventUserPromptSubmit,
		Input{Cwd: "/tmp/proj/alpha", Prompt: "저장 엔진 이야기를 다시 해 보자"})
	if strings.Contains(r.out, "기록되지 않은 결정") {
		t.Errorf("표시가 없는데 들이밀었다:\n%s", r.out)
	}
}

// ── 세션 진입 블록의 언어 ────────────────────────────────────────────────
//
// 세션 진입 블록은 ①층의 유일한 지시 통로다 — 에이전트가 읽고 행동하는 글이라
// 대화 언어와 어긋나면 `cb capture` 를 부르는 정확도가 같이 떨어진다. 그래서
// **두 언어 모두** 계약·도메인·미확인 구간이 실려 나오는지 검사한다.
func cfgEN(t *testing.T) *config.Config {
	t.Helper()
	c := cfg(t)
	c.Lang = "en"
	return c
}

func TestSessionStartInEnglishCarriesDomainAndContract(t *testing.T) {
	r := runHook(t, cfgEN(t), t.TempDir(), EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})
	for _, want := range []string{"alpha", "cb capture", "Recent decisions"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("영어 세션 진입에 %q 가 없다:\n%s", want, r.out)
		}
	}
	// 한국어 안내 문구가 남아 있으면 옮기다 만 자리가 있는 것이다.
	// (노트 요약은 볼트 데이터라 한국어일 수 있으니 검사 대상이 아니다.)
	for _, ko := range []string{"최근 결정", "도메인 접두어", "되돌리기 어려운"} {
		if strings.Contains(r.out, ko) {
			t.Errorf("영어인데 한국어 %q 가 남았다:\n%s", ko, r.out)
		}
	}
}

func TestSessionStartInEnglishForbidsCaptureInExcludedDir(t *testing.T) {
	r := runHook(t, cfgEN(t), t.TempDir(), EventSessionStart, Input{Cwd: "/tmp/proj/secret"})
	if !strings.Contains(r.out, "exclusion zone") {
		t.Errorf("제외 구역임을 안 알린다:\n%s", r.out)
	}
	if strings.Contains(r.out, "call `cb capture`") {
		t.Errorf("제외 구역인데 기록하라고 한다:\n%s", r.out)
	}
	if !strings.Contains(r.out, "Recall still works") {
		t.Errorf("회수는 되는데 안 알린다:\n%s", r.out)
	}
}

func TestSessionStartInEnglishShowsPending(t *testing.T) {
	stateDir := t.TempDir()
	s := daemon.NewStore(stateDir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPending(daemon.Pending{
		Path: "/t/a.jsonl", Domain: "alpha", Turns: 9, Signals: []string{"결정"}}); err != nil {
		t.Fatal(err)
	}
	r := runHook(t, cfgEN(t), stateDir, EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})
	// 단수는 단수로 — "1 unreviewed segments" 는 비문이다.
	if !strings.Contains(r.out, "flagged 1 unreviewed segment.") {
		t.Errorf("미확인 구간을 안 알린다:\n%s", r.out)
	}
	if !strings.Contains(r.out, "9 turns") {
		t.Errorf("구간 항목이 영어로 안 나온다:\n%s", r.out)
	}
}

// "0건" 과 "확인할 수 없다" 는 영어에서도 다른 사실이다.
func TestSessionStartInEnglishDistinguishesBrokenStateFromZero(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "state.json"), []byte("{깨짐"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := runHook(t, cfgEN(t), stateDir, EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})
	if !strings.Contains(r.out, "Cannot check for unreviewed segments") {
		t.Errorf("상태 파일이 깨졌는데 조용하다:\n%s", r.out)
	}
	if !strings.Contains(r.out, "safety net") {
		t.Errorf("안전망이 꺼졌다는 사실을 안 알린다:\n%s", r.out)
	}
}
