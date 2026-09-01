package sync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// ★ **git 저장소가 아니어도 붙는다.**
//
// 앱만 받은 사람에게 "먼저 터미널에서 git init 을 치세요" 라고 말하는 것은 그
// 사람이 할 수 없는 일을 시키는 것이다. 회사 볼트는 만들자마자 회사 리모트에
// 붙어야 그 결정이 개인 머신에만 남지 않는다.
func TestSetRemoteInitialisesRepo(t *testing.T) {
	dir := t.TempDir()
	const url = "https://git-codecommit.ap-northeast-2.amazonaws.com/v1/repos/vault"
	if err := SetRemote(dir, url); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}
	if got := gitOut(t, dir, "remote", "get-url", "origin"); got != url {
		t.Errorf("origin = %q, want %q", got, url)
	}
	got, err := Remote(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != url {
		t.Errorf("Remote() = %q, want %q", got, url)
	}
}

// 이미 있는 origin 은 **바꾼다.** add 를 먼저 하면 "already exists" 로 죽는다.
func TestSetRemoteReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	if err := SetRemote(dir, "https://example.com/first.git"); err != nil {
		t.Fatal(err)
	}
	if err := SetRemote(dir, "https://example.com/second.git"); err != nil {
		t.Fatalf("두 번째 SetRemote: %v", err)
	}
	if got := gitOut(t, dir, "remote", "get-url", "origin"); got != "https://example.com/second.git" {
		t.Errorf("origin = %q — 바뀌지 않았다", got)
	}
	// 리모트가 두 개가 되면 push 대상이 갈린다.
	if got := gitOut(t, dir, "remote"); got != "origin" {
		t.Errorf("리모트 목록 = %q, want origin 하나", got)
	}
}

// 리모트가 없는 것은 **고장이 아니다** — 아직 안 붙인 볼트다. 에러로 내면
// 앱의 볼트 화면이 정상 상태를 빨갛게 그린다.
func TestRemoteEmptyIsNotAnError(t *testing.T) {
	got, err := Remote(t.TempDir())
	if err != nil {
		t.Errorf("리모트 없는 자리에서 에러가 났다: %v", err)
	}
	if got != "" {
		t.Errorf("Remote() = %q, want \"\"", got)
	}
}

func TestSetRemoteRejectsEmptyURL(t *testing.T) {
	if err := SetRemote(t.TempDir(), "   "); err == nil {
		t.Error("빈 URL 인데 에러가 안 났다 — 그러면 origin 이 빈 값으로 박힌다")
	}
}

// ── 리모트를 떼는 길 ──────────────────────────────────────────────────
//
// 2026-09-01 사업주 지적("처음 설치한 사람은 저게 없을 거 아니냐")에서 나왔다.
// 리모트가 **없는** 상태는 이미 정상으로 다룬다 — sync 는 조용히 건너뛰고 doctor 는
// "이 머신에서만 쓴다" 로 초록을 낸다.
//
// 그런데 그 반대가 막혀 있었다: **한 번 넣은 리모트를 뺄 수 없다.** 앱은 빈 값
// 저장을 막고(버튼이 죽는다), CLI 는 빈 URL 을 거부한다(위 시험). 둘 다 "실수로
// origin 이 빈 값으로 박히는 것" 을 막으려는 것이라 각각은 옳은데, 합치면 **오타로
// 넣은 주소를 영영 못 지우는** 상태가 된다.
//
// 그래서 지우는 것은 **따로 명시하는 동작**으로 낸다. 빈 값이 실수로 새어 들어가는
// 길은 그대로 막아 둔다.

// ★★★ 리모트를 뗄 수 있다.
func TestRemoveRemoteDetachesIt(t *testing.T) {
	a, _ := pair(t)
	if url, _ := Remote(a); url == "" {
		t.Fatal("픽스처에 리모트가 없다")
	}
	if err := RemoveRemote(a); err != nil {
		t.Fatal(err)
	}
	if url, _ := Remote(a); url != "" {
		t.Errorf("리모트가 %q 로 남았다", url)
	}
}

// ★ 없는 것을 떼는 것은 **고장이 아니다.** 이미 원하는 상태다.
func TestRemoveRemoteIsIdempotent(t *testing.T) {
	a, _ := pair(t)
	if err := RemoveRemote(a); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRemote(a); err != nil {
		t.Errorf("두 번째에 실패했다: %v", err)
	}
}

// ★★ **볼트는 그대로다.** 리모트를 떼는 것은 동기화를 끊는 일이지 기록을 버리는
// 일이 아니다. 커밋 이력도 파일도 남아야 다시 붙일 수 있다.
func TestRemoveRemoteKeepsTheVault(t *testing.T) {
	a, _ := pair(t)
	before := git(t, a, "log", "--oneline")
	if err := RemoveRemote(a); err != nil {
		t.Fatal(err)
	}
	if after := git(t, a, "log", "--oneline"); after != before {
		t.Errorf("이력이 달라졌다:\n전: %s후: %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(a, "seed.md")); err != nil {
		t.Errorf("파일이 사라졌다: %v", err)
	}
}
