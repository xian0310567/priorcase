package hook

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
)

// 세션 중간의 신선도 — **긴 세션에서 남의 결정을 못 보는 것**을 고친다.
//
// # 고치려는 고장
//
// 동기화는 세션 경계에서만 돈다. 혼자 쓰는 볼트에서는 그것으로 충분하다 —
// 내가 없는 동안 볼트가 바뀔 일이 없다.
//
// 공유 볼트는 다르다. 세 시간짜리 세션이면 그동안 동료가 내린 결정을 **한 건도
// 못 본다.** 회수는 로컬 파일만 읽으므로, 그 세 시간 내내 낡은 볼트를 훑으면서
// "관련 결정 없음" 이라고 답한다. 조용하다.
//
// # 왜 백그라운드인가
//
// 회수는 매 프롬프트마다 돈다. 거기서 네트워크를 타면 그 지연을 사람이 매번
// 겪는다. 그래서 띄워만 놓고 안 기다린다 — 이번 회수는 낡은 볼트를 보지만
// 다음 회수부터 신선하다. 신선도는 **누적되는 값**이라 그것으로 충분하다.

func sharedCfg(shared bool) *config.Config {
	return &config.Config{Vaults: []config.Vault{{Name: "회사", Path: "/v", Shared: shared}}}
}

// ★ 처음에는 받는다. 도장이 없다는 것은 이 세션에서 아직 안 받았다는 뜻이다.
func TestFreshenDueWithoutStamp(t *testing.T) {
	if !dueForFreshen(time.Time{}, false, time.Now()) {
		t.Fatal("도장이 없는데 안 받는다")
	}
}

// ★ 방금 받았으면 안 받는다. 매 프롬프트마다 프로세스를 띄우면 대화 한 번에
// 수십 개가 뜬다.
func TestFreshenNotDueRightAfter(t *testing.T) {
	now := time.Now()
	if dueForFreshen(now.Add(-30*time.Second), true, now) {
		t.Fatal("30초 전에 받았는데 또 받는다")
	}
}

// ★ 창을 넘기면 받는다.
func TestFreshenDueAfterInterval(t *testing.T) {
	now := time.Now()
	if !dueForFreshen(now.Add(-freshenInterval-time.Second), true, now) {
		t.Fatalf("%v 가 지났는데 안 받는다", freshenInterval)
	}
}

// ★★ **개인 볼트만이면 아무 일도 안 한다.** 혼자 쓰는 볼트는 내가 없는 동안
// 바뀌지 않으므로, 세션 중간에 네트워크를 타는 것은 순수한 낭비다.
func TestFreshenSkipsPrivateOnlyConfigs(t *testing.T) {
	spawned := 0
	o := Options{Config: sharedCfg(false), StateDir: t.TempDir()}
	o.spawn = func(string, ...string) error { spawned++; return nil }
	o.freshen()
	if spawned != 0 {
		t.Fatalf("개인 볼트인데 %d번 띄웠다", spawned)
	}
}

// ★★★ 공유 볼트가 있으면 띄운다. 그리고 **기다리지 않는다** — 이 시험이
// 끝난다는 사실 자체가 그것을 보인다.
func TestFreshenSpawnsForSharedVault(t *testing.T) {
	var got []string
	o := Options{Config: sharedCfg(true), StateDir: t.TempDir()}
	o.spawn = func(bin string, args ...string) error { got = args; return nil }
	o.freshen()
	if len(got) == 0 {
		t.Fatal("공유 볼트인데 안 띄웠다")
	}
	// **받기만 한다.** 세션 중간에 미는 것은 다른 문제다 — 그쪽은 Stop 이 이미
	// 디바운스를 걸어 다루고 있고, 여기서 같이 하면 그 창이 무의미해진다.
	if len(got) != 2 || got[0] != "sync" || got[1] != "--pull" {
		t.Errorf("받는 명령이 아니다: %v", got)
	}
}

// ★★★ **플래그 이름이 CLI 와 같아야 한다.**
//
// 우리는 자식을 안 기다리므로, 이름이 틀리면 자식이 즉시 죽고도 **아무 일도 안
// 일어난 것과 구별되지 않는다.** 실제로 `--pull-only` 라고 썼다가 이 시험이 없어
// 못 잡을 뻔했다 — 부분문자열로 "pull 이 들어 있나" 만 보던 단언은 그것도 통과시킨다.
//
// 여기서는 진짜 CLI 에 물어본다. 흉내를 내면 검사하는 것이 우리 흉내뿐이 된다.
func TestFreshenArgsMatchTheRealCLI(t *testing.T) {
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "prior")
	build := exec.Command("go", "build", "-o", bin, "./cmd/prior")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("빌드할 수 없다: %v\n%s", err, out)
	}
	// `--help` 가 아니라 실제로 돌린다. 없는 플래그면 cobra 가 거부한다.
	// 설정이 없어 동기화 자체는 실패해도 좋다 — 보는 것은 플래그 파싱이다.
	cmd := exec.Command(bin, append(freshenArgs, "--config", filepath.Join(t.TempDir(), "none.toml"))...)
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "unknown flag") {
		t.Fatalf("freshenArgs 가 CLI 와 안 맞는다 (%v): %s", freshenArgs, out)
	}
}

// repoRoot 는 go.mod 가 있는 자리까지 올라간다.
func repoRoot(t *testing.T) string {
	t.Helper()
	d, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		d = filepath.Dir(d)
	}
	t.Fatal("go.mod 를 못 찾았다")
	return ""
}

// ★★ 도장을 **띄우기 전에** 찍는다.
//
// 나중에 찍으면 자식이 도는 동안 온 프롬프트마다 또 띄운다 — 느린 네트워크일수록
// 더 많이 뜨고, 그건 정확히 느릴 때 최악으로 구는 설계다.
func TestFreshenStampsBeforeSpawning(t *testing.T) {
	dir := t.TempDir()
	o := Options{Config: sharedCfg(true), StateDir: dir}
	o.spawn = func(string, ...string) error { return nil }

	o.freshen()
	first := 0
	o.spawn = func(string, ...string) error { first++; return nil }
	o.freshen() // 바로 다시 — 창 안이므로 안 띄워야 한다
	if first != 0 {
		t.Fatalf("연달아 %d번 띄웠다 — 도장이 안 찍혔거나 늦게 찍힌다", first)
	}
}

// ★ 상태 디렉토리가 없으면 조용히 아무것도 안 한다. 도장을 못 찍으면 매번 띄우게
// 되는데, 그건 안 하느니만 못하다.
func TestFreshenWithoutStateDirDoesNothing(t *testing.T) {
	spawned := 0
	o := Options{Config: sharedCfg(true)}
	o.spawn = func(string, ...string) error { spawned++; return nil }
	o.freshen()
	if spawned != 0 {
		t.Fatalf("도장 찍을 자리가 없는데 %d번 띄웠다", spawned)
	}
}
