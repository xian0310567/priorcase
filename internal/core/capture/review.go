package capture

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/i18n"
	"github.com/xian0310567/priorcase/internal/core/index"
	"github.com/xian0310567/priorcase/internal/core/schema"
	"github.com/xian0310567/priorcase/internal/core/store"
)

type ReviewRequest struct {
	Stem    string
	Outcome string // 빈 문자열이면 변경 없음
	Status  string
	// Summary 는 한 줄 요약을 고친다. 빈 문자열이면 변경 없음.
	//
	// **회수에 주입되는 것은 summary 한 줄뿐이다.** 여기에 틀린 사실이 박히면
	// 그 오류가 앞으로 계속 대화에 실려 나간다 — 본문은 아무도 안 열어 볼 수 있어도
	// 이 줄은 반드시 읽힌다. 그런데 이 도구에는 그걸 고칠 길이 없었다.
	//
	// 실제로 그 상태를 만났다: 2026-08-12 에 시뮬레이션 숫자가 틀린 채로 summary 에
	// 박혔고, 회고 절에 정정을 적어도 회수는 여전히 틀린 한 줄을 주입했다.
	Summary       string
	Retrospective string
	Supersedes    []string // 뒤집는 대상의 stem (여럿 가능)
}

// Review 는 기존 결정의 outcome·status·회고·supersedes 를 갱신하고, 뒤이은
// 색인 갱신에서 읽지 못해 빠진 노트를 준다.
// supersedes 는 양방향으로 연결한다 — 옛 노트도 superseded 로 바꾸고 related 를 채운다.
func Review(l *store.Layout, r ReviewRequest) (ReviewResult, error) {
	path, err := l.ResolveStem(r.Stem)
	if err != nil {
		return ReviewResult{}, err
	}
	n, err := l.Read(path)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("대상 없음: %s (%w)", r.Stem, err)
	}
	// 고칠 대상이 더 새 판이면 손대지 않는다. 아래 supersede 대상(old)에도 같은 가드가 있다.
	if err := refuseFutureNote(n); err != nil {
		return ReviewResult{}, err
	}

	if r.Outcome != "" {
		n.Meta.Outcome = r.Outcome
	}
	// **철회에는 이유가 있어야 한다.**
	//
	// 철회된 노트는 파일로 남지만 회수에서 통째로 빠진다(search.scoreAll).
	// 왜 뺐는지가 안 적히면 나중에 옵시디언에서 그 노트를 연 사람은 status 한 줄만
	// 보고 아무것도 알 수 없다 — 그 노트는 이 시스템에 대해 아무 말도 안 하면서
	// 자리만 차지한다. "조용히 틀리느니 시끄럽게 멈춘다" 가 여기서는 "빼는 이유를
	// 남겨라" 다.
	//
	// 다른 status 에는 안 건다. superseded 는 후속 노트가 이유를 말하고,
	// regretted 는 계속 회수되므로 사람이 본문에서 읽을 기회가 있다.
	if r.Status == store.StatusRetracted && strings.TrimSpace(r.Retrospective) == "" {
		return ReviewResult{}, fmt.Errorf(
			"철회에는 이유가 필요하다 — --retro 로 왜 이 노트가 결정이 아닌지 적어라 " +
				"(회수에서 빠지므로 나중에 아무도 못 묻는다)")
	}
	if r.Status != "" {
		n.Meta.Status = r.Status
	}
	if r.Summary != "" {
		n.Meta.Summary = r.Summary
	}

	links, olds, err := supersedeAll(l, r.Supersedes, n.Stem)
	if err != nil {
		return ReviewResult{}, err
	}
	if len(links) > 0 {
		n.Meta.Supersedes = links
	}
	if r.Retrospective != "" {
		n.Body = appendRetrospective(n.Body, r.Retrospective, l.Lang())
	}

	// 두 노트를 모두 검증한 뒤에야 쓰기 시작한다. 옛 노트를 먼저 쓰고 새 노트
	// 검증에서 실패하면 옛 노트만 superseded 로 남아 양방향 연결이 반쪽짜리
	// 상태로 디스크에 고정된다 — supersedes 링크는 없는데 옛 노트는 이미
	// 뒤집힌 것으로 기록돼, 회수 시 두 노트 다 사실과 다르게 잡힌다.
	for _, old := range olds {
		if err := refuseFutureNote(old); err != nil {
			return ReviewResult{}, err
		}
		if err := schema.Validate(l.DecisionMarker(), old.Stem, old.Meta); err != nil {
			return ReviewResult{}, fmt.Errorf("옛 노트 검증 실패: %w", err)
		}
	}
	if err := schema.Validate(l.DecisionMarker(), n.Stem, n.Meta); err != nil {
		return ReviewResult{}, fmt.Errorf("검증 실패: %w", err)
	}

	for _, old := range olds {
		if err := l.Write(old); err != nil {
			return ReviewResult{}, err
		}
	}
	if err := l.Write(n); err != nil {
		return ReviewResult{}, err
	}
	// Do 와 같은 안내를 낸다 — 여기도 노트를 이미 쓴 뒤라, 색인만 낡았다는
	// 사실을 알려주지 않으면 사용자는 갱신 자체가 안 된 줄 알고 다시 시도한다.
	idx, err := index.Write(l)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("노트는 썼으나 색인 갱신에 실패했다: %w", err)
	}
	return ReviewResult{Skipped: idx.Skipped, IndexPreserved: idx.Preserved}, nil
}

// ReviewResult 는 갱신의 부수 결과다. 색인 갱신이 이 함수 안에서 일어나므로,
// 거기서 나온 경고를 호출자가 볼 유일한 통로다.
type ReviewResult struct {
	Skipped []store.SkippedNote
	// IndexPreserved 는 색인 자리에 있던 남의 파일을 대피시킨 경로다.
	IndexPreserved string
}

func appendUnique(ss []string, v string) []string {
	for _, s := range ss {
		if s == v {
			return ss
		}
	}
	return append(ss, v)
}

var (
	// retroHeadRe 는 줄 전체가 회고 절 헤더인 줄을 찾는다. 정확히 그 줄만
	// 매칭하도록 (?m)^...$ 로 앵커링한다 — 안 그러면 "### 회고" 같은 하위
	// 헤더의 부분 문자열에도 걸린다.
	//
	// **두 언어를 다 알아본다.** 본문 템플릿이 lang 을 따라가므로 볼트에 `## 회고` 와
	// `## Retrospective` 가 섞인다 — 사용자가 lang 을 바꿨거나, 팀이 볼트를 공유하거나.
	// 한쪽만 알아보면 못 찾은 노트 끝에 **다른 언어의 절이 새로 생겨서** 회고가 둘로
	// 갈라진다. 국제화가 만들 뻔한 결함이라 여기서 막는다.
	retroHeadRe = regexp.MustCompile(`(?m)^## (회고|Retrospective)[ \t]*$`)
	// sectionHeadRe 는 다음 최상위 절의 시작(줄 맨 앞의 "## ")을 찾는다.
	sectionHeadRe = regexp.MustCompile(`(?m)^## `)
)

// retroHeading 은 새로 만들 회고 절의 제목이다.
//
// 본문에 이미 영어 절 제목(`## Decision` 등)이 있으면 영어로 간다. 그것이 노트의
// 언어를 알려 주는 유일한 단서다 — frontmatter 에는 언어 필드가 없다.
func retroHeading(body string, lang i18n.Lang) string {
	if englishSectionRe.MatchString(body) {
		return "Retrospective"
	}
	return lang.T("회고", "Retrospective")
}

// englishSectionRe 는 영어 본문 템플릿의 절 제목이다. capture 가 쓰는 것과 같아야 한다.
var englishSectionRe = regexp.MustCompile(`(?m)^## (Decision|Rationale|Alternatives considered|Risks)[ \t]*$`)

// appendRetrospective 는 회고 절에 내용을 붙인다.
//
// 절이 없으면 본문 끝에 새로 만든다. 절이 이미 있으면 그 절 *안에* 붙인다 —
// 뒤에 다른 절(예: "## 부록")이 있으면 그 앞에 삽입해, 두 번째 회고가 엉뚱한
// 절 뒤로 밀려나지 않게 한다.
func appendRetrospective(body []byte, text string, lang i18n.Lang) []byte {
	s := string(body)
	text = strings.TrimRight(text, "\n")

	loc := retroHeadRe.FindStringIndex(s)
	if loc == nil {
		// 절이 없으면 새로 만든다. 제목은 **그 노트를 따라간다** — 본문이 영어면
		// 영어 제목을, 아니면 설정 언어를 쓴다. 노트 하나 안에서 언어가 섞이면
		// 다음 갱신 때 또 못 찾는다.
		return []byte(strings.TrimRight(s, "\n") + "\n\n## " + retroHeading(s, lang) + "\n\n" + text + "\n")
	}

	afterHead := loc[1]
	rest := s[afterHead:]

	nextRel := -1
	if m := sectionHeadRe.FindStringIndex(rest); m != nil {
		nextRel = m[0]
	}

	var section, tail string
	if nextRel >= 0 {
		section = strings.TrimSpace(rest[:nextRel])
		tail = rest[nextRel:] // 다음 절의 "## " 로 바로 시작한다
	} else {
		section = strings.TrimSpace(rest)
	}

	var b strings.Builder
	b.WriteString(s[:afterHead])
	b.WriteString("\n\n")
	if section != "" {
		b.WriteString(section)
		b.WriteString("\n\n")
	}
	b.WriteString(text)
	b.WriteString("\n")
	if tail != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimRight(tail, "\n"))
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// refuseFutureNote 는 **더 새 판으로 쓰인 노트를 고치려는 것**을 막는다.
//
// 읽는 것은 안전하다 — ParseFrontmatter 가 모르는 값을 그대로 받고 Extra 가 잉여 키를
// 보존한다. 그런데 **쓰는 것**은 다르다. 우리가 모르는 규칙으로 쓰인 노트를 우리 규칙으로
// 되쓰면 조용히 망가뜨린다.
//
// 팀이 볼트를 공유하면 한 명이 먼저 올린 상태가 정상이다. 그때 나머지가 그 사람의
// 결정을 뭉개는 자리가 여기다.
func refuseFutureNote(n store.Note) error {
	if !schema.IsFuture(n.Meta) {
		return nil
	}
	return fmt.Errorf(
		"이 결정은 더 새 판(schema %d)으로 쓰였다. 지금 prior 는 판 %d 까지 안다 — "+
			"고치면 망가뜨릴 수 있어 멈춘다. prior 를 올려라",
		n.Meta.Schema, schema.Current)
}
