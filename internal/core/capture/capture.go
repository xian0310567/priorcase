// Package capture 는 결정 노트를 만들고 갱신한다.
// 볼트에 쓰는 유일한 경로이므로 스키마 검증이 여기를 통과해야만 한다.
package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/index"
	"github.com/xian0310567/casebook/internal/core/schema"
	"github.com/xian0310567/casebook/internal/core/search"
	"github.com/xian0310567/casebook/internal/core/store"
)

type Request struct {
	Domain        string
	Slug          string
	Summary       string
	Date          string // 비면 오늘
	Supersedes    string
	SourceSession string
	Tags          []string
	Related       []string
	Body          []byte
}

type Result struct {
	Path    string
	Related []search.Hit
}

// Do 는 결정 노트를 만들고 색인을 갱신한 뒤, 관련 과거 결정을 함께 준다.
// 관련 결정을 돌려주는 것이 "편승" 이다 — 기록하는 순간이 곧 결정하는 순간이므로
// 그때 과거 결정이 따라 나오는 것이 가장 정확한 타이밍이다.
func Do(l *store.Layout, c *config.Config, r Request) (Result, error) {
	if r.Date == "" {
		r.Date = time.Now().Format("2006-01-02")
	}
	path, err := l.DecisionPath(r.Domain, r.Slug, r.Date)
	if err != nil {
		return Result{}, err
	}
	stem := strings.TrimSuffix(filepath.Base(path), ".md")

	m := store.Meta{
		Type: "decision", Date: r.Date, Domain: []string{r.Domain},
		Summary: r.Summary, Status: "active", Outcome: "pending",
		Supersedes: r.Supersedes, Related: r.Related,
		Tags: ensureDecisionTag(r.Tags), SourceSession: r.SourceSession,
	}
	if err := schema.Validate(stem, m); err != nil {
		return Result{}, fmt.Errorf("스키마 검증 실패: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return Result{}, fmt.Errorf("같은 경로에 이미 결정이 있다: %s", l.RelPath(path))
	}

	// 편승: 쓰기 **전에** 검색한다 — 자기 자신이 결과에 끼지 않게
	related := search.Recall(l, c, r.Summary+" "+r.Slug,
		search.Options{CrossProject: true, Limit: 3, MinScore: 1})

	body := r.Body
	if len(body) == 0 {
		body = []byte("## 결정\n\n## 근거\n\n## 고려한 대안\n\n## 예상 리스크\n\n## 회고\n")
	}
	if err := l.Write(store.Note{Path: path, Stem: stem, Meta: m, Body: body}); err != nil {
		return Result{}, err
	}
	if _, err := index.Write(l); err != nil {
		return Result{}, fmt.Errorf("노트는 썼으나 색인 갱신에 실패했다: %w", err)
	}
	return Result{Path: path, Related: related}, nil
}

// ensureDecisionTag 는 decision 태그를 보장한다. 회수의 1차 구분자다.
func ensureDecisionTag(tags []string) []string {
	for _, t := range tags {
		if t == "decision" {
			return tags
		}
	}
	return append([]string{"decision"}, tags...)
}
