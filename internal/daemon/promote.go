package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/judge"
	"github.com/xian0310567/priorcase/internal/core/promote"
	"github.com/xian0310567/priorcase/internal/core/store"
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
	// Only 는 이 구간 하나만 처리한다. 비면 전부 돈다.
	//
	// **필터로 둔 이유는 쓰기 경로를 하나로 유지하기 위해서다.** 집기(ClaimPending)·
	// 원장·크레딧·해소가 전부 아래 루프 안에 있다. 한 건짜리 함수를 따로 만들면
	// 그 부기가 두 벌이 되고, 한쪽만 고쳐진 채로 남는다 — 이 프로젝트가 죄목으로
	// 드는 바로 그것이다.
	Only string
	// Author 는 승격된 노트에 박을 사람이다. 비면 설정·git 신원에서 정한다.
	//
	// 훅은 자기 세션의 cwd 를 알고, 데몬은 모른다. 그래서 호출부가 정해 넘긴다 —
	// 여기서 짐작하면 훅과 데몬이 같은 구간에 다른 답을 쓴다.
	Author string
	// OnResult 는 구간 하나가 끝날 때마다 그 결과를 준다. nil 이면 안 부른다.
	//
	// 원장에도 같은 것이 남지만, 호출자가 **이번 호출의 결과**를 알려면 되읽어야
	// 하고 그건 다른 프로세스가 그 사이에 쓴 것과 섞인다. 앱의 [결정이다] 는
	// "방금 내가 누른 것이 어떻게 됐나" 를 알아야 한다.
	OnResult func(Promotion)
	// Err 는 사람에게 보이는 진행 보고다. nil 이면 버린다.
	Err io.Writer
	// Label 은 보고 줄 앞에 붙는 이름이다 ("prior hook session-end" 같은).
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
// 재귀 차단: 판별기가 띄운 세션에도 훅이 붙는다. PRIORCASE_JUDGE 가 있으면 그
// 세션이므로 즉시 끝낸다. 안 그러면 판별기가 판별기를 부른다.
func Promote(ctx context.Context, o PromoteOptions) {
	if os.Getenv("PRIORCASE_JUDGE") == "1" {
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
	if o.Only != "" {
		var one []Pending
		for _, p := range items {
			if p.ID() == o.Only {
				one = append(one, p)
			}
		}
		if len(one) == 0 {
			// **조용히 아무것도 안 하면 안 된다.** 앱의 [결정이다] 가 이걸 부르는데,
			// 그 구간이 이미 해소됐거나 id 가 틀렸을 때 사람은 눌렀는데 아무 일도
			// 안 일어난 것으로 본다.
			o.report(fmt.Sprintf("그런 구간이 없다: %s (이미 처리됐거나 id 가 다르다)", o.Only))
			return
		}
		items = one
	}
	items = ownFirst(items, o.First)

	budget := o.Budget
	if budget <= 0 {
		budget = DefaultPromoteBudget
	}
	// 한 번만 정한다 — 구간마다 다시 구하면 파일을 그만큼 더 읽는다.
	author := o.Author
	if author == "" {
		author = o.Config.AuthorFor("")
	}

	deadline := time.Now().Add(budget)

	for _, p := range items {
		// **취소를 예산보다 먼저 본다.** 예산은 우리가 정한 것이고 취소는 호스트가
		// 하는 것이라, 취소가 예산보다 먼저 올 수 있다 — 훅은 SessionEnd 에서
		// 120초 상한 아래 도는데 그전에 죽을 수 있고, 데몬은 종료 신호를 받는다.
		//
		// 이걸 안 보면 남은 구간이 **전부 즉시 실패로 기록된다.** 판별기 호출이
		// 취소된 컨텍스트에서 곧장 에러를 내기 때문이다. 실측으로 그 상태였다 —
		// 원장 62건 중 52건이 이 인공물이었고, 같은 초에 20건·17건씩 뭉쳐 있었다.
		//
		// 그리고 그냥 소음이 아니다. 실패 **전에** ClaimPending 이 이미 도장을
		// 찍으므로, 아무 일도 안 한 구간이 claimTTL(5분) 동안 건너뛰어진다.
		// 미확인 구간이 안 줄어드는 이유가 이것이었다.
		if err := ctx.Err(); err != nil {
			o.report("중단됐다 — 남은 구간은 다음 기회에 넘긴다")
			return
		}
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
		// **설정에 없는 도메인은 판별기를 부르기 전에 걸러낸다.**
		//
		// 안 그러면 판별기를 부르고 나서 capture 가 "알 수 없는 도메인 접두어" 로
		// 거부한다 — 호출 한 번(실측 10~45초)이 통째로 버려지고, 그 구간은 다음에도
		// 같은 일을 반복한다. 예산이 있으므로 그 낭비가 **다른 구간의 기회를 먹는다.**
		//
		// 실제로 그 상태다. 2026-08-12 개명(casebook → priorcase) 때 이미 표시돼
		// 있던 구간의 도메인을 안 옮겼고, 그래서 옛 이름을 단 구간 7건이 큐에 남았다.
		if _, ok := o.Config.FolderFor(p.Domain); !ok {
			o.report(fmt.Sprintf("설정에 없는 도메인이라 건너뛴다 (%s): %q — "+
				"이름을 바꿨다면 prior pending --resolve 로 지우거나 설정에 추가하라",
				p.ID(), p.Domain))
			continue
		}
		day := p.DecidedOn()

		// **집어 간다.** 승격이 스캔 소유권 밖으로 나온 뒤로(D12) 훅과 데몬이 동시에
		// 같은 구간을 집을 수 있다 — 판별기는 비결정적이라 같은 대화에 slug 가 다른
		// 결정 노트가 둘 생긴다.
		if ok, cerr := ClaimPending(o.StateDir, p.ID(), time.Now().UTC()); cerr != nil || !ok {
			// **지목해서 부른 경우에는 조용히 넘어가면 안 된다.**
			//
			// 전체를 도는 중이라면 남이 집어 간 구간을 건너뛰는 것이 맞다. 그런데
			// Only 로 그 하나를 지목했다면 호출자는 그것이 처리되기를 기다린다 —
			// 조용히 끝나면 "그런 구간이 없다" 로 보이고, 실제 원인(방금 실패한
			// 시도의 도장이 claimTTL 동안 남아 있다)이 안 드러난다.
			//
			// 실측으로 그 상태를 만났다: 판별기가 죽은 뒤 바로 재시도하니 5분간
			// "구간이 없다" 고 나왔다.
			if o.Only != "" {
				o.report(fmt.Sprintf("이미 처리 중인 구간이다 (%s) — "+
					"조금 전 시도가 %v 동안 선점하고 있다. 그 뒤에 다시 하라",
					p.ID(), claimTTL))
			}
			continue
		}
		r := promote.One(ctx, j, o.Layout, o.Config, promote.Segment{
			ID: p.ID(), Domain: p.Domain, Date: day, Excerpt: p.Excerpt,
			Session: p.SessionID, Author: author,
		})

		// **중단은 실패가 아니다.** 호출 도중에 취소되면 판별기는 컨텍스트 에러를
		// 내는데, 그걸 "판별기 실패" 로 남기면 원장이 거짓말을 한다 — 판별기는
		// 멀쩡했고 우리가 시간을 못 준 것뿐이다. 실측에서 이 구별이 없어 원장 62건
		// 중 52건이 "판별기 실행 실패" 로 보였고, 그것 때문에 판별기가 고장 났다고
		// 오진했다. 원장의 존재 이유가 바로 이 구별이다.
		if r.Err != nil && ctx.Err() != nil {
			o.report("중단됐다 — 남은 구간은 다음 기회에 넘긴다")
			return
		}

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
		} else {
			// 실패가 아니면 이 구간은 곧 해소된다 — 발췌를 여기 남기지 않으면
			// 사라진다. 같은 상한(TrimLedgerText)을 쓴다: 한 줄이 스캐너 상한을
			// 넘으면 그 줄만이 아니라 **그 뒤가 통째로 안 읽힌다.**
			rec.Excerpt = TrimLedgerText(p.Excerpt)
		}
		if lerr := AppendPromotion(o.StateDir, rec); lerr != nil {
			o.report(fmt.Sprintf("승격 원장을 쓰지 못했다: %v", lerr))
		}
		if o.OnResult != nil {
			o.OnResult(rec)
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
