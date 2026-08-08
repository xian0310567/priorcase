package hook

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/xian0310567/casebook/internal/core/judge"
	"github.com/xian0310567/casebook/internal/core/promote"
	"github.com/xian0310567/casebook/internal/daemon"
)

// safetyNet 은 stop·pre-compact·session-end 가 하는 일이다.
//
// **컨텍스트를 주입하지 않는다.** 이 셋의 stdout 은 대화에 들어가지 않으므로 조용해야 한다.
//
// 옛 구현에서 이 자리가 판별 LLM 을 부르던 곳이다. 지금은 데몬이 할 일이고, 훅은
// **데몬이 없을 때만** 대신 훑는다. 소유권은 flock 으로 판정하므로 둘이 겹치지 않는다.
func (o Options) safetyNet(ctx context.Context) error {
	// stop_hook_active 는 "이 Stop 이 훅 때문에 다시 발동했다" 는 뜻이다.
	// 여기서 또 일하면 루프가 된다.
	if o.Input.StopHookActive {
		return nil
	}
	if o.Input.TranscriptPath == "" || o.StateDir == "" {
		return nil
	}

	r, owned, err := daemon.ScanOnce(o.StateDir, o.Config, o.Layout, o.Input.TranscriptPath)
	if err != nil {
		return err
	}
	if !owned {
		return nil // cb watch 가 돌고 있다 — 주인이 따로 있다
	}

	// **세션이 끝나거나 압축될 때가 마지막 기회다.** 그때까지 에이전트가 기록하지
	// 않았으면 판별기가 대신 만든다. Stop 에서는 하지 않는다 — 대화가 이어지는
	// 중이라 에이전트에게 먼저 기회를 준다(주입 ②).
	if o.Event == EventSessionEnd || o.Event == EventPreCompact {
		o.promote(ctx)
	}
	// 사람이 볼 수 있게 stderr 로만 남긴다. 조용히 훑고 끝나면 동작하는지 알 수 없다.
	if r.Turns > 0 {
		msg := fmt.Sprintf("훑음 — 발화 %d", r.Turns)
		if r.Flagged {
			msg += fmt.Sprintf(" · 표시함 (시그널 %v)", r.Signals)
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

	for _, p := range items {
		if p.Domain == "" {
			continue
		}
		day := p.When()
		if i := strings.Index(day, "~"); i > 0 {
			day = day[:i] // 여러 날에 걸쳤으면 첫날로
		}
		r := promote.One(ctx, j, o.Layout, o.Config, promote.Segment{
			ID: p.ID(), Domain: p.Domain, Date: day, Excerpt: p.Excerpt, Session: p.SessionID,
		})
		switch {
		case r.Err != nil:
			fmt.Fprintf(o.Err, "cb hook %s: 승격 실패 (%s): %v\n", o.Event, p.ID(), r.Err)
			// 실패한 구간은 지우지 않는다 — 다음 기회에 다시 시도한다.
		case r.Recorded:
			fmt.Fprintf(o.Err, "cb hook %s: 자동 기록 %s\n", o.Event, o.Layout.RelPath(r.Path))
			_ = daemon.ResolvePending(o.StateDir, p.ID())
		default:
			// 기록할 결정이 아니라는 판정. 표시를 지운다 — 안 지우면 매 세션 다시 묻는다.
			fmt.Fprintf(o.Err, "cb hook %s: 기록 안 함 (%s): %s\n", o.Event, p.ID(), r.Reason)
			_ = daemon.ResolvePending(o.StateDir, p.ID())
		}
	}
}
