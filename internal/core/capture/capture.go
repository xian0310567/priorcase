// Package capture 는 결정 노트를 만들고 갱신한다.
// 볼트에 쓰는 유일한 경로이므로 스키마 검증이 여기를 통과해야만 한다.
package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/schema"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

type Request struct {
	Domain     string
	Slug       string
	Summary    string
	Date       string // 비면 오늘
	Supersedes []string
	// SupersedeReason 은 **왜 그 결정을 뒤집는가** 다. Supersedes 와 짝이다.
	//
	// 뒤집히는 **옛 노트**에 적힌다 — 새 노트가 아니다. 사유는 옛 결정의 성질이고,
	// 회수에 옛 노트가 올라오는 순간 그 자리에 이유가 있어야 읽는 쪽이 "이건 왜
	// 버렸지" 를 다시 파지 않는다. 자세한 배치 근거는 markOverturned 참고.
	//
	// 비어도 된다. 강제하면 아직 이 인자를 안 넘기는 호출부에서 뒤집기가 통째로 막힌다.
	SupersedeReason string
	SourceSession   string
	// Author 는 이 결정을 내린 사람이다. 비면 호출부가 설정·git 에서 정해 넣는다.
	//
	// **여기서 자동으로 채우지 않는다.** capture 는 core 이고, "지금 어느 디렉토리에서
	// 도는가" 는 어댑터가 아는 것이다 — core 가 cwd 를 짐작하면 훅·MCP·CLI 가 서로
	// 다른 답을 얻는다.
	Author  string
	Tags    []string
	Related []string
	Body    []byte
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
	// DroppedRelated 는 대상이 없어서 빼 버린 related 값이다 (relatedcheck.go).
	// **비어 있지 않으면 호출부가 반드시 알려야 한다** — 조용히 빼면 에이전트는
	// 링크를 걸었다고 믿고, 사람은 백링크가 없는 이유를 영영 모른다.
	DroppedRelated []DroppedLink
	// Skipped 는 볼트에서 **읽지 못한** 결정 노트다. 비어 있지 않으면 편승 검색이
	// 볼트 전체를 보지 못했다는 뜻이므로 호출부가 알려야 한다 — "관련 결정이 없다"
	// 와 "그 노트를 못 읽었다" 는 다르다.
	Skipped []store.SkippedNote
}

// Do 는 결정 노트를 만들고 관련 과거 결정을 함께 준다.
// 관련 결정을 돌려주는 것이 "편승" 이다 — 기록하는 순간이 곧 결정하는 순간이므로
// 그때 과거 결정이 따라 나오는 것이 가장 정확한 타이밍이다.
func Do(l *store.Layout, c *config.Config, r Request) (Result, error) {
	if r.Date == "" {
		r.Date = time.Now().Format("2006-01-02")
	}
	// **볼트를 여기서 정한다.** 도메인이 볼트를 고르는 규칙은 하나여야 하고,
	// capture 는 도메인을 이미 알고 있다 — 호출부에 맡기면 훅·CLI·MCP 가 서로
	// 다른 볼트에 쓸 수 있고, 그 어긋남은 파일이 엉뚱한 자리에 생긴 뒤에야 드러난다.
	l, err := l.For(r.Domain)
	if err != nil {
		return Result{}, err
	}
	path, err := l.DecisionPath(r.Domain, r.Slug, r.Date)
	if err != nil {
		return Result{}, err
	}
	stem := strings.TrimSuffix(filepath.Base(path), ".md")

	// **링크는 여기서 한 번 접는다.** 예전에는 받은 문자열을 그대로 frontmatter 에
	// 넣었다 — supersedes 가 겪은 것과 같은 사고다(supersede.go 주석의 실측 셋).
	// 맨 stem 은 옵시디언이 링크로 안 읽고, 경로 조각은 ResolveStem 이 막아 둔
	// 경로 순회를 그대로 우회한다.
	relatedLinks, droppedRelated, err := resolveRelated(l, r.Related)
	if err != nil {
		return Result{}, err
	}

	m := store.Meta{
		Type: "decision", Date: r.Date, Author: r.Author, Domain: []string{r.Domain},
		Summary: r.Summary, Status: "active", Outcome: "pending",
		Related: relatedLinks,
		Tags:    ensureDecisionTag(r.Tags), SourceSession: r.SourceSession,
	}

	// --supersedes 는 prior review 와 같은 로직(supersedeAll)을 탄다 — 대상 검증,
	// "[[stem]]" 형식, 옛 노트의 status·related·번복 사유 갱신이 두 명령에서 동일하다.
	//
	// 날짜는 r.Date 를 넘긴다 — 뒤집는 결정이 내려진 날이 곧 번복이 일어난 날이다.
	// (review 는 노트 date 가 과거일 수 있어 오늘을 쓴다. reviewDate 주석 참고.)
	supLinks, olds, err := supersedeAll(l, r.Supersedes, stem, r.SupersedeReason, r.Date)
	if err != nil {
		return Result{}, err
	}
	m.Supersedes = supLinks

	// Review 와 같은 불변식: **모든** 노트를 검증한 뒤에야 쓰기 시작한다.
	// 새 노트 검증이 실패했는데 옛 노트가 이미 superseded 로 바뀌어 있으면,
	// 뒤집은 결정은 없는데 옛 결정만 뒤집힌 반쪽 상태가 디스크에 남는다.
	// 대상이 여럿이면 그 위험도 여럿이므로, 하나라도 실패하면 아무것도 안 쓴다.
	for _, old := range olds {
		if err := refuseFutureNote(old); err != nil {
			return Result{}, err
		}
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
	// **건너뜀 목록은 여기서 받는다.** 예전에는 버리고(`_`) 색인 갱신이 주는 것을
	// 썼는데, 색인을 없앤 뒤로 이 호출이 볼트를 훑는 유일한 자리다. 버리면
	// "읽지 못한 노트가 있다" 를 아무도 말하지 않게 된다.
	related, skipped, relatedErr := search.Recall(l, c, r.Summary+" "+r.Slug,
		search.Options{CrossProject: true, Limit: 3, MinScore: 1})

	body := r.Body
	if len(body) == 0 {
		body = []byte(l.Lang().T(
			"## 결정\n\n## 근거\n\n## 고려한 대안\n\n## 예상 리스크\n\n## 회고\n",
			"## Decision\n\n## Rationale\n\n## Alternatives considered\n\n## Risks\n\n## Retrospective\n"))
	}
	for _, old := range olds {
		if err := l.Write(old); err != nil {
			return Result{}, err
		}
	}
	if err := l.Write(store.Note{Path: path, Stem: stem, Meta: m, Body: body}); err != nil {
		return Result{}, err
	}
	return Result{Path: path, Related: related, RelatedErr: relatedErr, Skipped: skipped,
		DroppedRelated: droppedRelated}, nil
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
	lang := l.Lang()
	stems, err := l.DecisionStems(prefix)
	if err != nil {
		return err
	}
	key := slugKey(stem)
	for _, s := range stems {
		if slugKey(s) == key {
			// **이 에러는 에이전트가 읽고 행동을 바꾸는 지시문이다.** MCP capture 실패
			// 텍스트로 그대로 올라가므로(tools.go 가 err 를 되돌려준다) 다른 에러
			// 메시지와 달리 국제화 대상이다.
			return fmt.Errorf(lang.T(
				"유사한 결정이 이미 있다: %q (하이픈·공백·밑줄·대소문자만 다르다). "+
					"뒤집는 결정이면 --supersedes 를 쓰고, 정말 다른 결정이면 slug 를 구별되게 바꿔라",
				"A near-duplicate decision already exists: %q (it differs only in hyphens, spaces, "+
					"underscores, or case). If this overturns it, pass --supersedes; if it is genuinely "+
					"a different decision, change the slug so the two are distinguishable."), s)
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
