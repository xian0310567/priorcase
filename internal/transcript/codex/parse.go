// Package codex 는 Codex CLI 의 세션 기록(rollout JSONL)을 읽는다.
//
// 파일은 `~/.codex/sessions/YYYY/MM/DD/rollout-<시각>-<UUID>.jsonl` 에 덧붙여
// 쓰인다. 우리는 읽기만 한다.
//
// # Claude Code 와 다른 점
//
// 줄 하나가 `{type, timestamp, payload}` 로 한 겹 더 싸여 있다. 발화는
// `type=response_item` 안의 `payload.type=message` 이고, 실제 글은
// `payload.content[].text` 다 — 블록 종류가 `input_text`(사람) 와
// `output_text`(에이전트) 로 갈린다. `payload.role` 도 있어서 둘이 겹치는데,
// **role 을 정본으로 본다** (블록 종류는 호스트가 늘릴 수 있다).
//
// 세션 정보는 첫 줄 `type=session_meta` 하나에만 있다. Claude Code 는 모든 줄이
// cwd 를 실어 나르지만 여기는 아니다 — 그래서 **구간 중간부터 읽으면 cwd 가
// 안 잡힌다.** 호출자가 그것을 어떻게 다루는지가 이 파서의 계약에서 중요하다
// (Meta 가 비면 도메인 해석이 기본값으로 떨어진다).
//
// # 사고(reasoning)는 여기서도 못 읽는다
//
// `payload.type=reasoning` 이 있지만 본문은 `encrypted_content` 다. 실측으로
// 세션 120개의 reasoning 블록 12,809개가 **전부** 암호화돼 있었고 평문
// `content` 는 0개였다. Claude Code 와 같은 한계다 — 사고 안에서만 내려지고
// 밖으로 한 줄도 안 나온 결정은 어느 호스트에서도 볼 수 없다.
package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/xian0310567/priorcase/internal/transcript"
	"github.com/xian0310567/priorcase/internal/transcript/toolsum"
)

// envelope 는 줄 하나의 겉껍질이다. 스키마 전체를 옮기지 않는다 — 호스트가 필드를
// 늘려도 우리가 깨지지 않아야 한다.
type envelope struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// sessionMeta 는 첫 줄에만 나온다.
type sessionMeta struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
}

// item 은 response_item 의 payload 다. 종류마다 쓰는 필드가 다르다.
type item struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`

	Name string `json:"name"`

	// Arguments·Input 은 **판마다 모양이 다르다.** function_call 은 JSON 을 담은
	// 문자열이고, tool_search_call 은 객체다.
	//
	// 그래서 string 으로 받으면 안 된다. 실측에서 `Arguments string` 으로 뒀더니
	// 세션 1,729개에서 177줄이 파싱에 실패했고, **그 파일들은 체크포인트가 영영
	// 전진하지 못한다** — 깨진 줄이 있으면 전진시키지 않는 것이 규칙이기 때문이다.
	// 도구 하나의 인자 모양 때문에 그 세션의 이후 결정이 전부 사라진다.
	//
	// RawMessage 로 받고 argsJSON 이 둘 다 객체로 펴 준다.
	Arguments json.RawMessage `json:"arguments"`
	Input     json.RawMessage `json:"input"`
}

// argsJSON 은 도구 인자를 객체 JSON 으로 편다.
//
// 두 모양을 받는다. 따옴표로 시작하면 **JSON 을 담은 문자열**이라 한 겹 벗기고,
// 아니면 이미 객체다. 어느 쪽도 아니면 빈 값을 준다 — toolsum 이 이름만 남긴다.
func argsJSON(raw json.RawMessage) json.RawMessage {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return nil
	}
	if t[0] == '"' {
		var s string
		if json.Unmarshal(t, &s) != nil {
			return nil
		}
		return json.RawMessage(s)
	}
	return t
}

// maxLine 은 한 줄의 상한이다. function_call_output 에 큰 출력이 통째로 들어오는
// 일이 있어 넉넉히 잡는다.
//
// **넘는 줄은 건너뛰되 깨진 줄로 세지 않는다.** 이게 claudecode 와 다른 판단이고,
// 실측이 그렇게 시켰다.
//
// 깨진 줄이 있으면 호출자는 체크포인트를 전진시키지 않는다 — 못 읽은 내용이 있으니
// 다음에 다시 읽으라는 뜻이다. 그런데 **크기 초과는 다음에도 똑같이 초과한다.**
// 일시적 실패가 아니라 영구 조건이라, 깨진 줄로 세면 그 파일은 영영 멈추고 그
// 줄 뒤의 결정이 전부 사라진다. 한 줄을 잃는 것과 파일 전체를 잃는 것의 차이다.
//
// 그 줄이 무엇인지도 쟀다. 세션 1,729개에서 8MB 를 넘긴 줄은 18개(파일 2개)뿐이고
// **전부 `compacted` 였다** — 호스트가 대화를 압축하며 남기는 요약이다. 우리가 이미
// 발화로 읽은 내용의 재탕이라, 이 도구가 잃을 것이 가장 적은 줄이기도 하다.
const maxLine = 8 << 20 // 8MB

// Parse 는 rollout JSONL 을 읽어 발화만 뽑는다.
//
// 반환값 넷이 다 쓰이라고 있는 것이다 — claudecode.Parse 와 같은 계약이다.
//
//   - consumed: **개행으로 끝난 줄까지의 바이트 수.** 마지막 줄이 쓰이는 중이면
//     여기 안 들어간다. 호출자는 이 값으로만 체크포인트를 옮긴다.
//   - bad: 완결됐는데 파싱에 실패한 줄 수. 0 이 아니면 체크포인트를 전진시키면 안 된다.
func Parse(r io.Reader) (turns []transcript.Turn, meta transcript.Meta, consumed int64, bad int, err error) {
	br := bufio.NewReaderSize(r, 64<<10)

	for {
		line, readErr := br.ReadString('\n')

		// 개행 없이 끝났다 = 아직 쓰이는 중인 줄이다. 파싱하지 않고 consumed 에도
		// 넣지 않는다. 다음 스캔이 이 줄을 처음부터 다시 읽는다.
		if readErr != nil {
			if readErr != io.EOF {
				return turns, meta, consumed, bad, readErr
			}
			break
		}

		consumed += int64(len(line))

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > maxLine {
			// 건너뛰되 bad 로 세지 않는다 — 위 maxLine 주석 참고.
			continue
		}

		var env envelope
		if json.Unmarshal([]byte(trimmed), &env) != nil {
			bad++
			continue
		}

		switch env.Type {
		case "session_meta":
			var m sessionMeta
			if json.Unmarshal(env.Payload, &m) != nil {
				bad++
				continue
			}
			if meta.SessionID == "" {
				// id 와 session_id 가 둘 다 있는 판이 있다. 있는 쪽을 쓴다.
				if m.SessionID != "" {
					meta.SessionID = m.SessionID
				} else {
					meta.SessionID = m.ID
				}
			}
			if meta.Cwd == "" {
				meta.Cwd = m.Cwd
			}
		case "response_item":
			var it item
			if json.Unmarshal(env.Payload, &it) != nil {
				bad++
				continue
			}
			ts, _ := time.Parse(time.RFC3339, env.Timestamp)
			turns = append(turns, it.turns(ts)...)
		}
		// event_msg·turn_context·world_state 는 발화가 아니다.
		//
		// **event_msg 를 안 읽는 이유가 중요하다.** 거기에도 user_message·
		// agent_message 가 있어서 읽으면 같은 말이 두 번 잡힌다 — 턴 수가 두 배로
		// 세지고, 임계가 절반 만에 차서 안전망이 소음이 된다. 정본은 response_item 이다.
	}
	return turns, meta, consumed, bad, nil
}

// turns 는 response_item 하나에서 발화를 뽑는다. 발화가 아니면 아무것도 안 준다.
func (it *item) turns(ts time.Time) []transcript.Turn {
	mk := func(k transcript.Kind, text string) transcript.Turn {
		// Codex 에는 서브에이전트 표시가 없다. 있으면 Sidechain 에 실어야 한다.
		return transcript.Turn{Kind: k, Text: text, Timestamp: ts}
	}

	switch it.Type {
	case "message":
		k := transcript.KindAssistant
		if it.Role == "user" {
			k = transcript.KindUser
		}
		var out []transcript.Turn
		for _, b := range it.Content {
			// 블록 종류로 사람/에이전트를 다시 가리지 않는다 — role 이 정본이다.
			// 여기서 보는 것은 "글이 있는 블록인가" 뿐이다.
			if b.Type != "input_text" && b.Type != "output_text" && b.Type != "text" {
				continue
			}
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			out = append(out, mk(k, b.Text))
		}
		return out

	case "function_call", "custom_tool_call", "tool_search_call":
		// **발화가 아니지만 일어난 일이다.** 턴 수에는 안 센다 — Kind.Counts() 가
		// KindTool 을 뺀다. 발췌에는 싣는다: 되돌리기 어려운 선택은 산문이 아니라
		// 편집과 명령으로 남는 경우가 많다. Codex 에서 특히 그렇다 — 실측 상위
		// 도구가 exec_command · apply_patch 다.
		args := argsJSON(it.Arguments)
		if len(args) == 0 {
			args = argsJSON(it.Input)
		}
		if line := toolsum.Line(it.Name, args); line != "" {
			return []transcript.Turn{mk(transcript.KindTool, line)}
		}
		return nil
	}

	// reasoning 은 암호화돼 있어 담을 것이 없다 (패키지 주석 참고).
	// function_call_output·custom_tool_call_output 은 결과 본문이라 안 담는다 —
	// 크고, 무엇을 했는지는 호출만으로 충분하다.
	return nil
}
