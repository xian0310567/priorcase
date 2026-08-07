package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/xian0310567/casebook/internal/core/capture"
	"github.com/xian0310567/casebook/internal/core/search"
	"github.com/xian0310567/casebook/internal/daemon"
)

// 도구 출력은 전부 텍스트다. 구조화 출력(Out 타입)을 쓰지 않는 이유: 이 도구들의
// 산출물은 모델이 읽고 판단할 산문이고, 스키마를 붙이면 편승 블록·경고처럼
// "곁다리로 따라오는 것"을 담을 자리가 애매해진다.
type noOutput = any

func (s *server) addTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name: "casebook_recall",
		Description: "이 워크스페이스의 과거 결정을 찾는다. " +
			"새 작업이나 주제로 넘어갈 때 먼저 부른다 — 이미 뒤집힌 결정을 다시 제안하지 않기 위해서다.",
	}, s.recall)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "casebook_capture",
		Description: "결정을 기록한다. 되돌리기 어려운 선택(아키텍처·스키마·외부 서비스·가격), " +
			"대안을 검토해 하나를 고른 경우, 실측으로 통념이 깨진 경우가 대상이다. " +
			"자잘한 것까지 남기면 회수가 어려워진다.",
	}, s.capture)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "casebook_review",
		Description: "기존 결정의 결과(outcome)·상태·회고를 갱신하거나, 그 결정을 뒤집는다. " +
			"뒤집힌 결정이 그대로 남아 있으면 회수가 오염된다.",
	}, s.review)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "casebook_pending",
		Description: "데몬(cb watch)이 표시한 미확인 구간을 본다. 이전 세션에서 결정을 내리고도 " +
			"기록하지 않고 지나간 자리다. 확인 후 실제 결정이면 casebook_capture 로 남기고, " +
			"아니면 resolve 로 지운다 — 쌓아 두면 다음 세션에도 그대로 뜬다.",
	}, s.pending)
}

// ── recall ──────────────────────────────────────────────────────────────

type recallArgs struct {
	Query string `json:"query" jsonschema:"찾을 주제나 키워드"`
	Limit int    `json:"limit,omitempty" jsonschema:"최대 결과 수 (기본 3)"`
	// 포인터인 이유: 기본값이 true 라서 bool 로 받으면 '지정 안 함' 과 'false' 가
	// 구별되지 않는다.
	CrossProject *bool `json:"cross_project,omitempty" jsonschema:"현재 프로젝트 밖의 결정도 찾는다 (기본 true)"`
}

func (s *server) recall(ctx context.Context, req *sdk.CallToolRequest, a recallArgs) (*sdk.CallToolResult, noOutput, error) {
	if strings.TrimSpace(a.Query) == "" {
		return nil, nil, fmt.Errorf("query 가 비어 있다")
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 3
	}
	crossProject := true
	if a.CrossProject != nil {
		crossProject = *a.CrossProject
	}

	cwd, _ := os.Getwd()
	hits, skipped, err := search.Recall(s.l, s.c, a.Query, search.Options{
		Cwd: cwd, CrossProject: crossProject, Limit: limit, MinScore: 1,
	})
	if err != nil {
		return nil, nil, err
	}

	body := search.RenderInject(s.l, hits)
	if body == "" {
		// 빈 응답을 내면 모델이 도구가 고장난 것으로 읽고 다시 부르거나 포기한다.
		// "찾았는데 없다" 를 명시적으로 말해야 다음 행동이 갈린다.
		body = fmt.Sprintf("%q 와 관련된 과거 결정을 찾지 못했다.\n", a.Query)
	}
	return textResult(body + renderSkipped(s.l, skipped)), nil, nil
}

// ── capture ─────────────────────────────────────────────────────────────

type captureArgs struct {
	Domain     string   `json:"domain" jsonschema:"결정이 속한 프로젝트 도메인 접두어"`
	Slug       string   `json:"slug" jsonschema:"파일명에 들어갈 짧은 주제어"`
	Summary    string   `json:"summary" jsonschema:"한 줄 요약 — 회수 때 이것만 주입되므로 그 자체로 읽혀야 한다"`
	Body       string   `json:"body,omitempty" jsonschema:"본문 마크다운 (## 결정 / ## 근거 / ## 고려한 대안 / ## 예상 리스크 / ## 회고)"`
	Tags       []string `json:"tags,omitempty" jsonschema:"프로젝트를 넘어 쓰일 교훈이면 lesson 을 넣는다"`
	Related    []string `json:"related,omitempty" jsonschema:"관련 문서의 위키링크 또는 stem"`
	Supersedes string   `json:"supersedes,omitempty" jsonschema:"이 결정이 뒤집는 기존 결정의 stem"`
	Date       string   `json:"date,omitempty" jsonschema:"YYYY-MM-DD (기본: 오늘)"`
	SessionID  string   `json:"session_id,omitempty" jsonschema:"이 결정이 나온 대화의 세션 id. 세션 진입 컨텍스트에 적혀 있으면 그대로 넘긴다"`
}

func (s *server) capture(ctx context.Context, req *sdk.CallToolRequest, a captureArgs) (*sdk.CallToolResult, noOutput, error) {
	res, err := capture.Do(s.l, s.c, capture.Request{
		Domain:        a.Domain,
		Slug:          a.Slug,
		Summary:       a.Summary,
		Date:          a.Date,
		Supersedes:    a.Supersedes,
		SourceSession: a.SessionID,
		Tags:          a.Tags,
		Related:       a.Related,
		Body:          []byte(a.Body),
	})
	if err != nil {
		return nil, nil, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "기록됨: %s\n", s.l.RelPath(res.Path))
	// 편승 — capture 시점이 곧 결정 시점이라 과거 결정이 가장 정확하게 닿는 순간이다.
	if inject := search.RenderInject(s.l, res.Related); inject != "" {
		b.WriteString("\n" + inject)
	}
	// "관련 결정이 없다" 와 "찾아보지 못했다" 는 다른 사실이다.
	if res.RelatedErr != nil {
		fmt.Fprintf(&b, "\n(관련 결정을 찾아보지 못했다: %v)\n", res.RelatedErr)
	}
	b.WriteString(renderSkipped(s.l, res.Skipped))
	return textResult(b.String()), nil, nil
}

// ── review ──────────────────────────────────────────────────────────────

type reviewArgs struct {
	Stem          string `json:"stem" jsonschema:"갱신할 결정의 파일명 (확장자 제외)"`
	Outcome       string `json:"outcome,omitempty" jsonschema:"pending | good | bad"`
	Status        string `json:"status,omitempty" jsonschema:"active | superseded | regretted"`
	Retrospective string `json:"retrospective,omitempty" jsonschema:"## 회고 에 붙일 내용"`
	Supersedes    string `json:"supersedes,omitempty" jsonschema:"이 결정이 뒤집는 결정의 stem"`
}

func (s *server) review(ctx context.Context, req *sdk.CallToolRequest, a reviewArgs) (*sdk.CallToolResult, noOutput, error) {
	skipped, err := capture.Review(s.l, capture.ReviewRequest{
		Stem:          a.Stem,
		Outcome:       a.Outcome,
		Status:        a.Status,
		Retrospective: a.Retrospective,
		Supersedes:    a.Supersedes,
	})
	if err != nil {
		return nil, nil, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "갱신됨: %s\n", a.Stem)
	// capture 와 달리 core 가 편승을 주지 않으므로 어댑터가 직접 만든다. 질의어는
	// 갱신한 결정의 요약이다 — 같은 주제의 이웃 결정이 걸린다.
	b.WriteString(s.piggyback(a.Stem))
	b.WriteString(renderSkipped(s.l, skipped))
	return textResult(b.String()), nil, nil
}

// piggyback 은 stem 이 가리키는 결정을 질의어 삼아 이웃 결정을 찾는다.
// 실패해도 본 작업은 이미 끝났으므로 사실만 적고 넘어간다.
func (s *server) piggyback(stem string) string {
	path, err := s.l.ResolveStem(stem)
	if err != nil {
		return ""
	}
	note, err := s.l.Read(path)
	if err != nil {
		return ""
	}
	cwd, _ := os.Getwd()
	hits, _, err := search.Recall(s.l, s.c, note.Meta.Summary, search.Options{
		Cwd: cwd, CrossProject: true, Limit: 3, MinScore: 1,
	})
	if err != nil {
		return fmt.Sprintf("\n(관련 결정을 찾아보지 못했다: %v)\n", err)
	}
	// 방금 갱신한 노트 자신은 뺀다.
	kept := hits[:0]
	for _, h := range hits {
		if h.Note.Stem != stem {
			kept = append(kept, h)
		}
	}
	if inject := search.RenderInject(s.l, kept); inject != "" {
		return "\n" + inject
	}
	return ""
}

// ── 공통 ────────────────────────────────────────────────────────────────

func textResult(s string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: s}}}
}

// **stdio MCP 에는 세션 id 가 없다.** 예전에는 req.Session.ID() 를 source_session 에
// 넣었는데 실측으로 언제나 빈 문자열이었다 — SDK 의 세션 id 는 HTTP 전송에서만 붙는다.
// 죽은 배선이라 걷어내고, 대신 에이전트가 인자로 넘기게 했다. 세션 진입 컨텍스트
// (Claude Code 훅 또는 이 서버의 instructions)가 그 값을 알려 준다.

// ── pending ─────────────────────────────────────────────────────────────

type pendingToolArgs struct {
	Resolve string `json:"resolve,omitempty" jsonschema:"지울 구간의 id. 비우면 목록만 본다"`
}

func (s *server) pending(ctx context.Context, req *sdk.CallToolRequest, a pendingToolArgs) (*sdk.CallToolResult, noOutput, error) {
	if s.stateDir == "" {
		return nil, nil, fmt.Errorf("데몬 연동이 꺼져 있다 — 상태 디렉토리를 정하지 못했다")
	}
	if a.Resolve != "" {
		if err := daemon.ResolvePending(s.stateDir, a.Resolve); err != nil {
			return nil, nil, err
		}
		return textResult(fmt.Sprintf("지웠다: %s\n", a.Resolve)), nil, nil
	}

	items, err := daemon.ReadPending(s.stateDir)
	if err != nil {
		return nil, nil, err
	}
	if len(items) == 0 {
		// "없다" 를 명시한다. 빈 응답은 도구가 고장난 것으로 읽힌다.
		return textResult("미확인 구간이 없다. 데몬이 표시한 것이 없거나 전부 확인됐다.\n"), nil, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "미확인 구간 %d건:\n", len(items))
	for _, p := range items {
		domain := p.Domain
		if domain == "" {
			domain = "(도메인 미상)"
		}
		fmt.Fprintf(&b, "\n- id: %s\n  때: %s · 도메인: %s · 발화 %d · 시그널 %s\n  대화: %s (바이트 %d~%d)\n",
			p.ID(), p.At.Format("2006-01-02 15:04"), domain, p.Turns,
			strings.Join(p.Signals, "·"), p.Path, p.From, p.To)
	}
	b.WriteString("\n각 구간의 대화를 확인하고, 실제 결정이면 casebook_capture 로 남긴 뒤 " +
		"casebook_pending(resolve: <id>) 로 지운다.\n")
	return textResult(b.String()), nil, nil
}
