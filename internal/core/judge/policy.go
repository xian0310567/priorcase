package judge

import (
	"strings"

	"github.com/xian0310567/priorcase/internal/core/config"
)

// ★★★ **판별기를 고르는 통로는 여기 하나다.**
//
// # 왜 문을 하나로 두는가
//
// 판별기를 만드는 자리가 다섯이었다 — 훅의 가용성 검사, 데몬의 구간별 선택,
// 데몬 상태 보고, 승격 게이트, doctor. 각자 `judge.Find*` 를 직접 불렀다.
//
// 그 상태에서 "판별기를 끈다" 는 스위치를 넣으면 **한 자리만 빠뜨려도 그 경로로
// 계속 켜진다.** 그리고 그 새는 경로가 하필 대화 발췌를 외부 CLI 로 넘기는
// 경로다 — 사람은 껐다고 믿는데 발췌는 계속 나간다. 조용하고, 되돌릴 수 없다.
//
// 같은 고장을 이 프로젝트가 호스트 목록에서 이미 겪었고 그래서
// `daemon.ResolveHosts` 라는 문 하나를 만들고 `internal/arch` 가 우회를 막는다
// (daemon/hostsel.go 의 §). 여기도 같은 모양이다 — arch 의
// TestJudgeSelectionGoesThroughOneDoor 가 `judge.Find*` 직접 호출을 막는다.
//
// # 개인정보 계약
//
// priorcase 자체는 telemetry 도 없고 볼트를 어디에도 올리지 않는다. **판별기만이
// 유일하게 밖으로 나가는 경로**이고, 그래서 그 스위치가 설정에 명시적으로 있어야
// 한다 (config.Capture.Judge 의 §).

// For 는 설정이 허용한 판별기를 준다. 없으면 nil 이다.
//
// prefer 는 **그 대화를 만든 호스트**의 종류다 — Codex 대화를 판정하려고 Claude
// 쿼터를 쓰는 것은 사용자가 기대하지 않는 일이다 (daemon/judgepick.go 의 §).
// 모드가 종류를 못 박은 경우(claude·codex)에는 무시된다.
func For(c *config.Config, prefer Flavor) Judge {
	if c == nil {
		return nil
	}
	cap := c.Capture
	switch cap.JudgeMode() {
	case config.JudgeOff:
		return nil
	case config.JudgeCustom:
		// 경로가 비었는데 custom 이면 설정이 모순이다. **자동으로 떨어지지 않는다** —
		// 그건 사람이 지목하지 않은 CLI 를 대신 고르는 것이다. 진단이 말한다.
		if strings.TrimSpace(cap.JudgePath) == "" {
			return nil
		}
		if j := FindFlavorAt(cap.JudgePath, cap.JudgeModel); j != nil {
			return j
		}
		return nil
	case config.JudgeClaude:
		return only(FindFlavor(FlavorClaude, cap.JudgeModel))
	case config.JudgeCodex:
		return only(FindFlavor(FlavorCodex, ""))
	case config.JudgeAuto, config.JudgeUnset:
		// **Unset 도 예전처럼 돈다.** 빈 값을 off 로 읽으면 이미 쓰던 사람의
		// 자동 기록이 조용히 멈춘다 (config.Capture.Judge 의 §). 대신 세션
		// 진입이 매번 알리고 정하게 한다.
		return FindFor(prefer, cap.JudgePath, cap.JudgeModel)
	default:
		// 모르는 값 — 오타다. **켜지 않는다.** `judge = "of"` 가 판별기를 켠 채로
		// 도는 것보다 꺼진 채로 도는 편이 낫다. 진단이 오타를 짚는다.
		return nil
	}
}

// only 는 nil 인터페이스 함정을 막는다.
//
// `var j Judge = (*CLI)(nil)` 은 **nil 이 아니다** — 인터페이스가 타입을 들고 있어서
// `j != nil` 이 참이 된다. 호출부는 전부 `!= nil` 로 가용성을 판단하므로, 그대로
// 돌려주면 "판별기가 있다" 고 보고한 뒤 부를 때 죽는다.
func only(c *CLI) Judge {
	if c == nil {
		return nil
	}
	return c
}

// Available 은 이 설정에서 판별기를 쓸 수 있는지만 본다.
//
// 가용성만 필요한 자리(훅의 스캔 게이트·데몬 상태 보고)가 판별기 객체를 만들지
// 않게 하려는 것이다 — 그 자리에서 CLI 를 들고 있으면 나중에 누군가 그걸 그냥
// 부르게 되고, 그러면 정책이 또 갈라진다.
func Available(c *config.Config, prefer Flavor) bool { return For(c, prefer) != nil }
