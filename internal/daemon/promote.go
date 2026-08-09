package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/judge"
	"github.com/xian0310567/casebook/internal/core/promote"
	"github.com/xian0310567/casebook/internal/core/store"
)

// PromoteOptions 는 승격 한 판에 필요한 것이다.
type PromoteOptions struct {
	StateDir string
	Config   *config.Config
	Layout   *store.Layout
	// First 는 먼저 처리할 transcript 경로다. 비면 순서를 안 바꾼다.
	//
	// 훅은 자기 세션을 먼저 한다 — 세션이 끝나는 것이 *그* 대화의 마지막 기회이기
	// 때문이다. 데몬은 특정 세션의 편을 들 이유가 없으므로 비운다.
	First string
	// Budget 은 이번 판에 쓸 수 있는 총 시간이다. 0 이면 DefaultPromoteBudget.
	Budget time.Duration
	// Err 는 사람에게 보이는 진행 보고다. nil 이면 버린다.
	Err io.Writer
	// Label 은 보고 줄 앞에 붙는 이름이다 ("cb hook session-end" 같은).
	Label string
}

// DefaultPromoteBudget 은 승격 한 판의 총 시간이다.
//
// **judge.DefaultTimeout < 이 값** 이어야 한다. 판별기 한 건이 예산보다 길면 그 한
// 건이 예산을 통째로 먹어 두 번째 구간이 영영 안 돈다. arch 테스트가 강제한다.
const DefaultPromoteBudget = 90 * time.Second

// Promote 는 표시된 구간을 판별기에 넘겨 결정 노트로 승격한다.
//
// **훅과 데몬이 같은 코드를 쓴다.** 승격은 볼트에 쓰는 일이고, 쓰기 경로가 둘로
// 갈라지면 한쪽만 고쳐진 채로 남는다 — 이 프로젝트가 죄목으로 드는 바로 그것이다.
// 훅은 `First` 로 자기 세션을 앞세우고, 데몬은 비운다. 그 차이뿐이다.
//
// 판별기가 없으면 아무것도 하지 않는다. 그때는 표시만 남고 에이전트가 판단한다 —
// 그것이 판별기 없는 설치의 정상 동작이고 경고를 낼 일이 아니다.
//
// 재귀 차단: 판별기가 띄운 세션에도 훅이 붙는다. CASEBOOK_JUDGE 가 있으면 그
// 세션이므로 즉시 끝낸다. 안 그러면 판별기가 판별기를 부른다.
func Promote(ctx context.Context, o PromoteOptions) {
	if os.Getenv("CASEBOOK_JUDGE") == "1" {
		return
	}
	j := judge.Find(o.Config.Capture.JudgePath, o.Config.Capture.JudgeModel)
	if j == nil {
		return
	}
	items, err := ReadPending(o.StateDir)
	if err != nil || len(items) == 0 {
		return
	}
	items = ownFirst(items, o.First)

	budget := o.Budget
	if budget <= 0 {
		budget = DefaultPromoteBudget
	}
	deadline := time.Now().Add(budget)

	for _, p := range items {
		if time.Now().After(deadline) {
			o.report("시간이 다 돼 나머지는 다음 기회에 넘긴다")
			return
		}
		if p.Domain == "" {
			// 조용히 넘기면 "안전망이 도는데 아무것도 안 남는" 상태가 된다.
			// 새 사용자가 정확히 이 상태에 빠진다 — 설정에 도메인이 없으면 그렇다.
			o.report(fmt.Sprintf("도메인을 알 수 없어 기록하지 못했다 (%s) — "+
				"설정에 [[domain]] 을 추가하거나 default_domain 을 적어라", p.ID()))
			continue
		}
		day := p.When()
		if i := strings.Index(day, "~"); i > 0 {
			day = day[:i] // 여러 날에 걸쳤으면 첫날로
		}

		// **집어 간다.** 승격이 스캔 소유권 밖으로 나온 뒤로(D12) 훅과 데몬이 동시에
		// 같은 구간을 집을 수 있다 — 판별기는 비결정적이라 같은 대화에 slug 가 다른
		// 결정 노트가 둘 생긴다.
		if ok, cerr := ClaimPending(o.StateDir, p.ID(), time.Now().UTC()); cerr != nil || !ok {
			continue
		}
		r := promote.One(ctx, j, o.Layout, o.Config, promote.Segment{
			ID: p.ID(), Domain: p.Domain, Date: day, Excerpt: p.Excerpt, Session: p.SessionID,
		})

		// **세 갈래 전부 원장에 남긴다.** 진행 보고는 사람이 그 순간 보지 않으면
		// 사라지고, 표시는 곧 해소돼 지워진다. 원장이 없으면 "판별기가 보고 기록할
		// 게 아니라고 했다" 와 "판별기가 아예 안 돌았다" 가 같아 보인다.
		rec := Promotion{
			At: time.Now().UTC(), ID: p.ID(), Domain: p.Domain,
			Recorded: r.Recorded, Reason: TrimLedgerText(r.Reason),
		}
		if r.Path != "" {
			rec.Path = o.Layout.RelPath(r.Path)
		}
		if r.Err != nil {
			rec.Err = TrimLedgerText(r.Err.Error())
		}
		if lerr := AppendPromotion(o.StateDir, rec); lerr != nil {
			o.report(fmt.Sprintf("승격 원장을 쓰지 못했다: %v", lerr))
		}

		switch {
		case r.Err != nil:
			o.report(fmt.Sprintf("승격 실패 (%s): %v", p.ID(), r.Err))
			// 실패한 구간은 지우지 않는다 — 다음 기회에 다시 시도한다.
		case r.Recorded:
			o.report("자동 기록 " + o.Layout.RelPath(r.Path))
			// **방금 만든 노트를 그 자리에서 소모시킨다.** 안 그러면 다음 스캔이 이
			// 노트를 "새로 생겼다" 로 세어 아직 아무도 안 본 다음 구간을 면제한다.
			if cerr := CreditNoteFor(o.StateDir, p.Path, day, p.SessionID); cerr != nil {
				o.report(fmt.Sprintf("크레딧을 새기지 못했다: %v", cerr))
			}
			_ = ResolvePending(o.StateDir, p.ID())
		default:
			// 기록할 결정이 아니라는 판정. 표시를 지운다 — 안 지우면 매 세션 다시 묻는다.
			o.report(fmt.Sprintf("기록 안 함 (%s): %s", p.ID(), r.Reason))
			_ = ResolvePending(o.StateDir, p.ID())
		}
	}
}

func (o PromoteOptions) report(msg string) {
	if o.Err == nil {
		return
	}
	if o.Label != "" {
		fmt.Fprintf(o.Err, "%s: %s\n", o.Label, msg)
		return
	}
	fmt.Fprintln(o.Err, msg)
}

// ownFirst 는 주어진 transcript 의 구간을 앞으로 옮긴다. 순서만 바꾸고 버리지 않는다.
func ownFirst(items []Pending, transcript string) []Pending {
	if transcript == "" {
		return items
	}
	out := make([]Pending, 0, len(items))
	for _, p := range items {
		if p.Path == transcript {
			out = append(out, p)
		}
	}
	for _, p := range items {
		if p.Path != transcript {
			out = append(out, p)
		}
	}
	return out
}
