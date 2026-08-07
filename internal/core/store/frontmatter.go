package store

import (
	"bytes"
	"fmt"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// Meta 는 결정 노트의 frontmatter 10키다. 순서는 EmitFrontmatter 가 정한다.
type Meta struct {
	Type          string   `yaml:"type"`
	Date          string   `yaml:"date"`
	Domain        []string `yaml:"domain"`
	Summary       string   `yaml:"summary"`
	Status        string   `yaml:"status"`
	Outcome       string   `yaml:"outcome"`
	Supersedes    string   `yaml:"supersedes"`
	Related       []string `yaml:"related"`
	Tags          []string `yaml:"tags"`
	SourceSession string   `yaml:"source_session"`
}

var fence = []byte("---")

// ParseFrontmatter 는 --- 로 감싼 YAML 블록과 그 뒤 본문을 나눈다.
func ParseFrontmatter(data []byte) (Meta, []byte, error) {
	var m Meta
	if !bytes.HasPrefix(data, fence) {
		return m, nil, fmt.Errorf("frontmatter 가 없다 (--- 로 시작하지 않는다)")
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
	dec.KnownFields(true) // 10키 외의 잉여 키를 조용히 버리지 않는다
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
		panic(fmt.Sprintf("casebook: YAML 스칼라 마샬 실패: %v", err))
	}
	q := strings.TrimRight(string(out), "\n")
	if strings.ContainsAny(q, "\n") {
		// emitter 가 긴 스칼라를 접으면 frontmatter 가 깨진다. 조용히 깨지느니 죽는다.
		panic("casebook: YAML 스칼라가 여러 줄로 접혔다 — 방출기를 고쳐야 한다")
	}
	return q
}

// bare 는 따옴표 없이 인라인 배열에 넣는다.
func bare(items []string) string { return "[" + strings.Join(items, ", ") + "]" }

func quoted(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = quote(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// EmitFrontmatter 는 casebook 의 유일한 frontmatter 방출기다.
// 키 순서가 이 함수 본문의 리터럴이므로 방출기가 둘이 될 수 없다.
func EmitFrontmatter(m Meta) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("type: " + m.Type + "\n")
	b.WriteString("date: " + m.Date + "\n")
	b.WriteString("domain: " + bare(m.Domain) + "\n")
	b.WriteString("summary: " + quote(m.Summary) + "\n")
	b.WriteString("status: " + m.Status + "\n")
	b.WriteString("outcome: " + m.Outcome + "\n")
	b.WriteString("supersedes: " + quote(m.Supersedes) + "\n")
	b.WriteString("related: " + quoted(m.Related) + "\n")
	b.WriteString("tags: " + bare(m.Tags) + "\n")
	b.WriteString("source_session: " + quote(m.SourceSession) + "\n")
	b.WriteString("---\n")
	return []byte(b.String())
}

// EmitNote 는 frontmatter 와 본문을 합친다. 사이에 빈 줄 하나.
func EmitNote(m Meta, body []byte) []byte {
	out := EmitFrontmatter(m)
	out = append(out, '\n')
	return append(out, body...)
}
