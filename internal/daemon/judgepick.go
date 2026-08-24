package daemon

import (
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/judge"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
)

// judgeFor 는 **그 대화를 만든 호스트의 CLI** 를 앞에 둔 판별기를 준다.
//
// # 왜 대화마다 다른가
//
// 판별기는 호스트 CLI 에 셸아웃한다(judge 패키지 주석). 그런데 어느 CLI 를 쓸지는
// 취향이 아니라 **경계**다 — Codex 에서 일한 대화를 판정하려고 Claude 쿼터를 쓰는
// 것은 사용자가 기대하지 않는 일이고, 실제로 사용자가 그것을 보고 물었다.
//
// `Promote` 는 여러 호스트의 밀린 구간을 한 판에 돌린다. 그래서 판별기를 판 전체에
// 하나로 정할 수 없고, **구간의 transcript 경로마다** 정해야 한다.
//
// # 경로로 가르는 이유
//
// 호스트가 자기 기록을 어디에 쌓는지는 `hosts` 가 알고 있고(`hosts.For`), 그것이
// 이미 파서를 고르는 근거다. 판별기도 같은 근거로 고르면 둘이 어긋날 수 없다 —
// "codex 파서로 읽고 claude 로 판정한다" 같은 상태가 만들어지지 않는다.
//
// 못 가르면 claude 를 앞에 둔다. 그게 이 도구의 원래 동작이고, 사슬이라 codex 도
// 뒤에 붙으므로 잃는 것이 없다.
func judgeFor(c *config.Config, path string, rs []hosts.Resolved) judge.Judge {
	return judge.FindFor(preferredFlavor(path, rs), c.Capture.JudgePath, c.Capture.JudgeModel)
}

// preferredFlavor 는 그 transcript 를 만든 호스트의 판별기 종류다.
//
// 판단만 떼어 둔 이유는 테스트다 — judgeFor 는 실제로 CLI 를 찾으므로 머신에 무엇이
// 깔려 있는지에 결과가 달린다. 판단은 그것과 무관하게 못박을 수 있어야 한다.
func preferredFlavor(path string, rs []hosts.Resolved) judge.Flavor {
	if h := hosts.For(path, rs); h != nil && h.Host.ID == "codex" {
		return judge.FlavorCodex
	}
	return judge.FlavorClaude
}
