// Package hook 은 casebook 을 Claude Code 훅으로 낸다.
//
// **이 어댑터의 존재 이유는 한 칸이다** — 주제 전환 시 회수 강제 주입. MCP 에는 서버가
// 대화 중간에 텍스트를 밀어넣는 채널이 없어서, 그 한 칸만 호스트 훅으로 채울 수 있다
// (스펙 §9). 나머지 칸은 MCP 로 이미 동등하다.
//
// 세 가지 규율이 있다.
//
//  1. **무슨 일이 있어도 exit 0.** 훅이 실패해서 대화가 막히면, 사용자는 casebook 을
//     지우지 고치지 않는다. 진단은 stderr 로만 낸다.
//  2. **stdout 은 에이전트 컨텍스트다.** user-prompt-submit·session-start 의 stdout 은
//     그대로 주입되므로 경고가 한 줄도 섞이면 안 된다. MCP 의 stdio 와 같은 규율이다.
//  3. **훅은 기록을 판별하지 않는다.** 옛 구현은 훅이 LLM 을 불러 뒤늦게 추측했다.
//     결정이 내려진 걸 아는 것은 결정을 내린 에이전트 자신이다.
package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Event 는 casebook 이 다루는 훅 이벤트다.
type Event string

const (
	EventSessionStart     Event = "session-start"
	EventUserPromptSubmit Event = "user-prompt-submit"
	EventStop             Event = "stop"
	EventPreCompact       Event = "pre-compact"
	EventSessionEnd       Event = "session-end"
)

// Events 는 cb init 이 배선하는 이벤트 전부다. 순서가 settings.json 에 그대로 간다.
var Events = []Event{EventSessionStart, EventUserPromptSubmit, EventStop, EventPreCompact, EventSessionEnd}

// claudeCodeName 은 Claude Code 설정에서 쓰는 이벤트 이름이다.
func (e Event) claudeCodeName() string {
	switch e {
	case EventSessionStart:
		return "SessionStart"
	case EventUserPromptSubmit:
		return "UserPromptSubmit"
	case EventStop:
		return "Stop"
	case EventPreCompact:
		return "PreCompact"
	case EventSessionEnd:
		return "SessionEnd"
	}
	return ""
}

// Input 은 Claude Code 가 stdin 으로 주는 것 중 우리가 쓰는 부분이다.
//
// 스키마 전체를 옮기지 않는다 — 호스트가 필드를 늘려도 우리가 깨지지 않아야 한다.
type Input struct {
	Cwd            string `json:"cwd"`
	Source         string `json:"source"`          // session-start
	Prompt         string `json:"prompt"`          // user-prompt-submit
	SessionID      string `json:"session_id"`      // stop · pre-compact · session-end
	TranscriptPath string `json:"transcript_path"` // 〃
	StopHookActive bool   `json:"stop_hook_active"`
}

// ParseInput 은 stdin 을 읽는다. 깨져 있어도 에러를 내지 않는다 — 훅이 입력 하나
// 때문에 시끄러워질 이유가 없고, 어차피 할 수 있는 일은 조용히 끝내는 것뿐이다.
// 대신 **파싱 실패를 두 번째 반환값으로 알린다.** 호출자가 stderr 로 낼지 정한다.
func ParseInput(r io.Reader) (Input, error) {
	var in Input
	b, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return in, fmt.Errorf("stdin 을 읽을 수 없다: %w", err)
	}
	if strings.TrimSpace(string(b)) == "" {
		return in, nil // 빈 입력은 정상이다 (테스트·수동 실행)
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return in, fmt.Errorf("훅 입력이 JSON 이 아니다: %w", err)
	}
	return in, nil
}
