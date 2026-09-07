package judge

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
)

// stubCLI 는 실행 가능한 가짜 판별기를 만들고 그 경로를 준다.
//
// **진짜 CLI 를 찾게 두면 시험이 머신에 달린다** — 이 기계에는 claude 가 있고 CI 에는
// 없다. 그러면 "off 인데 도는" 고장을 CI 가 못 잡는다.
func stubCLI(t *testing.T, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("셸 스텁은 유닉스에서만 만든다")
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func cfgWith(cap config.Capture) *config.Config { return &config.Config{Capture: cap} }

// ★★★ **off 는 무슨 일이 있어도 아무것도 안 준다.**
//
// 이것이 개인정보 계약의 전부다. 여기가 새면 사람은 껐다고 믿는데 대화 발췌가
// 계속 밖으로 나간다 — 조용하고 되돌릴 수 없다.
func TestOffNeverReturnsAJudge(t *testing.T) {
	path := stubCLI(t, "claude")
	for _, cap := range []config.Capture{
		{Judge: "off"},
		{Judge: "off", JudgePath: path}, // 경로가 적혀 있어도
		{Judge: "OFF"},                  // 대소문자
		{Judge: "  off  "},              // 공백
		{Judge: "off", JudgeModel: "claude-haiku-4-5"},
	} {
		if j := For(cfgWith(cap), FlavorClaude); j != nil {
			t.Errorf("judge=%q 인데 판별기가 나왔다: %#v", cap.Judge, j)
		}
		if Available(cfgWith(cap), FlavorClaude) {
			t.Errorf("judge=%q 인데 Available 이 참이다", cap.Judge)
		}
	}
}

// ★★ **모르는 값은 켜지 않는다.** `judge = "of"` 오타 하나가 발췌를 내보내면 안 된다.
func TestUnknownModeDoesNotEnable(t *testing.T) {
	for _, v := range []string{"of", "on", "true", "yes", "claude-code"} {
		if For(cfgWith(config.Capture{Judge: v}), FlavorClaude) != nil {
			t.Errorf("judge=%q (모르는 값)인데 판별기가 나왔다", v)
		}
	}
}

// ★★ **안 정한 설정은 예전처럼 돈다.** 빈 값을 off 로 읽으면 이미 쓰던 사람의
// 자동 기록이 조용히 멈춘다 — 멈춘 것은 큐에 쌓일 뿐 아무 말도 안 한다.
func TestUnsetKeepsWorkingLikeBefore(t *testing.T) {
	path := stubCLI(t, "claude")
	// judge_path 로 지목한 옛 설정은 custom 으로 읽혀 그대로 돈다.
	if For(cfgWith(config.Capture{JudgePath: path}), FlavorClaude) == nil {
		t.Error("judge_path 만 적힌 옛 설정에서 판별기가 사라졌다 — 조용한 기능 중단이다")
	}
	if m := (config.Capture{JudgePath: path}).JudgeMode(); m != config.JudgeCustom {
		t.Errorf("judge_path 만 있는 설정의 모드가 %q — custom 이어야 한다", m)
	}
	// 아무것도 안 적힌 설정은 "안 정함" 이고, 그건 물어봐야 하는 상태다.
	if (config.Capture{}).JudgeMode().Decided() {
		t.Error("빈 설정이 '정했다' 로 읽힌다 — 그러면 세션 진입이 묻지 않는다")
	}
}

// custom 인데 경로가 없으면 **자동으로 떨어지지 않는다.** 사람이 지목하지 않은
// CLI 를 대신 고르는 것은 그 사람이 승인한 적 없는 서비스로 발췌를 보내는 것이다.
func TestCustomWithoutPathDoesNotFallBack(t *testing.T) {
	if For(cfgWith(config.Capture{Judge: "custom"}), FlavorClaude) != nil {
		t.Error("custom 인데 경로가 없는데 판별기가 나왔다")
	}
}

// custom 은 지목한 그것만 쓴다.
func TestCustomUsesOnlyThatPath(t *testing.T) {
	path := stubCLI(t, "claude")
	j := For(cfgWith(config.Capture{Judge: "custom", JudgePath: path}), FlavorClaude)
	if j == nil {
		t.Fatal("지목한 경로가 실행 가능한데 판별기가 없다")
	}
	c, ok := j.(*CLI)
	if !ok {
		t.Fatalf("사슬이 나왔다 (%T) — 지목한 것 하나만 써야 한다", j)
	}
	if c.Path != path {
		t.Errorf("경로가 %q — 지목한 %q 여야 한다", c.Path, path)
	}
}

// nil 설정에서 죽지 않는다. 훅은 무슨 일이 있어도 exit 0 이다.
func TestNilConfigIsSafe(t *testing.T) {
	if For(nil, FlavorClaude) != nil {
		t.Error("nil 설정인데 판별기가 나왔다")
	}
	if Available(nil, FlavorClaude) {
		t.Error("nil 설정인데 Available 이 참이다")
	}
}

// ★ **nil 인터페이스 함정.** `var j Judge = (*CLI)(nil)` 은 nil 이 아니다 —
// 호출부는 전부 `!= nil` 로 가용성을 판단하므로, 그대로 돌려주면 "있다" 고 보고한 뒤
// 부를 때 죽는다.
func TestTypedNilNeverEscapes(t *testing.T) {
	// claude/codex 가 이 머신에 없을 수도 있고 있을 수도 있다. 어느 쪽이든
	// **nil 이거나 진짜 CLI** 여야 한다.
	for _, mode := range []string{"claude", "codex"} {
		j := For(cfgWith(config.Capture{Judge: mode}), FlavorClaude)
		if j == nil {
			continue
		}
		if c, ok := j.(*CLI); ok && c == nil {
			t.Fatalf("judge=%s 가 타입 있는 nil 을 냈다 — 가용성 검사가 통과하고 호출이 죽는다", mode)
		}
	}
}
