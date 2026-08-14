package daemon

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// 이 파일은 **밀린 구간을 데몬이 스스로 소화하게** 한다.
//
// # 왜 필요한가
//
// 승격을 부르는 자리가 둘인데 둘 다 "방금 생긴 것" 만 본다:
//
//   - 훅(SessionEnd·PreCompact) — 호스트의 120초 상한 아래 돌므로 예산이 90초다.
//     실측으로 한 판에 평균 1.4건, 고친 뒤에도 3건이다.
//   - 데몬의 drain — **그 판에 새 표시가 생겼을 때만**(flagged) 부른다.
//
// 그래서 이미 쌓인 것은 아무도 안 건드린다. 실측(2026-08-14): 미확인 구간 30건이
// 그대로 남아 있었고, 새 구간이 처리 속도보다 빨리 쌓였다. 그 상태가 앱에
// "확인 큐 30건" 이라는 사람이 눌러야 할 화면으로 나타났고, 그게 자동 기록이라는
// 전제를 사람에게 떠넘기는 일이었다.
//
// # 왜 데몬인가
//
// 데몬에는 호스트의 상한이 없다. 느리고 꾸준하게 갚는 것이 여기서만 가능하다.
//
// **데몬을 안 켠 사람에게는 아무 일도 안 일어난다.** 데몬은 선택이다
// (2026-08-07 결정). 그 사람의 밀린 구간은 훅이 세션마다 조금씩 갚는다.

// DefaultBacklogInterval 은 밀린 구간을 소화하러 도는 주기다.
//
// 5분인 이유: 판별기 한 건이 10~28초(실측 p95 28.5초)라 한 판에 몇 건이 돈다.
// 더 자주 돌면 호스트 CLI 를 두들기고, 더 뜸하면 쌓이는 속도를 못 따라간다.
const DefaultBacklogInterval = 5 * time.Minute

// BacklogBudget 은 소화 한 판의 상한이다.
//
// 훅의 90초보다 길게 잡는다 — 여기엔 호스트 상한이 없다. 그렇다고 무한정 길게
// 두지는 않는다: 승격이 도는 동안 감시 루프가 그 고루틴을 기다리지는 않지만,
// 한 판이 길면 그만큼 **같은 구간을 훅과 동시에 물** 창이 넓어진다.
const BacklogBudget = 4 * time.Minute

// maxBacklogInterval 은 물러설 수 있는 최대 주기다.
//
// 판별기가 계속 실패하면(로그인이 풀렸다든지) 5분마다 부르는 것은 낭비다.
// 성과가 없으면 주기를 두 배씩 늘리고, 한 건이라도 판정되면 원래대로 돌린다.
const maxBacklogInterval = time.Hour

// backlogState 는 소화 루프의 상태다.
type backlogState struct {
	// running 은 소화가 도는 중인지다. **한 번에 하나만 돈다** — 두 판이 겹치면
	// 같은 구간을 두 번 판별기에 물릴 수 있고, 그건 노트가 둘 생길 수 있다는 뜻이다.
	running atomic.Bool
	// idle 은 성과가 없던 연속 판 수다. 물러서는 데 쓴다.
	idle atomic.Int32
}

// wait 는 다음 소화까지 기다릴 시간이다. 성과가 없을수록 길어진다.
func (b *backlogState) wait(base time.Duration) time.Duration {
	d := base
	for i := int32(0); i < b.idle.Load(); i++ {
		d *= 2
		if d >= maxBacklogInterval {
			return maxBacklogInterval
		}
	}
	return d
}

// chewBacklog 는 밀린 구간을 한 판 소화한다. 고루틴으로 돈다.
//
// **감시 루프를 막지 않는다.** 판별기 호출은 초 단위인데 select 안에서 돌리면
// 그동안 fsnotify 이벤트가 쌓이고, 대화가 한창인 파일의 쓰기 알림을 놓친다.
//
// 이미 도는 중이면 아무것도 안 한다 — 주기가 한 판보다 짧아도 겹치지 않는다.
func (d *watcher) chewBacklog(ctx context.Context) {
	if !d.backlog.running.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer d.backlog.running.Store(false)

		items, err := ReadPending(d.o.StateDir)
		if err != nil {
			d.emit(Event{Kind: "error", Err: fmt.Errorf("밀린 구간을 읽을 수 없다: %w", err)})
			return
		}
		if len(items) == 0 {
			// 밀린 것이 없으면 **물러섬을 되돌린다.** 다음에 쌓였을 때 한 시간을
			// 기다리면 안 된다.
			d.backlog.idle.Store(0)
			return
		}

		judged := 0
		Promote(ctx, PromoteOptions{
			StateDir: d.o.StateDir, Config: d.o.Config, Layout: d.l,
			Budget:   BacklogBudget,
			Err:      d.promoteWriter(),
			Label:    "prior watch 밀린 것",
			OnResult: func(p Promotion) {
				// **에러는 성과가 아니다.** 판별기가 못 도는 상태에서 5분마다
				// 부르면 호스트 CLI 만 두들긴다. 기각도 성과다 — 그 구간은 해소된다.
				if p.Err == "" {
					judged++
				}
			},
		})

		if judged == 0 {
			d.backlog.idle.Add(1)
			d.emit(Event{Kind: "backlog", Note: fmt.Sprintf(
				"밀린 구간 %d건 — 이번 판에 판정 0건, 다음은 %v 뒤",
				len(items), d.backlog.wait(d.backlogInterval()))})
			return
		}
		d.backlog.idle.Store(0)
		d.emit(Event{Kind: "backlog", Note: fmt.Sprintf(
			"밀린 구간 %d건 중 %d건을 판정했다", len(items), judged)})
	}()
}

// backlogInterval 은 설정된 주기다. 0 이면 기본값.
func (d *watcher) backlogInterval() time.Duration {
	if d.o.BacklogInterval > 0 {
		return d.o.BacklogInterval
	}
	return DefaultBacklogInterval
}
