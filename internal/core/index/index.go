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

// Result 는 색인 생성의 결과다.
//
// Rows 만 돌려주던 시절에는 "47행 생성" 이 완전한 색인인지 6건을 흘린 색인인지
// 구별할 수단이 호출자에게 없었다. Skipped 가 그 자리를 채운다 — 비어 있지
// 않으면 색인은 불완전하고, 호출자는 그 사실을 사용자에게 반드시 알려야 한다.
type Result struct {
	// Rows 는 색인 표에 실제로 들어간 행(노트) 수다.
	Rows int
	// Skipped 는 읽지 못해 색인에서 빠진 결정 노트다. 경로 오름차순.
	Skipped []store.SkippedNote
}

// Build 는 색인 문서 전체와 그 결과(행 수·건너뛴 노트)를 만든다.
// 최신 날짜가 위로 온다.
//
// 행 수를 방출된 markdown 에서 세지 않고 별도로 돌려주는 이유: 문자열을 다시
// 스캔해서(예: "\n| 2" 부분 문자열 개수) 세면 연대가 "2"로 시작하는(2000~2999년)
// 우연에 기대게 된다. 여기서는 이미 손에 쥔 notes 슬라이스의 길이를 그대로
// 돌려주므로 그런 가정이 아예 필요 없다.
func Build(l *store.Layout) ([]byte, Result, error) {
	notes, skipped, err := l.List()
	if err != nil {
		return nil, Result{}, err
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
	return []byte(b.String()), Result{Rows: len(notes), Skipped: skipped}, nil
}

// Write 는 색인을 디스크에 쓰고 결과를 준다.
//
// 건너뛴 노트가 있어도 쓰기는 한다 — 47행짜리 색인이 색인 없음보다는 낫다.
// 대신 Result.Skipped 로 불완전함을 알린다. 에러로 바꾸지 않는 이유는
// cli/index.go 의 주석에 적었다.
func Write(l *store.Layout) (Result, error) {
	out, res, err := Build(l)
	if err != nil {
		return Result{}, err
	}
	p := l.IndexPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return Result{}, err
	}
	// WriteFileAtomic 을 쓴다 — os.WriteFile 은 기존 파일을 먼저 비운 뒤 쓰기
	// 때문에 중간에 실패하면 색인이 잘린 채로 남는다.
	if err := store.WriteFileAtomic(p, out, 0o644); err != nil {
		return Result{}, err
	}
	return res, nil
}
