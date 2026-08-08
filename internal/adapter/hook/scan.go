package hook

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/xian0310567/casebook/internal/core/judge"
	"github.com/xian0310567/casebook/internal/core/promote"
	"github.com/xian0310567/casebook/internal/daemon"
)

// safetyNet 은 stop·pre-compact·session-end 가 하는 일이다.
//
// **컨텍스트를 주입하지 않는다.** 이 셋의 stdout 은 대화에 들어가지 않으므로 조용해야 한다.
//
// 옛 구현에서 이 자리가 판별 LLM 을 부르던 곳이다. 지금도 **훑기**는 데몬이 있으면
// 데몬이, 없으면 훅이 한다 — 소유권을 flock 으로 판정하므로 둘이 겹치지 않는다.
//
// **승격은 그 소유권과 무관하다 (D12).** 데몬은 판별기를 부르지 않고 세션이 끝난
// 것도 모르므로, 훅이 락을 못 얻어도 SessionEnd·PreCompact 에서는 승격한다.
// 그래서 데몬이 도는 동안에도 훅이 상태 파일을 고친다 — 상태 파일이 디스크 정본인
// 이유가 이것이다.
func (o Options) safetyNet(ctx context.Context) error {
	// stop_hook_active 는 "이 Stop 이 훅 때문에 다시 발동했다" 는 뜻이다.
	// 여기서 또 일하면 루프가 된다.
	if o.Input.StopHookActive {
		return nil
	}
	if o.Input.TranscriptPath == "" || o.StateDir == "" {
		return nil
	}

	// 판별기가 있으면 시그널 필터를 건너뛴다 — 판정은 판별기가 한다. 언어에 묶인
	// 키워드가 판별기 앞을 막는 일을 없앤다.
	judgeAvailable := judge.Find(o.Config.Capture.JudgePath, o.Config.Capture.JudgeModel) != nil
	r, owned, serr := daemon.ScanOnce(o.StateDir, o.Config, o.Layout, o.Input.TranscriptPath, judgeAvailable)

	// **세션이 끝나거나 압축될 때가 마지막 기회다.** 그때까지 에이전트가 기록하지
	// 않았으면 판별기가 대신 만든다. Stop 에서는 하지 않는다 — 대화가 이어지는
	// 중이라 에이전트에게 먼저 기회를 준다(주입 ②).
	//
	// **소유권 게이트 앞에 둔다.** ScanOnce 의 락은 *훑기*의 주인을 하나로 정하는
	// 것이고, 승격은 이미 표시된 구간을 읽어 처리할 뿐이라 훑기와 겹치지 않는다.
	// 게이트 뒤에 두면 `cb watch` 를 켜는 것이 자동 기록을 끄는 행위가 된다 —
	// 데몬의 drain 은 판별기를 부르지 않고, 데몬은 세션이 끝난 것도 모른다.
	//
	// 스캔이 실패해도 부른다. 이미 표시된 구간은 그것과 무관하게 처리해야 한다.
	if o.Event == EventSessionEnd || o.Event == EventPreCompact {
		o.promote(ctx)
	}

	if serr != nil {
		return serr
	}
	if !owned {
		return nil // cb watch 가 훑기의 주인이다 — 훑기는 그쪽이 한다
	}
	// 사람이 볼 수 있게 stderr 로만 남긴다. 조용히 훑고 끝나면 동작하는지 알 수 없다.
	if r.Turns > 0 {
		msg := fmt.Sprintf("훑음 — 발화 %d", r.Turns)
		if r.Flagged {
			if len(r.Signals) > 0 {
				msg += fmt.Sprintf(" · 표시함 (시그널 %v)", r.Signals)
			} else {
				msg += " · 표시함 (시그널 없음 — 판별기가 판정한다)"
			}
		}
		if r.Recorded {
			msg += " · 이미 기록됨"
		}
		if !r.Advanced {
			msg += fmt.Sprintf(" · ⚠️ 체크포인트 미전진 (깨진 줄 %d)", r.Bad)
		}
		fmt.Fprintf(o.Err, "cb hook %s: %s\n", o.Event, msg)
	}
	return nil
}

// promote 는 아직 기록되지 않은 구간을 판별기에 넘긴다.
//
// **판별기가 없으면 아무것도 하지 않는다.** 그때는 표시만 남고 에이전트가 판단한다 —
// 그것이 판별기 없는 설치의 정상 동작이고, 경고를 낼 일이 아니다.
//
// 재귀 차단: 판별기가 띄운 세션에도 훅이 붙는다. CASEBOOK_JUDGE 가 있으면 그
// 세션이므로 즉시 끝낸다. 안 그러면 판별기가 판별기를 부른다.
// promoteBudget 은 승격에 쓸 수 있는 총 시간이다.
//
// 훅은 대화 흐름 위에 있어서 여기서 멎으면 사용자가 그대로 겪는다. 판별기 한 건이
// 최대 90초이므로 상한이 없으면 pending 20건에 훅이 몇 분을 쓴다.
const promoteBudget = 75 * time.Second

// ownFirst 는 이 세션의 구간을 앞으로 옮긴다. 순서만 바꾸고 버리지 않는다.
func ownFirst(items []daemon.Pending, transcript string) []daemon.Pending {
	if transcript == "" {
		return items
	}
	out := make([]daemon.Pending, 0, len(items))
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

func (o Options) promote(ctx context.Context) {
	if os.Getenv("CASEBOOK_JUDGE") == "1" {
		return
	}
	j := judge.Find(o.Config.Capture.JudgePath, o.Config.Capture.JudgeModel)
	if j == nil {
		return
	}
	items, err := daemon.ReadPending(o.StateDir)
	if err != nil || len(items) == 0 {
		return
	}

	// **내 세션 것을 먼저 한다.** 세션이 끝나는 것은 *이* 대화의 마지막 기회이고,
	// 다른 프로젝트의 구간은 그쪽 세션이 끝날 때가 그쪽의 마지막 기회다.
	items = ownFirst(items, o.Input.TranscriptPath)

	// **총 시한을 둔다.** 판별기 한 건이 최대 90초인데 전 프로젝트의 pending 을 모두
	// 직렬로 돌면 SessionEnd 훅이 몇 분씩 멎는다 — 훅은 대화 흐름 위에 있으므로
	// 그건 사용자가 겪는 정지다. 못 한 것은 남는다. 다음 기회에 다시 온다.
	deadline := time.Now().Add(promoteBudget)

	for _, p := range items {
		if time.Now().After(deadline) {
			fmt.Fprintf(o.Err, "cb hook %s: 시간이 다 돼 나머지는 다음 기회에 넘긴다\n", o.Event)
			return
		}
		if p.Domain == "" {
			// 조용히 넘기면 "안전망이 도는데 아무것도 안 남는" 상태가 된다.
			// 새 사용자가 정확히 이 상태에 빠진다 — 설정에 도메인이 없으면 그렇다.
			fmt.Fprintf(o.Err, "cb hook %s: 도메인을 알 수 없어 기록하지 못했다 (%s) — "+
				"설정에 [[domain]] 을 추가하거나 default_domain 을 적어라\n", o.Event, p.ID())
			continue
		}
		day := p.When()
		if i := strings.Index(day, "~"); i > 0 {
			day = day[:i] // 여러 날에 걸쳤으면 첫날로
		}

		// **집어 간다.** 동시에 끝나는 두 세션이 같은 구간을 각자 판별기에 넘기면
		// 같은 대화에 결정 노트가 둘 생긴다 (판별기는 비결정적이라 slug 가 갈린다).
		if ok, cerr := daemon.ClaimPending(o.StateDir, p.ID(), time.Now().UTC()); cerr != nil || !ok {
			continue
		}
		r := promote.One(ctx, j, o.Layout, o.Config, promote.Segment{
			ID: p.ID(), Domain: p.Domain, Date: day, Excerpt: p.Excerpt, Session: p.SessionID,
		})

		// **세 갈래 전부 원장에 남긴다.** stderr 는 사람이 그 순간 보지 않으면
		// 사라지고, 표시는 곧 해소돼 지워진다. 원장이 없으면 "판별기가 보고
		// 기록할 게 아니라고 했다" 와 "판별기가 아예 안 돌았다" 가 같아 보인다.
		rec := daemon.Promotion{
			At: time.Now().UTC(), ID: p.ID(), Domain: p.Domain,
			Recorded: r.Recorded, Reason: r.Reason,
		}
		if r.Path != "" {
			rec.Path = o.Layout.RelPath(r.Path)
		}
		if r.Err != nil {
			rec.Err = daemon.TrimLedgerText(r.Err.Error())
		}
		rec.Reason = daemon.TrimLedgerText(rec.Reason)
		if lerr := daemon.AppendPromotion(o.StateDir, rec); lerr != nil {
			fmt.Fprintf(o.Err, "cb hook %s: 승격 원장을 쓰지 못했다: %v\n", o.Event, lerr)
		}

		switch {
		case r.Err != nil:
			fmt.Fprintf(o.Err, "cb hook %s: 승격 실패 (%s): %v\n", o.Event, p.ID(), r.Err)
			// 실패한 구간은 지우지 않는다 — 다음 기회에 다시 시도한다.
		case r.Recorded:
			fmt.Fprintf(o.Err, "cb hook %s: 자동 기록 %s\n", o.Event, o.Layout.RelPath(r.Path))
			// **방금 만든 노트를 그 자리에서 소모시킨다.** 안 그러면 다음 스캔이 이
			// 노트를 "새로 생겼다" 로 세어 **아직 아무도 안 본 다음 구간**을 면제한다 —
			// 안전망이 자기 출력으로 자기를 억제하는 off-by-one 이다. 노트에 출처
			// 필드가 없어 사후에는 구분할 수 없으므로 여기서 처리해야 한다.
			if cerr := daemon.CreditNoteFor(o.StateDir, p.Path, day, p.SessionID); cerr != nil {
				fmt.Fprintf(o.Err, "cb hook %s: 크레딧을 새기지 못했다: %v\n", o.Event, cerr)
			}
			_ = daemon.ResolvePending(o.StateDir, p.ID())
		default:
			// 기록할 결정이 아니라는 판정. 표시를 지운다 — 안 지우면 매 세션 다시 묻는다.
			fmt.Fprintf(o.Err, "cb hook %s: 기록 안 함 (%s): %s\n", o.Event, p.ID(), r.Reason)
			_ = daemon.ResolvePending(o.StateDir, p.ID())
		}
	}
}
