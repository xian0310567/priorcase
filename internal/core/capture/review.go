package capture

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/xian0310567/casebook/internal/core/index"
	"github.com/xian0310567/casebook/internal/core/schema"
	"github.com/xian0310567/casebook/internal/core/store"
)

type ReviewRequest struct {
	Stem          string
	Outcome       string // 빈 문자열이면 변경 없음
	Status        string
	Retrospective string
	Supersedes    string // 뒤집는 대상의 stem
}

// Review 는 기존 결정의 outcome·status·회고·supersedes 를 갱신한다.
// supersedes 는 양방향으로 연결한다 — 옛 노트도 superseded 로 바꾸고 related 를 채운다.
func Review(l *store.Layout, r ReviewRequest) error {
	path, err := l.ResolveStem(r.Stem)
	if err != nil {
		return err
	}
	n, err := l.Read(path)
	if err != nil {
		return fmt.Errorf("대상 없음: %s (%w)", r.Stem, err)
	}

	if r.Outcome != "" {
		n.Meta.Outcome = r.Outcome
	}
	if r.Status != "" {
		n.Meta.Status = r.Status
	}

	var old store.Note
	hasOld := false
	if r.Supersedes != "" {
		link, o, err := supersede(l, r.Supersedes, n.Stem)
		if err != nil {
			return err
		}
		n.Meta.Supersedes, old, hasOld = link, o, true
	}
	if r.Retrospective != "" {
		n.Body = appendRetrospective(n.Body, r.Retrospective)
	}

	// 두 노트를 모두 검증한 뒤에야 쓰기 시작한다. 옛 노트를 먼저 쓰고 새 노트
	// 검증에서 실패하면 옛 노트만 superseded 로 남아 양방향 연결이 반쪽짜리
	// 상태로 디스크에 고정된다 — supersedes 링크는 없는데 옛 노트는 이미
	// 뒤집힌 것으로 기록돼, 회수 시 두 노트 다 사실과 다르게 잡힌다.
	if hasOld {
		if err := schema.Validate(l.DecisionMarker(), old.Stem, old.Meta); err != nil {
			return fmt.Errorf("옛 노트 검증 실패: %w", err)
		}
	}
	if err := schema.Validate(l.DecisionMarker(), n.Stem, n.Meta); err != nil {
		return fmt.Errorf("검증 실패: %w", err)
	}

	if hasOld {
		if err := l.Write(old); err != nil {
			return err
		}
	}
	if err := l.Write(n); err != nil {
		return err
	}
	// Do 와 같은 안내를 낸다 — 여기도 노트를 이미 쓴 뒤라, 색인만 낡았다는
	// 사실을 알려주지 않으면 사용자는 갱신 자체가 안 된 줄 알고 다시 시도한다.
	if _, err := index.Write(l); err != nil {
		return fmt.Errorf("노트는 썼으나 색인 갱신에 실패했다: %w", err)
	}
	return nil
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
	// retroHeadRe 는 줄 전체가 "## 회고" 인 헤더 줄을 찾는다. 정확히 그 줄만
	// 매칭하도록 (?m)^...$ 로 앵커링한다 — 안 그러면 "### 회고" 같은 하위
	// 헤더의 부분 문자열에도 걸린다.
	retroHeadRe = regexp.MustCompile(`(?m)^## 회고[ \t]*$`)
	// sectionHeadRe 는 다음 최상위 절의 시작(줄 맨 앞의 "## ")을 찾는다.
	sectionHeadRe = regexp.MustCompile(`(?m)^## `)
)

// appendRetrospective 는 "## 회고" 절에 내용을 붙인다.
//
// 절이 없으면 본문 끝에 새로 만든다. 절이 이미 있으면 그 절 *안에* 붙인다 —
// 뒤에 다른 절(예: "## 부록")이 있으면 그 앞에 삽입해, 두 번째 회고가 엉뚱한
// 절 뒤로 밀려나지 않게 한다.
func appendRetrospective(body []byte, text string) []byte {
	s := string(body)
	text = strings.TrimRight(text, "\n")

	loc := retroHeadRe.FindStringIndex(s)
	if loc == nil {
		return []byte(strings.TrimRight(s, "\n") + "\n\n## 회고\n\n" + text + "\n")
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
