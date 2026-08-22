package hook

import (
	"fmt"
	"strings"
	"time"

	"github.com/xian0310567/priorcase/internal/core/sync"
)

// 세션 경계에서 볼트를 리모트와 맞춘다.
//
// **집에서 내린 결정이 회사에서 회수되려면 누가 볼트를 옮겨야 한다.** 볼트는 그냥
// 마크다운 디렉토리라 git 이면 충분하고, 우리가 더할 것은 "매번 기억하지 않아도
// 되게" 뿐이다.
//
// # 예산을 이벤트마다 다르게 준다
//
// session-start 의 pull 은 **에이전트가 컨텍스트를 받기 전에 사람이 기다리는
// 시간**이다. 회사 VPN·캡티브 포털에서 git 이 매달리면 매 세션이 그만큼 느려진다.
// 못 가져오면 그 세션은 어제 것으로 돌지만, 그 손해가 매 세션 몇 초보다 작다.
//
// session-end 의 push 는 대화가 이미 끝난 뒤라 조금 더 준다. 여기서 못 밀면
// 그 결정이 다른 머신에서 **영영 안 보인다** — 이쪽이 잃는 것이 더 크다.
const (
	pullBudget = 5 * time.Second
	pushBudget = 15 * time.Second
)

// syncPull 은 세션 진입에서 리모트를 가져오고, **지난 세션이 남긴 것이 있으면
// 그것도 민다.**
//
// session-end 가 못 뜨는 경우가 있다 — 터미널을 닫거나 죽이면 훅이 안 돈다.
// 진입에서 pull 만 하면 그 커밋은 다음에도, 그다음에도 안 밀리고, 회사에 가서
// "어제 것이 없네" 가 된다.
//
// **평소에는 공짜다.** Status 는 로컬만 보므로(네트워크를 안 탄다) 밀 것이 없으면
// push 를 아예 안 부른다 — 세션 진입에 네트워크를 한 번 더 태우지 않는다.
func (o Options) syncPull() {
	catchUp := false
	if o.Config != nil {
		for _, v := range o.Config.Vaults {
			if st := sync.Status(v.Path); st.HasRemote && (st.Ahead > 0 || st.Dirty > 0) {
				catchUp = true
				break
			}
		}
	}
	o.doSync(sync.Options{Timeout: pullBudget, Stamp: sync.ThisBuild()}, true, catchUp)
}

// syncPush 는 세션 종료에서 볼트를 밀어낸다.
func (o Options) syncPush() {
	o.doSync(sync.Options{Timeout: pushBudget, Stamp: sync.ThisBuild()}, false, true)
}

// doSync 는 **실패해도 아무것도 막지 않는다.**
//
// 훅은 무슨 일이 있어도 exit 0 이다 — 회사망에서 push 가 막혔다고 세션이 죽으면
// 안 된다. 대신 조용히 넘어가지도 않는다: 실패는 stderr 로 내고 도장을 남겨
// doctor 가 나중에 읽는다. 그게 이 프로젝트가 경계하는 "조용한 무동작" 의 답이다.
//
// **성공은 아무것도 안 낸다.** 매번 "2개 보냄" 이 뜨면 그 줄은 곧 배경이 되고,
// 그때는 실패 줄도 같이 안 보인다.
func (o Options) doSync(so sync.Options, doPull, doPush bool) {
	if o.Config == nil {
		return
	}
	rs := sync.All(o.Config, so, doPull, doPush, sync.CommitMessage(time.Now()))

	var bad []string
	for _, v := range rs {
		if !v.Failed() {
			continue
		}
		bad = append(bad, v.Name)
		// **stdout 이 아니다.** session-start 의 stdout 은 통째로 에이전트
		// 컨텍스트라, 여기 섞이면 매 세션 컨텍스트를 축내고 실패 문구를
		// 과거 결정으로 오독할 여지까지 생긴다.
		if o.Err != nil {
			for _, r := range v.Results {
				if r.Err != nil {
					fmt.Fprintf(o.Err, "볼트 동기화 실패 (%s): %v\n", v.Name, r.Err)
				}
			}
		}
	}
	if o.StateDir == "" {
		return
	}
	detail := ""
	if len(bad) > 0 {
		detail = "실패한 볼트: " + strings.Join(bad, ", ")
	}
	_ = sync.WriteStamp(o.StateDir, sync.Stamp{
		At: time.Now(), OK: len(bad) == 0, Detail: detail,
	})
}
