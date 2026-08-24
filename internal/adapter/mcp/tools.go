package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/xian0310567/priorcase/internal/core/capture"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/worklog"
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

	// note 를 capture 앞에 둔 것은 **읽는 사람을 위한 순서일 뿐이다.**
	//
	// 처음에는 "자주 불려야 하는 쪽을 앞에 등록하면 모델이 그걸 기본으로 삼는다" 는
	// 계산이었다. 실측으로 깨졌다 — go-sdk 는 도구를 map 에 담고 `tools/list` 를
	// **이름 순으로** 낸다(features.go 의 sortedKeys = slices.Sorted). 등록 순서는
	// 클라이언트에 아예 도달하지 않고, 실제 순서는 capture → note → pending → recall 다.
	//
	// 그러니 note 를 실제로 불리게 만드는 지렛대는 순서가 아니라 **설명 문구와
	// instructions** 다. 여기 그 둘을 다 걸었다: 원장 기각 23건 중 11건이 "아직
	// 미결정" 이었고, 그때 갈 곳이 없어서 전부 사라졌다.
	sdk.AddTool(srv, &sdk.Tool{
		Name: "priorcase_note",
		Description: lang.T(
			"확정 전의 것을 작업 로그에 남긴다. 검토한 대안과 각각을 왜 기각했는지, "+
				"측정값과 그 방법, 걸린 제약, 아직 못 정한 것과 그것이 풀리는 조건이 대상이다. "+
				"회수에 자동 주입되지 않으므로 자주 불러도 아무것도 나빠지지 않는다 — "+
				"확정되기 전이라고 미루지 마라. 확정된 결정은 priorcase_capture 로 올린다.",
			"Record something that has not settled yet into the worklog: alternatives you weighed and why "+
				"each was ruled out, measurements and how you took them, constraints you hit, what is still "+
				"open and what would settle it. Recall never auto-injects the worklog, so calling this often "+
				"costs nothing — do not defer just because nothing is settled. Settled decisions go to priorcase_capture."),
		InputSchema: noteSchema(lang),
	}, s.note)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "priorcase_capture",
		Description: lang.T(
			"확정된 결정을 기록한다. 되돌리기 어려운 선택(아키텍처·스키마·외부 서비스·가격), "+
				"대안을 검토해 하나를 고른 경우, 실측으로 통념이 깨진 경우, "+
				"그리고 코드를 읽어도 알 수 없는 조직·프로세스 제약이 대상이다. "+
				"본문에 결론만 쓰지 말고 근거와 기각한 대안을 같이 남겨라. "+
				"기존 결정을 뒤집는 것이면 supersedes 와 함께 supersede_reason 에 **무엇이 뒤집었는지**를 남겨라. "+
				"아직 확정 전이면 priorcase_note 를 쓴다 — 버리지는 마라.",
			"Record a settled decision. Use it for choices that are hard to reverse (architecture, schema, "+
				"external services, pricing), for picking one option after weighing alternatives, when a "+
				"measurement overturned an assumption, and for organizational or process constraints that no "+
				"amount of code-reading reveals. Record the rationale and the rejected options, not just the "+
				"conclusion. If it overturns an existing decision, pass supersedes together with "+
				"supersede_reason — **what overturned it**. If it has not settled yet use priorcase_note — "+
				"but do not throw it away."),
		InputSchema: captureSchema(lang),
	}, s.capture)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "priorcase_review",
		// **번복 이유와 summary 를 설명에 박아 둔다.** 인자만 열어 두면 모델이 안 쓴다 —
		// 실볼트 18노트 중 번복 사유가 남은 것이 0건이었고, outcome 이 bad 로 바뀐 노트의
		// summary 가 여전히 뒤집힌 결론을 말하는 상태로 회수에 실려 나갔다.
		Description: lang.T(
			"기존 결정의 결과(outcome)·상태·요약·회고를 갱신하거나, 그 결정을 뒤집는다. "+
				"뒤집힌 결정이 그대로 남아 있으면 회수가 오염된다. "+
				"**무엇이 뒤집었는지를 supersede_reason 에 한 줄로 남겨라** — 계기가 없으면 "+
				"다음 사람이 그 번복을 신뢰하지 못하고 원래 안으로 되돌린다. "+
				"결론이 바뀌었으면 summary 도 함께 고쳐라. 회수가 주입하는 것은 그 한 줄뿐이라 "+
				"본문만 고치면 낡은 결론이 계속 대화에 실려 나간다.",
			"Update an existing decision's outcome, status, summary, or retrospective — or overturn it. "+
				"An overturned decision left as-is pollutes recall. "+
				"**Record what overturned it in supersede_reason**, in one line — without the trigger the "+
				"next person cannot trust the reversal and will swing back to the original. "+
				"If the conclusion changed, fix summary too: recall injects only that one line, so editing "+
				"the body alone leaves the stale conclusion in circulation."),
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
		// 사람이 찾을 때도 참고 문서를 본다 — 훅과 같은 코퍼스를 봐야
		// "훅은 주는데 recall 은 안 준다" 가 안 생긴다.
		IncludeReferences: true,
		ReferenceLimit:    limit,
	})
	if err != nil {
		return nil, nil, err
	}

	body := search.RenderInject(s.l, hits)

	// **작업 로그는 여기서만 나온다.** 자동 주입에는 절대 안 섞고, 물어봤을 때만
	// 붙인다 — 그것이 등급을 나눈 이유다. 회수는 Limit 3 · MinScore 1 의 고정
	// 슬롯이라, 작업 로그가 그 슬롯을 놓고 결정 노트와 경쟁하면 볼트가 커질수록
	// 결정 노트가 밀려난다.
	//
	// 결정 노트 뒤에 붙인다. 등급이 그대로 읽히는 순서여야 한다.
	// 작업 로그를 못 읽어도 회수 자체는 성공시킨다 — 결정 노트는 이미 손에 있고,
	// 하위 계층 하나 때문에 상위 계층까지 못 주는 것이 더 나쁘다. 대신 침묵하지 않는다.
	//
	// **cross_project 를 여기서도 지킨다.** 예전에는 안 넘겨서 그 인자가 반쪽만
	// 지켜졌다 — 결정 노트는 좁혀졌는데 작업 로그는 전 도메인이 나왔다.
	notes, werr := worklog.Search(s.l, search.ExtractKeywords(a.Query),
		worklog.Scope(s.c, cwd, crossProject), limit)
	// **경고를 body 에 바로 붙이지 않는다.** 아래 `body == ""` 폴백이 "찾았는데 없다"
	// 를 명시하는 자리인데, 여기서 body 를 채워 버리면 결정 노트도 0건이고 작업 로그도
	// 실패한 최악의 경우에 그 문장이 안 나온다 — 모델은 괄호 친 에러만 받는다.
	warn := ""
	if werr != nil {
		warn = fmt.Sprintf("\n(작업 로그를 찾아보지 못했다: %v)\n", werr)
	}
	if len(notes) > 0 {
		var w strings.Builder
		w.WriteString("\n[작업 로그 — 확정 전 기록]\n")
		for _, h := range notes {
			fmt.Fprintf(&w, "- %s %s · %s → %s\n",
				h.Date, h.Time, h.Title, s.l.RelPath(h.Path))
		}
		body += w.String()
	}

	if body == "" {
		// 빈 응답을 내면 모델이 도구가 고장난 것으로 읽고 다시 부르거나 포기한다.
		// "찾았는데 없다" 를 명시적으로 말해야 다음 행동이 갈린다.
		body = fmt.Sprintf("%q 와 관련된 과거 결정도 작업 로그도 찾지 못했다.\n", a.Query)
	}
	return textResult(body + warn + renderSkipped(s.l, skipped)), nil, nil
}

// ── capture ─────────────────────────────────────────────────────────────

type captureArgs struct {
	Domain     string   `json:"domain"`
	Slug       string   `json:"slug"`
	Summary    string   `json:"summary"`
	Body       string   `json:"body,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Related    []string `json:"related,omitempty"`
	Supersedes []string `json:"supersedes,omitempty"`
	// SupersedeReason 은 **왜 그것을 뒤집는가** 다. core 가 필드를 만들어 둔 뒤에도
	// 여기 인자가 없어서 에이전트는 영영 못 넘겼다 — 실볼트 18노트 중 번복 사유가
	// 남은 것이 0건이었던 실제 원인이 이 한 줄의 부재였다.
	SupersedeReason string `json:"supersede_reason,omitempty"`
	Date            string `json:"date,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	// Author 는 이 결정을 내린 사람이다. 비면 설정·git 신원에서 정한다.
	Author string `json:"author,omitempty"`
}

func (s *server) capture(ctx context.Context, req *sdk.CallToolRequest, a captureArgs) (*sdk.CallToolResult, noOutput, error) {
	author := a.Author
	if author == "" {
		wd, _ := os.Getwd()
		author = s.c.AuthorFor(wd)
	}
	res, err := capture.Do(s.l, s.c, capture.Request{
		Author:          author,
		Domain:          a.Domain,
		Slug:            a.Slug,
		Summary:         a.Summary,
		Date:            a.Date,
		Supersedes:      a.Supersedes,
		SupersedeReason: a.SupersedeReason,
		SourceSession:   a.SessionID,
		Tags:            a.Tags,
		Related:         a.Related,
		Body:            []byte(a.Body),
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

// ── note ────────────────────────────────────────────────────────────────

type noteArgs struct {
	Domain    string   `json:"domain"`
	Summary   string   `json:"summary"`
	Body      string   `json:"body,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Date      string   `json:"date,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
}

func (s *server) note(ctx context.Context, req *sdk.CallToolRequest, a noteArgs) (*sdk.CallToolResult, noOutput, error) {
	res, err := worklog.Append(s.l, worklog.Entry{
		Domain:  a.Domain,
		Date:    a.Date,
		Title:   a.Summary,
		Body:    a.Body,
		Session: a.SessionID,
		Tags:    a.Tags,
	})
	if err != nil {
		return nil, nil, err
	}
	// **편승을 붙이지 않는다.** capture 는 결정 시점이라 과거 결정이 가장 정확하게
	// 닿는 순간이지만, note 는 자주 불리라고 만든 것이다. 매번 회수 결과를 딸려
	// 보내면 그 자체가 부르기를 망설이게 만드는 비용이 된다.
	return textResult(fmt.Sprintf("작업 로그에 남겼다: %s\n", s.l.RelPath(res.Path))), nil, nil
}

// ── review ──────────────────────────────────────────────────────────────

type reviewArgs struct {
	Stem    string `json:"stem"`
	Outcome string `json:"outcome,omitempty"`
	Status  string `json:"status,omitempty"`
	// Summary 가 없던 것이 **실제 손해를 냈다.** 볼트의 codecommit 노트는 outcome 이
	// bad 로 뒤집힌 뒤에도 summary 가 옛 결론을 그대로 말했다 — 회수가 주입하는 유일한
	// 한 줄이 거짓말을 하는 상태로 계속 돌았고, 고칠 인자가 여기 없어서 에이전트는
	// 본문에 정정을 적는 것 말고 할 수 있는 일이 없었다. 본문은 주입되지 않는다.
	Summary       string   `json:"summary,omitempty"`
	Retrospective string   `json:"retrospective,omitempty"`
	Supersedes    []string `json:"supersedes,omitempty"`
	// SupersedeReason 은 **무엇이 이 판단을 뒤집었는가** 다. review 는 supersedes 없이도
	// 번복 이유를 남길 수 있는 유일한 경로다.
	SupersedeReason string `json:"supersede_reason,omitempty"`
}

func (s *server) review(ctx context.Context, req *sdk.CallToolRequest, a reviewArgs) (*sdk.CallToolResult, noOutput, error) {
	_, err := capture.Review(s.l, capture.ReviewRequest{
		Stem:            a.Stem,
		Outcome:         a.Outcome,
		Status:          a.Status,
		Summary:         a.Summary,
		Retrospective:   a.Retrospective,
		Supersedes:      a.Supersedes,
		SupersedeReason: a.SupersedeReason,
	})
	if err != nil {
		return nil, nil, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "갱신됨: %s\n", a.Stem)
	// capture 와 달리 core 가 편승을 주지 않으므로 어댑터가 직접 만든다. 질의어는
	// 갱신한 결정의 요약이다 — 같은 주제의 이웃 결정이 걸린다.
	b.WriteString(s.piggyback(a.Stem))
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
