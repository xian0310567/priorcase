// Package promote 는 표시된 구간을 판별기에 넘겨 결정 노트로 승격한다.
//
// **쓰기는 반드시 `capture.Do` 를 거친다.** 판별기가 준 값을 직접 파일로 쓰면 스키마
// 검증·유사 slug 거부·색인 갱신을 우회하게 되고, 그건 이 프로젝트가 죄목으로 드는
// "쓰기 경로가 둘로 갈라진다" 그 자체다 (스펙 §4.1). 판별기는 **무엇을 쓸지만** 정하고
// 어떻게 쓰는지는 core 가 정한다.
package promote

import (
	"context"
	"fmt"
	"strings"

	"github.com/xian0310567/casebook/internal/core/capture"
	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/judge"
	"github.com/xian0310567/casebook/internal/core/store"
)

// Result 는 승격 시도 하나의 결과다.
type Result struct {
	ID       string // 대상 pending 의 id
	Recorded bool
	Path     string // 만들어진 노트 (Recorded 일 때)
	Reason   string // 안 만든 이유 (판별기가 준 것)
	Err      error
}

// Segment 는 승격 대상이다. daemon.Pending 을 그대로 받지 않는 이유는 core 가
// daemon 을 모르기 때문이다 (§4.1) — 어댑터가 옮겨 담는다.
type Segment struct {
	ID      string
	Domain  string
	Date    string
	Excerpt string
	Session string
}

// One 은 구간 하나를 승격한다.
//
// 판별기가 record=false 를 주면 **그것도 성공이다** — 기록할 결정이 아니라는 판정이
// 나온 것이고, 호출자는 그 구간을 지워도 된다. 에러는 판별기를 못 불렀을 때만이다.
func One(ctx context.Context, j judge.Judge, l *store.Layout, c *config.Config, s Segment) Result {
	if j == nil {
		return Result{ID: s.ID, Err: fmt.Errorf("판별기가 없다")}
	}
	if strings.TrimSpace(s.Excerpt) == "" {
		return Result{ID: s.ID, Reason: "발췌가 비었다"}
	}
	if s.Domain == "" {
		// 도메인을 모르면 어디에 쓸지 정할 수 없다. 판별기에 물어봐야 소용없다.
		return Result{ID: s.ID, Reason: "도메인을 알 수 없다"}
	}
	v, err := j.Decide(ctx, judge.Request{
		Excerpt:  s.Excerpt,
		Domain:   s.Domain,
		Date:     s.Date,
		Existing: existingSummaries(l, s.Domain),
	})
	if err != nil {
		return Result{ID: s.ID, Err: err}
	}
	if !v.Record {
		return Result{ID: s.ID, Reason: v.Reason}
	}

	res, err := capture.Do(l, c, capture.Request{
		Domain:        s.Domain,
		Slug:          v.Slug,
		Summary:       v.Summary,
		Date:          s.Date,
		SourceSession: s.Session,
		Tags:          v.Tags,
		Body:          []byte(v.Body),
	})
	if err != nil {
		// 유사 slug 거부도 여기로 온다 — 그건 실패가 아니라 "이미 있다" 는 뜻이라
		// 이유로 옮긴다. 호출자가 그 구간을 지울 수 있어야 한다.
		if strings.Contains(err.Error(), "유사한 결정이 이미 있다") {
			return Result{ID: s.ID, Reason: err.Error()}
		}
		return Result{ID: s.ID, Err: err}
	}
	return Result{ID: s.ID, Recorded: true, Path: res.Path}
}

// existingSummaries 는 그 도메인의 기존 결정 요약이다. 판별기가 중복을 거르는 데 쓴다.
//
// 읽기 실패는 조용히 넘긴다 — 중복 판정 재료가 없을 뿐이고, 그것 때문에 승격 자체를
// 막으면 볼트가 잠깐 안 읽힌다고 기록이 통째로 멎는다.
func existingSummaries(l *store.Layout, domain string) []string {
	notes, _, err := l.List()
	if err != nil {
		return nil
	}
	var out []string
	for _, n := range notes {
		for _, d := range n.Meta.Domain {
			if d == domain && n.Meta.Summary != "" {
				out = append(out, n.Meta.Summary)
				break
			}
		}
	}
	// 너무 많으면 판별기 입력이 커진다. 최근 것이 중복 판정에 더 쓸모 있다.
	if len(out) > 30 {
		out = out[len(out)-30:]
	}
	return out
}
