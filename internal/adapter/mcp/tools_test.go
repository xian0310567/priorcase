package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/testutil"
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
	ctx := context.Background()
	srvT, cliT := sdk.NewInMemoryTransports()

	srv := New(c, store.NewLayout(c), "test")
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

func TestListToolsExposesThree(t *testing.T) {
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
	for _, want := range []string{"casebook_recall", "casebook_capture", "casebook_review"} {
		if !got[want] {
			t.Errorf("%s 가 목록에 없다 (있는 것: %v)", want, got)
		}
	}
	if len(res.Tools) != 3 {
		t.Errorf("도구 %d개, 3개여야 한다", len(res.Tools))
	}
}

// initialize 응답에 행동 계약이 실리는지 — MCP 에서 세션 진입 컨텍스트를 넣을 수
// 있는 유일한 자리다 (스펙 §8·§9).
func TestInitializeCarriesInstructions(t *testing.T) {
	cs, _, _ := connect(t)
	got := cs.InitializeResult().Instructions
	if !strings.Contains(got, "casebook_recall") {
		t.Errorf("initialize 에 행동 계약이 없다:\n%s", got)
	}
}

func TestRecallFindsDecision(t *testing.T) {
	cs, _, _ := connect(t)
	out := text(t, call(t, cs, "casebook_recall", map[string]any{"query": "저장 엔진"}))
	if !strings.Contains(out, "저장 엔진을 임베디드 DB 로 고른다") {
		t.Errorf("회수 결과에 해당 결정이 없다:\n%s", out)
	}
}

func TestRecallWithNoMatchSaysSo(t *testing.T) {
	cs, _, _ := connect(t)
	out := text(t, call(t, cs, "casebook_recall", map[string]any{"query": "zzzz존재하지않는주제zzzz"}))
	if strings.TrimSpace(out) == "" {
		t.Error("결과가 없을 때 빈 응답을 냈다 — 모델이 도구가 고장난 것으로 읽는다")
	}
}

// 읽지 못한 노트는 **응답 본문**에 실려야 한다. stderr 로 보내면 호스트 로그로 가고
// 에이전트 컨텍스트에는 안 들어간다 — MCP 경로에서 침묵이 부활하는 지점이다.
func TestRecallReportsSkippedInResponse(t *testing.T) {
	c := testutil.VaultConfig(t)
	broken := filepath.Join(c.Vault, "alpha", "decisions", "alpha-결정-깨짐-2026-01-01.md")
	if err := os.WriteFile(broken, []byte("---\ntitle: 구 스키마\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := connectWith(t, c)

	out := text(t, call(t, cs, "casebook_recall", map[string]any{"query": "저장 엔진"}))
	if !strings.Contains(out, "깨짐") {
		t.Errorf("건너뛴 노트가 응답에 없다 — 에이전트가 알 방법이 없다:\n%s", out)
	}
}

func TestCaptureWritesNoteAndPiggybacks(t *testing.T) {
	c := testutil.VaultConfig(t)
	cs := connectWith(t, c)

	out := text(t, call(t, cs, "casebook_capture", map[string]any{
		"domain":  "alpha",
		"slug":    "캐시계층",
		"summary": "저장 엔진 앞에 캐시 계층을 둔다",
		"date":    "2026-08-07",
		"body":    "## 결정\n캐시를 둔다.\n",
	}))

	path := filepath.Join(c.Vault, "alpha", "decisions", "alpha-결정-캐시계층-2026-08-07.md")
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
	r := call(t, cs, "casebook_capture", map[string]any{
		"domain": "존재하지않는도메인", "slug": "x", "summary": "y", "date": "2026-08-07",
	})
	if !r.IsError {
		t.Errorf("알 수 없는 도메인을 받아들였다:\n%s", text(t, r))
	}
}

func TestReviewUpdatesOutcome(t *testing.T) {
	c := testutil.VaultConfig(t)
	cs := connectWith(t, c)

	out := text(t, call(t, cs, "casebook_review", map[string]any{
		"stem":          "alpha-결정-저장엔진-2026-08-01",
		"outcome":       "good",
		"retrospective": "1년 써 보니 옳았다.",
	}))

	path := filepath.Join(c.Vault, "alpha", "decisions", "alpha-결정-저장엔진-2026-08-01.md")
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
