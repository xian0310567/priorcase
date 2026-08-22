package hook

import "testing"

// **이 파일이 지키는 사실은 실측이다.**
//
// Codex 바이너리(0.130.0)의 `HookEventsToml` 문자열 테이블에 박힌 이벤트는 열 개다:
//
//	PreToolUse PermissionRequest PostToolUse PreCompact PostCompact
//	SessionStart UserPromptSubmit SubagentStart SubagentStop Stop
//
// **SessionEnd 가 없다.** 우리 push 가 정확히 거기 하나에 걸려 있었으므로(run.go),
// 이 사실이 깨지면 개인 맥의 볼트가 조용히 안 밀린다 — 이 프로젝트가 제일 싫어하는
// 고장 모양이다. 그래서 주석이 아니라 테스트로 둔다.

func TestCodexHasNoSessionEnd(t *testing.T) {
	for _, e := range EventsFor(HostCodex) {
		if e == EventSessionEnd {
			t.Fatalf("Codex 배선에 session-end 가 들어갔다 — Codex 에는 그 이벤트가 없다")
		}
	}
	// 나머지 넷은 반드시 있어야 한다. 하나라도 빠지면 그 기능이 Codex 에서 죽는다.
	for _, want := range []Event{EventSessionStart, EventUserPromptSubmit, EventStop, EventPreCompact} {
		found := false
		for _, e := range EventsFor(HostCodex) {
			if e == want {
				found = true
			}
		}
		if !found {
			t.Errorf("Codex 배선에 %s 가 빠졌다", want)
		}
	}
}

func TestClaudeCodeKeepsAllFive(t *testing.T) {
	if got, want := len(EventsFor(HostClaudeCode)), len(Events); got != want {
		t.Fatalf("Claude Code 이벤트 수 = %d, 원하는 값 %d", got, want)
	}
}

func TestEventNameForHost(t *testing.T) {
	tests := []struct {
		ev   Event
		host Host
		want string
	}{
		{EventSessionStart, HostClaudeCode, "SessionStart"},
		{EventSessionStart, HostCodex, "SessionStart"},
		{EventUserPromptSubmit, HostCodex, "UserPromptSubmit"},
		{EventStop, HostCodex, "Stop"},
		{EventPreCompact, HostCodex, "PreCompact"},
		{EventSessionEnd, HostClaudeCode, "SessionEnd"},
		// 빈 문자열이 "이 호스트에 이 이벤트가 없다" 는 뜻이다.
		{EventSessionEnd, HostCodex, ""},
	}
	for _, tt := range tests {
		if got := tt.ev.NameFor(tt.host); got != tt.want {
			t.Errorf("%s.NameFor(%s) = %q, 원하는 값 %q", tt.ev, tt.host, got, tt.want)
		}
	}
}

func TestParseHost(t *testing.T) {
	for _, in := range []string{"codex", "Codex", "CODEX"} {
		h, err := ParseHost(in)
		if err != nil || h != HostCodex {
			t.Errorf("ParseHost(%q) = %v, %v", in, h, err)
		}
	}
	if h, err := ParseHost(""); err != nil || h != HostClaudeCode {
		t.Errorf("빈 값은 기본값이어야 한다: %v, %v", h, err)
	}
	// **모르는 호스트는 조용히 기본값으로 떨어지지 않는다.** 오타 하나가 주입을
	// 통째로 죽이는데 아무 말도 안 하면, 그건 이 프로젝트가 금지한 조용한 무동작이다.
	if _, err := ParseHost("cursor"); err == nil {
		t.Error("모르는 호스트인데 에러가 없다")
	}
}
