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
	// RelatedErr 는 편승 검색이 실패한 원인이다.
	//
	// 기록은 성공했는데 편승 검색만 실패한 경우 기록까지 실패시키지는 않는다 —
	// 노트는 이미 디스크에 있고, 실패한 것은 곁다리 정보뿐이다. 대신 조용히
	// 넘어가지도 않는다: 호출부가 이 값을 사용자에게 알려야 "관련 결정이 없다"
	// 와 "찾아보지 못했다" 가 구별된다.
	RelatedErr error
	// Skipped 는 색인 갱신에서 읽지 못해 빠진 결정 노트다. 비어 있지 않으면
	// 방금 쓴 노트는 색인에 들어갔지만 색인 자체는 불완전하다 — 호출부가
	// 알려야 한다.
	Skipped []store.SkippedNote
	// IndexPreserved 는 색인 자리에 있던 남의 파일을 대피시킨 경로다.
	// 비어 있지 않으면 호출자가 **반드시** 사용자에게 알려야 한다.
	IndexPreserved string
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
		Related: r.Related,
		Tags:    ensureDecisionTag(r.Tags), SourceSession: r.SourceSession,
	}

	// --supersedes 는 cb review 와 같은 로직(supersede)을 탄다 — 대상 검증,
	// "[[stem]]" 형식, 옛 노트의 status·related 갱신이 두 명령에서 동일하다.
	var old store.Note
	hasOld := false
	if r.Supersedes != "" {
		link, o, err := supersede(l, r.Supersedes, stem)
		if err != nil {
			return Result{}, err
		}
		m.Supersedes, old, hasOld = link, o, true
	}

	// Review 와 같은 불변식: 두 노트를 모두 검증한 뒤에야 쓰기 시작한다.
	// 새 노트 검증이 실패했는데 옛 노트가 이미 superseded 로 바뀌어 있으면,
	// 뒤집은 결정은 없는데 옛 결정만 뒤집힌 반쪽 상태가 디스크에 남는다.
	if hasOld {
		if err := schema.Validate(l.DecisionMarker(), old.Stem, old.Meta); err != nil {
			return Result{}, fmt.Errorf("옛 노트 검증 실패: %w", err)
		}
	}
	if err := schema.Validate(l.DecisionMarker(), stem, m); err != nil {
		return Result{}, fmt.Errorf("스키마 검증 실패: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return Result{}, fmt.Errorf("같은 경로에 이미 결정이 있다: %s", l.RelPath(path))
	}
	if err := checkNearDuplicate(l, r.Domain, stem); err != nil {
		return Result{}, err
	}

	// 편승: 쓰기 **전에** 검색한다 — 자기 자신이 결과에 끼지 않게.
	// 여기서 실패해도 기록은 계속한다. 대신 원인을 Result 에 실어 보낸다.
	//
	// 여기서 나온 건너뜀 목록은 버린다(`_`). 바로 아래 index.Write 가 같은
	// l.List() 를 같은 볼트에 다시 돌려 같은 목록을 더 최신 상태로 주기
	// 때문이다 — 둘 다 실으면 사용자에게 같은 경고가 두 번 나간다.
	related, _, relatedErr := search.Recall(l, c, r.Summary+" "+r.Slug,
		search.Options{CrossProject: true, Limit: 3, MinScore: 1})

	body := r.Body
	if len(body) == 0 {
		body = []byte("## 결정\n\n## 근거\n\n## 고려한 대안\n\n## 예상 리스크\n\n## 회고\n")
	}
	if hasOld {
		if err := l.Write(old); err != nil {
			return Result{}, err
		}
	}
	if err := l.Write(store.Note{Path: path, Stem: stem, Meta: m, Body: body}); err != nil {
		return Result{}, err
	}
	idx, err := index.Write(l)
	if err != nil {
		return Result{}, fmt.Errorf("노트는 썼으나 색인 갱신에 실패했다: %w", err)
	}
	return Result{Path: path, Related: related, RelatedErr: relatedErr,
		Skipped: idx.Skipped, IndexPreserved: idx.Preserved}, nil
}

// slugKey 는 유사 slug 비교용 정규화 키다. 하이픈·공백·밑줄을 접고 대소문자를
// 무시한다 — "세번째" 와 "세-번째", "Retry-Policy" 와 "retry_policy" 가 같은
// 키가 된다.
func slugKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(store.NFC(s)) {
		switch r {
		case '-', '_', ' ', '\t':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// checkNearDuplicate 는 하이픈·대소문자만 다른 결정이 이미 있는지 본다
// (감사 결함 4, 스펙 §11 "유사 slug 중복 생성 시도 → 거부").
//
// 예전에는 중복 검사가 os.Stat 하나뿐이라 --slug 세번째 와 --slug 세-번째 가
// 둘 다 통과했다 — 같은 결정이 두 노트로 갈라지면 회수가 둘 다 물어오고
// 어느 쪽이 정본인지 알 수 없게 된다.
//
// 비교는 한 도메인의 결정 폴더 안에서만 한다. stem 에 날짜가 들어 있으므로
// 날짜가 다른 결정은 키가 달라져 자동으로 빠진다 — 전 볼트를 훑을 필요가 없다.
func checkNearDuplicate(l *store.Layout, prefix, stem string) error {
	stems, err := l.DecisionStems(prefix)
	if err != nil {
		return err
	}
	key := slugKey(stem)
	for _, s := range stems {
		if slugKey(s) == key {
			return fmt.Errorf("유사한 결정이 이미 있다: %q (하이픈·공백·밑줄·대소문자만 다르다). "+
				"뒤집는 결정이면 --supersedes 를 쓰고, 정말 다른 결정이면 slug 를 구별되게 바꿔라", s)
		}
	}
	return nil
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
