package mcp

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/daemon"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// connect 는 픽스처 볼트를 얹은 서버에 in-memory 클라이언트를 붙인다.
// 실제 전송(stdio)과 같은 코드 경로를 타므로 도구 등록·스키마·직렬화가 다 검증된다.
func connect(t *testing.T) (*sdk.ClientSession, *config.Config, *store.Layout) {
	t.Helper()
	c := testutil.VaultConfig(t)
	return connectWith(t, c), c, store.NewLayout(c)
}

func connectWith(t *testing.T, c *config.Config) *sdk.ClientSession {
	t.Helper()
	return connectWithState(t, c, t.TempDir())
}

func connectWithState(t *testing.T, c *config.Config, stateDir string) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	srvT, cliT := sdk.NewInMemoryTransports()

	srv := New(c, store.NewLayout(c), "test", stateDir)
	if _, err := srv.Connect(ctx, srvT, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "v0"}, nil).
		Connect(ctx, cliT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// text 는 도구 응답의 텍스트 본문을 이어 붙인다.
func text(t *testing.T, r *sdk.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func call(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	r, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s 호출이 프로토콜 수준에서 실패했다: %v", name, err)
	}
	return r
}

func TestListToolsExposesAll(t *testing.T) {
	cs, _, _ := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("%s 에 설명이 없다 — 모델이 언제 부를지 판단할 근거가 없다", tool.Name)
		}
	}
	// 목록이 정본이다 — 개수는 여기서 센다. 숫자를 따로 박아 두면 도구를 늘릴 때
	// 숫자만 고치고 이름은 안 더하는 반쪽 수정이 통과한다.
	want := []string{"priorcase_recall", "priorcase_note", "priorcase_capture", "priorcase_review", "priorcase_pending"}
	for _, w := range want {
		if !got[w] {
			t.Errorf("%s 가 목록에 없다 (있는 것: %v)", w, got)
		}
	}
	if len(res.Tools) != len(want) {
		t.Errorf("도구 %d개, %d개여야 한다 (있는 것: %v)", len(res.Tools), len(want), got)
	}
}

// **등록 순서는 클라이언트에 도달하지 않는다.** note 를 추가할 때 "자주 불릴 것을
// 앞에 등록하면 모델이 그걸 기본으로 삼는다" 로 배치했는데, 실제 목록은 이름 순으로
// 나왔다 — go-sdk 가 도구를 map 에 담고 sortedKeys 로 낸다(features.go).
//
// 이 사실을 못 박아 두는 이유: 순서를 지렛대로 착각하면 도구 하나를 옮겨 놓고
// "이제 더 불리겠지" 하고 끝낸다. 실제 지렛대는 설명 문구와 instructions 뿐이다.
func TestListToolsOrderIsNameSortedNotRegistration(t *testing.T) {
	cs, _, _ := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("도구 목록이 이름 순이 아니다: %v — SDK 동작이 바뀌었으면 "+
			"tools.go 의 등록 순서 주석도 다시 봐야 한다", names)
	}
}

// note 는 작업 로그에 실제로 쓰여야 한다. 그리고 **결정 노트 폴더에는 아무것도
// 안 생겨야 한다** — 두 계층이 섞이면 등급을 나눈 의미가 사라진다.
func TestNoteWritesToWorklogNotDecisions(t *testing.T) {
	c := testutil.VaultConfig(t)
	cs := connectWith(t, c)

	out := text(t, call(t, cs, "priorcase_note", map[string]any{
		"domain":  "alpha",
		"summary": "인덱스 자료구조 셋을 재 봤다",
		"body":    "#### 측정\nB-tree 가 3배 빨랐다.\n",
		"date":    "2026-08-09",
	}))
	if !strings.Contains(out, "작업 로그") {
		t.Errorf("응답이 어디에 남겼는지 말하지 않는다:\n%s", out)
	}

	wl := filepath.Join(c.DefaultVaultPath(), "alpha", "99-alpha-작업-로그.md")
	body, err := os.ReadFile(wl)
	if err != nil {
		t.Fatalf("작업 로그가 생기지 않았다 (%s): %v", wl, err)
	}
	if !strings.Contains(string(body), "인덱스 자료구조 셋을 재 봤다") {
		t.Errorf("항목이 작업 로그에 없다:\n%s", body)
	}
	if !strings.Contains(string(body), "## 2026-08-09") {
		t.Errorf("날짜 헤딩이 없다 — rollup 이 그 주를 통째로 못 본다:\n%s", body)
	}

	ents, err := os.ReadDir(filepath.Join(c.DefaultVaultPath(), "alpha", "decisions"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.Contains(e.Name(), "인덱스") {
			t.Errorf("note 가 결정 노트를 만들었다: %s", e.Name())
		}
	}
}

// **회수는 작업 로그도 찾아야 한다 — 물어봤을 때만.** 등급을 나눈 이유가 자동
// 주입을 지키는 것이라, 물어본 자리에서까지 안 나오면 쌓을 이유가 없어진다.
func TestRecallFindsWorklogEntries(t *testing.T) {
	c := testutil.VaultConfig(t)
	cs := connectWith(t, c)

	call(t, cs, "priorcase_note", map[string]any{
		"domain":  "alpha",
		"summary": "졸업요건 판정을 규칙엔진으로 뺄지 검토중",
		"body":    "#### 대안\n하드코딩은 학과가 늘 때마다 배포가 필요해서 기각.\n",
		"date":    "2026-08-09",
	})

	out := text(t, call(t, cs, "priorcase_recall", map[string]any{"query": "졸업요건 규칙엔진"}))
	if !strings.Contains(out, "졸업요건 판정을 규칙엔진으로 뺄지 검토중") {
		t.Errorf("회수가 작업 로그 항목을 못 찾았다:\n%s", out)
	}
	if !strings.Contains(out, "작업 로그") {
		t.Errorf("작업 로그 절 제목이 없다 — 등급이 안 읽힌다:\n%s", out)
	}
}

// initialize 응답에 행동 계약이 실리는지 — MCP 에서 세션 진입 컨텍스트를 넣을 수
// 있는 유일한 자리다 (스펙 §8·§9).
func TestInitializeCarriesInstructions(t *testing.T) {
	cs, _, _ := connect(t)
	got := cs.InitializeResult().Instructions
	if !strings.Contains(got, "priorcase_recall") {
		t.Errorf("initialize 에 행동 계약이 없다:\n%s", got)
	}
}

func TestRecallFindsDecision(t *testing.T) {
	cs, _, _ := connect(t)
	out := text(t, call(t, cs, "priorcase_recall", map[string]any{"query": "저장 엔진"}))
	if !strings.Contains(out, "저장 엔진을 임베디드 DB 로 고른다") {
		t.Errorf("회수 결과에 해당 결정이 없다:\n%s", out)
	}
}

func TestRecallWithNoMatchSaysSo(t *testing.T) {
	cs, _, _ := connect(t)
	out := text(t, call(t, cs, "priorcase_recall", map[string]any{"query": "zzzz존재하지않는주제zzzz"}))
	if strings.TrimSpace(out) == "" {
		t.Error("결과가 없을 때 빈 응답을 냈다 — 모델이 도구가 고장난 것으로 읽는다")
	}
}

// 읽지 못한 노트는 **응답 본문**에 실려야 한다. stderr 로 보내면 호스트 로그로 가고
// 에이전트 컨텍스트에는 안 들어간다 — MCP 경로에서 침묵이 부활하는 지점이다.
func TestRecallReportsSkippedInResponse(t *testing.T) {
	c := testutil.VaultConfig(t)
	broken := filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-깨짐-2026-01-01.md")
	if err := os.WriteFile(broken, []byte("---\ntitle: 구 스키마\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := connectWith(t, c)

	out := text(t, call(t, cs, "priorcase_recall", map[string]any{"query": "저장 엔진"}))
	if !strings.Contains(out, "깨짐") {
		t.Errorf("건너뛴 노트가 응답에 없다 — 에이전트가 알 방법이 없다:\n%s", out)
	}
}

func TestCaptureWritesNoteAndPiggybacks(t *testing.T) {
	c := testutil.VaultConfig(t)
	cs := connectWith(t, c)

	out := text(t, call(t, cs, "priorcase_capture", map[string]any{
		"domain":  "alpha",
		"slug":    "캐시계층",
		"summary": "저장 엔진 앞에 캐시 계층을 둔다",
		"date":    "2026-08-07",
		"body":    "## 결정\n캐시를 둔다.\n",
	}))

	path := filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-캐시계층-2026-08-07.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("노트가 디스크에 없다: %v", err)
	}
	// 편승 — capture 시점이 곧 결정 시점이라 과거 결정이 딸려 나와야 한다 (스펙 §8).
	if !strings.Contains(out, "저장 엔진을 임베디드 DB 로 고른다") {
		t.Errorf("응답에 관련 과거 결정이 붙지 않았다:\n%s", out)
	}
}

// 스키마 검증을 통과 못 하면 거부한다 (스펙 §7.1). 설정에 없는 도메인이 그 예다.
func TestCaptureRejectsUnknownDomain(t *testing.T) {
	cs, _, _ := connect(t)
	r := call(t, cs, "priorcase_capture", map[string]any{
		"domain": "존재하지않는도메인", "slug": "x", "summary": "y", "date": "2026-08-07",
	})
	if !r.IsError {
		t.Errorf("알 수 없는 도메인을 받아들였다:\n%s", text(t, r))
	}
}

func TestReviewUpdatesOutcome(t *testing.T) {
	c := testutil.VaultConfig(t)
	cs := connectWith(t, c)

	out := text(t, call(t, cs, "priorcase_review", map[string]any{
		"stem":          "alpha-결정-저장엔진-2026-08-01",
		"outcome":       "good",
		"retrospective": "1년 써 보니 옳았다.",
	}))

	path := filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", "alpha-결정-저장엔진-2026-08-01.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "outcome: good") {
		t.Errorf("outcome 이 디스크에 반영되지 않았다:\n%s", got)
	}
	if !strings.Contains(string(got), "1년 써 보니 옳았다") {
		t.Errorf("회고가 본문에 들어가지 않았다:\n%s", got)
	}
	if strings.TrimSpace(out) == "" {
		t.Error("review 응답이 비었다")
	}
}

// ── priorcase_pending ────────────────────────────────────────────────────

// pending 을 심고 목록·해소가 실제로 도는지. 데몬과 MCP 는 다른 프로세스이고
// 상태 파일 하나로만 만난다 — 그 접점이 도는지가 이 테스트의 요지다.
func seedPending(t *testing.T, dir string) daemon.Pending {
	t.Helper()
	s := daemon.NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	p := daemon.Pending{
		SessionID: "S9", Path: "/t/a.jsonl", Cwd: "/tmp/proj/alpha", Domain: "alpha",
		Turns: 11, Signals: []string{"결정"}, From: 0, To: 900, At: time.Now().UTC(),
	}
	if err := s.AddPending(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPendingToolListsAndResolves(t *testing.T) {
	c := testutil.VaultConfig(t)
	stateDir := t.TempDir()
	p := seedPending(t, stateDir)
	cs := connectWithState(t, c, stateDir)

	out := text(t, call(t, cs, "priorcase_pending", map[string]any{}))
	if !strings.Contains(out, p.ID()) {
		t.Fatalf("목록에 id 가 없다 — 지울 방법이 없다:\n%s", out)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("도메인이 없다:\n%s", out)
	}

	if r := call(t, cs, "priorcase_pending", map[string]any{"resolve": p.ID()}); r.IsError {
		t.Fatalf("해소 실패: %s", text(t, r))
	}
	left, err := daemon.ReadPending(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("해소 후 %d건 남았다", len(left))
	}
}

// **면제된 구간도 `priorcase_pending` 에는 나온다.** 사람이 직접 물었기 때문이다 —
// 물어본 사람에게 감추는 것은 면제가 아니라 은폐다. 거르는 자리는 instructions 뿐이다
// (짝: TestInstructionsHideQuietPending).
func TestPendingToolShowsQuietItems(t *testing.T) {
	c := testutil.VaultConfig(t)
	stateDir := t.TempDir()
	s := daemon.NewStore(stateDir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	p := daemon.Pending{
		SessionID: "S9", Path: "/t/조용한.jsonl", Cwd: "/tmp/proj/alpha", Domain: "alpha",
		Turns: 11, Signals: []string{"결정"}, From: 0, To: 900, Quiet: true, At: time.Now().UTC(),
	}
	if err := s.AddPending(p); err != nil {
		t.Fatal(err)
	}
	cs := connectWithState(t, c, stateDir)

	out := text(t, call(t, cs, "priorcase_pending", map[string]any{}))
	if !strings.Contains(out, p.ID()) {
		t.Errorf("면제된 구간이 도구 목록에서 감춰졌다 — 지울 방법이 없어진다:\n%s", out)
	}
}

// 없을 때 빈 응답을 내면 모델이 도구가 고장난 것으로 읽는다.
func TestPendingToolSaysWhenEmpty(t *testing.T) {
	cs := connectWithState(t, testutil.VaultConfig(t), t.TempDir())
	out := text(t, call(t, cs, "priorcase_pending", map[string]any{}))
	if strings.TrimSpace(out) == "" {
		t.Error("빈 응답을 냈다")
	}
	if !strings.Contains(out, "없다") {
		t.Errorf("없다는 사실을 말하지 않는다:\n%s", out)
	}
}

// instructions 는 initialize 때 한 번 만들어진다 — 데몬이 심어 둔 pending 이
// 거기 실려야 세션 진입에서 바로 보인다.
func TestInitializeCarriesPendingFromDaemon(t *testing.T) {
	stateDir := t.TempDir()
	seedPending(t, stateDir)
	cs := connectWithState(t, testutil.VaultConfig(t), stateDir)

	ins := cs.InitializeResult().Instructions
	if !strings.Contains(ins, "미확인 구간이 1건") {
		t.Errorf("세션 진입에 미확인 구간이 안 실렸다:\n%s", ins)
	}
}
