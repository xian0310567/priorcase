// Package split 은 폴백 도메인에 쌓인 프로젝트를 자기 도메인으로 떼어낸다.
//
// **`prior doctor` 의 폴백 적체 검사(health/fallback.go)가 찾아낸 것을 실행하는
// 자리다.** 찾아 놓고 사람이 손으로 파일 13개를 옮기고 위키링크를 뒤지게 하면
// 그 검사는 결국 안 읽힌다 — 이 프로젝트가 "진단만 하고 무엇을 하라는 말이
// 없으면 사용자는 그 경고를 무시하는 법을 배운다" 고 적어 둔 그대로다.
//
// # 왜 계획과 실행을 가르는가
//
// 볼트는 git 으로 여러 머신에 오간다. 파일을 옮기고 이름을 바꾸는 것은 **되돌리기
// 어려운 변경**이고, 위키링크를 놓치면 옵시디언에서 링크가 통째로 끊긴다.
// 그래서 기본은 계획만 낸다 (`prior init` 과 같은 규약).
//
// # 무엇을 옮기는가
//
// 폴백 도메인 하나만 달린 결정 중 head 에 그 낱말이 걸리는 것이다. 판정은 회수와
// **같은 함수**(search.Head·search.Matches)를 쓴다 — 여기서 따로 구현하면
// `prior doctor` 가 센 것과 실제로 옮기는 것이 조용히 달라진다.
package split

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// Move 는 노트 하나의 이동이다.
type Move struct {
	From, To         string // 절대 경로
	OldStem, NewStem string
}

// Relink 는 옮겨진 stem 을 가리키던 문서 하나다.
type Relink struct {
	Path  string // 절대 경로
	Count int    // 고칠 링크 수
}

// Plan 은 한 번의 분리 계획이다. 실행 전에 전부 여기 담긴다.
type Plan struct {
	Prefix  string // 새 도메인 접두어
	Folder  string // 볼트 안 폴더 이름
	Dir     string // 새 결정 폴더의 절대 경로
	Moves   []Move
	Relinks []Relink
	// Skipped 는 이름이 부딪혀 접두어 중복을 못 지운 것이다. 조용히 넘기지 않는다.
	Skipped []string
}

// stemDate 는 파일명 끝의 `-YYYY-MM-DD` 다.
var stemDate = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)

// Build 는 계획을 세운다. 파일은 하나도 건드리지 않는다.
func Build(c *config.Config, l *store.Layout, notes []store.Note, token, prefix string) (*Plan, error) {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return nil, fmt.Errorf("옮길 낱말이 비었다")
	}
	if prefix = strings.TrimSpace(prefix); prefix == "" {
		prefix = token
	}
	fallback := strings.TrimSpace(c.DefaultDomain)
	if fallback == "" {
		return nil, fmt.Errorf("폴백 도메인(default_domain)이 없어 옮길 대상을 정할 수 없다")
	}
	if strings.EqualFold(prefix, fallback) {
		return nil, fmt.Errorf("새 도메인 이름이 폴백 도메인과 같다: %q", prefix)
	}
	for _, d := range c.Domain {
		if strings.EqualFold(d.Prefix, prefix) {
			return nil, fmt.Errorf("도메인 %q 는 이미 있다 — 다른 이름을 --as 로 줘라", prefix)
		}
	}

	p := &Plan{Prefix: prefix, Folder: prefix}
	marker := l.DecisionMarker()
	if marker == "" {
		return nil, fmt.Errorf("결정 파일명 규약(decision_file)에서 표식을 유도할 수 없다")
	}

	// ① 옮길 노트를 고른다. 판정은 회수와 같은 함수를 쓴다.
	taken := map[string]bool{} // 새 stem 충돌 검사
	for _, n := range notes {
		if len(n.Meta.Domain) != 1 || !strings.EqualFold(n.Meta.Domain[0], fallback) {
			continue
		}
		if !search.Matches(search.Head(n, marker), token) {
			continue
		}
		slug, date := slugDateOf(n, marker)
		if slug == "" {
			p.Skipped = append(p.Skipped, n.Stem+" (파일명이 규약과 다르다)")
			continue
		}
		// 접두어가 slug 앞에 그대로 또 붙는 것을 막는다:
		// `common-결정-twincrew-P0…` → `twincrew-결정-P0…`
		trimmed := strings.TrimPrefix(slug, prefix+"-")
		cand := trimmed
		if cand == "" || taken[newStem(prefix, marker, cand, date)] {
			cand = slug // 부딪히면 원래 slug 를 쓴다 — 잃는 것보다 중복이 낫다
		}
		to, err := l.DecisionPathIn(p.Folder, prefix, cand, date)
		if err != nil {
			return nil, fmt.Errorf("%s: 새 경로를 만들 수 없다: %w", n.Stem, err)
		}
		ns := strings.TrimSuffix(filepath.Base(to), ".md")
		if taken[ns] {
			p.Skipped = append(p.Skipped, n.Stem+" (새 이름이 다른 노트와 부딪힌다)")
			continue
		}
		taken[ns] = true
		p.Moves = append(p.Moves, Move{From: n.Path, To: to, OldStem: n.Stem, NewStem: ns})
		if p.Dir == "" {
			p.Dir = filepath.Dir(to)
		}
	}
	if len(p.Moves) == 0 {
		return p, nil
	}

	// ② 그 stem 들을 가리키는 문서를 찾는다.
	renames := map[string]string{}
	for _, m := range p.Moves {
		renames[m.OldStem] = m.NewStem
	}
	p.Relinks = scanRelinks(l.Vault(), renames)
	return p, nil
}

// newStem 은 DecisionPath 를 부르지 않고 새 stem 을 예측한다 (충돌 검사용).
func newStem(prefix, marker, slug, date string) string {
	return prefix + marker + store.Slugify(slug) + "-" + date
}

// slugDateOf 는 stem 에서 slug 와 날짜를 뽑는다. 규약과 다르면 빈 slug.
func slugDateOf(n store.Note, marker string) (slug, date string) {
	stem := n.Stem
	date = strings.TrimSpace(n.Meta.Date)
	switch {
	case date != "" && strings.HasSuffix(stem, "-"+date):
		stem = stem[:len(stem)-len(date)-1]
	default:
		m := stemDate.FindString(stem)
		if m == "" {
			return "", ""
		}
		date = strings.TrimPrefix(m, "-")
		stem = strings.TrimSuffix(stem, m)
	}
	i := strings.Index(stem, marker)
	if i <= 0 {
		return "", ""
	}
	return stem[i+len(marker):], date
}
