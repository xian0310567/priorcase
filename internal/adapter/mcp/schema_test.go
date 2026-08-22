package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// ★★ 손으로 지은 스키마가 구조체와 어긋나면 **도구 호출이 통째로 실패한다.**
//
// SDK 는 InputSchema 가 채워져 있으면 리플렉션을 건너뛰고, 그 스키마로 입력을
// 검증한다. 필드 이름·타입·필수 여부가 json 태그와 하나라도 다르면 그 도구는
// 영영 못 쓴다 — 그런데 컴파일은 통과한다. 진짜 핸드셰이크로만 잡을 수 있다.
func TestToolSchemasMatchArgStructs(t *testing.T) {
	for _, lang := range []string{"ko", "en"} {
		t.Run(lang, func(t *testing.T) {
			c := testutil.VaultConfig(t)
			c.Lang = lang
			cs := connectWith(t, c)

			lt, err := cs.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			// 기대 목록이 **정본**이다. 도구를 늘리면서 여기를 안 고치면 아래
			// "모르는 도구" 가 잡는다 — 그래서 개수는 want 에서 센다. 숫자를 손으로
			// 박아 두면 도구를 하나 더하고 숫자만 올려 놓고 스키마 대응 검사는
			// 빠뜨리는 일이 생긴다(실제로 note 를 더할 때 그럴 뻔했다).
			want := map[string]struct {
				props    []string
				required []string
			}{
				"priorcase_recall":  {[]string{"query", "limit", "cross_project"}, []string{"query"}},
				"priorcase_note":    {[]string{"domain", "summary", "body", "tags", "date", "session_id"}, []string{"domain", "summary"}},
				"priorcase_capture": {[]string{"domain", "slug", "summary", "body", "tags", "related", "supersedes", "supersede_reason", "date", "session_id"}, []string{"domain", "slug", "summary"}},
				"priorcase_review":  {[]string{"stem", "outcome", "status", "summary", "retrospective", "supersedes", "supersede_reason"}, []string{"stem"}},
				"priorcase_pending": {[]string{"resolve"}, nil},
			}
			if len(lt.Tools) != len(want) {
				t.Fatalf("도구 %d개, want %d", len(lt.Tools), len(want))
			}
			seen := map[string]bool{}
			for _, tool := range lt.Tools {
				w, ok := want[tool.Name]
				if !ok {
					t.Errorf("모르는 도구: %s — 스키마 ↔ 구조체 대응 검사를 안 받고 있다", tool.Name)
					continue
				}
				seen[tool.Name] = true
				raw, err := json.Marshal(tool.InputSchema)
				if err != nil {
					t.Fatal(err)
				}
				var s struct {
					Type       string                     `json:"type"`
					Properties map[string]json.RawMessage `json:"properties"`
					Required   []string                   `json:"required"`
				}
				if err := json.Unmarshal(raw, &s); err != nil {
					t.Fatal(err)
				}
				if s.Type != "object" {
					t.Errorf("%s: type=%q", tool.Name, s.Type)
				}
				for _, k := range w.props {
					if _, ok := s.Properties[k]; !ok {
						t.Errorf("%s: 속성 %q 가 없다 — 그 인자를 영영 못 넘긴다", tool.Name, k)
					}
				}
				if len(s.Properties) != len(w.props) {
					t.Errorf("%s: 속성 %d개, want %d — 구조체와 어긋났다", tool.Name, len(s.Properties), len(w.props))
				}
				if strings.Join(s.Required, ",") != strings.Join(w.required, ",") {
					t.Errorf("%s: required=%v, want %v", tool.Name, s.Required, w.required)
				}
				if tool.Description == "" {
					t.Errorf("%s: 설명이 비었다", tool.Name)
				}
			}
			for name := range want {
				if !seen[name] {
					t.Errorf("%s 가 등록되지 않았다 — 기대 목록에만 있다", name)
				}
			}
		})
	}
}

// 스키마가 진짜로 통하는지는 **실제 호출**로만 안다. 도구 전부를 두 언어에서 부른다.
func TestEveryToolIsCallableInBothLanguages(t *testing.T) {
	for _, lang := range []string{"ko", "en"} {
		t.Run(lang, func(t *testing.T) {
			c := testutil.VaultConfig(t)
			c.Lang = lang
			cs := connectWith(t, c)
			ctx := context.Background()

			calls := []struct {
				name string
				args map[string]any
			}{
				{"priorcase_recall", map[string]any{"query": "저장", "limit": 2, "cross_project": true}},
				{"priorcase_note", map[string]any{
					"domain": "alpha", "summary": "스키마가 통하는지 본다 " + lang,
					"body": "#### 대안\n안 해 봤다.\n", "tags": []string{"t"},
					"date": "2026-08-09", "session_id": "S1",
				}},
				{"priorcase_capture", map[string]any{
					"domain": "alpha", "slug": "스키마검증-" + lang, "summary": "스키마가 통하는지 본다",
					"body": "## x\n", "tags": []string{"t"}, "related": []string{}, "date": "2026-08-09",
					"session_id": "S1", "supersede_reason": "",
				}},
				// review 는 인자를 **전부** 실어 보낸다. summary·supersede_reason 은 core 에
				// 필드만 있고 여기 인자가 없어서 에이전트가 영영 못 쓰던 것들이라,
				// 배선이 끊기면 바로 잡혀야 한다. 사유만 오는 번복은 status 도 함께
				// 바뀌어야 통과한다(capture.Review 가 그걸 요구한다).
				{"priorcase_review", map[string]any{
					"stem": "alpha-결정-스키마검증-" + lang + "-2026-08-09", "outcome": "bad",
					"status": "regretted", "summary": "실측으로 뒤집혔다 " + lang,
					"retrospective": "돌려봤더니 아니었다.", "supersede_reason": "실측 23건 중 0건이 기록됐다",
				}},
				{"priorcase_pending", map[string]any{}},
			}
			for _, call := range calls {
				res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: call.name, Arguments: call.args})
				if err != nil {
					t.Fatalf("%s 호출 실패 — 스키마가 구조체와 어긋났다: %v", call.name, err)
				}
				if res.IsError {
					t.Errorf("%s 가 에러를 냈다: %s", call.name, text(t, res))
				}
			}
		})
	}
}

// 모르는 인자를 넘겨도 스키마 때문에 호출이 죽으면 안 된다 — 옛 클라이언트가 있다.
func TestUnknownArgumentDoesNotBreakCall(t *testing.T) {
	c := testutil.VaultConfig(t)
	cs := connectWith(t, c)
	_, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
		Name: "priorcase_recall", Arguments: map[string]any{"query": "저장", "미래에생길인자": 1}})
	if err != nil {
		t.Errorf("모르는 인자 하나에 호출이 죽었다: %v", err)
	}
}

// 필수 인자를 빼면 거부돼야 한다 — 스키마가 실제로 검증에 쓰이는지 확인한다.
func TestRequiredArgumentIsEnforced(t *testing.T) {
	c := testutil.VaultConfig(t)
	cs := connectWith(t, c)
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
		Name: "priorcase_capture", Arguments: map[string]any{"slug": "x", "summary": "y"}})
	if err == nil && (res == nil || !res.IsError) {
		t.Error("domain 없이 통과했다 — 스키마의 required 가 안 먹는다")
	}
}

var _ = config.Config{}
