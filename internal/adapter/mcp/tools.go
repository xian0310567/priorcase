package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/xian0310567/priorcase/internal/core/capture"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/daemon"
)

// 도구 출력은 전부 텍스트다. 구조화 출력(Out 타입)을 쓰지 않는 이유: 이 도구들의
// 산출물은 모델이 읽고 판단할 산문이고, 스키마를 붙이면 편승 블록·경고처럼
// "곁다리로 따라오는 것"을 담을 자리가 애매해진다.
type noOutput = any

func (s *server) addTools(srv *sdk.Server) {
	// **도구 설명과 인자 스키마는 모델이 읽는 글이다.** 대화 언어와 어긋나면 도구
	// 선택과 인자 구성이 같이 나빠지므로 `lang` 을 따라간다. 스키마를 손으로 짓는
	// 이유는 schema.go 주석에 있다 (구조체 태그는 컴파일 타임 상수라 안 된다).
	lang := s.l.Lang()

	sdk.AddTool(srv, &sdk.Tool{
		Name: "priorcase_recall",
		Description: lang.T(
			"이 워크스페이스의 과거 결정을 찾는다. "+
				"새 작업이나 주제로 넘어갈 때 먼저 부른다 — 이미 뒤집힌 결정을 다시 제안하지 않기 위해서다.",
			"Find past decisions in this workspace. "+
				"Call this first when moving to a new task or topic, so you don't propose something that was already overturned."),
		InputSchema: recallSchema(lang),
	}, s.recall)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "priorcase_capture",
		Description: lang.T(
			"결정을 기록한다. 되돌리기 어려운 선택(아키텍처·스키마·외부 서비스·가격), "+
				"대안을 검토해 하나를 고른 경우, 실측으로 통념이 깨진 경우가 대상이다. "+
				"자잘한 것까지 남기면 회수가 어려워진다.",
			"Record a decision. Use it for choices that are hard to reverse (architecture, schema, "+
				"external services, pricing), for picking one option after weighing alternatives, and "+
				"when a measurement overturned an assumption. Recording trivia makes recall worse."),
		InputSchema: captureSchema(lang),
	}, s.capture)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "priorcase_review",
		Description: lang.T(
			"기존 결정의 결과(outcome)·상태·회고를 갱신하거나, 그 결정을 뒤집는다. "+
				"뒤집힌 결정이 그대로 남아 있으면 회수가 오염된다.",
			"Update an existing decision's outcome, status, or retrospective — or overturn it. "+
				"An overturned decision left as-is pollutes recall."),
		InputSchema: reviewSchema(lang),
	}, s.review)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "priorcase_pending",
		Description: lang.T(
			"데몬(prior watch)이 표시한 미확인 구간을 본다. 이전 세션에서 결정을 내리고도 "+
				"기록하지 않고 지나간 자리다. 확인 후 실제 결정이면 priorcase_capture 로 남기고, "+
				"아니면 resolve 로 지운다 — 쌓아 두면 다음 세션에도 그대로 뜬다.",
			"List unreviewed conversation segments flagged by the safety net. These are places where a "+
				"decision was likely made in an earlier session but never recorded. If it was a real "+
				"decision, record it with priorcase_capture; otherwise clear it with resolve — "+
				"left alone it reappears every session."),
		InputSchema: pendingSchema(lang),
	}, s.pending)
}

// ── recall ──────────────────────────────────────────────────────────────

type recallArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
	// 포인터인 이유: 기본값이 true 라서 bool 로 받으면 '지정 안 함' 과 'false' 가
	// 구별되지 않는다.
	CrossProject *bool `json:"cross_project,omitempty"`
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
	Domain     string   `json:"domain"`
	Slug       string   `json:"slug"`
	Summary    string   `json:"summary"`
	Body       string   `json:"body,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Related    []string `json:"related,omitempty"`
	Supersedes string   `json:"supersedes,omitempty"`
	Date       string   `json:"date,omitempty"`
	SessionID  string   `json:"session_id,omitempty"`
	// Author 는 이 결정을 내린 사람이다. 비면 설정·git 신원에서 정한다.
	Author     string   `json:"author,omitempty"`
}

func (s *server) capture(ctx context.Context, req *sdk.CallToolRequest, a captureArgs) (*sdk.CallToolResult, noOutput, error) {
	author := a.Author
	if author == "" {
		wd, _ := os.Getwd()
		author = s.c.AuthorFor(wd)
	}
	res, err := capture.Do(s.l, s.c, capture.Request{
		Author:        author,
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
	Stem          string `json:"stem"`
	Outcome       string `json:"outcome,omitempty"`
	Status        string `json:"status,omitempty"`
	Retrospective string `json:"retrospective,omitempty"`
	Supersedes    string `json:"supersedes,omitempty"`
}

func (s *server) review(ctx context.Context, req *sdk.CallToolRequest, a reviewArgs) (*sdk.CallToolResult, noOutput, error) {
	rr, err := capture.Review(s.l, capture.ReviewRequest{
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
	b.WriteString(renderSkipped(s.l, rr.Skipped))
	b.WriteString(renderPreserved(s.l, rr.IndexPreserved))
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
	Resolve string `json:"resolve,omitempty"`
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
			p.ID(), p.When(), domain, p.Turns,
			strings.Join(p.Signals, "·"), p.Path, p.From, p.To)
	}
	b.WriteString("\n각 구간의 대화를 확인하고, 실제 결정이면 priorcase_capture 로 남긴 뒤 " +
		"priorcase_pending(resolve: <id>) 로 지운다.\n")
	return textResult(b.String()), nil, nil
}
