// Package index 는 전 프로젝트 결정을 한 표로 만든다.
package index

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xian0310567/casebook/internal/core/store"
)

const header = "| 날짜 | domain | summary | status | outcome | 링크 |\n" +
	"| --- | --- | --- | --- | --- | --- |\n"

// escapeCell 은 표 셀 안에서 파이프가 열을 쪼개지 않게 한다.
func escapeCell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}

// Build 는 색인 문서 전체와 그 안의 행(노트) 수를 만든다. 최신 날짜가 위로 온다.
//
// 행 수를 별도로 돌려주는 이유: 방출된 markdown 문자열을 다시 스캔해서
// (예: "\n| 2" 부분 문자열 개수) 세면 연대가 "2"로 시작하는(2000~2999년)
// 우연에 기대게 된다. 여기서는 이미 손에 쥔 notes 슬라이스의 길이를 그대로
// 돌려주므로 그런 가정이 아예 필요 없다.
func Build(l *store.Layout) ([]byte, int, error) {
	notes, err := l.List()
	if err != nil {
		return nil, 0, err
	}
	sort.SliceStable(notes, func(i, j int) bool {
		if notes[i].Meta.Date != notes[j].Meta.Date {
			return notes[i].Meta.Date > notes[j].Meta.Date
		}
		return notes[i].Stem < notes[j].Stem
	})

	var b strings.Builder
	b.WriteString("---\ntitle: 결정 색인\ntags: [index, decision]\n---\n\n")
	b.WriteString("# 결정 색인\n\n> 자동 생성된다. 직접 편집하지 마라 — `cb index` 가 덮어쓴다.\n\n")
	b.WriteString(header)
	for _, n := range notes {
		domain := "-"
		if len(n.Meta.Domain) > 0 {
			domain = n.Meta.Domain[0]
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s | %s | [[%s]] |\n",
			n.Meta.Date, domain, escapeCell(n.Meta.Summary),
			n.Meta.Status, n.Meta.Outcome, n.Stem)
	}
	return []byte(b.String()), len(notes), nil
}

// Write 는 색인을 디스크에 쓰고 행 수를 준다.
func Write(l *store.Layout) (int, error) {
	out, n, err := Build(l)
	if err != nil {
		return 0, err
	}
	p := l.IndexPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return 0, err
	}
	if err := os.WriteFile(p, out, 0o644); err != nil {
		return 0, err
	}
	return n, nil
}
