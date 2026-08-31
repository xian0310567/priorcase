package claudecode

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/xian0310567/priorcase/internal/transcript"
)

// 실제 Claude Code JSONL 의 레코드 종류를 다 담은 픽스처. 값은 합성이지만 **형태는
// 실물 그대로**다 (필드 구성을 실 transcript 에서 확인해 옮겼다).
func fixture() []string {
	return []string{
		`{"type":"user","isSidechain":false,"cwd":"/tmp/proj/alpha","sessionId":"S1","timestamp":"2026-08-07T01:00:00.000Z","message":{"role":"user","content":"저장 엔진을 무엇으로 할까"}}`,
		`{"type":"assistant","isSidechain":false,"cwd":"/tmp/proj/alpha","sessionId":"S1","timestamp":"2026-08-07T01:00:01.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"임베디드 DB 로 하는 게 낫겠다"}]}}`,
		`{"type":"assistant","isSidechain":false,"cwd":"/tmp/proj/alpha","sessionId":"S1","timestamp":"2026-08-07T01:00:02.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{}}]}}`,
		`{"type":"user","isSidechain":false,"cwd":"/tmp/proj/alpha","sessionId":"S1","timestamp":"2026-08-07T01:00:03.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"파일 내용"}]}}`,
		`{"type":"assistant","isSidechain":false,"cwd":"/tmp/proj/alpha","sessionId":"S1","timestamp":"2026-08-07T01:00:04.000Z","message":{"role":"assistant","content":[{"type":"text","text":"임베디드 DB 로 하겠다"}]}}`,
		// isMeta — 회수 훅이 주입한 컨텍스트다. 사람이 한 말이 아니다.
		`{"type":"user","isMeta":true,"isSidechain":false,"cwd":"/tmp/proj/alpha","sessionId":"S1","timestamp":"2026-08-07T01:00:05.000Z","message":{"role":"user","content":"[과거 결정 참조]\n- 2026-08-01 저장 엔진을 임베디드 DB 로 고른다"}}`,
		`{"type":"system","subtype":"hook","sessionId":"S1","hookAdditionalContext":"결정 결정 결정"}`,
		`{"type":"attachment","sessionId":"S1","attachment":{}}`,
		`{"type":"file-history-snapshot","messageId":"m1","snapshot":{}}`,
	}
}

func parse(t *testing.T, body string) ([]transcript.Turn, transcript.Meta, int64, int) {
	t.Helper()
	turns, meta, consumed, bad, err := Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return turns, meta, consumed, bad
}

// nonTool 은 도구 활동을 뺀 발화다. 감사 결함 6 이 지키려던 것은 "툴 한 번에 턴이
// 여러 개 차는 것" 을 막는 것이라, 도구 활동이 Turn 으로 들어온 뒤로는 이쪽으로 봐야
// 원래 계약을 검사하게 된다. (thinking 은 발화라서 남긴다 — 임계에 안 셀 뿐이다.)
func nonTool(turns []transcript.Turn) []transcript.Turn {
	var out []transcript.Turn
	for _, tn := range turns {
		if tn.Kind != transcript.KindTool {
			out = append(out, tn)
		}
	}
	return out
}

func kinds(turns []transcript.Turn) []transcript.Kind {
	var k []transcript.Kind
	for _, tn := range turns {
		k = append(k, tn.Kind)
	}
	return k
}

// 감사 결함 6 — tool_use·tool_result 를 세면 어시스턴트 응답 한 번에 턴이 여러 개 찬다.
func TestToolRecordsAreNotTurns(t *testing.T) {
	body := strings.Join(fixture(), "\n") + "\n"
	turns, _, _, bad := parse(t, body)

	if bad != 0 {
		t.Fatalf("깨진 줄 %d개, 0이어야 한다", bad)
	}
	// ★ 계약이 정밀해졌다 (2026-08-09). 도구 활동은 이제 Turn 으로 들어오지만
	// **임계에는 안 센다**. 감사 결함 6 이 막으려던 것은 "툴 한 번에 턴이 여러 개
	// 차는 것" 이지 "도구 활동을 아예 안 보는 것" 이 아니었다 — 되돌리기 어려운
	// 선택은 산문이 아니라 편집과 명령으로 남는 경우가 많다.
	want := []transcript.Kind{transcript.KindUser, transcript.KindThinking, transcript.KindAssistant}
	c := nonTool(turns)
	if got := kinds(c); len(got) != len(want) {
		t.Fatalf("임계에 세는 Turn %d개 (%v), %d개여야 한다 (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if c[i].Kind != want[i] {
			t.Errorf("counted[%d].Kind = %s, want %s", i, c[i].Kind, want[i])
		}
	}
	// 턴 수 임계에 세는 것은 둘뿐 — thinking 은 안 센다.
	n := 0
	for _, tn := range turns {
		if tn.Kind.Counts() {
			n++
		}
	}
	if n != 2 {
		t.Errorf("세는 턴 %d개, 2개여야 한다", n)
	}
}

// **isMeta 를 거르지 않으면 매 세션이 시그널에 걸린다.** 회수 훅이 주입하는
// "[과거 결정 참조]" 가 isMeta user 레코드로 들어오고, 거기엔 "결정" 이 항상 있다.
// 자기가 주입한 글을 자기가 읽고 "결정이 있다" 고 판정하는 자기 참조 고리다.
func TestMetaRecordsAreExcluded(t *testing.T) {
	body := strings.Join(fixture(), "\n") + "\n"
	turns, _, _, _ := parse(t, body)
	for _, tn := range turns {
		if strings.Contains(tn.Text, "과거 결정 참조") {
			t.Fatalf("isMeta 로 주입된 회수 결과가 Turn 으로 들어왔다: %q", tn.Text)
		}
	}
}

// 훅이 붙인 system 레코드도 대화가 아니다.
func TestSystemRecordsAreExcluded(t *testing.T) {
	body := strings.Join(fixture(), "\n") + "\n"
	turns, _, _, _ := parse(t, body)
	for _, tn := range turns {
		if strings.Contains(tn.Text, "결정 결정 결정") {
			t.Fatal("system 훅 컨텍스트가 Turn 으로 들어왔다")
		}
	}
}

// 감사 결함 1 — 마지막 줄이 쓰이는 도중이면 개행이 아직 없다. 그 줄을 처리하려 들면
// 파싱이 깨지고, 옛 구현은 그걸 "내용 없는 구간" 으로 오판해 체크포인트를 전진시켜
// **그 구간의 결정을 영원히 잃었다.**
//
// 여기서는 완결된 줄까지만 소비한다. consumed 가 잘린 줄 앞에서 멈추므로, 다음 스캔이
// 그 줄을 처음부터 다시 읽는다 — 그때는 쓰기가 끝나 있다.
func TestTruncatedLastLineIsNotConsumed(t *testing.T) {
	lines := fixture()
	complete := strings.Join(lines, "\n") + "\n"
	truncated := complete + `{"type":"assistant","cwd":"/tmp/proj/alpha","sessionId":"S1","message":{"role":"assis`

	turns, _, consumed, bad := parse(t, truncated)

	if bad != 0 {
		t.Errorf("깨진 줄 %d개 — 잘린 마지막 줄은 '깨진 줄' 이 아니라 '아직 안 온 줄' 이다", bad)
	}
	if consumed != int64(len(complete)) {
		t.Errorf("consumed = %d, 완결 구간 %d 여야 한다 (잘린 줄을 삼켰다)", consumed, len(complete))
	}
	if got := len(nonTool(turns)); got != 3 {
		t.Errorf("앞 구간 발화 %d개, 3개여야 한다 — 잘린 줄 하나가 구간 전체를 삼켰다", got)
	}
}

// 완결됐지만 JSON 이 깨진 줄은 다르다. 기다려도 고쳐지지 않는다. 건너뛰되 **세어서
// 알린다** — 호출자는 bad > 0 이면 체크포인트를 전진시키지 않는다.
func TestCorruptCompleteLineIsCountedNotSwallowed(t *testing.T) {
	lines := fixture()
	lines = append(lines[:5], append([]string{`{"type":"user","message":{`}, lines[5:]...)...)
	body := strings.Join(lines, "\n") + "\n"

	turns, _, consumed, bad := parse(t, body)

	if bad != 1 {
		t.Errorf("bad = %d, 1이어야 한다 — 깨진 줄이 조용히 사라졌다", bad)
	}
	if consumed != int64(len(body)) {
		t.Errorf("consumed = %d, %d 여야 한다", consumed, len(body))
	}
	if got := len(nonTool(turns)); got != 3 {
		t.Errorf("발화 %d개, 3개여야 한다 — 깨진 줄 하나가 나머지를 삼켰다", got)
	}
}

func TestMetaCarriesSessionAndCwd(t *testing.T) {
	body := strings.Join(fixture(), "\n") + "\n"
	_, meta, _, _ := parse(t, body)
	if meta.SessionID != "S1" {
		t.Errorf("SessionID = %q, want S1", meta.SessionID)
	}
	if meta.Cwd != "/tmp/proj/alpha" {
		t.Errorf("Cwd = %q, want /tmp/proj/alpha", meta.Cwd)
	}
}

func TestEmptyInput(t *testing.T) {
	turns, _, consumed, bad := parse(t, "")
	if len(turns) != 0 || consumed != 0 || bad != 0 {
		t.Errorf("빈 입력에서 turns=%d consumed=%d bad=%d", len(turns), consumed, bad)
	}
}

// 서브에이전트 대화도 결정을 담는다 — 시그널 검색 대상에서 빼지 않는다.
func TestSidechainIsKeptAndMarked(t *testing.T) {
	body := `{"type":"assistant","isSidechain":true,"sessionId":"S1","cwd":"/tmp/proj/alpha","timestamp":"2026-08-07T01:00:00.000Z","message":{"role":"assistant","content":[{"type":"text","text":"서브에이전트 결론"}]}}` + "\n"
	turns, _, _, _ := parse(t, body)
	if len(turns) != 1 {
		t.Fatalf("Turn %d개, 1개여야 한다", len(turns))
	}
	if !turns[0].Sidechain {
		t.Error("Sidechain 표시가 안 됐다")
	}
}

// Claude Code 는 thinking 을 암호화된 signature 로만 저장하고 본문은 빈 문자열로 둔다
// (실측: 파일 1173개 · 블록 13451개 전부). 빈 본문으로 Turn 을 만들면 **내용 없는
// 발화**가 대량으로 생겨 턴 수 임계와 시그널 검색이 둘 다 오염된다.
func TestEmptyThinkingProducesNoTurn(t *testing.T) {
	body := `{"type":"assistant","sessionId":"S1","cwd":"/tmp/proj/alpha","timestamp":"2026-08-07T01:00:00.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"","signature":"AAAA"}]}}` + "\n"
	turns, _, _, bad := parse(t, body)
	if len(turns) != 0 {
		t.Errorf("본문 없는 thinking 에서 Turn %d개가 나왔다: %+v", len(turns), turns)
	}
	if bad != 0 {
		t.Errorf("bad = %d — 빈 thinking 은 깨진 줄이 아니다", bad)
	}
}

// ★★ 도구 활동은 Turn 으로 들어오지만 **임계에는 절대 안 센다.**
//
// 감사 결함 6 이 그것 때문에 생겼다 — 툴 한 번에 tool_use + tool_result 로 두 턴이
// 차서, 어시스턴트가 툴을 세 번 부르면 여섯 턴이 찼다. 실측으로 레코드 5920개 중
// 실제 발화는 991개였다(6배). 발췌에 실으면서 이 규칙이 깨지면 안전망이 대화가
// 없는데도 계속 발동한다.
func TestToolActivityNeverCountsTowardThreshold(t *testing.T) {
	if transcript.KindTool.Counts() {
		t.Fatal("KindTool 이 임계에 센다 — 감사 결함 6 이 되살아난다")
	}
	body := strings.Join(fixture(), "\n") + "\n"
	turns, _, _, _ := parse(t, body)

	tools, counted := 0, 0
	for _, tn := range turns {
		if tn.Kind == transcript.KindTool {
			tools++
		}
		if tn.Kind.Counts() {
			counted++
		}
	}
	if tools == 0 {
		t.Fatal("도구 활동이 하나도 안 잡혔다 — 67.6% 를 여전히 버리고 있다")
	}
	if counted != 2 {
		t.Errorf("임계에 세는 발화 %d개, 2개(user+assistant)여야 한다", counted)
	}
}

// 도구 활동은 **무엇을 했는지**를 담아야 한다. 이름만으로는 판별기가 못 쓴다.
func TestToolActivityCarriesTarget(t *testing.T) {
	lines := []string{
		`{"type":"assistant","cwd":"/tmp/p","sessionId":"S1","timestamp":"2026-08-07T01:00:00.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"a","name":"Edit","input":{"file_path":"internal/core/store/frontmatter.go"}}]}}`,
		`{"type":"assistant","cwd":"/tmp/p","sessionId":"S1","timestamp":"2026-08-07T01:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"b","name":"Bash","input":{"command":"go test ./... -race\nsecond line"}}]}}`,
	}
	turns, _, _, _ := parse(t, strings.Join(lines, "\n")+"\n")
	var got []string
	for _, tn := range turns {
		if tn.Kind == transcript.KindTool {
			got = append(got, tn.Text)
		}
	}
	if len(got) != 2 {
		t.Fatalf("도구 활동 %d개, 2개여야 한다: %v", len(got), got)
	}
	if got[0] != "Edit internal/core/store/frontmatter.go" {
		t.Errorf("편집 대상이 안 담겼다: %q", got[0])
	}
	if got[1] != "Bash go test ./... -race" {
		t.Errorf("명령 첫 줄이 안 담겼다 (여러 줄은 첫 줄만): %q", got[1])
	}
}

// ★ 명령줄에는 자격증명이 섞인다. 발화보다 위험하다 — 이 줄은 state.json 에 남고
// 판별기에게도 넘어가므로, 우리가 새로 만드는 노출이다.
func TestToolActivityRedactsSecrets(t *testing.T) {
	for _, tc := range []struct{ cmd, mustNot string }{
		{`export GITHUB_TOKEN=ghp_abcdefghijklmnop`, "ghp_abcdefghijklmnop"},
		{`curl -H "Authorization: Bearer sk-abc123456789"`, "sk-abc123456789"},
		{`psql "password=hunter2secret"`, "hunter2secret"},
		{`aws --key AKIAIOSFODNN7EXAMPLE`, "AKIAIOSFODNN7EXAMPLE"},
	} {
		line := `{"type":"assistant","cwd":"/tmp/p","sessionId":"S1","timestamp":"2026-08-07T01:00:00.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"a","name":"Bash","input":{"command":` +
			mustJSON(tc.cmd) + `}}]}}`
		turns, _, _, _ := parse(t, line+"\n")
		for _, tn := range turns {
			if tn.Kind == transcript.KindTool && strings.Contains(tn.Text, tc.mustNot) {
				t.Errorf("자격증명이 그대로 남았다: %q", tn.Text)
			}
		}
	}
}

func mustJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// ───────────────────────── tool_result (2026-08-19) ─────────────────────────
//
// 배경: content-block switch 에 tool_result case 가 없어서 결과를 통째로 버렸다.
// 그래서 **AskUserQuestion 의 사용자 답변이 발췌에 없었고**, 판별기가 원장에
// (promotions.jsonl 11행) "실제로 무엇을 정했는지 불명확" 이라 적고 record=false 를
// 냈다. 최근 7일 판정 23건 / 자동 기록 0건이 그렇게 나왔다.

// useRec 는 tool_use 레코드 한 줄이다 (결과가 이름을 찾을 수 있게 id 를 준다).
func useRec(id, name, path string) string {
	return `{"type":"assistant","cwd":"/tmp/p","sessionId":"S1","timestamp":"2026-08-07T01:00:00.000Z",` +
		`"message":{"role":"assistant","content":[{"type":"tool_use","id":` + mustJSON(id) +
		`,"name":` + mustJSON(name) + `,"input":{"file_path":` + mustJSON(path) + `}}]}}`
}

// resultRec 는 tool_result 레코드 한 줄이다. 실물처럼 **user 레코드**에 담긴다 —
// 그래서 이걸 발화로 세면 턴 수 관문이 오염된다.
func resultRec(id, body string, isErr bool) string {
	e := "false"
	if isErr {
		e = "true"
	}
	return `{"type":"user","cwd":"/tmp/p","sessionId":"S1","timestamp":"2026-08-07T01:00:01.000Z",` +
		`"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":` + mustJSON(id) +
		`,"content":` + mustJSON(body) + `,"is_error":` + e + `}]}}`
}

// toolTexts 는 도구 줄(호출+결과)만 뽑는다.
func toolTexts(turns []transcript.Turn) []string {
	var out []string
	for _, tn := range turns {
		if tn.Kind == transcript.KindTool {
			out = append(out, tn.Text)
		}
	}
	return out
}

// resultTexts 는 **결과 줄만** 뽑는다. 호출 줄("Read foo.go")과 달리 결과 줄에는
// 화살표 표지가 있다.
func resultTexts(turns []transcript.Turn) []string {
	var out []string
	for _, s := range toolTexts(turns) {
		if strings.Contains(s, "→") {
			out = append(out, s)
		}
	}
	return out
}

func joinLines(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

// 실측한 AskUserQuestion 결과의 두 문형. 값만 줄였고 **형태는 실물 그대로**다.
const askAnsweredBody = `Your questions have been answered: "늘어난 기록을 어디에 담을까요?"="2계층 — 결정 노트 + 작업로그 (권장)", ` +
	`"판별기가 언제 판정하게 할까요?"="둘 다 — 도중엔 작업로그, 끝날 때 결정 (권장)". You can now continue with these answers in mind.`

const askUserAnsweredBody = `The user answered: "git 커밋에 박힐 이름을 무엇으로 할까요?"="LeeJeongHan", ` +
	`"회사 레포와 개인 레포의 git 이메일을 분리할까요?"=(no option selected) notes: 분리하는데 개인 계정은 내일 등록할게.`

// ★★ 이 테스트가 이 변경의 전부다. 사용자가 **실제로 무엇을 골랐는지**가 발췌에 남아야 한다.
func TestAskUserQuestionAnswerSurvives(t *testing.T) {
	body := joinLines(
		useRec("tq", "AskUserQuestion", ""),
		resultRec("tq", askAnsweredBody, false),
	)
	turns, _, _, bad := parse(t, body)
	if bad != 0 {
		t.Fatalf("깨진 줄 %d개", bad)
	}
	got := resultTexts(turns)
	if len(got) != 1 {
		t.Fatalf("결과 줄 %d개, 1개여야 한다: %v", len(got), got)
	}
	// 고른 답이 **글자 그대로** 남아야 한다. 이름만 남으면 고치기 전과 같다.
	for _, want := range []string{
		"AskUserQuestion",
		"2계층 — 결정 노트 + 작업로그 (권장)",
		"둘 다 — 도중엔 작업로그, 끝날 때 결정 (권장)",
	} {
		if !strings.Contains(got[0], want) {
			t.Errorf("답변이 잘렸다 — %q 가 없다:\n%s", want, got[0])
		}
	}
}

// ★ 이름으로만 판별하면 **체크포인트에 걸린 답변을 도로 잃는다.**
//
// 데몬은 체크포인트부터 읽으므로, 호출(assistant)이 이전 구간이고 결과(user)만 이번
// 구간에 오는 일이 흔하다. 그때 id→이름 지도가 비어 있어 이름을 모른다. 머리말로도
// 판별해야 그 답변이 200B 로 잘려 사라지지 않는다.
func TestAskUserQuestionAnswerSurvivesWithoutItsToolUse(t *testing.T) {
	for _, body := range []string{askAnsweredBody, askUserAnsweredBody} {
		turns, _, _, _ := parse(t, joinLines(resultRec("없는id", body, false)))
		got := resultTexts(turns)
		if len(got) != 1 {
			t.Fatalf("결과 줄 %d개, 1개여야 한다: %v", len(got), got)
		}
		if len(got[0]) < len(body)/2 {
			t.Errorf("호출을 못 본 답변이 잘렸다 (%dB → %dB):\n%s", len(body), len(got[0]), got[0])
		}
	}
	// 실제로 고른 답이 남았는지도 본다.
	turns, _, _, _ := parse(t, joinLines(resultRec("없는id", askUserAnsweredBody, false)))
	if got := resultTexts(turns); len(got) == 0 || !strings.Contains(got[0], "LeeJeongHan") {
		t.Errorf("답변 본문이 안 남았다: %v", got)
	}
}

// ★★ 결과는 **발화가 아니다.** 감사 결함 6 이 정확히 그것이었다 — 툴 한 번에
// tool_use + tool_result 로 두 턴이 차서 min_turns=6 관문이 6배 빨리 채워졌다.
//
// 실측 대조(스냅샷 85개 파일): 이 변경 전후로 세는 발화가 user 171 · assistant 288 로
// **완전히 같았다**. 전체 Turn 만 1809 → 2957 로 늘었다.
func TestToolResultNeverCountsTowardThreshold(t *testing.T) {
	// 사용자가 끼어들면 한 레코드에 결과와 사람의 말이 같이 온다. 사람의 말만 세야 한다.
	mixed := `{"type":"user","cwd":"/tmp/p","sessionId":"S1","timestamp":"2026-08-07T01:00:02.000Z",` +
		`"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tb","content":"ok 0.3s"},` +
		`{"type":"text","text":"그만하고 다른 걸 하자"}]}}`
	body := joinLines(
		useRec("tb", "Bash", "/tmp/x"),
		resultRec("tb", "ok github.com/x/y 0.301s", false),
		mixed,
	)
	turns, _, _, _ := parse(t, body)

	counted := 0
	for _, tn := range turns {
		if tn.Kind.Counts() {
			counted++
		}
	}
	if counted != 1 {
		t.Errorf("세는 발화 %d개, 1개(사람의 말)여야 한다 — 결과가 관문을 오염시킨다", counted)
	}
	if got := len(resultTexts(turns)); got != 2 {
		t.Errorf("결과 줄 %d개, 2개여야 한다 — 결과를 여전히 버리고 있다", got)
	}
	// user 레코드에 실렸다고 사람의 말로 둔갑하면 안 된다.
	for _, tn := range turns {
		if tn.Kind == transcript.KindUser && strings.Contains(tn.Text, "0.301s") {
			t.Error("도구 결과가 사람의 발화로 들어왔다")
		}
	}
}

// 조회성 도구의 덤프는 버린다. **무엇을 봤는지는 호출 줄이 이미 말한다.**
//
// 실측: tool_result 본문 3,610,097B 중 Read 하나가 1,353,763B(37.5%)이고 p50 이 6,296B 다.
func TestLookupResultsAreDropped(t *testing.T) {
	dump := strings.Repeat("1\tpackage main 2\timport \"fmt\" ", 400)
	for _, name := range []string{"Read", "Grep", "Glob", "WebFetch"} {
		body := joinLines(useRec("t1", name, "/tmp/big.go"), resultRec("t1", dump, false))
		turns, _, _, _ := parse(t, body)
		if got := resultTexts(turns); len(got) != 0 {
			t.Errorf("%s 덤프가 발췌에 들어왔다: %.80s…", name, got[0])
		}
		// 호출 줄은 그대로 남아야 한다 — 그게 "무엇을 봤나" 다.
		if got := toolTexts(turns); len(got) != 1 || !strings.HasPrefix(got[0], name+" ") {
			t.Errorf("%s 호출 줄이 사라졌다: %v", name, got)
		}
	}
}

// ★ 단 **실패한 조회는 살린다.** "pdftoppm is not installed" 는 덤프가 아니라
// 계획을 뒤집는 사실이다 — 무엇이 이 선택을 뒤집었나가 우리가 찾는 것이다.
func TestFailedLookupSurvives(t *testing.T) {
	const msg = "pdftoppm is not installed. Install poppler-utils to enable PDF page rendering."
	body := joinLines(useRec("t1", "Read", "/tmp/a.pdf"), resultRec("t1", msg, true))
	turns, _, _, _ := parse(t, body)
	got := resultTexts(turns)
	if len(got) != 1 {
		t.Fatalf("실패한 조회가 버려졌다: %v", got)
	}
	if !strings.Contains(got[0], "poppler-utils") {
		t.Errorf("실패 사유가 안 담겼다: %q", got[0])
	}
	if !strings.Contains(got[0], "실패") {
		t.Errorf("실패 표시가 없다 — 성공한 출력과 구별이 안 된다: %q", got[0])
	}
}

// 사용자가 도구 실행을 거부한 것도 **사용자의 결정**이다. is_error 가 안 붙는 경우가
// 있어 머리말로도 잡는다.
func TestUserRefusalSurvives(t *testing.T) {
	const msg = "The user doesn't want to proceed with this tool use. The tool use was rejected " +
		"(eg. if it was a file edit, the new_string was NOT written to the file). STOP what you are doing."
	turns, _, _, _ := parse(t, joinLines(resultRec("없는id", msg, false)))
	got := resultTexts(turns)
	if len(got) != 1 || !strings.Contains(got[0], "doesn't want to proceed") {
		t.Errorf("사용자의 거부가 사라졌다: %v", got)
	}
}

// ★ 거부 문구를 본문 전체에서 찾으면 안 된다.
//
// 실측에서 걸렸다 — 전문 검색을 돌렸더니 **소스 파일을 읽은 Bash 결과**가 걸렸다
// (파일 안에 "rejected" 가 들어 있었다). 호스트 문구는 언제나 본문 맨 앞에 온다.
func TestRefusalMarkerIsNotMatchedDeepInsideBody(t *testing.T) {
	body := strings.Repeat("x", 600) + " The user doesn't want to proceed " + strings.Repeat("y", 600)
	turns, _, _, _ := parse(t, joinLines(useRec("t1", "Bash", "/tmp/x"), resultRec("t1", body, false)))
	got := resultTexts(turns)
	if len(got) != 1 {
		t.Fatalf("결과 줄 %d개", len(got))
	}
	// 거부로 오인했다면 320B 예산을 받았을 것이다. 일반 결과 예산으로 잘려야 한다.
	if len(got[0]) > 260 {
		t.Errorf("본문 깊숙한 문구를 거부로 오인했다 (%dB): %.120s…", len(got[0]), got[0])
	}
}

// 짧은 결과는 그대로, 긴 결과는 **가운데**를 버린다.
//
// 앞만 자르면 결말(ok/FAIL)을 잃고, 뒤만 자르면 무엇을 한 건지를 잃는다.
func TestLongResultKeepsHeadAndTail(t *testing.T) {
	const short = "ok github.com/xian0310567/priorcase/internal/core/judge 0.301s"
	turns, _, _, _ := parse(t, joinLines(useRec("t1", "Bash", "/tmp/x"), resultRec("t1", short, false)))
	got := resultTexts(turns)
	if len(got) != 1 || !strings.HasSuffix(got[0], short) {
		t.Fatalf("짧은 결과가 그대로 안 담겼다: %v", got)
	}

	long := "START-여기가앞머리 " + strings.Repeat("중간은버려도된다 ", 200) + "FAIL-여기가결말"
	turns, _, _, _ = parse(t, joinLines(useRec("t2", "Bash", "/tmp/x"), resultRec("t2", long, false)))
	got = resultTexts(turns)
	if len(got) != 1 {
		t.Fatalf("결과 줄 %d개", len(got))
	}
	if !strings.Contains(got[0], "START-여기가앞머리") {
		t.Errorf("앞머리가 없다: %q", got[0])
	}
	if !strings.Contains(got[0], "FAIL-여기가결말") {
		t.Errorf("결말이 없다 — 뒤를 잘랐다: %q", got[0])
	}
	if !strings.Contains(got[0], "생략") {
		t.Errorf("생략 표시가 없다 — 잘린 줄을 온전한 출력으로 읽으면 안 된다: %q", got[0])
	}
}

// ★ 한 줄이 발췌 예산(daemon.maxExcerpt=6000)을 넘으면 excerpt 가 거기서 멈춰
// **그 앞이 통째로 사라진다.** 상한이 바이트여야 하는 이유가 이것이다.
func TestResultLinesStayWellUnderExcerptBudget(t *testing.T) {
	huge := strings.Repeat("한글이라한글자가삼바이트다 ", 4000) // ≈160KB
	for _, tc := range []struct {
		name string
		max  int
	}{
		{"Bash", 260},
		{"AskUserQuestion", 1300},
	} {
		turns, _, _, _ := parse(t, joinLines(
			useRec("t1", tc.name, "/tmp/x"),
			resultRec("t1", huge, false),
		))
		got := resultTexts(turns)
		if len(got) != 1 {
			t.Fatalf("%s: 결과 줄 %d개", tc.name, len(got))
		}
		if len(got[0]) > tc.max {
			t.Errorf("%s 결과가 %dB — %dB 이하여야 한다 (발췌가 여기서 끊긴다)", tc.name, len(got[0]), tc.max)
		}
		// 바이트로 잘라도 한글이 깨지면 안 된다. 깨진 줄은 판별기가 못 읽는다.
		if !utf8.ValidString(got[0]) {
			t.Errorf("%s 결과가 UTF-8 문자 가운데서 잘렸다", tc.name)
		}
	}
}

// ★ 결과 본문에도 자격증명이 섞인다 — `env` 출력, 설정 파일 덤프. 발화보다 위험하다.
// 가림은 toolsum 것을 그대로 쓴다(복제된 보안 규칙은 반드시 어긋난다).
func TestToolResultRedactsSecrets(t *testing.T) {
	for _, tc := range []struct{ body, mustNot string }{
		{"GITHUB_TOKEN=ghp_abcdefghijklmnop", "ghp_abcdefghijklmnop"},
		{"Authorization: Bearer sk-abc123456789", "sk-abc123456789"},
		{"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE", "AKIAIOSFODNN7EXAMPLE"},
	} {
		turns, _, _, _ := parse(t, joinLines(
			useRec("t1", "Bash", "/tmp/x"),
			resultRec("t1", tc.body, false),
		))
		for _, s := range resultTexts(turns) {
			if strings.Contains(s, tc.mustNot) {
				t.Errorf("결과 본문의 자격증명이 그대로 남았다: %q", s)
			}
		}
	}
}

// content 는 문자열이거나 블록 배열이다 — 실측 1,249건 중 문자열 1,212(97.0%) ·
// 배열 37(3.0%). 배열 안에는 text 말고 tool_reference·image 도 섞여 온다.
func TestToolResultContentBlockArray(t *testing.T) {
	line := `{"type":"user","cwd":"/tmp/p","sessionId":"S1","timestamp":"2026-08-07T01:00:01.000Z",` +
		`"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[` +
		`{"type":"text","text":"빌드 통과"},` +
		`{"type":"image","source":{"type":"base64","data":"AAAA"}},` +
		`{"type":"tool_reference","name":"Bash"}]}]}}`
	turns, _, _, bad := parse(t, joinLines(useRec("t1", "Bash", "/tmp/x"), line))
	if bad != 0 {
		t.Fatalf("깨진 줄 %d개 — 배열 모양 content 를 못 읽었다", bad)
	}
	got := resultTexts(turns)
	if len(got) != 1 || !strings.Contains(got[0], "빌드 통과") {
		t.Fatalf("배열 모양 결과에서 본문을 못 뽑았다: %v", got)
	}
	if strings.Contains(got[0], "AAAA") {
		t.Error("이미지 데이터가 발췌에 들어왔다")
	}
}

// 빈 결과는 줄을 만들지 않는다 (실측: ToolSearch·Read 에 본문 0B 가 나온다).
// 내용 없는 줄이 발췌 예산과 반복 접기를 오염시킨다.
func TestEmptyToolResultProducesNoTurn(t *testing.T) {
	for _, empty := range []string{"", "   \n\t "} {
		turns, _, _, bad := parse(t, joinLines(useRec("t1", "Bash", "/tmp/x"), resultRec("t1", empty, false)))
		if bad != 0 {
			t.Fatalf("bad = %d", bad)
		}
		if got := resultTexts(turns); len(got) != 0 {
			t.Errorf("빈 결과에서 줄이 나왔다: %v", got)
		}
	}
}

// 결과 줄은 **어느 도구의 결과인지**를 달고 있어야 한다. 호출과 결과 사이에 다른
// 도구가 끼어드는 일이 흔해서, 앞줄을 보고 짐작하게 두면 판별기가 틀린다.
func TestToolResultCarriesToolName(t *testing.T) {
	body := joinLines(
		useRec("a", "Edit", "internal/core/store/frontmatter.go"),
		useRec("b", "Bash", "/tmp/x"),
		resultRec("b", "ok 0.3s", false),
		resultRec("a", "The file frontmatter.go has been updated successfully.", false),
	)
	turns, _, _, _ := parse(t, body)
	got := resultTexts(turns)
	if len(got) != 2 {
		t.Fatalf("결과 줄 %d개, 2개여야 한다: %v", len(got), got)
	}
	if !strings.HasPrefix(got[0], "Bash ") {
		t.Errorf("첫 결과가 Bash 것이 아니다: %q", got[0])
	}
	if !strings.HasPrefix(got[1], "Edit ") {
		t.Errorf("둘째 결과가 Edit 것이 아니다: %q", got[1])
	}
}

// ★★ 명령 첫 줄만 담으면 **아무것도 안 담는 것과 같다.**
//
// 실측으로 드러났다 — 실 트랜스크립트에서 발췌 12줄이 전부 `cd /Users/…` 였다.
// 에이전트는 거의 모든 Bash 를 `cd`·`export` 로 시작한다. 실제로 한 일은 그다음이다.
func TestBashActivitySkipsPrelude(t *testing.T) {
	for _, tc := range []struct{ cmd, want string }{
		{"cd /Users/x/proj\ngo test ./... -race", "Bash go test ./... -race"},
		{"cd /a && export GOTOOLCHAIN=auto && git checkout -b feat/x", "Bash git checkout -b feat/x"},
		{"export A=1\nset -e\nmake build", "Bash make build"},
		{"cd /a\ncd /b", "Bash cd /a"}, // 전부 준비 동작이면 첫 조각이라도 준다
		{"go build ./...", "Bash go build ./..."},
	} {
		line := `{"type":"assistant","cwd":"/tmp/p","sessionId":"S1","timestamp":"2026-08-07T01:00:00.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"a","name":"Bash","input":{"command":` +
			mustJSON(tc.cmd) + `}}]}}`
		turns, _, _, _ := parse(t, line+"\n")
		var got string
		for _, tn := range turns {
			if tn.Kind == transcript.KindTool {
				got = tn.Text
			}
		}
		if got != tc.want {
			t.Errorf("command %q → %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

// ★★★ **`sdk` 는 우리 자신이 넣은 프롬프트다.**
//
// 판별기는 호스트 CLI 를 띄워 돌리는데 그 세션도 자기 transcript 를 남긴다.
// 거르지 않으면 데몬이 **판별기의 프롬프트를 사람의 발화로 읽고** 다시 판정한다 —
// isMeta 가드가 막는 것과 같은 종류의 고리다.
//
// 실측(2026-08-31): user 레코드 6,772개 중 948개(14%)가 `promptSource: sdk` 였고
// 전부 priorcase 자신의 것이었다(판별기 지시문 25,372자짜리 624회 · 헬스체크 272회).
func TestSDKPromptsAreNotTurns(t *testing.T) {
	body := `{"type":"user","promptSource":"sdk","timestamp":"2026-08-07T01:00:00Z","message":{"content":"너는 개발 대화를 읽고 무엇을 남길지 정하는 판별기다. JSON 하나만 출력하라."}}
{"type":"user","promptSource":"typed","timestamp":"2026-08-07T01:00:01Z","message":{"content":"저장 엔진을 정하자"}}
{"type":"user","timestamp":"2026-08-07T01:00:02Z","message":{"content":"promptSource 가 없는 옛 레코드도 살아야 한다"}}
`
	turns, _, _, bad := parse(t, body)
	if bad != 0 {
		t.Fatalf("bad=%d", bad)
	}
	for _, tn := range turns {
		if strings.Contains(tn.Text, "판별기다") {
			t.Fatalf("판별기 프롬프트가 발화로 들어왔다: %.50q", tn.Text)
		}
	}
	if len(turns) != 2 {
		t.Fatalf("발화 %d개 — typed 와 무표시 둘이어야 한다: %v", len(turns), turns)
	}
}

// **`typed` 만 남기지 않는다.** promptSource 가 없는 레코드가 정상 발화의 다수라
// (호스트 판이 올라가며 붙은 필드다) 화이트리스트로 좁히면 옛 transcript 가
// 통째로 사라진다. 위 테스트의 셋째 줄이 그 계약이다.
func TestMissingPromptSourceStillCounts(t *testing.T) {
	turns, _, _, _ := parse(t, "{\"type\":\"user\",\"timestamp\":\"2026-08-07T01:00:00Z\",\"message\":{\"content\":\"옛 레코드\"}}\n")
	if len(turns) != 1 {
		t.Fatalf("promptSource 없는 레코드가 사라졌다: %v", turns)
	}
}

// ★★ **API 차단 알림은 어시스턴트 발화로 저장된다** — 종류로는 못 가른다.
// 그리고 그 글을 **인용한** 발화는 살아야 한다 (transcript/harness.go 의 앵커링).
func TestHarnessTextIsNotATurn(t *testing.T) {
	body := `{"type":"assistant","timestamp":"2026-08-07T01:00:00Z","message":{"content":[{"type":"text","text":"API Error: Opus 5's safeguards flagged this message (https://www.anthropic.com/legal/aup)."}]}}
{"type":"assistant","timestamp":"2026-08-07T01:00:01Z","message":{"content":[{"type":"text","text":"차단 원인을 보면 API Error: 로 시작하는 줄이 발화로 저장되고 있었다"}]}}
{"type":"user","timestamp":"2026-08-07T01:00:02Z","message":{"content":"<command-name>/effort</command-name>"}}
{"type":"user","timestamp":"2026-08-07T01:00:03Z","message":{"content":"이어서 진행해줘"}}
`
	turns, _, _, _ := parse(t, body)
	if len(turns) != 2 {
		t.Fatalf("발화 %d개 — 인용과 실제 프롬프트 둘만 남아야 한다: %v", len(turns), kinds(turns))
	}
	if !strings.Contains(turns[0].Text, "차단 원인을 보면") {
		t.Errorf("인용이 사라졌다: %q", turns[0].Text)
	}
	if turns[1].Text != "이어서 진행해줘" {
		t.Errorf("실제 프롬프트가 사라졌다: %q", turns[1].Text)
	}
}
