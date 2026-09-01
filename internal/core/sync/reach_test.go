package sync

import (
	"strings"
	"testing"
	"time"
)

// 리모트에 **실제로 닿는지** 확인한다.
//
// # 왜 필요한가
//
// 2026-09-01 사업주 지적: 앱에서 회사 CodeCommit 주소를 넣었는데 "이게 정상적으로
// 접근이 가능한지 확인이 안 되잖아."
//
// 맞다. `SetRemote` 는 `git remote set-url` 만 했다. 주소가 문법적으로만 맞으면
// 저장되고, **틀렸다는 사실은 다음 동기화가 실패할 때까지 안 드러난다.** 그런데
// 동기화는 세션 경계에서 조용히 돌고 훅은 무슨 일이 있어도 exit 0 이다 — 즉
// 오타 하나로 그 볼트의 결정이 아무 데도 안 가는 상태가 **며칠씩 안 보인다.**
//
// CodeCommit 은 자격증명까지 필요해서 "주소는 맞는데 못 붙는" 경우가 흔하다.
// 그래서 문법이 아니라 **실제 접근**을 봐야 한다.

// ★★★ 닿는 리모트는 통과한다.
func TestReachableRemotePasses(t *testing.T) {
	a, _ := pair(t)
	url, err := Remote(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckRemote(url, 10*time.Second); err != nil {
		t.Fatalf("닿는 리모트인데 실패했다: %v", err)
	}
}

// ★★★ 없는 리모트는 **에러로** 말한다. 조용히 통과하면 이 기능이 없는 것과 같다.
func TestUnreachableRemoteFails(t *testing.T) {
	err := CheckRemote("/그런/경로는/없다.git", 10*time.Second)
	if err == nil {
		t.Fatal("없는 리모트가 통과했다")
	}
	// 사람이 무엇을 고쳐야 하는지 알아야 한다.
	if !strings.Contains(err.Error(), "닿을 수 없다") {
		t.Errorf("무엇이 문제인지 안 말한다: %v", err)
	}
}

// ★ 빈 주소는 **고장이 아니다.** 리모트가 없는 볼트는 정상이다(이 머신에서만 쓴다).
func TestEmptyRemoteIsNotChecked(t *testing.T) {
	if err := CheckRemote("", time.Second); err != nil {
		t.Fatalf("빈 주소를 고장이라 했다: %v", err)
	}
}

// ★★ 예산을 넘기면 **끊는다.** 이 검사는 사람이 [저장]을 누르고 기다리는
// 자리에서도 도는데, 회사망에서 git 이 매달리면 앱이 통째로 멈춘 것처럼 보인다.
func TestCheckRemoteIsCutOff(t *testing.T) {
	start := time.Now()
	// 라우팅 불가 주소 — 응답이 안 온다.
	_ = CheckRemote("https://10.255.255.1/none.git", 700*time.Millisecond)
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("%v 나 붙잡고 있었다 — 예산을 안 지킨다", el)
	}
}
