package store

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// Meta 는 결정 노트의 frontmatter 다. 순서는 EmitFrontmatter 가 정한다.
type Meta struct {
	Type string `yaml:"type"`
	Date string `yaml:"date"`

	// Author 는 이 결정을 내린 사람이다. **팀에서는 이게 절반이다** — "왜 이렇게
	// 했지" 의 다음 질문이 언제나 "누가 정했지" 이고, 그 사람에게 물어보는 것이
	// 노트를 다시 읽는 것보다 빠를 때가 많다.
	//
	// **비어 있으면 방출하지 않는다.** 혼자 쓰는 볼트에서는 자명해서 소음이고,
	// 무엇보다 기존 노트를 다시 저장할 때 바이트가 안 바뀌어야 한다.
	//
	// 스키마 판을 올리지 않는다. 이 패키지의 규칙대로 순수 증분 키는 옛 바이너리가
	// Extra 로 보존하므로 데이터를 잃지 않는다 (schema.Current 주석 참고).
	Author string `yaml:"author,omitempty"`

	Domain  []string `yaml:"domain"`
	Summary string   `yaml:"summary"`

	// SummaryHistory 는 review 가 갈아치운 **옛 summary** 들이다. 오래된 것이 앞.
	//
	// summary 를 고칠 수 있게 만든 순간 생긴 구멍이다: 고치면 원래 뭐라고 적었는지가
	// 사라진다. 그런데 "우리가 한때 무엇을 믿었는가" 는 번복 기록의 절반이다 —
	// 틀린 판단을 지우면 남는 건 정답뿐이고, 다음 사람은 같은 오답을 다시 판다.
	//
	// **본문이 아니라 여기에 둔다.** 본문에 두면 review --summary 가 본문을 건드리게
	// 되는데, summary 한 줄만 고치러 온 사람의 노트 본문이 바뀌는 것은 놀라움이다
	// (review_test.go 의 TestReviewCanCorrectSummary 가 그 계약을 못 박고 있다).
	// 게다가 회수의 head 는 stem+summary+tags 라, 옛 summary 를 head 밖에 두는 것이
	// 오히려 맞다 — 틀려서 갈아치운 줄이 계속 검색에 걸리면 회수가 오염된다.
	//
	// 비어 있으면 방출하지 않는다. 기존 노트의 바이트가 안 바뀐다.
	SummaryHistory []string `yaml:"summary_history,omitempty"`

	Status  string `yaml:"status"`
	Outcome string `yaml:"outcome"`
	// Supersedes 는 이 결정이 뒤집은 결정들이다. **여럿일 수 있다.**
	//
	// 예전에는 `string` 이었고, 그 한 칸이 실제로 데이터를 잃었다: 2026-08-13
	// `방향전환-개인도구-다중볼트` 가 전제 6개를 폐기 선언했는데 엮인 것은 1건뿐이고,
	// 나머지는 본문 산문으로 밀려나 두 노트가 "superseded 인데 무엇이 뒤집었는지
	// 아무 데도 없는" 상태로 남았다. 회사 머신의 옛 바이너리가 그 노트를 다시 쓰면서
	// 첫 값만 남기고 나머지를 related 로 내린 것이 2026-08-21 에 실제로 관측됐다 —
	// 두 머신의 스키마가 갈리면 마지막에 쓴 쪽이 이긴다.
	Supersedes LinkList `yaml:"supersedes"`

	// SupersededReason 는 **이 결정이 왜 뒤집혔는가** 다. 뒤집는 쪽이 아니라
	// 뒤집힌 쪽에 적힌다 — capture/supersede.go 가 옛 노트에 쓴다.
	//
	// 없던 자리다. supersede() 가 옛 노트에 하던 일은 status="superseded" 와
	// related 한 줄이 전부여서, **무엇이** 뒤집었는지(링크)는 남고 **왜** 뒤집혔는지는
	// 한 글자도 안 남았다. 실측: 실볼트 18노트 중 번복 사유가 기록된 것 0건.
	//
	// supersedes 바로 뒤에 방출한다 — 둘은 같은 사건의 양쪽이고, 옵시디언에서
	// 붙어 있어야 사람이 한눈에 짝을 본다.
	//
	// 비어 있으면 방출하지 않는다 — 기존 18노트를 다시 저장해도 바이트가 안 바뀐다.
	SupersededReason string `yaml:"superseded_reason,omitempty"`

	Related       []string `yaml:"related"`
	Tags          []string `yaml:"tags"`
	SourceSession string   `yaml:"source_session"`

	// Schema 는 이 노트를 쓴 priorcase 의 스키마 판이다. **없으면 1 이다.**
	//
	// 팀이 볼트를 공유하면 버전이 갈린다 — 한 명이 먼저 올리면 나머지가 그 사람의
	// 노트를 만난다. 판을 안 적어 두면 옛 바이너리가 새 값(예: 아직 모르는 status)을
	// "허용값 밖" 으로 보고 거부하는데, 그건 남의 결정을 지우는 것과 같다.
	//
	// 1 일 때는 방출하지 않는다 — 기존 노트의 바이트를 안 건드리기 위해서다.
	Schema int `yaml:"schema,omitempty"`

	// Extra 는 **위 키 밖의 키를 사용자가 쓴 그대로** 담는다.
	//
	// (예전 주석은 "10키" 라고 못 박았는데, summary_history·superseded_reason 이
	// 늘면서 숫자가 틀렸다. 개수는 이 구조체가 정본이므로 세지 않는다.)
	//
	// 이것이 없으면 사용자가 Obsidian 에서 노트에 `aliases:` 한 줄만 넣어도 파싱이
	// 실패하고, 그 결정이 색인·회수·review 에서 통째로 사라진다. 조용히 버리지 않으려고
	// KnownFields(true) 를 켰는데, 버리지 않는 대신 **읽기를 포기해** 결과적으로 더 크게
	// 잃고 있었다. 이제 버리지도 잃지도 않는다 — 받아서 되쓴다.
	//
	// yaml.Node 로 받는 이유는 스타일 보존이다. map[string]any 로 받으면 사용자가 쓴
	// `[a, b]` 가 되쓸 때 블록 목록으로 바뀌어, 고치지도 않은 줄의 바이트가 달라진다.
	Extra map[string]yaml.Node `yaml:",inline"`
}

// LinkList 는 **스칼라와 시퀀스를 둘 다 받는** 링크 목록이다.
//
// 이 타입이 없으면 `supersedes` 를 다중값으로 올리는 순간 **디스크의 기존 노트가
// 전부 파싱 불능이 된다.** EmitFrontmatter 가 supersedes 를 omitempty 없이 항상
// 쓰므로 실볼트 191건 전부가 `supersedes: ""` 를 갖고 있고, yaml.v3 는 `!!str` 를
// `[]string` 에 넣지 못한다. readNote 가 실패하면 List() 가 전건을 건너뛰어
// 색인 0행·회수 0건이 된다.
//
// **스키마 판을 올려도 못 막는다.** schema.IsFuture 는 Validate 안의 열거값 검사만
// 완화하는데, 파싱은 그보다 먼저 ParseFrontmatter 에서 일어난다. frontmatter.go 의
// "순수 증분 키는 옛 바이너리가 Extra 로 보존한다" 규칙도 **키 추가**에만 적용되고
// 기존 키의 **타입 변경**에는 적용되지 않는다.
//
// 그래서 마이그레이션이 아니라 관용으로 푼다 — 읽을 때 두 표기를 다 받고,
// 쓸 때는 하나면 스칼라로 되돌린다(아래 emitLinkList).
type LinkList []string

func (ll *LinkList) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		var s string
		if err := n.Decode(&s); err != nil {
			return err
		}
		if strings.TrimSpace(s) == "" {
			*ll = nil // 빈 스칼라는 "없다" 다. 빈 문자열 한 개가 아니다.
			return nil
		}
		*ll = LinkList{s}
		return nil
	case yaml.SequenceNode:
		var ss []string
		if err := n.Decode(&ss); err != nil {
			return err
		}
		if len(ss) == 0 {
			*ll = nil // 빈 시퀀스와 빈 스칼라를 같은 것으로 접는다
			return nil
		}
		*ll = LinkList(ss)
		return nil
	}
	return fmt.Errorf("supersedes 는 문자열이거나 목록이어야 한다 (줄 %d)", n.Line)
}

// emitLinkList 는 **하나 이하면 스칼라, 여럿이면 시퀀스**로 쓴다.
//
// 언제나 시퀀스로 쓰면 기존 191건 전부의 바이트가 바뀌어 diff 가 소음이 되고,
// 옛 바이너리가 그 노트를 통째로 못 읽는다. Author(비면 줄 자체를 안 쓴다)와
// Schema(1이면 안 쓴다)가 이미 세운 "재저장해도 바이트 불변" 규칙과 같은 이유다.
func emitLinkList(ll LinkList) string {
	switch len(ll) {
	case 0:
		return `""`
	case 1:
		return quote(wikilink(ll[0]))
	}
	return quoted(wikilinks(ll))
}

// 결정 노트의 status 값. schema.Validate 가 허용 목록을 들고 있고, 여기 상수는
// **그 값을 코드에서 부르는 이름**이다 — 리터럴이 흩어지면 오타가 조용히 통과한다.
const (
	StatusActive     = "active"     // 현행
	StatusSuperseded = "superseded" // 뒤집혔다 — 후속이 있다
	StatusRegretted  = "regretted"  // 했는데 나빴다 — 계속 떠야 한다
	// StatusRetracted 는 **애초에 결정이 아니었다** 는 뜻이다.
	//
	// 판별기 오기록이나 사람의 착오로 만들어진 노트를 위한 자리다. regretted 와
	// 다르다: 후회는 계속 눈앞에 있어야 하지만 철회는 근거가 아니므로 회수에서
	// 통째로 빠진다(search.scoreAll). 파일은 지우지 않는다.
	StatusRetracted = "retracted"
)

var fence = []byte("---")

// ParseFrontmatter 는 --- 로 감싼 YAML 블록과 그 뒤 본문을 나눈다.
//
// 본문은 그대로 돌려주지 않는다: 닫는 펜스와 본문 사이의 선행 빈 줄을 전부
// 걷어낸다. 즉 이 함수는 왕복 무손실이 아니다 — 본문 앞 빈 줄 개수는 보존되지
// 않고, EmitNote 가 붙이는 빈 줄 하나로 정규화된다. (그 대신 emit∘parse 가
// 멱등이 된다. 아래 주석 참고.)
// ErrNoFrontmatter 는 파일이 --- 로 시작하지 않는다는 뜻이다.
//
// **센티널로 두는 이유:** 부르는 쪽마다 이게 고장인지 아닌지가 다르다.
// 결정 노트에는 고장이지만(규약 위반), 참고 문서 훑기에는 그냥 참여하지 않는
// 평범한 마크다운이다. 문구로 구별하게 하면 문구를 고치는 순간 판정이 깨진다.
var ErrNoFrontmatter = errors.New("frontmatter 가 없다 (--- 로 시작하지 않는다)")

func ParseFrontmatter(data []byte) (Meta, []byte, error) {
	var m Meta
	if !bytes.HasPrefix(data, fence) {
		return m, nil, ErrNoFrontmatter
	}
	rest := data[len(fence):]
	if i := bytes.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	} else {
		return m, nil, fmt.Errorf("frontmatter 가 닫히지 않았다")
	}
	end := bytes.Index(rest, append([]byte("\n"), fence...))
	if end < 0 {
		return m, nil, fmt.Errorf("frontmatter 가 닫히지 않았다")
	}
	head := rest[:end+1]
	body := rest[end+1+len(fence):]
	// 닫는 펜스와 본문 사이의 구분(빈 줄 0개 이상)을 전부 걷어낸다.
	// EmitNote 는 항상 정확히 빈 줄 하나(개행 1개)를 구분자로 붙이므로, 여기서
	// 선행 개행을 하나만 벗기면(원래 브리프 코드) 빈 줄이 있던 노트를 한 번
	// 방출할 때마다 빈 줄이 하나씩 늘어나 멱등성이 깨진다 — TestEmitIsIdempotent
	// 로 실측됨. 선행 개행을 전부 벗겨야 body 가 "구분자 없는 순수 본문"이 되고
	// EmitNote 가 매번 같은 구분자를 붙여 고정점에 도달한다.
	body = bytes.TrimLeft(body, "\n")

	dec := yaml.NewDecoder(bytes.NewReader(head))
	dec.KnownFields(true) // Meta 밖의 잉여 키를 조용히 버리지 않는다
	if err := dec.Decode(&m); err != nil {
		return m, nil, fmt.Errorf("frontmatter 파싱 실패: %w", err)
	}
	return m, body, nil
}

// quote 는 문자열을 YAML 큰따옴표 스칼라로 만든다.
// 손으로 이스케이프하지 않는다 — yaml 에 맡긴다.
func quote(s string) string {
	n := yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: s}
	out, err := yaml.Marshal(&n)
	if err != nil {
		panic(fmt.Sprintf("priorcase: YAML 스칼라 마샬 실패: %v", err))
	}
	q := strings.TrimRight(string(out), "\n")
	if strings.ContainsAny(q, "\n") {
		// emitter 가 긴 스칼라를 접으면 frontmatter 가 깨진다. 조용히 깨지느니 죽는다.
		panic("priorcase: YAML 스칼라가 여러 줄로 접혔다 — 방출기를 고쳐야 한다")
	}
	return q
}

// bare 는 따옴표 없이 인라인 배열에 넣는다.
func bare(items []string) string { return "[" + strings.Join(items, ", ") + "]" }

// wikilink 는 stem 을 `[[stem]]` 으로 만든다. 이미 그 모양이면 그대로 둔다.
//
// **옵시디언은 `[[ ]]` 가 있어야 링크로 만든다.** 맨 문자열로 두면 속성 창에 회색
// 글자로 보이고 **그래프에도 백링크에도 그 관계가 없다** — related 를 적은 목적이
// 통째로 사라진다.
//
// 실측으로 물렸다. 볼트 274건에서 맨 문자열이 57건이었고, 한 노트 안에서도 섞여 있었다:
//
//	related:
//	  - editup-결정-ga4-word채널-…              ← 링크 안 됨
//	  - "[[editup-결정-ga4-gp1510-인수기준-…]]"   ← 링크 됨
//
// 사용자가 옵시디언 속성 창을 보고 "세 개 중 하나만 의존성이 걸려 있다" 고 지적했다.
// 깨진 대상(존재하지 않는 노트)을 고치는 것과는 **다른 결함**이다 — 이쪽은 대상이
// 멀쩡한데도 관계가 안 생긴다.
//
// **호출부에 맡기지 않는다.** capture·review·MCP·CLI·판별기가 각자 넣는데, 실제로
// 어떤 경로는 대괄호를 붙이고 어떤 경로는 안 붙였다. 방출기가 유일한 쓰기 경로이므로
// 여기서 강제하면 누가 무엇을 주든 링크가 된다.
func wikilink(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	if strings.HasPrefix(t, "[[") && strings.HasSuffix(t, "]]") {
		return t
	}
	// 대괄호가 한쪽만 있거나 겹친 것도 벗겨서 다시 씌운다 — 손으로 고치다 생긴다.
	return "[[" + strings.Trim(t, "[]") + "]]"
}

// wikilinks 는 목록 전체에 wikilink 를 적용한다. 빈 항목은 버린다.
func wikilinks(items []string) []string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		if v := wikilink(s); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func quoted(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = quote(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// EmitFrontmatter 는 priorcase 의 유일한 frontmatter 방출기다.
// 키 순서가 이 함수 본문의 리터럴이므로 방출기가 둘이 될 수 없다.
func EmitFrontmatter(m Meta) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("type: " + m.Type + "\n")
	b.WriteString("date: " + m.Date + "\n")
	// 비었으면 줄 자체를 안 쓴다 — 기존 노트를 다시 저장해도 바이트가 안 바뀐다.
	if strings.TrimSpace(m.Author) != "" {
		b.WriteString("author: " + quote(m.Author) + "\n")
	}
	b.WriteString("domain: " + bare(m.Domain) + "\n")
	b.WriteString("summary: " + quote(m.Summary) + "\n")
	// 아래 두 키는 **비면 줄 자체를 안 쓴다**. author·schema 와 같은 규칙이다 —
	// 기존 노트를 다시 저장해도 바이트가 안 바뀌어야 한다(실볼트 18노트 전부가
	// 이 두 키가 없는 상태다).
	if len(m.SummaryHistory) > 0 {
		b.WriteString("summary_history: " + quoted(m.SummaryHistory) + "\n")
	}
	b.WriteString("status: " + m.Status + "\n")
	b.WriteString("outcome: " + m.Outcome + "\n")
	b.WriteString("supersedes: " + emitLinkList(m.Supersedes) + "\n")
	if strings.TrimSpace(m.SupersededReason) != "" {
		b.WriteString("superseded_reason: " + quote(m.SupersededReason) + "\n")
	}
	b.WriteString("related: " + quoted(wikilinks(m.Related)) + "\n")
	b.WriteString("tags: " + bare(m.Tags) + "\n")
	b.WriteString("source_session: " + quote(m.SourceSession) + "\n")
	// 판이 1(기본)이면 안 쓴다. 기존 노트가 재기록될 때 바이트가 안 바뀐다.
	if m.Schema > 1 {
		fmt.Fprintf(&b, "schema: %d\n", m.Schema)
	}
	b.WriteString(emitExtra(m.Extra))
	b.WriteString("---\n")
	return []byte(b.String())
}

// emitExtra 는 Meta 밖의 키를 **알려진 키 뒤에** 되쓴다.
//
// 키 순서를 사전순으로 고정한다 — 맵은 순회 순서가 무작위라, 안 그러면 같은 노트를
// 두 번 저장할 때마다 바이트가 달라져 diff 가 소음이 된다.
//
// 방출에 실패한 키는 **버리지 않고 건너뛰지도 않는다** — 애초에 파싱된 노드라
// 실패할 일이 없지만, 만약 실패하면 그 키를 잃는 것이므로 원문을 그대로 못 쓰느니
// 주석으로라도 남긴다.
func emitExtra(extra map[string]yaml.Node) string {
	if len(extra) == 0 {
		return ""
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		n := extra[k]
		out, err := yaml.Marshal(map[string]yaml.Node{k: n})
		if err != nil {
			fmt.Fprintf(&b, "# priorcase: %q 를 되쓸 수 없었다 (%v)\n", k, err)
			continue
		}
		b.Write(out)
	}
	return b.String()
}

// EmitNote 는 frontmatter 와 본문을 합친다. 사이에 빈 줄 하나.
func EmitNote(m Meta, body []byte) []byte {
	out := EmitFrontmatter(m)
	out = append(out, '\n')
	return append(out, body...)
}
