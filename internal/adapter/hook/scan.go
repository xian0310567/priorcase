package hook

import (
	"context"
	"fmt"

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
