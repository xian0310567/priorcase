package retro

import (
	"time"

	"github.com/xian0310567/priorcase/internal/core/store"
)

// 이 파일은 **대화 도중에 결과를 물을 때**를 정한다. 회고 "큐" 와는 다른 물음이다.
//
// # 왜 큐가 아니라 대화인가
//
// 실측(2026-08-14): 결정 157건 중 결과가 적힌 것이 **2건(1.3%)** 이다. 회고 큐에는
// 52건이 쌓여 있는데 아무도 안 본다. 목록으로 모아 두면 그것을 보러 가는 일 자체가
// 따로 시간을 내는 일이 되고, 따로 시간을 내는 일은 안 일어난다.
//
// 결과를 아는 순간은 **그 주제를 다시 다룰 때**다. 회수가 그 결정을 이미 눈앞에
// 꺼내 놓은 그 자리에서 물으면, 답하는 비용이 한 줄이다.
//
// # 왜 1위만 보나
//
// 주입은 결정 셋을 싣는다. 셋 다 물으면 매 프롬프트가 질문 셋으로 덮이고, 그러면
// 사람도 에이전트도 무시하는 법을 배운다 — 이 프로젝트가 반복해서 경계하는 것이다.
// **1위는 지금 하는 일과 가장 가까운 과거 결정**이고, 그것 하나면 족하다.
//
// # 왜 7일인가
//
// 실측으로 회고 큐 52건의 나이가 1~13일(중앙값 5일)이다. 어제 내린 결정에 결과를
// 물으면 답이 "아직 모른다" 뿐이고, 답할 수 없는 질문은 소음이다. 7일로 자르면
// 지금 볼트에서 23건이 남는다.
//
// **뒤집힌 결정은 나이를 안 본다.** 뒤집혔다는 것 자체가 결과가 났다는 뜻이다 —
// 하루 만에 뒤집혔어도 물을 값이 있고, 오히려 그때가 이유가 가장 선명하다.

// AskAge 는 결과를 묻기 전에 기다리는 최소 기간이다.
const AskAge = 7 * 24 * time.Hour

// Ask 는 주입된 결정 중 **지금 결과를 물어볼 것 하나**를 고른다.
//
// 고를 것이 없으면 두 번째 반환값이 false 다. 억지로 하나를 고르지 않는다 —
// 매 프롬프트마다 무언가를 묻는 것이 이 물음을 죽이는 가장 빠른 길이다.
func Ask(hits []store.Note, now time.Time) (store.Note, bool) {
	if len(hits) == 0 {
		return store.Note{}, false
	}
	n := hits[0]
	// 참고 문서에는 결과랄 것이 없다. 결정만 묻는다.
	if n.IsReference() {
		return store.Note{}, false
	}
	// **이미 답한 것을 또 묻지 않는다.** outcome 이 빈 값은 판 1 노트이거나 사람이
	// 지운 것인데, 둘 다 "아직 모른다" 와 같은 뜻이다.
	if o := n.Meta.Outcome; o != "" && o != "pending" {
		return store.Note{}, false
	}
	// 이미 후회한 것으로 표시했으면 물을 것이 없다.
	if n.Meta.Status == "regretted" {
		return store.Note{}, false
	}
	if n.Meta.Status == "superseded" {
		return n, true
	}
	d, err := time.Parse("2006-01-02", n.Meta.Date)
	if err != nil {
		// **날짜를 모르면 묻지 않는다.** 나이를 못 재는데 물으면 방금 내린 결정에
		// 결과를 묻게 되고, 답할 수 없는 질문은 소음이다.
		return store.Note{}, false
	}
	if now.Sub(d) < AskAge {
		return store.Note{}, false
	}
	return n, true
}
