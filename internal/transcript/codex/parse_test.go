package codex

import (
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/transcript"
)

// line 은 rollout JSONL 한 줄을 만든다.
func line(t string, payload string) string {
	return `{"type":"` + t + `","timestamp":"2026-08-12T01:02:03.000Z","payload":` + payload + `}` + "\n"
}

func kinds(turns []transcript.Turn) []transcript.Kind {
	var out []transcript.Kind
	for _, tn := range turns {
		out = append(out, tn.Kind)
	}
	return out
}

// ★ 세션 정보는 첫 줄 하나에만 있다.
//
// Claude Code 는 모든 줄이 cwd 를 실어 나르지만 Codex 는 session_meta 한 줄뿐이다.
// 그 줄을 못 읽으면 도메인 해석이 통째로 기본값으로 떨어진다.
func TestParseTakesMetaFromSessionMeta(t *testing.T) {
	in := line("session_meta", `{"id":"019f7151","cwd":"/Users/x/project/omni","cli_version":"1.2"}`) +
		line("response_item", `{"type":"message","role":"user","content":[{"type":"input_text","text":"저장 엔진을 정하자"}]}`)

	turns, meta, consumed, bad, err := Parse(strings.NewReader(in))
	if err != nil || bad != 0 {
		t.Fatalf("err=%v bad=%d", err, bad)
	}
	if meta.SessionID != "019f7151" {
		t.Errorf("SessionID=%q", meta.SessionID)
	}
	if meta.Cwd != "/Users/x/project/omni" {
		t.Errorf("Cwd=%q — 도메인 해석이 기본값으로 떨어진다", meta.Cwd)
	}
	if consumed != int64(len(in)) {
		t.Errorf("consumed=%d, want %d", consumed, len(in))
	}
	if len(turns) != 1 || turns[0].Kind != transcript.KindUser {
		t.Errorf("발화 %v", kinds(turns))
	}
	if turns[0].Timestamp.IsZero() {
		t.Error("시각을 못 읽었다 — 구간의 날짜가 어긋난다")
	}
}

// ★★ **role 이 정본이다. 블록 종류가 아니다.**
//
// input_text/output_text 로도 사람과 에이전트가 갈리는 것처럼 보이지만, 그건
// 호스트가 늘릴 수 있는 열거값이다. 새 종류가 하나 생기면 그 발화가 통째로
// 어긋난 쪽으로 분류된다 — role 로 보면 그런 일이 없다.
func TestParseUsesRoleNotBlockType(t *testing.T) {
	// 일부러 어긋나게 만든다: role=assistant 인데 블록은 input_text.
	in := line("session_meta", `{"id":"s1","cwd":"/x"}`) +
		line("response_item", `{"type":"message","role":"assistant","content":[{"type":"input_text","text":"SQLite 로 간다"}]}`)

	turns, _, _, _, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("발화 %d개", len(turns))
	}
	if turns[0].Kind != transcript.KindAssistant {
		t.Errorf("Kind=%v — 블록 종류를 보고 사람으로 분류했다", turns[0].Kind)
	}
}

// ★★ **event_msg 를 읽으면 같은 말이 두 번 잡힌다.**
//
// Codex 는 발화를 response_item 과 event_msg 양쪽에 남긴다. 둘 다 읽으면 턴 수가
// 두 배로 세지고, 임계가 절반 만에 차서 안전망이 소음이 된다 — 그러면 에이전트가
// 그것을 무시하는 법을 배운다. 정본은 response_item 이다.
func TestParseIgnoresEventMsgToAvoidDoubleCounting(t *testing.T) {
	in := line("session_meta", `{"id":"s1","cwd":"/x"}`) +
		line("response_item", `{"type":"message","role":"user","content":[{"type":"input_text","text":"이걸로 가자"}]}`) +
		line("event_msg", `{"type":"user_message","message":"이걸로 가자"}`) +
		line("event_msg", `{"type":"agent_message","message":"알겠습니다"}`) +
		line("response_item", `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"알겠습니다"}]}`)

	turns, _, _, bad, err := Parse(strings.NewReader(in))
	if err != nil || bad != 0 {
		t.Fatalf("err=%v bad=%d", err, bad)
	}
	if len(turns) != 2 {
		t.Fatalf("발화 %d개 (%v) — event_msg 까지 세면 4개가 된다", len(turns), kinds(turns))
	}
	n := 0
	for _, tn := range turns {
		if tn.Kind.Counts() {
			n++
		}
	}
	if n != 2 {
		t.Errorf("세어야 할 턴이 %d개다", n)
	}
}

// ★ 도구 호출은 담되 턴으로 세지 않는다.
//
// 되돌리기 어려운 선택은 산문이 아니라 편집과 명령으로 남는 경우가 많다. Codex 는
// 특히 그렇다 — 실측 상위 도구가 exec_command · apply_patch 다. 다만 턴으로 세면
// 감사 결함 6 이 재현된다 (도구 한 번에 임계가 확 찬다).
func TestParseCarriesToolCallsButDoesNotCountThem(t *testing.T) {
	in := line("session_meta", `{"id":"s1","cwd":"/x"}`) +
		line("response_item", `{"type":"function_call","name":"exec_command","arguments":"{\"command\":\"cd /x && go test ./...\"}"}`) +
		line("response_item", `{"type":"custom_tool_call","name":"apply_patch","input":"{\"path\":\"internal/a.go\"}"}`) +
		line("response_item", `{"type":"function_call_output","call_id":"c1","output":"아주 긴 출력…"}`)

	turns, _, _, bad, err := Parse(strings.NewReader(in))
	if err != nil || bad != 0 {
		t.Fatalf("err=%v bad=%d", err, bad)
	}
	if len(turns) != 2 {
		t.Fatalf("발화 %d개 (%v) — 출력(function_call_output)까지 담으면 발췌가 터진다",
			len(turns), kinds(turns))
	}
	for _, tn := range turns {
		if tn.Kind != transcript.KindTool {
			t.Errorf("Kind=%v, 도구여야 한다", tn.Kind)
		}
		if tn.Kind.Counts() {
			t.Error("도구를 턴으로 셌다 — 임계가 몇 배로 빨리 찬다 (감사 결함 6)")
		}
	}
	// 준비 동작(cd)을 건너뛰고 실제로 한 일이 남아야 한다.
	if !strings.Contains(turns[0].Text, "go test") {
		t.Errorf("명령 요약이 %q — cd 만 담으면 아무것도 안 담는 것과 같다", turns[0].Text)
	}
	if !strings.Contains(turns[1].Text, "internal/a.go") {
		t.Errorf("대상 파일이 안 남았다: %q", turns[1].Text)
	}
}

// ★★ **자격증명은 도구 줄에서 가려야 한다.**
//
// 이 줄은 상태 파일(state.json)에 남고 판별기에게도 넘어간다 — 우리가 새로 만드는
// 노출이다. 호스트가 늘어날 때마다 이 방어를 복제하면 반드시 한쪽이 어긋나므로
// toolsum 하나를 공유한다. 이 테스트는 Codex 경로가 정말 그것을 지나는지 본다.
func TestParseRedactsSecretsInToolLines(t *testing.T) {
	in := line("session_meta", `{"id":"s1","cwd":"/x"}`) +
		line("response_item", `{"type":"function_call","name":"exec_command","arguments":"{\"command\":\"curl -H 'Authorization: Bearer sk-verysecrettoken12345' https://x\"}"}`)

	turns, _, _, _, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("발화 %d개", len(turns))
	}
	if strings.Contains(turns[0].Text, "sk-verysecrettoken12345") {
		t.Errorf("토큰이 그대로 남았다 — state.json 과 판별기로 샌다: %q", turns[0].Text)
	}
	if !strings.Contains(turns[0].Text, "가림") {
		t.Errorf("가림 표시가 없다: %q", turns[0].Text)
	}
}

// ★★ **쓰이는 중인 마지막 줄은 소비하지 않는다.**
//
// 개행 없이 끝난 줄은 아직 쓰이는 중이다. 그걸 consumed 에 넣으면 체크포인트가
// 그 줄을 지나쳐 버리고, 완성된 내용은 **영영 안 읽힌다** (감사 결함 1).
func TestParseDoesNotConsumePartialLine(t *testing.T) {
	full := line("session_meta", `{"id":"s1","cwd":"/x"}`) +
		line("response_item", `{"type":"message","role":"user","content":[{"type":"input_text","text":"확정"}]}`)
	partial := `{"type":"response_item","timestamp":"2026-08-12T01:02:04.000Z","payload":{"type":"mes`

	turns, _, consumed, bad, err := Parse(strings.NewReader(full + partial))
	if err != nil || bad != 0 {
		t.Fatalf("err=%v bad=%d — 쓰이는 중인 줄을 깨진 줄로 세면 안 된다", err, bad)
	}
	if consumed != int64(len(full)) {
		t.Errorf("consumed=%d, want %d — 다음 스캔이 그 줄을 못 읽는다", consumed, len(full))
	}
	if len(turns) != 1 {
		t.Errorf("발화 %d개", len(turns))
	}
}

// ★ 깨진 줄은 세되 나머지는 계속 읽는다.
//
// bad != 0 이면 호출자가 체크포인트를 전진시키지 않는다. 그런데 여기서 멈춰 버리면
// 뒤에 있는 멀쩡한 발화를 못 본다 — 한 줄 때문에 세션 전체가 사라진다.
func TestParseCountsBadLinesAndKeepsGoing(t *testing.T) {
	in := line("session_meta", `{"id":"s1","cwd":"/x"}`) +
		"{이건 JSON 이 아니다\n" +
		line("response_item", `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"계속 읽혔다"}]}`)

	turns, meta, _, bad, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if bad != 1 {
		t.Errorf("bad=%d, want 1", bad)
	}
	if len(turns) != 1 || turns[0].Text != "계속 읽혔다" {
		t.Errorf("깨진 줄 뒤가 안 읽혔다: %v", kinds(turns))
	}
	if meta.Cwd != "/x" {
		t.Errorf("Cwd=%q", meta.Cwd)
	}
}

// ★ reasoning 은 암호화돼 있어 담을 것이 없다.
//
// 실측으로 세션 120개의 12,809 블록이 전부 그랬다. 파서가 이걸 발화로 만들면
// 빈 턴이 대량으로 생겨 임계를 오염시킨다.
func TestParseSkipsEncryptedReasoning(t *testing.T) {
	in := line("session_meta", `{"id":"s1","cwd":"/x"}`) +
		line("response_item", `{"type":"reasoning","encrypted_content":"gAAAAA…","content":[],"summary":[]}`)

	turns, _, _, bad, err := Parse(strings.NewReader(in))
	if err != nil || bad != 0 {
		t.Fatalf("err=%v bad=%d", err, bad)
	}
	if len(turns) != 0 {
		t.Errorf("발화 %d개 (%v) — 암호화된 사고는 담을 것이 없다", len(turns), kinds(turns))
	}
}

// 빈 글은 발화가 아니다. 담으면 시그널 없는 턴이 임계만 채운다.
func TestParseSkipsEmptyText(t *testing.T) {
	in := line("session_meta", `{"id":"s1","cwd":"/x"}`) +
		line("response_item", `{"type":"message","role":"user","content":[{"type":"input_text","text":"   "}]}`)

	turns, _, _, _, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 0 {
		t.Errorf("빈 글을 발화로 셌다: %d개", len(turns))
	}
}

// ★★ **크기를 넘는 줄은 건너뛰되 깨진 줄로 세면 안 된다.**
//
// 깨진 줄이 있으면 호출자가 체크포인트를 전진시키지 않는다 — "다음에 다시 읽어라"
// 라는 뜻이다. 그런데 크기 초과는 다음에도 똑같이 초과하므로, 깨진 줄로 세면 그
// 파일이 **영영 멈추고 그 줄 뒤의 결정이 전부 사라진다.**
//
// 실측: 세션 1,729개에서 8MB 를 넘긴 줄은 18개뿐이고 전부 compacted(대화 요약)였다.
// 한 줄을 잃는 것과 파일 전체를 잃는 것의 차이다.
func TestParseSkipsOversizeLineWithoutBlockingCheckpoint(t *testing.T) {
	huge := strings.Repeat("가", maxLine) // 룬 하나가 3바이트라 넉넉히 넘는다
	in := line("session_meta", `{"id":"s1","cwd":"/x"}`) +
		line("compacted", `{"type":"message","text":"`+huge+`"}`) +
		line("response_item", `{"type":"message","role":"user","content":[{"type":"input_text","text":"그 뒤의 결정"}]}`)

	turns, _, consumed, bad, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if bad != 0 {
		t.Errorf("bad=%d — 크기 초과를 깨진 줄로 세면 이 파일은 영영 전진하지 못한다", bad)
	}
	if consumed != int64(len(in)) {
		t.Errorf("consumed=%d, want %d — 건너뛴 줄도 소비는 해야 다음 줄로 간다",
			consumed, len(in))
	}
	if len(turns) != 1 || turns[0].Text != "그 뒤의 결정" {
		t.Errorf("초과 줄 뒤가 안 읽혔다: %v", turns)
	}
}

// ★ event_msg 의 발화 필드를 읽으면 두 배로 세진다.
//
// 위 TestParseIgnoresEventMsg… 가 개수를 보는 반면, 이건 **본문이 새어 나오지
// 않는지**를 본다. event_msg 만 있고 response_item 이 없으면 발화는 0이어야 한다.
func TestParseTakesNothingFromEventMsgAlone(t *testing.T) {
	in := line("session_meta", `{"id":"s1","cwd":"/x"}`) +
		line("event_msg", `{"type":"user_message","message":"이 문장이 새면 안 된다","role":"user"}`) +
		line("event_msg", `{"type":"agent_message","message":"이것도","role":"assistant"}`)

	turns, _, _, bad, err := Parse(strings.NewReader(in))
	if err != nil || bad != 0 {
		t.Fatalf("err=%v bad=%d", err, bad)
	}
	if len(turns) != 0 {
		t.Errorf("event_msg 에서 발화 %d개가 나왔다 (%v) — response_item 과 겹쳐 두 배로 세진다",
			len(turns), turns)
	}
}
