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
	"unicode/utf8"

	"github.com/xian0310567/priorcase/internal/transcript"
	"github.com/xian0310567/priorcase/internal/transcript/toolsum"
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
	// Name·Input·ID 는 tool_use 블록의 것이다. **무엇을 했는지**를 발췌에 싣기 위해 읽는다.
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	ID    string          `json:"id"`
	// ToolUseID·Content·IsError 는 tool_result 블록의 것이다.
	//
	// **결과 블록에는 도구 이름이 없다.** `tool_use_id` 로 호출을 가리킬 뿐이라, 파일을
	// 훑으며 id→이름 을 들고 다녀야 "무엇의 결과인가" 를 안다. 이름을 모르면 선별 기준을
	// 못 고르므로(조회성 도구를 버리는 규칙이 특히 그렇다) 이 지도가 없으면 안 된다.
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// maxLine 은 한 줄의 상한이다. tool_result 에 큰 파일이 통째로 들어오는 일이 있어
// bufio.Scanner 의 기본 64KB 로는 부족하다. 넘으면 그 줄은 깨진 줄로 센다.
const maxLine = 8 << 20 // 8MB

// Parse 는 JSONL 을 읽어 발화만 뽑는다.
//
// 반환값 넷이 다 쓰이라고 있는 것이다. 특히 뒤의 둘을 구조체로 감싸지 않은 이유는
// [[priorcase-결정-건너뛰기정책-침묵금지-2026-08-07]] 와 같다 — 필드로 두면 안 보는 게
// 티가 안 나는데, 여기서 안 보면 **구간이 통째로 사라진다.**
//
//   - consumed: **개행으로 끝난 줄까지의 바이트 수.** 호출자는 이 값으로만 체크포인트를
//     옮긴다. 마지막 줄이 쓰이는 중이면 그 줄은 여기 안 들어간다 (감사 결함 1).
//   - bad: 완결됐는데 파싱에 실패한 줄 수. 0 이 아니면 **체크포인트를 전진시키면 안 된다.**
func Parse(r io.Reader) (turns []transcript.Turn, meta transcript.Meta, consumed int64, bad int, err error) {
	br := bufio.NewReaderSize(r, 64<<10)

	// tools 는 tool_use 의 id→이름 이다. 결과 블록이 이름을 안 들고 다니므로 여기 쌓아
	// 둔다. **한 스캔 안에서만 유효하다** — 데몬은 체크포인트부터 읽으니 호출이 이전
	// 구간에 있으면 이름을 모른다. 그래서 선별은 이름에만 기대면 안 되고, 본문 머리말로도
	// 판별한다(resultLimit).
	tools := map[string]string{}

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

		turns = append(turns, rec.turns(tools)...)
	}
	return turns, meta, consumed, bad, nil
}

// turns 는 레코드 하나에서 발화를 뽑는다. 발화가 아니면 아무것도 안 준다.
//
// tools 는 id→도구이름 지도다. 읽기도 하고 쓰기도 한다 — tool_use 를 보면 채우고,
// tool_result 를 보면 찾는다.
func (rec *record) turns(tools map[string]string) []transcript.Turn {
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
		case "tool_use":
			// **발화가 아니지만 일어난 일이다.** 턴 수에는 안 센다(감사 결함 6) —
			// Kind.Counts() 가 KindTool 을 뺀다. 발췌에는 싣는다: 되돌리기 어려운
			// 선택은 산문이 아니라 편집과 명령으로 남는 경우가 많다.
			if b.ID != "" && b.Name != "" {
				tools[b.ID] = b.Name
			}
			if line := toolsum.Line(b.Name, b.Input); line != "" {
				out = append(out, mk(transcript.KindTool, line))
			}
		case "tool_result":
			// ★ 2026-08-19. 여기 case 가 없어서 **AskUserQuestion 의 사용자 답변이
			// 통째로 사라졌다.** 판별기가 원장에 직접 적었다(promotions.jsonl 11행) —
			// "AskUserQuestion 답변이 발췌에 없어서 실제로 무엇을 정했는지 불명확" →
			// record=false. 최근 7일 판정 23건 / 자동 기록 0건이 그렇게 나왔다.
			//
			// **KindTool 로 담는 것이 요점이다.** 감사 결함 6 은 "tool_use + tool_result
			// 로 턴이 두 개 찬다" 였고, 그건 지금도 옳다 — 결과는 발화가 아니다.
			// Kind.Counts() 가 KindTool 을 빼므로 min_turns=6 관문의 뜻은 그대로
			// "사람·에이전트가 여섯 번 주고받았다" 이다. 이 규칙이 깨지면 안전망이
			// 대화도 없는데 도구 호출만으로 계속 발동한다.
			if line := resultLine(tools[b.ToolUseID], &b); line != "" {
				out = append(out, mk(transcript.KindTool, line))
			}
		}
		// image 는 담지 않는다 (실측 4건). 발췌는 판별기에게 넘길 글이라 그림을 실을 자리가 없다.
	}
	return out
}

// unknownTool 은 결과가 가리키는 tool_use 를 이 스캔에서 못 본 경우의 이름이다.
// 데몬은 체크포인트부터 읽으므로 호출이 이전 구간에 있으면 이름이 없다.
const unknownTool = "도구"

// 결과 본문의 상한이다. 단위가 **바이트**인 것이 중요하다 — 발췌(daemon.maxExcerpt=6000)가
// 바이트로 세고, 한 줄이 남은 예산을 넘으면 excerpt 가 거기서 **break** 해서 그 앞이
// 통째로 사라진다. 룬으로 세면 한국어 한 글자가 3바이트라 예산이 3배로 어긋난다.
const (
	// AskUserQuestion 은 **사용자가 실제로 무엇을 골랐는가**다. 가장 후하게 준다.
	// 실측(파일 78개): 결과 4건 전부 합쳐 2,872B(p50 665 · 최대 1,288)뿐이라 후하게
	// 줘도 예산을 거의 안 먹는데, 버렸을 때 잃는 것은 결정 그 자체다.
	maxAskResult = 1200
	// 실패·거부·중단. **결정을 뒤집는 쪽**이라 살리되 신호는 앞머리에 있다.
	// 실측: is_error=true 21건(Bash 18 · Read 1 · AskUserQuestion 1 · Skill 1), 83~936B.
	maxFailResult = 320
	// 나머지(Bash·Edit·Write·MCP·Task…). 실측 본문 p50 1,471B · p90 7,460B · 최대 45,995B —
	// 그대로 담으면 결과 두어 개가 발췌 6000B 를 다 먹는다. 앞뒤만 남긴다.
	maxToolResult = 200
)

// lookupTools 는 조회성 도구다. 결과가 곧 파일·검색 덤프라 담을 값이 없다 —
// **무엇을 봤는지는 tool_use 줄이 이미 말한다** ("Read internal/…/parse.go").
//
// 실측이 이 목록을 정했다: tool_result 본문 3,610,097B 중 Read 하나가 1,353,763B(37.5%)
// 이고 그 p50 이 6,296B 다. 200B 로 잘라 담아 봐야 "파일 앞머리 몇 줄"이라 판별기에게
// 쓸모가 없으면서 예산만 먹는다.
//
// **단 실패한 조회는 살린다** (resultLimit 이 실패를 먼저 본다). "pdftoppm is not
// installed" 는 덤프가 아니라 계획을 뒤집는 사실이다.
var lookupTools = map[string]bool{
	"Read": true, "Glob": true, "Grep": true, "LS": true,
	"NotebookRead": true, "WebFetch": true, "WebSearch": true,
	"ToolSearch": true, "TaskOutput": true,
}

// askMarkers 는 AskUserQuestion 결과의 머리말이다 (실측한 두 가지 문형).
//
// 이름으로만 판별하면 안 된다: 데몬은 체크포인트 구간만 읽으므로 호출이 이전 구간에
// 있으면 tools 지도에 이름이 없다. 그때 답변이 일반 결과로 취급돼 200B 로 잘리면
// **고친 것이 도로 없어진다.**
var askMarkers = []string{
	"Your questions have been answered:",
	"The user answered:",
}

// failMarkers 는 호스트가 붙이는 거부·중단 문구다. is_error 가 안 붙는 경우가 있어
// 따로 본다.
var failMarkers = []string{
	"The user doesn't want to proceed",
	"[Request interrupted by user",
	"Permission for this action was denied",
}

// markerWindow 는 머리말을 찾는 범위다. 본문 전체를 뒤지면 안 된다 — 실측에서 거부
// 문구를 전문 검색했더니 **소스 파일을 읽은 Bash 결과**가 걸렸다(파일 안에 "rejected"
// 가 있었다). 호스트 문구는 언제나 본문 맨 앞에 온다.
const markerWindow = 240

// resultLine 은 tool_result 하나를 발췌에 실을 한 줄로 줄인다. 버릴 것이면 빈 문자열.
func resultLine(name string, b *block) string {
	body := resultBody(b.Content)
	limit, keep := resultLimit(name, body, b.IsError)
	if !keep {
		return ""
	}
	// **자르기 전에 가린다.** 순서가 뒤집히면 잘린 조각에 토큰의 앞부분이 남는다.
	// 가림 규칙은 toolsum 것을 그대로 쓴다 — 호스트별로 복제하면 한쪽만 고쳐져
	// 조용히 새기 때문이다(toolsum 의 패키지 주석).
	body = toolsum.Redact(collapse(body))
	if body == "" {
		return ""
	}
	if name == "" {
		name = unknownTool
	}
	sep := " → "
	if b.IsError {
		// 실패를 표시해 준다. 판별기가 "무엇이 이 선택을 뒤집었나" 를 보려면
		// 결과 본문만으로는 부족하다 — 성공한 출력과 모양이 같은 실패가 흔하다.
		sep = " 실패 → "
	}
	return name + sep + summarize(body, limit)
}

// resultLimit 은 결과 하나에 줄 상한을 고른다. keep=false 면 버린다.
//
// 순서가 규칙이다. 답변 > 실패 > 조회성 버리기 > 나머지.
func resultLimit(name, body string, isErr bool) (limit int, keep bool) {
	head := body
	if len(head) > markerWindow {
		head = head[:markerWindow]
	}
	switch {
	case name == "AskUserQuestion" && !isErr:
		return maxAskResult, true
	case hasAny(head, askMarkers):
		return maxAskResult, true
	case isErr || hasAny(head, failMarkers):
		return maxFailResult, true
	case lookupTools[name]:
		return 0, false
	default:
		return maxToolResult, true
	}
}

func hasAny(s string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// resultBody 는 결과 블록의 본문을 문자열로 편다.
//
// content 는 문자열이거나 블록 배열이다 — 실측 1,249건 중 문자열 1,212(97.0%) ·
// 배열 37(3.0%). 배열 안은 text 21 · tool_reference 20 · image 4 라, text 만 잇는다.
func resultBody(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, ib := range blocks {
		if ib.Type == "text" && strings.TrimSpace(ib.Text) != "" {
			parts = append(parts, ib.Text)
		}
	}
	return strings.Join(parts, " ")
}

// collapse 는 공백 덩어리를 한 칸으로 눌러 한 줄로 만든다.
//
// 도구 출력은 줄바꿈과 들여쓰기가 절반이다(Read 는 앞에 줄번호까지 붙는다). 눌러야
// 같은 예산에 실제 내용이 더 들어가고, 발췌에서 결과 한 건이 화면을 세로로 먹지 않는다.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// summarize 는 긴 결과의 **가운데**를 버린다.
//
// 도구 출력은 앞(무엇을 했나)과 뒤(어떻게 끝났나)에 신호가 몰린다. 테스트 출력이
// 전형이다 — 가운데는 통과한 패키지 목록이고, 판별기가 볼 것은 첫 줄과 마지막의
// ok/FAIL 이다. 앞만 자르면 결말을 잃고, 뒤만 자르면 무엇을 한 건지를 잃는다.
func summarize(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	const mark = " …(생략)… "
	head := limit * 2 / 3
	return cutHead(s, head) + mark + cutTail(s, limit-head)
}

// cutHead·cutTail 은 바이트로 자르되 UTF-8 문자 가운데서 끊지 않는다. 한국어가 깨진 채
// 판별기에게 넘어가면 그 줄은 읽히지 않는다.
func cutHead(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func cutTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}
