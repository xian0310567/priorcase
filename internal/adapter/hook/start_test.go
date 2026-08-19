package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/daemon"
)

// pendingStore 는 상태 디렉토리 하나에 구간들을 심는다.
func pendingStore(t *testing.T, items ...daemon.Pending) string {
	t.Helper()
	dir := t.TempDir()
	s := daemon.NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	for _, p := range items {
		if p.At.IsZero() {
			p.At = time.Now()
		}
		if err := s.AddPending(p); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// ★★ **면제는 이제 "표시에서만 뺀다" 이고, 그 뺄셈은 여기서 일어난다.**
//
// 예전 면제는 pending 자체를 지웠다. 그래서 판별기도 그 구간을 못 봤고, 그게 최근
// 7일 판정 23건 · 자동 기록 0건이던 고장의 한 갈래였다. 고친 뒤로 면제는 Pending.Quiet
// 표시만 남기므로, **묻지 않았는데 들이미는 통로가 직접 빼지 않으면 면제가 사라진다** —
// 즉 이 필터가 빠지면 조용히 넘어가기로 한 구간이 세션마다 다시 뜬다.
//
// 두 통로를 한 테스트에서 같이 본다. daemon.ForNudge 주석이 "한 곳이 빠져도 아무도
// 모른다" 를 이 함수를 둔 이유로 들고 있으므로, 빠짐을 잡는 자리도 한 곳이어야 한다.
func TestNudgeChannelsDropQuietPending(t *testing.T) {
	quiet := daemon.Pending{
		Path: "/t/quiet.jsonl", From: 0, Domain: "alpha", Turns: 9,
		Days: []string{"2026-08-08"}, Signals: []string{"결정"},
		Excerpt: "에이전트: 조용히 넘어가기로 한 구간이다", Quiet: true,
	}
	sd := pendingStore(t, quiet)

	start := runHook(t, cfg(t), sd, EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})
	if strings.Contains(start.out, "미확인 구간") {
		t.Errorf("세션 진입 안내가 면제된 구간을 들이밀었다:\n%s", start.out)
	}

	prompt := runHook(t, cfg(t), sd, EventUserPromptSubmit,
		Input{Cwd: "/tmp/proj/alpha", Prompt: "이제 인덱스 전략을 정하자"})
	if strings.Contains(prompt.out, "기록되지 않은 결정") {
		t.Errorf("회수 주입이 면제된 구간을 들이밀었다:\n%s", prompt.out)
	}
}

// TestNudgeChannelsCountOnlyNudgeable 은 **개수까지** 면제를 반영하는지 본다.
//
// 목록에서만 빼고 개수를 원본으로 세면 "미확인 구간이 2건 있다" 뒤에 한 건만
// 나열되는 글이 나간다. 에이전트가 읽고 행동하는 글이라 그 어긋남은 그냥 오타가
// 아니다 — 없는 구간을 찾으러 transcript 를 다시 뒤진다.
func TestNudgeChannelsCountOnlyNudgeable(t *testing.T) {
	sd := pendingStore(t,
		daemon.Pending{Path: "/t/a.jsonl", From: 0, Domain: "alpha", Turns: 9,
			Signals: []string{"결정"}, Excerpt: "살아 있는 구간"},
		daemon.Pending{Path: "/t/b.jsonl", From: 0, Domain: "alpha", Turns: 9,
			Signals: []string{"결정"}, Excerpt: "면제된 구간", Quiet: true},
	)

	start := runHook(t, cfg(t), sd, EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})
	if !strings.Contains(start.out, "미확인 구간이 1건") {
		t.Errorf("면제를 뺀 개수가 아니다:\n%s", start.out)
	}
	if strings.Contains(start.out, "면제된 구간") {
		t.Errorf("면제된 구간이 목록에 남았다:\n%s", start.out)
	}

	prompt := runHook(t, cfg(t), sd, EventUserPromptSubmit,
		Input{Cwd: "/tmp/proj/alpha", Prompt: "이제 인덱스 전략을 정하자"})
	if !strings.Contains(prompt.out, "살아 있는 구간") {
		t.Fatalf("면제 안 된 구간까지 사라졌다:\n%s", prompt.out)
	}
	// nudge 는 "가장 최근 것 하나 + 나머지는 개수" 로 낸다. 면제된 것이 남아 있으면
	// 그 개수가 늘어난다.
	if strings.Contains(prompt.out, "더 있다") {
		t.Errorf("면제된 구간이 나머지 개수에 섞였다:\n%s", prompt.out)
	}
}

// TestBrokenStateStillReportsThroughFilter 는 면제 필터가 **읽기 실패를 삼키지
// 않는지** 본다. ForNudge 를 에러 분기보다 앞에 두면 "0건" 과 "확인할 수 없다" 가
// 다시 같아진다 — 안전망이 죽은 것을 할 일이 없는 것으로 읽게 되는, 이 블록이
// 애초에 막으려던 상태다.
func TestBrokenStateStillReportsThroughFilter(t *testing.T) {
	sd := t.TempDir()
	if err := os.WriteFile(filepath.Join(sd, "state.json"), []byte("{깨짐"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := runHook(t, cfg(t), sd, EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})
	if !strings.Contains(r.out, "확인할 수 없다") {
		t.Errorf("면제 필터가 읽기 실패를 삼켰다:\n%s", r.out)
	}
}
