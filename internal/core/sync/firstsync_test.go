package sync

import (
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
)

// **새 볼트의 첫 동기화** — 한 번도 안 돌아본 길이었다.
//
// 2026-09-01: 회사 볼트를 만들고 리모트를 붙인 뒤 `prior sync` 를 돌리니 둘이 났다.
//
//	pull 실패: fatal: Updating an unborn branch with changes added to the index.
//	commit 실패: fatal: Author identity unknown
//
// 둘 다 **커밋이 하나도 없는 저장소**에서만 나는 고장이라, 이미 쓰던 볼트로는
// 영영 재현되지 않는다. 그런데 그 상태가 곧 **모든 새 볼트의 첫 경험**이다.
//
// 게다가 조용하다 — 훅은 무슨 일이 있어도 exit 0 이라, 사업주가 손으로 `prior sync`
// 를 치기 전까지 결정 63건이 백업 없이 로컬에만 있었다.

// bare 는 커밋이 하나도 없는(unborn) 작업 사본과 그 리모트를 만든다.
func unborn(t *testing.T) string {
	t.Helper()
	a, _ := pair(t)
	// 새 볼트를 흉내 낸다: 커밋 이력 없이 파일만 있고 인덱스에 올라가 있다.
	root := t.TempDir()
	git(t, root, "init", "-b", "main", "fresh")
	fresh := root + "/fresh"
	git(t, fresh, "remote", "add", "origin", a+"/../remote.git")
	write(t, fresh, "editup/decisions/x.md", "---\ntype: decision\n---\n본문\n")
	git(t, fresh, "add", "-A")
	return fresh
}

// ★★★ 커밋이 없는 저장소에서 pull 은 **건너뛴다.**
//
// 가져올 것이 없다 — 이 저장소에는 아직 아무것도 없고, 인덱스에 올린 것을
// rebase 로 얹을 바닥도 없다. 실패로 다루면 새 볼트의 첫 동기화가 언제나 빨간불이다.
func TestPullSkipsUnbornBranch(t *testing.T) {
	r := Pull(unborn(t))
	if r.Err != nil {
		t.Fatalf("첫 동기화에서 죽었다: %v", r.Err)
	}
	if r.Skipped == "" {
		t.Error("건너뛴 이유를 안 말한다 — 조용히 성공하면 가져온 줄 안다")
	}
}

// ★★★ 신원이 없어도 **첫 커밋이 된다.**
//
// 전역 git 에 user.email 이 없는 사람이 흔하다(사업주 머신이 그랬다 — name 만 있었다).
// 이미 쓰던 볼트는 로컬 설정을 갖고 있어서 우연히 됐고, 그래서 이 고장이 새 볼트에서만
// 났다. 앱만 받은 사람에게 "터미널에서 git config 를 치세요" 는 길이 아니다.
//
// # 이 테스트는 한 번 거짓말을 했다 (2026-09-02)
//
// 예전 판은 `Push` 가 "Author identity" 로 안 죽는지만 봤다. **그건 macOS 에서
// 언제나 통과한다** — git 이 설정을 못 찾으면 OS 계정에서 신원을 지어내기 때문이다:
//
//	GIT_CONFIG_GLOBAL=/nonexistent git commit
//	→ Lee Jeonghan <eonghan@Leeui-MacBookPro.local>     (GECOS 에서 추론)
//
// 우분투 CI 러너는 GECOS 에 전체 이름이 없어 추론이 안 되고 그대로 죽는다. 즉 개발
// 머신에서만 초록이고 CI 에서만 빨간, **고장을 가려 주는 테스트**였다.
//
// 그래서 판정을 바꿨다: 커밋이 됐는지가 아니라 **EnsureIdentity 가 그 저장소에 신원을
// 실제로 심었는지**를 본다. 이건 OS 가 무엇을 지어내든 결과가 같다.
func TestEnsureIdentityWritesOneWhenNoVaultCanDonate(t *testing.T) {
	fresh := unborn(t)
	// 이 저장소에 신원이 없다. 전역·시스템도 못 쓰게 막는다.
	t.Setenv("GIT_CONFIG_GLOBAL", t.TempDir()+"/nonexistent")
	t.Setenv("GIT_CONFIG_SYSTEM", t.TempDir()+"/nonexistent")

	// 볼트가 하나뿐이라 **물려줄 곳이 없다.** 앱만 받은 사람의 첫 볼트가 이 모양이다.
	c := &config.Config{Vaults: []config.Vault{{Name: "회사", Path: fresh}}}
	EnsureIdentity(c, fresh, time.Second)

	// 저장소 **로컬** 설정에 심어야 한다. 전역을 건드리면 남의 프로젝트까지 바뀐다.
	name := gitConfig(fresh, "user.name", time.Second)
	email := gitConfig(fresh, "user.email", time.Second)
	if name == "" || email == "" {
		t.Fatalf("물려줄 곳이 없을 때 신원을 안 심었다 — 이 사람의 볼트는 영영 백업되지 않는다 (name=%q email=%q)", name, email)
	}
	if !strings.Contains(email, "@") {
		t.Errorf("메일 모양이 아니다: %q", email)
	}

	r := Push(fresh, "첫 커밋")
	if r.Err != nil && strings.Contains(r.Err.Error(), "Author identity") {
		t.Fatalf("신원을 심었는데도 커밋을 못 했다: %v", r.Err)
	}
}

// **물려받을 수 있으면 지어내지 않는다.** 사업주가 이미 쓰는 신원이 있으면 그것이
// 정답이고, OS 계정 폴백은 그 뒤의 마지막 수단이다.
func TestEnsureIdentityPrefersDonorOverOSAccount(t *testing.T) {
	fresh := unborn(t)
	donor := t.TempDir()
	git(t, donor, "init", "-b", "main", ".")
	git(t, donor, "config", "user.name", "이정한")
	git(t, donor, "config", "user.email", "jeonghan@example.com")
	t.Setenv("GIT_CONFIG_GLOBAL", t.TempDir()+"/nonexistent")
	t.Setenv("GIT_CONFIG_SYSTEM", t.TempDir()+"/nonexistent")

	c := &config.Config{Vaults: []config.Vault{
		{Name: "개인", Path: donor}, {Name: "회사", Path: fresh}}}
	EnsureIdentity(c, fresh, time.Second)

	if got := gitConfig(fresh, "user.email", time.Second); got != "jeonghan@example.com" {
		t.Errorf("다른 볼트의 신원을 안 물려받았다: %q", got)
	}
}

// ★ 다른 볼트에 신원이 있으면 **그것을 물려받는다.**
//
// 값을 지어내지 않는다 — 사업주가 이미 그 신원으로 커밋하고 있으므로, 새 볼트도
// 같은 사람의 것으로 남는 편이 맞다. 원장에 두 사람이 있는 것처럼 보이면 안 된다.
func TestIdentityIsInheritedFromAnotherVault(t *testing.T) {
	old, _ := pair(t)
	git(t, old, "config", "user.email", "me@example.com")
	git(t, old, "config", "user.name", "나")

	fresh := unborn(t)
	c := &config.Config{Vaults: []config.Vault{
		{Name: "default", Path: old},
		{Name: "회사", Path: fresh},
	}}
	EnsureIdentity(c, fresh, time.Second)

	if got := strings.TrimSpace(git(t, fresh, "config", "user.email")); got != "me@example.com" {
		t.Errorf("이메일이 %q — 옛 볼트의 것을 물려받아야 한다", got)
	}
	if got := strings.TrimSpace(git(t, fresh, "config", "user.name")); got != "나" {
		t.Errorf("이름이 %q", got)
	}
}

// ★ 이미 신원이 있으면 **안 건드린다.** 사람이 일부러 다르게 둔 것일 수 있다.
func TestIdentityIsNotOverwritten(t *testing.T) {
	old, _ := pair(t)
	git(t, old, "config", "user.email", "old@example.com")
	git(t, old, "config", "user.name", "옛")

	fresh := unborn(t)
	git(t, fresh, "config", "user.email", "keep@example.com")
	git(t, fresh, "config", "user.name", "지킴")

	c := &config.Config{Vaults: []config.Vault{
		{Name: "default", Path: old}, {Name: "회사", Path: fresh},
	}}
	EnsureIdentity(c, fresh, time.Second)

	if got := strings.TrimSpace(git(t, fresh, "config", "user.email")); got != "keep@example.com" {
		t.Errorf("이메일을 덮어썼다: %q", got)
	}
}

// ★★★ 첫 push 는 **upstream 을 세운다.**
//
// 갓 만든 저장소의 브랜치에는 upstream 이 없어서 맨 `git push` 가 이렇게 죽는다:
// `fatal: The current branch main has no upstream branch.`
// 이것도 새 볼트에서만 나는 고장이라 이미 쓰던 볼트로는 재현되지 않는다.
func TestFirstPushSetsUpstream(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "--bare", "-b", "main", "remote.git")
	git(t, root, "init", "-b", "main", "fresh")
	fresh := root + "/fresh"
	git(t, fresh, "config", "user.email", "t@e.com")
	git(t, fresh, "config", "user.name", "t")
	git(t, fresh, "remote", "add", "origin", root+"/remote.git")
	write(t, fresh, "editup/decisions/x.md", "---\ntype: decision\n---\n본문\n")

	if r := Push(fresh, "첫 커밋"); r.Err != nil {
		t.Fatalf("첫 push 가 죽었다: %v", r.Err)
	}
	// 원격에 실제로 갔는지 본다 — "성공했다" 는 말만으로는 모른다.
	if out := git(t, root+"/remote.git", "log", "--oneline"); !strings.Contains(out, "첫 커밋") {
		t.Errorf("원격에 안 갔다: %s", out)
	}
	// 그리고 다음부터는 upstream 이 있어야 한다.
	if out := git(t, fresh, "rev-parse", "--abbrev-ref", "@{u}"); !strings.Contains(out, "origin/main") {
		t.Errorf("upstream 이 안 섰다: %s", out)
	}
}
