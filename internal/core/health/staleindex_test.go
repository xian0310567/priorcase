package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// 잔재가 없으면 **줄 자체가 없어야 한다.** 늘 뜨는 줄은 읽히지 않는다.
func TestStaleIndexQuietWhenAbsent(t *testing.T) {
	c := testutil.VaultConfig(t)
	c.Naming.Index = ""
	l := store.NewLayout(c)
	for _, ck := range Vault(c, l).Checks {
		if ck.Name == "낡은 색인" {
			t.Errorf("잔재가 없는데 줄이 생겼다: %s", ck.Detail)
		}
	}
}

func TestStaleIndexFoundByWellKnownName(t *testing.T) {
	c := testutil.VaultConfig(t)
	c.Naming.Index = "" // 키는 지웠다
	l := store.NewLayout(c)
	writeStale(t, l.Vault(), "_meta/00-결정-색인.md")

	got := find(t, Vault(c, l), "낡은 색인")
	if !strings.Contains(got.Detail, "00-결정-색인") {
		t.Errorf("어느 파일인지 안 말한다: %s", got.Detail)
	}
	// **다시 생기는 원인을 말해야 한다.** 지우라고만 하면 내일 또 지운다.
	if !strings.Contains(got.Fix, "옛 판") {
		t.Errorf("재발 원인(다른 머신이 옛 판)을 안 말한다: %s", got.Fix)
	}
}

func TestStaleIndexKeyAndFileTogether(t *testing.T) {
	c := testutil.VaultConfig(t)
	c.Naming.Index = "_meta/00-결정-색인.md"
	l := store.NewLayout(c)
	writeStale(t, l.Vault(), "_meta/00-결정-색인.md")

	got := find(t, Vault(c, l), "낡은 색인")
	for _, want := range []string{"[naming] index", "00-결정-색인"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("%q 를 안 말한다: %s", want, got.Detail)
		}
	}
}

// 영문 볼트의 옛 이름도 잡는다 — 2026-08-21 볼트 합치기 전에 그 이름을 쓰던 머신이 있었다.
func TestStaleIndexFindsEnglishName(t *testing.T) {
	c := testutil.VaultConfig(t)
	c.Naming.Index = ""
	l := store.NewLayout(c)
	writeStale(t, l.Vault(), "_meta/00-decision-index.md")

	if got := find(t, Vault(c, l), "낡은 색인"); !strings.Contains(got.Detail, "00-decision-index") {
		t.Errorf("영문 이름을 못 잡는다: %s", got.Detail)
	}
}

// **경고가 아니라 사실이다.** 고장이 아니라 잔재이므로 등급을 올리면 소음이 된다.
func TestStaleIndexIsNotAWarning(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	writeStale(t, l.Vault(), "_meta/00-결정-색인.md")
	if got := find(t, Vault(c, l), "낡은 색인"); got.Level != OK {
		t.Errorf("등급 = %v, want OK", got.Level)
	}
}

func writeStale(t *testing.T, vault, rel string) {
	t.Helper()
	p := filepath.Join(vault, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("# 결정 색인\n\n옛 판이 만든 것이다.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
