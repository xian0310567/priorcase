package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withHome(t *testing.T, home string, docsExists bool) {
	t.Helper()
	oldHome, oldStat := userHome, osStat
	userHome = func() (string, error) { return home, nil }
	osStat = func(p string) (fs.FileInfo, error) {
		if p == filepath.Join(home, "Documents") && !docsExists {
			return nil, os.ErrNotExist
		}
		return os.Stat(home) // 디렉토리인 것만 쓰이므로 홈으로 대신한다
	}
	t.Cleanup(func() { userHome, osStat = oldHome, oldStat })
}

// ★★★ **새 볼트는 지금 볼트 옆에 만든다.**
//
// 볼트를 여럿 두는 이유가 공유를 위한 분리다 — 프로젝트 폴더 단위로 git 에
// 올리거나 동기화한다. 그러려면 사람이 Finder 에서 보고 다룰 수 있는 자리여야
// 하고, 그 자리는 이미 정해져 있다: 지금 쓰는 볼트 옆.
func TestNewVaultGoesBesideTheCurrentOne(t *testing.T) {
	c := &Config{Vaults: []Vault{{Name: DefaultVaultName, Path: "/Users/x/Documents/Obsidian Vault"}}}
	got, err := c.NewVaultPath("회사")
	if err != nil {
		t.Fatal(err)
	}
	if want := "/Users/x/Documents/회사"; got != want {
		t.Errorf("%q — %q 여야 한다", got, want)
	}
}

// ★★ 볼트가 하나도 없으면 ~/Documents 다 — Obsidian 이 기본으로 여는 자리다.
func TestFirstVaultGoesToDocuments(t *testing.T) {
	withHome(t, t.TempDir(), true)
	c := &Config{}
	got, err := c.NewVaultPath("메모")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(got)) != "Documents" {
		t.Errorf("%q — Documents 밑이어야 한다", got)
	}
}

// ★★ Documents 가 없으면 홈이다. 없는 자리를 부모로 주면 만들기가 실패한다.
func TestFallsBackToHomeWithoutDocuments(t *testing.T) {
	home := t.TempDir()
	withHome(t, home, false)
	c := &Config{}
	got, err := c.NewVaultPath("메모")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(home, "메모") {
		t.Errorf("%q — 홈 밑이어야 한다", got)
	}
}

// ★★★ **부모 디렉토리를 벗어나면 안 된다.**
//
// 이 값은 그대로 os.MkdirAll 로 간다. 이름에 경로 구분자나 `..` 가 들어오면
// 사람이 의도하지 않은 자리에 폴더가 생긴다.
func TestNameCannotEscapeTheParent(t *testing.T) {
	c := &Config{Vaults: []Vault{{Name: DefaultVaultName, Path: "/Users/x/Documents/볼트"}}}
	for _, bad := range []string{"../탈출", "a/b", "..", ".", "..\\x", ".숨김", "/절대"} {
		if got, err := c.NewVaultPath(bad); err == nil {
			t.Errorf("%q 를 받아 줬다 → %q", bad, got)
		}
	}
}

// ★★★ **이름을 조용히 고치지 않는다.**
//
// 슬그머니 바꾸면 사람이 자기가 적은 이름으로 폴더를 찾다가 못 찾는다.
// 거부하고 무엇이 문제인지 말한다.
func TestBadCharsAreRejectedNotSanitized(t *testing.T) {
	c := &Config{Vaults: []Vault{{Name: DefaultVaultName, Path: "/v/볼트"}}}
	_, err := c.NewVaultPath("회사:비밀")
	if err == nil {
		t.Fatal("못 쓰는 문자를 받아 줬다")
	}
	if !strings.Contains(err.Error(), "회사:비밀") {
		t.Errorf("무엇이 문제인지 안 보여 준다: %v", err)
	}
}

// ★★ **공백은 정상이다.** 실측으로 지금 볼트 이름이 `Obsidian Vault` 다.
// 파일명 slug 규칙(store.Slugify)을 그대로 가져오면 공백이 `-` 가 된다.
func TestSpacesAreFineInVaultNames(t *testing.T) {
	c := &Config{Vaults: []Vault{{Name: DefaultVaultName, Path: "/v/볼트"}}}
	got, err := c.NewVaultPath("우리 회사")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/v/우리 회사" {
		t.Errorf("%q — 공백이 살아 있어야 한다", got)
	}
}

// ★★ 앞뒤 공백은 다듬는다 — 사람이 붙여넣기로 흘린 것까지 폴더 이름이 되면 안 된다.
func TestNameIsTrimmed(t *testing.T) {
	c := &Config{Vaults: []Vault{{Name: DefaultVaultName, Path: "/v/볼트"}}}
	got, err := c.NewVaultPath("  회사  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/v/회사" {
		t.Errorf("%q — 앞뒤 공백을 다듬어야 한다", got)
	}
}
