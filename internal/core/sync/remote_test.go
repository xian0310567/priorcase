package sync

import (
	"os/exec"
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
