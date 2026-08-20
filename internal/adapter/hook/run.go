package hook

import (
	"context"
	"fmt"
	"io"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// Options 는 훅 한 번을 도는 데 필요한 전부다.
type Options struct {
	Event    Event
	Input    Input
	Config   *config.Config
	Layout   *store.Layout
	StateDir string

	// Out 은 **에이전트 컨텍스트로 주입되는 자리**다. 여기 쓰는 것은 전부 모델이 읽는다.
	Out io.Writer
	// Err 은 사람과 로그가 보는 자리다. 진단은 전부 여기로.
	Err io.Writer
}

// Run 은 이벤트를 처리한다.
//
// 에러를 돌려주지만 **호출자는 그걸로 종료 코드를 바꾸지 않는다** — 훅은 언제나
// exit 0 이다. 에러는 stderr 로 흘려 사람이 볼 수 있게 하려는 것이다. 반환값으로
// 두는 이유는 테스트가 "조용히 아무것도 안 한 것" 과 "실패한 것" 을 구별해야 하기
// 때문이다. 구별이 안 되면 훅이 죽어 있어도 정상으로 보인다.
func Run(ctx context.Context, o Options) error {
	switch o.Event {
	case EventSessionStart:
		// **가져온 뒤에 컨텍스트를 만든다.** 순서가 반대면 그 세션은 어제 볼트로
		// 시작하고, 방금 다른 머신에서 내린 결정을 못 본 채 같은 것을 다시 정한다.
		o.syncPull()
		return o.sessionStart()
	case EventUserPromptSubmit:
		return o.userPromptSubmit()
	case EventStop, EventPreCompact, EventSessionEnd:
		err := o.safetyNet(ctx)
		// **세션이 끝날 때만 민다.** stop 은 턴마다 도는데 거기서 밀면 대화 한 번에
		// 네트워크를 수십 번 탄다. pre-compact 도 세션 중간이라 마찬가지다.
		//
		// 안전망 뒤에 두는 이유: 안전망이 이번 세션의 결정을 볼트에 쓸 수 있고,
		// 먼저 밀면 그것이 빠진다.
		if o.Event == EventSessionEnd {
			o.syncPush()
		}
		return err
	default:
		return fmt.Errorf("알 수 없는 훅 이벤트: %q (쓸 수 있는 것: %s)", o.Event, eventList())
	}
}

func eventList() string {
	s := ""
	for i, e := range Events {
		if i > 0 {
			s += " | "
		}
		s += string(e)
	}
	return s
}
