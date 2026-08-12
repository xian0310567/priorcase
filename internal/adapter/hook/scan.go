package hook

import (
	"context"
	"fmt"
	"time"

	"github.com/xian0310567/priorcase/internal/core/judge"
	"github.com/xian0310567/priorcase/internal/daemon"
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
	// 게이트 뒤에 두면 `prior watch` 를 켜는 것이 자동 기록을 끄는 행위가 된다 —
	// 데몬의 drain 은 판별기를 부르지 않고, 데몬은 세션이 끝난 것도 모른다.
	//
	// 스캔이 실패해도 부른다. 이미 표시된 구간은 그것과 무관하게 처리해야 한다.
	if o.Event == EventSessionEnd || o.Event == EventPreCompact {
		o.promote(ctx)
	}

	// **다른 호스트의 기록도 훑는다.** 훅은 자기를 부른 호스트의 transcript 하나만
	// 받으므로, 데몬을 안 켜면 Codex 같은 다른 호스트는 파서가 있어도 영영 안 읽힌다 —
	// 실측에서 체크포인트 162개가 전부 Claude Code 였고 Codex 는 0개였다.
	//
	// **세션이 끝날 때만 한다.** Stop 은 대화 도중이라 자주 오고, 거기서 수천 개
	// 파일을 stat 하면 그 지연을 사람이 매 턴 겪는다.
	//
	// 승격 뒤에 둔다. 자동 기록이 이 도구의 알파이자 오메가이므로, 시간이 모자랄 때
	// 밀려야 하는 쪽은 훑기다 — 못 훑은 파일은 다음 세션에 다시 후보가 되지만,
	// 승격을 못 하면 그 구간은 큐에 쌓인 채로 남는다.
	if o.Event == EventSessionEnd || o.Event == EventPreCompact {
		o.sweepOthers(judgeAvailable)
	}

	if serr != nil {
		return serr
	}
	if !owned {
		return nil // prior watch 가 훑기의 주인이다 — 훑기는 그쪽이 한다
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
		fmt.Fprintf(o.Err, "prior hook %s: %s\n", o.Event, msg)
	}
	return nil
}

// 시간 상한 셋은 **반드시 이 순서**여야 한다:
//
//	judge.DefaultTimeout  <  promoteBudget  <  promoteHookTimeout
//
// 어긋나면 각각 이렇게 깨진다.
//
//   - 판별기 상한 ≥ 예산 → 한 건이 예산을 통째로 먹어 두 번째 구간이 영영 못 돈다.
//     실제로 그랬다 (예산 75초, 판별기 90초).
//   - 예산 ≥ 훅 상한 → 예산을 다 쓰기 전에 호스트가 훅을 죽인다. 그러면 승격이
//     중간에 잘리고 원장에는 절반만 남는다.
//
// arch 테스트가 이 순서를 강제한다.
const (
	// promoteBudget 은 승격에 쓸 수 있는 총 시간이다. 훅은 대화 흐름 위에 있어서
	// 여기서 멎으면 사용자가 그대로 겪는다.
	promoteBudget = 90 * time.Second
	// promoteHookTimeout 은 승격하는 훅(SessionEnd·PreCompact)에 적어 둘 상한이다.
	// 예산을 다 쓰고도 마무리(원장 쓰기·pending 해소)할 여유를 남긴다.
	promoteHookTimeout = 120 * time.Second
	// sweepBudget 은 다른 호스트를 훑는 데 쓸 시간이다.
	//
	// **승격 예산과 함께 훅 상한 안에 들어가야 한다.** 승격(90초) + 훑기(10초) = 100초,
	// 훅 상한 120초. 남는 20초가 마무리 몫이다. arch 테스트가 이 관계를 지킨다.
	sweepBudget = daemon.DefaultSweepBudget
)

// promote 는 승격을 daemon 에 위임한다.
//
// **로직을 여기 두지 않는다.** 승격은 볼트에 쓰는 일이고, 훅과 데몬이 각자 구현하면
// 쓰기 경로가 둘로 갈라진다 — 그건 이 프로젝트가 죄목으로 드는 바로 그것이다.
// 훅이 더 아는 것은 "지금 끝나는 세션이 어느 것인가" 하나뿐이라 그것만 넘긴다.
func (o Options) promote(ctx context.Context) {
	daemon.Promote(ctx, daemon.PromoteOptions{
		StateDir: o.StateDir,
		Config:   o.Config,
		Layout:   o.Layout,
		First:    o.Input.TranscriptPath,
		Author:   o.Config.AuthorFor(o.Input.Cwd),
		Budget:   promoteBudget,
		Err:      o.Err,
		Label:    "prior hook " + string(o.Event),
	})
}

// sweepOthers 는 다른 호스트의 기록을 훑는다. 데몬이 돌면 아무것도 안 한다.
//
// **실패해도 훅을 죽이지 않는다.** 이건 덤으로 하는 일이고, 여기서 에러를 올리면
// 자기 transcript 처리와 승격까지 같이 실패한 것처럼 보인다.
func (o Options) sweepOthers(judgeAvailable bool) {
	r, owned, err := daemon.SweepOnce(daemon.SweepOptions{
		StateDir: o.StateDir, Config: o.Config, Layout: o.Layout,
		Skip:           o.Input.TranscriptPath,
		JudgeAvailable: judgeAvailable,
		Budget:         sweepBudget,
		Err:            o.Err,
	})
	if err != nil {
		fmt.Fprintf(o.Err, "prior hook %s: 다른 호스트 훑기 실패: %v\n", o.Event, err)
		return
	}
	if !owned {
		return // prior watch 가 훑기의 주인이다
	}
	// 조용히 끝나는 판이 대부분이다 (자란 파일이 없으면 할 일이 없다).
	if r.Seeded == 0 && r.Scanned == 0 && r.Skipped == 0 && r.Errs == 0 && r.Pruned == 0 {
		return
	}
	msg := fmt.Sprintf("다른 호스트 훑기 — 시딩 %d · 훑음 %d", r.Seeded, r.Scanned)
	if r.Pruned > 0 {
		msg += fmt.Sprintf(" · 정리 %d", r.Pruned)
	}
	if r.Flagged > 0 {
		msg += fmt.Sprintf(" · 표시 %d", r.Flagged)
	}
	if r.Errs > 0 {
		msg += fmt.Sprintf(" · 실패 %d", r.Errs)
	}
	// **못 본 파일은 반드시 알린다.** 조용히 짧게 훑고 끝나면 "결정이 없었다" 로 보인다.
	if r.Skipped > 0 {
		msg += fmt.Sprintf(" · ⚠️ 시간이 모자라 %d개를 못 봤다 (다음 세션에 다시 본다)", r.Skipped)
	}
	fmt.Fprintf(o.Err, "prior hook %s: %s\n", o.Event, msg)
}
