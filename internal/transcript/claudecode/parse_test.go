package claudecode

import (
	"encoding/json"
	"strings"
	"testing"

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
