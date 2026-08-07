// Package claudecode 는 Claude Code 의 transcript JSONL 을 읽는다.
//
// 파일은 `~/.claude/projects/<경로를 뭉갠 이름>/<세션UUID>.jsonl` 에 **덧붙여** 쓰인다.
// 우리는 읽기만 한다.
package claudecode

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/xian0310567/casebook/internal/transcript"
)

// record 는 JSONL 한 줄 중 우리가 쓰는 부분만 담는다. 스키마 전체를 옮기지 않는 이유:
// 호스트가 필드를 늘려도 우리가 깨지지 않아야 한다. 모르는 필드는 그냥 무시된다.
type record struct {
	Type      string `json:"type"`
	IsMeta    bool   `json:"isMeta"`
	Sidechain bool   `json:"isSidechain"`
	Cwd       string `json:"cwd"`
	SessionID string `json:"sessionId"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		// content 는 문자열이거나 블록 배열이다. 둘 다 받아야 해서 늦게 푼다.
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Thinking string `json:"thinking"`
}

// maxLine 은 한 줄의 상한이다. tool_result 에 큰 파일이 통째로 들어오는 일이 있어
// bufio.Scanner 의 기본 64KB 로는 부족하다. 넘으면 그 줄은 깨진 줄로 센다.
const maxLine = 8 << 20 // 8MB

// Parse 는 JSONL 을 읽어 발화만 뽑는다.
//
// 반환값 넷이 다 쓰이라고 있는 것이다. 특히 뒤의 둘을 구조체로 감싸지 않은 이유는
// [[casebook-결정-건너뛰기정책-침묵금지-2026-08-07]] 와 같다 — 필드로 두면 안 보는 게
// 티가 안 나는데, 여기서 안 보면 **구간이 통째로 사라진다.**
//
//   - consumed: **개행으로 끝난 줄까지의 바이트 수.** 호출자는 이 값으로만 체크포인트를
//     옮긴다. 마지막 줄이 쓰이는 중이면 그 줄은 여기 안 들어간다 (감사 결함 1).
//   - bad: 완결됐는데 파싱에 실패한 줄 수. 0 이 아니면 **체크포인트를 전진시키면 안 된다.**
func Parse(r io.Reader) (turns []transcript.Turn, meta transcript.Meta, consumed int64, bad int, err error) {
	br := bufio.NewReaderSize(r, 64<<10)

	for {
		line, readErr := br.ReadString('\n')

		// 개행 없이 끝났다 = 아직 쓰이는 중인 줄이다. 파싱하지 않고, consumed 에도
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
			bad++
			continue
		}

		var rec record
		if json.Unmarshal([]byte(trimmed), &rec) != nil {
			bad++
			continue
		}

		if meta.SessionID == "" {
			meta.SessionID = rec.SessionID
		}
		if meta.Cwd == "" {
			meta.Cwd = rec.Cwd
		}

		turns = append(turns, rec.turns()...)
	}
	return turns, meta, consumed, bad, nil
}

// turns 는 레코드 하나에서 발화를 뽑는다. 발화가 아니면 아무것도 안 준다.
func (rec *record) turns() []transcript.Turn {
	// isMeta 는 **호스트가 주입한 컨텍스트**다. 사람이 한 말이 아니다.
	//
	// 이걸 안 거르면 자기 참조 고리가 생긴다: 회수 훅이 "[과거 결정 참조]" 를 주입하고,
	// 그 글에는 "결정" 이 반드시 들어 있으니, 데몬이 그걸 읽고 "이 구간에 결정이 있다"
	// 고 판정한다. 회수가 켜져 있는 한 **모든 구간이 시그널에 걸린다.**
	if rec.IsMeta {
		return nil
	}
	// user·assistant 가 아니면 대화가 아니다 (system·attachment·file-history-* 등).
	if rec.Type != "user" && rec.Type != "assistant" {
		return nil
	}
	if len(rec.Message.Content) == 0 {
		return nil
	}

	ts, _ := time.Parse(time.RFC3339, rec.Timestamp)
	mk := func(k transcript.Kind, text string) transcript.Turn {
		return transcript.Turn{Kind: k, Text: text, Timestamp: ts, Sidechain: rec.Sidechain}
	}

	// content 가 문자열이면 사람이 친 프롬프트다.
	var s string
	if json.Unmarshal(rec.Message.Content, &s) == nil {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return []transcript.Turn{mk(transcript.KindUser, s)}
	}

	var blocks []block
	if json.Unmarshal(rec.Message.Content, &blocks) != nil {
		return nil
	}

	var out []transcript.Turn
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			k := transcript.KindAssistant
			if rec.Type == "user" {
				k = transcript.KindUser
			}
			out = append(out, mk(k, b.Text))
		case "thinking":
			if strings.TrimSpace(b.Thinking) == "" {
				continue
			}
			out = append(out, mk(transcript.KindThinking, b.Thinking))
		}
		// tool_use·tool_result·image 는 발화가 아니다 (감사 결함 6).
	}
	return out
}
