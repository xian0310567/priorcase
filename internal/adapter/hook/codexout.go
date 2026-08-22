package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// codexOut 은 컨텍스트를 모았다가 Codex 가 읽는 봉투에 담아 한 번에 낸다.
//
// # 왜 봉투가 필요한가
//
// Claude Code 는 user-prompt-submit·session-start 의 평문 stdout 을 그대로 대화에
// 넣는다. **Codex 는 그러지 않는다** — `hookSpecificOutput.additionalContext` 를 읽는다
// (실측: codex 0.130.0 바이너리에 `hookSpecificOutput`·`additionalContext`·
// `hookEventName` 문자열이 박혀 있고, 같은 맥에 깔린 oh-my-codex 훅도 그 모양으로 낸다).
//
// 담지 않으면 그 stdout 은 아무 데도 안 들어간다. 그리고 훅은 무슨 일이 있어도
// exit 0 이라 **아무 표시도 안 난다** — 회수가 통째로 죽었는데 겉으로는 멀쩡하다.
//
// # 왜 모았다가 한 번에 내는가
//
// 봉투가 JSON 이라 조각으로 흘려보낼 수 없다. 부르는 쪽(start.go·recall.go)은
// `fmt.Fprint` 를 여러 번 하므로, 그 사이에 끼어들어 모아 두었다가 끝에 한 번 낸다.
// 그래서 **호출부는 이 파일의 존재를 모른다** — 호스트별 분기가 회수 코드로 번지지 않는다.
type codexOut struct {
	ev  Event
	w   io.Writer
	buf bytes.Buffer
}

func (c *codexOut) Write(p []byte) (int, error) { return c.buf.Write(p) }

// Flush 는 모은 것을 봉투에 담아 낸다.
//
// **빈 것은 내지 않는다.** stop·pre-compact 는 stdout 이 없는 이벤트인데 거기서
// 빈 봉투를 내면 Codex 전사에 빈 developer 메시지가 턴마다 쌓인다. 늘 뜨는 것은
// 곧 배경이 되고, 배경이 되면 진짜 주입도 같이 안 읽힌다.
func (c *codexOut) Flush() {
	s := c.buf.String()
	if strings.TrimSpace(s) == "" || c.w == nil {
		return
	}
	name := c.ev.NameFor(HostCodex)
	if name == "" {
		// Codex 가 모르는 이벤트다. 낼 자리가 없으므로 조용히 버린다 —
		// 애초에 EventsFor 가 배선하지 않으므로 여기 올 일이 없다.
		return
	}
	env := codexEnvelope{}
	env.HookSpecificOutput.HookEventName = name
	env.HookSpecificOutput.AdditionalContext = s

	// **에러를 삼킨다.** 여기서 낼 곳은 stdout 뿐인데 stdout 은 컨텍스트 자리라
	// 진단을 섞을 수 없고, 훅은 어차피 exit 0 이다.
	_ = json.NewEncoder(c.w).Encode(env)
}

type codexEnvelope struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}
