package hook

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// 세션 중간에 볼트를 신선하게 유지한다.
//
// # 고치려는 고장
//
// 동기화는 세션 경계에서만 돈다(sync.go). 혼자 쓰는 볼트에서는 그것으로 충분하다 —
// 내가 없는 동안 볼트가 바뀔 일이 없다.
//
// **공유 볼트는 다르다.** 세 시간짜리 세션이면 그동안 동료가 내린 결정을 한 건도
// 못 본다. 회수는 로컬 파일만 읽으므로 그 세 시간 내내 낡은 볼트를 훑으면서
// "관련 결정 없음" 이라고 답한다. 조용하고, 그래서 이 프로젝트가 죄목으로 드는
// 실패 그대로다.
//
// # 왜 기다리지 않는가
//
// 회수는 매 프롬프트마다 돈다. 거기서 네트워크를 타면 그 지연을 사람이 **매번**
// 겪는다. 그래서 띄워만 놓고 안 기다린다 — 이번 회수는 낡은 볼트를 보지만 다음
// 회수부터 신선하다. 신선도는 누적되는 값이라 그것으로 충분하고, 매 프롬프트에
// 몇 초를 얹는 것보다 이쪽이 낫다.
//
// # 왜 받기만 하는가
//
// 미는 것은 다른 문제다. Stop 이 이미 디바운스를 걸어 다루고 있고(syncStop),
// 여기서 같이 하면 그 창이 무의미해진다. 그리고 미는 것은 세션이 끝날 때 해도
// 늦지 않다 — 받는 것과 달리 **내가 기다리는 사람이 아니다.**

// freshenInterval 은 다시 받기까지 기다리는 시간이다.
//
// 10분인 이유는 stopPushInterval 과 같다: 이 창을 놓쳐도 **잃지 않는다.** 다음
// 프롬프트가 받는다. 그래서 이 값은 "얼마나 신선한가" 가 아니라 "낡은 채로 답할
// 확률" 을 정하고, 짧게 잡을수록 네트워크만 더 탄다.
const freshenInterval = 10 * time.Minute

// freshenStamp 는 마지막으로 받은 시각을 적어 두는 파일이다.
//
// sync.Stamp 와 따로 두는 이유: 그쪽은 **밀기**의 결과(성공/실패)를 담고 doctor 가
// 읽는다. 여기 섞으면 세션 중간의 받기가 doctor 의 "마지막 시도" 를 덮어써서,
// 밀기가 실패한 사실이 조용히 사라진다.
const freshenStamp = "freshen.json"

// freshenArgs 는 백그라운드로 돌릴 명령이다.
//
// **CLI 의 플래그 이름과 정확히 같아야 한다.** 틀리면 자식이 즉시 죽는데 우리는
// 기다리지 않으므로 **아무 일도 안 일어난 것과 구별되지 않는다.** 2026-09-01 에
// 실제로 `--pull-only` 라고 썼다가 잡았다(진짜 이름은 `--pull`). 그래서 이 상수는
// 시험이 CLI 에 대고 확인한다.
var freshenArgs = []string{"sync", "--pull"}

type freshenAt struct {
	At time.Time `json:"at"`
}

// dueForFreshen 은 이번 프롬프트에서 받을 때가 됐는지다.
//
// **도장이 없으면 받는다.** 이 머신에서 아직 한 번도 안 했다는 뜻이다.
func dueForFreshen(last time.Time, have bool, now time.Time) bool {
	if !have {
		return true
	}
	return now.Sub(last) >= freshenInterval
}

// freshen 은 조건이 맞으면 받기를 띄운다. **무슨 일이 있어도 대화를 막지 않는다** —
// 실패는 조용히 넘긴다. 여기서 못 받아도 다음 프롬프트가 다시 시도하고, 세션
// 진입의 pull 이 최후의 그물이다.
func (o Options) freshen() {
	if o.Config == nil || o.StateDir == "" {
		// 도장을 찍을 자리가 없으면 매 프롬프트마다 띄우게 된다 — 안 하느니만 못하다.
		return
	}
	if !o.hasShared() {
		return
	}
	last, have := readFreshen(o.StateDir)
	if !dueForFreshen(last, have, time.Now()) {
		return
	}
	// **띄우기 전에 찍는다.** 나중에 찍으면 자식이 도는 동안 온 프롬프트마다 또
	// 띄운다 — 느린 네트워크일수록 더 많이 뜨고, 그건 정확히 느릴 때 최악으로
	// 구는 설계다.
	if err := writeFreshen(o.StateDir, time.Now()); err != nil {
		return
	}

	bin, err := os.Executable()
	if err != nil {
		return
	}
	spawn := o.spawn
	if spawn == nil {
		spawn = spawnDetached
	}
	_ = spawn(bin, freshenArgs...)
}

func (o Options) hasShared() bool {
	for _, v := range o.Config.Vaults {
		if v.Shared {
			return true
		}
	}
	return false
}

// spawnDetached 는 프로세스를 띄우고 **기다리지 않는다.**
//
// 출력을 버리는 것이 중요하다. 물려받으면 훅의 stdout 으로 새는데, session 훅의
// stdout 은 통째로 에이전트 컨텍스트라 거기 섞이면 동기화 로그가 과거 결정인 척한다.
func spawnDetached(bin string, args ...string) error {
	c := exec.Command(bin, args...)
	c.Stdout = nil
	c.Stderr = nil
	c.Stdin = nil
	if err := c.Start(); err != nil {
		return err
	}
	// **Wait 를 안 부른다.** 부르면 기다리게 되고, 그러면 백그라운드가 아니다.
	// 부모가 먼저 끝나면 자식은 init 이 거둔다 — 훅은 어차피 즉시 끝나는 프로세스라
	// 좀비가 오래 남지 않는다.
	return nil
}

func readFreshen(dir string) (time.Time, bool) {
	b, err := os.ReadFile(filepath.Join(dir, freshenStamp))
	if err != nil {
		return time.Time{}, false
	}
	var f freshenAt
	if err := json.Unmarshal(b, &f); err != nil {
		return time.Time{}, false
	}
	return f.At, true
}

func writeFreshen(dir string, at time.Time) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(freshenAt{At: at})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, freshenStamp), b, 0o644)
}
