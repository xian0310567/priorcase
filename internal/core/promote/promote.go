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

	"github.com/xian0310567/priorcase/internal/core/capture"
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/judge"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/core/worklog"
)

// Result 는 승격 시도 하나의 결과다.
type Result struct {
	ID string // 대상 pending 의 id
	// Recorded 는 어딘가에 남았다는 뜻이다. **어디에 남았는지는 Tier 를 봐라** —
	// 결정 노트와 작업 로그는 무게가 다르다.
	Recorded bool
	Tier     judge.Tier
	Path     string // 만들어진 노트 또는 덧붙인 작업 로그 (Recorded 일 때)
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
	// Scope 는 이 발췌가 대화의 한 토막인지 아크 전체인지다.
	// 비면 judge.ScopeMid — 도중 판정이고, 결정 노트는 나오지 않는다.
	Scope judge.Scope
	// Worklog 는 이 세션에서 이미 작업 로그에 쌓인 항목 제목들이다.
	// ScopeEnd 판정에서 발췌가 못 담은 앞부분을 여기가 대신한다.
	Worklog []string
	// Author 는 이 구간의 대화를 한 사람이다.
	//
	// **판별기가 노트를 쓰지만 결정을 내린 것은 사람이다.** 여기가 비면 ③ 이 만든
	// 노트만 author 가 없어서, 팀이 "누가 정했나" 를 물을 때 하필 자동 기록된 것들이
	// 통째로 답을 못 한다 — 그게 전체의 절반일 수 있다.
	//
	// core 는 cwd 를 모르므로 어댑터가 채워 넣는다 (§4.1).
	Author string
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
		Scope:    s.Scope,
		Existing: existingSummaries(l, s.Domain),
		Worklog:  s.Worklog,
	})
	if err != nil {
		return Result{ID: s.ID, Err: err}
	}
	// **판별기는 인터페이스다.** CLI 구현은 parse 가 등급을 채워 주지만, 다른 구현은
	// Verdict 를 직접 만들어 준다. Tier 가 빈 채로 오면 아래 switch 가 "알 수 없는
	// 등급" 으로 떨어뜨려 판정을 통째로 버린다 — 실제로 그렇게 깨졌다.
	v = v.Normalized()

	switch v.Tier {
	case judge.TierNone:
		return Result{ID: s.ID, Tier: judge.TierNone, Reason: v.Reason}

	case judge.TierWorklog:
		return toWorklog(l, s, v)

	case judge.TierDecision:
		// **도중 판정에서 온 decision 은 작업 로그로 내린다.**
		//
		// 지시문이 도중에는 decision 형식을 아예 안 보여 주지만, 모델 출력은
		// 우리가 통제하지 못한다. 여기서 막지 않으면 아크가 안 끝난 창에서
		// 결정 노트가 나오고, 그건 정확히 옛 실패의 거울상이다 — 예전엔 아무것도
		// 안 남았고 이번엔 파편이 잔뜩 남는다.
		//
		// 버리지는 않는다. 판별기가 남길 값어치가 있다고 봤으니 등급만 내린다.
		if s.Scope != judge.ScopeEnd {
			v.Reason = "도중 판정이라 결정 노트 대신 작업 로그로 내렸다"
			return toWorklog(l, s, v)
		}
		return toDecision(l, c, s, v)
	}
	return Result{ID: s.ID, Tier: judge.TierNone,
		Reason: fmt.Sprintf("알 수 없는 등급: %q", v.Tier)}
}

// toDecision 은 결정 노트를 만든다.
//
// **쓰기는 반드시 capture.Do 를 거친다** (이 패키지 주석 참고).
func toDecision(l *store.Layout, c *config.Config, s Segment, v judge.Verdict) Result {
	res, err := capture.Do(l, c, capture.Request{
		Author:        s.Author,
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
		//
		// **다만 버리지는 않는다.** 판별기가 결정 등급을 줬다는 것은 새 내용이
		// 있다고 봤다는 뜻이고, 유사 slug 는 파일명이 겹친다는 것일 뿐이다.
		// 작업 로그로 내려 두면 사람이 나중에 둘을 합칠 수 있다 — 예전에는
		// 여기서 조용히 사라졌다.
		if strings.Contains(err.Error(), "유사한 결정이 이미 있다") {
			v.Reason = err.Error()
			r := toWorklog(l, s, v)
			if r.Err == nil {
				r.Reason = err.Error()
			}
			return r
		}
		return Result{ID: s.ID, Err: err}
	}
	return Result{ID: s.ID, Recorded: true, Tier: judge.TierDecision, Path: res.Path}
}

// toWorklog 는 작업 로그에 항목을 덧붙인다.
func toWorklog(l *store.Layout, s Segment, v judge.Verdict) Result {
	res, err := worklog.Append(l, worklog.Entry{
		Domain:  s.Domain,
		Date:    s.Date,
		Title:   v.Summary,
		Body:    v.Body,
		Session: s.Session,
		Source:  s.ID,
		Tags:    v.Tags,
	})
	if err != nil {
		return Result{ID: s.ID, Err: err}
	}
	if res.Skipped {
		// 같은 구간이 이미 쌓여 있다. 데몬이 같은 구간을 다시 훑는 것은 정상이므로
		// 실패가 아니다 — 호출자가 그 구간을 지울 수 있어야 한다.
		return Result{ID: s.ID, Tier: judge.TierWorklog, Path: res.Path,
			Reason: "이미 작업 로그에 있다"}
	}
	return Result{ID: s.ID, Recorded: true, Tier: judge.TierWorklog,
		Path: res.Path, Reason: v.Reason}
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
