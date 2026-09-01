package config

import (
	"strings"
	"testing"
)

// 볼트를 **설정에서 뺀다.**
//
// 2026-09-01: 회사 볼트를 만들어 도메인 하나를 옮겼다가 되돌렸다("이건 실패인 것
// 같아"). 그런데 **뺄 길이 없었다** — 만드는 문만 있고 무르는 문이 없어서, 안 쓰는
// 볼트가 설정에 영영 남는다. 그 볼트는 doctor 가 계속 검사하고 sync 가 계속 훑는다.

func TestRemoveVaultDropsTheEntry(t *testing.T) {
	out, err := AddVault([]byte(liveish), "회사", "~/vaults/회사")
	if err != nil {
		t.Fatal(err)
	}
	out, err = RemoveVault(out, "회사")
	if err != nil {
		t.Fatal(err)
	}
	c := parseOrFail(t, out)
	for _, v := range c.Vaults {
		if v.Name == "회사" {
			t.Fatal("볼트가 남았다")
		}
	}
	// **나머지는 그대로다.** 하나를 빼다 기본 볼트가 흔들리면 회수가 통째로 죽는다.
	if len(c.Vaults) != 1 || c.Vaults[0].Name != DefaultVaultName {
		t.Errorf("기본 볼트가 상했다: %+v", c.Vaults)
	}
	if c.DefaultDomain != "common" {
		t.Errorf("설정의 나머지가 깨졌다: %q", c.DefaultDomain)
	}
}

// ★★ **쓰는 도메인이 있으면 거부한다.** 그냥 빼면 그 프로젝트의 기록이 갈 곳을
// 잃는데, 그건 조용하다 — 회수는 0건을 내고 화면은 멀쩡해 보인다.
func TestRemoveVaultRefusesWhenDomainsUseIt(t *testing.T) {
	out, _ := AddVault([]byte(liveish), "회사", "~/vaults/회사")
	out, err := BindDomain(out, "omni", "회사")
	if err != nil {
		t.Fatal(err)
	}
	err = func() error { _, e := RemoveVault(out, "회사"); return e }()
	if err == nil {
		t.Fatal("쓰는 도메인이 있는데 뺐다")
	}
	// **누가 쓰는지 말해야 한다.** 거부만 하면 사람은 무엇을 먼저 옮겨야 할지 모른다.
	if !strings.Contains(err.Error(), "omni") {
		t.Errorf("어느 프로젝트가 막는지 안 말한다: %v", err)
	}
}

// ★★ **기본 볼트는 못 뺀다.** 그것이 빠지면 어느 도메인도 쓸 자리가 없다.
func TestRemoveVaultRefusesDefault(t *testing.T) {
	out, _ := AddVault([]byte(liveish), "회사", "~/vaults/회사")
	if _, err := RemoveVault(out, DefaultVaultName); err == nil {
		t.Fatal("기본 볼트를 뺐다")
	}
}

// ★ 없는 볼트를 빼라는 것은 실수다 — 조용히 성공하면 오타를 못 알아챈다.
func TestRemoveVaultRefusesUnknown(t *testing.T) {
	if _, err := RemoveVault([]byte(liveish), "없는볼트"); err == nil {
		t.Fatal("모르는 볼트인데 통과했다")
	}
}
