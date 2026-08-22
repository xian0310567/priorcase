package hook

import (
	"fmt"
	"strings"
)

// Host 는 훅을 부르는 에이전트 호스트다.
//
// # 왜 자동 감지가 아닌가
//
// 페이로드로 가를 수는 있다 — Claude Code 는 `session_id` 를, Codex 는 `turn_id`·
// `thread_id` 를 준다. 하지만 그 추론이 틀리면 **주입이 조용히 사라진다**: Codex 에
// 평문을 내면 그 stdout 은 아무 데도 안 들어가고, 훅은 exit 0 이라 아무 일도 없는
// 것처럼 보인다. 이 프로젝트가 제일 싫어하는 고장 모양이다.
//
// 그래서 명시한다. **사람이 타이핑하는 값이 아니다** — `prior init` 이 설정 파일에
// 써 넣고, 그 파일을 읽는 것은 호스트뿐이다.
type Host string

const (
	HostClaudeCode Host = "claude-code"
	HostCodex      Host = "codex"
)

// ParseHost 는 플래그 값을 Host 로 만든다. 빈 값은 기본값(Claude Code)이다.
//
// **모르는 값을 기본값으로 떨어뜨리지 않는다.** `--host codx` 오타 하나가 회수 주입을
// 통째로 죽이는데 아무 말도 안 하면 고칠 수가 없다.
func ParseHost(s string) (Host, error) {
	switch h := Host(strings.ToLower(strings.TrimSpace(s))); h {
	case "":
		return HostClaudeCode, nil
	case HostClaudeCode, HostCodex:
		return h, nil
	default:
		return "", fmt.Errorf("모르는 호스트: %q (쓸 수 있는 것: %s, %s)",
			s, HostClaudeCode, HostCodex)
	}
}

// EventsFor 는 그 호스트에 배선할 이벤트다.
//
// **호스트가 모르는 이벤트는 빼고 준다.** 이름을 지어 심어 봐야 영영 안 불리는데,
// 설정 파일에는 다섯 줄이 보이므로 배선된 줄 알게 된다.
func EventsFor(h Host) []Event {
	var out []Event
	for _, e := range Events {
		if e.NameFor(h) != "" {
			out = append(out, e)
		}
	}
	return out
}

// NameFor 는 그 호스트의 설정 파일에서 쓰는 이벤트 이름이다.
// **빈 문자열은 "이 호스트에 그 이벤트가 없다" 는 뜻이다.**
func (e Event) NameFor(h Host) string {
	if h == HostCodex && e == EventSessionEnd {
		// Codex 0.130.0 의 HookEventsToml 에 SessionEnd 가 없다(host_test.go 참고).
		// 세션 종료에 걸어 두던 볼트 push 는 sync.go 가 Stop 으로 옮겨 받는다.
		return ""
	}
	// 나머지 넷은 Codex 와 Claude Code 가 이름까지 같다.
	return e.claudeCodeName()
}
