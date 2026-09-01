package hook

import (
	"context"
	"fmt"
	"io"
	"os"

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
	// Host 는 훅을 부른 에이전트다. 빈 값은 Claude Code 다.
	// 컨텍스트 주입의 **출력 형식**과 볼트 push 가 걸리는 **자리**가 여기 달렸다.
	Host Host

	// Out 은 **에이전트 컨텍스트로 주입되는 자리**다. 여기 쓰는 것은 전부 모델이 읽는다.
	Out io.Writer
	// Err 은 사람과 로그가 보는 자리다. 진단은 전부 여기로.
	Err io.Writer

	// spawn 은 백그라운드 프로세스를 띄운다. **시험이 갈아 끼울 수 있게 필드다** —
	// 진짜 프로세스를 띄우는 시험은 느리고 기계마다 다르게 군다. 비어 있으면
	// 진짜로 띄운다(freshen.go).
	spawn func(bin string, args ...string) error
}

// Run 은 이벤트를 처리한다.
//
// 에러를 돌려주지만 **호출자는 그걸로 종료 코드를 바꾸지 않는다** — 훅은 언제나
// exit 0 이다. 에러는 stderr 로 흘려 사람이 볼 수 있게 하려는 것이다. 반환값으로
// 두는 이유는 테스트가 "조용히 아무것도 안 한 것" 과 "실패한 것" 을 구별해야 하기
// 때문이다. 구별이 안 되면 훅이 죽어 있어도 정상으로 보인다.
func Run(ctx context.Context, o Options) error {
	// ★★★ **판별기가 띄운 세션에서는 훅이 아무것도 하지 않는다.**
	//
	// promote.go·arc.go 가 이미 같은 가드를 갖고 있고 그 주석이 이유를 적어 뒀다 —
	// "판별기가 띄운 세션에도 훅이 붙는다". 그런데 그 둘은 **쓰기 경로만** 막았고
	// 회수 주입 경로가 빠져 있었다. TestJudgeSessionDoesNotRecurse 의 이름이
	// "훅이 아무것도 하지 않는다" 인데 실제로는 SessionEnd 만 덮고 있었다.
	//
	// 실측(2026-08-31): 홈 디렉토리에 쌓인 판별기 세션 7개가 **전부** 회수 주입을
	// 4,007~4,889자씩 받았고 **전부 API safeguard 로 차단됐다.** 이 머신의 차단
	// 42회 중 16회가 그것이다 — 사람은 못 보는 자리에서 판별기가 계속 죽고 있었다.
	//
	// 주입이 그 자체로 차단을 일으킨다는 증거는 없다(같은 노트가 실린 무차단 세션이
	// 58건이다). 막는 이유는 다른 것이다: **판별기에게 볼트 회수를 보여 줄 이유가
	// 하나도 없다.** 판별기는 발췌 하나를 등급 매기는 일만 하고, 그 프롬프트에
	// 4,889자를 더하는 것은 순수한 낭비이며 판정을 흔들 수 있다.
	//
	// **디스패치 앞에 둔다.** 이벤트마다 걸면 새 이벤트가 생길 때 또 빠진다 —
	// 이 고장이 정확히 그렇게 났다.
	if os.Getenv("PRIORCASE_JUDGE") == "1" {
		return nil
	}
	// **호스트별 분기를 여기 한 곳에 가둔다.** 회수·세션진입 코드는 자기가 어느
	// 호스트로 나가는지 몰라야 한다 — 알기 시작하면 출력 형식 조건문이 그 안으로 번진다.
	if o.Host == HostCodex && o.Out != nil {
		c := &codexOut{ev: o.Event, w: o.Out}
		o.Out = c
		defer c.Flush()
	}
	switch o.Event {
	case EventSessionStart:
		// **컨텍스트를 비우고 다시 시작하면 "이미 봤다" 도 지운다** (seen.go).
		// 안 지우면 회수가 요약 대신 포인터만 내는데 정작 요약은 컨텍스트에 없다 —
		// 절약이 조용한 품질 저하로 바뀐다. resume 은 컨텍스트가 살아 있으므로 둔다.
		if o.Input.Source == "clear" {
			o.resetSeen()
		}
		// **가져온 뒤에 컨텍스트를 만든다.** 순서가 반대면 그 세션은 어제 볼트로
		// 시작하고, 방금 다른 머신에서 내린 결정을 못 본 채 같은 것을 다시 정한다.
		o.syncPull()
		return o.sessionStart()
	case EventUserPromptSubmit:
		// **회수하기 전에 신선도를 챙긴다.** 공유 볼트에서만, 창을 넘겼을 때만,
		// 그리고 기다리지 않는다 (freshen.go).
		o.freshen()
		return o.userPromptSubmit()
	case EventStop, EventPreCompact, EventSessionEnd:
		// 압축은 앞부분을 날린다 — 이미 주입한 요약도 같이 사라진다. 표시를
		// 안 지우면 그다음부터 포인터만 나가고 근거는 어디에도 없게 된다.
		if o.Event == EventPreCompact {
			o.resetSeen()
		}
		err := o.safetyNet(ctx)
		// **세션이 끝날 때만 민다.** stop 은 턴마다 도는데 거기서 밀면 대화 한 번에
		// 네트워크를 수십 번 탄다. pre-compact 도 세션 중간이라 마찬가지다.
		//
		// 안전망 뒤에 두는 이유: 안전망이 이번 세션의 결정을 볼트에 쓸 수 있고,
		// 먼저 밀면 그것이 빠진다.
		if o.Event == EventSessionEnd {
			o.syncPush()
		}
		// **Codex 에는 SessionEnd 가 없다.** 그 호스트에서는 위 줄이 영영 안 돌므로
		// Stop 이 대타를 선다 — 대신 디바운스가 붙는다 (sync.go).
		if o.Event == EventStop && o.Host == HostCodex {
			o.syncStop()
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
