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
	return c
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
