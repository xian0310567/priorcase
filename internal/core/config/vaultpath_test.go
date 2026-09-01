package config

import "testing"

// 볼트가 **잘못된 자리에 만들어졌을 때** 고칠 길이 있어야 한다.
//
// # 고치려는 고장
//
// 2026-09-01: 앱에서 회사 볼트를 만들었더니 `~/Documents/회사` 에 생겼다.
// `vault add` 는 경로를 안 묻고 **기존 볼트 옆**에 만드는데, 기본 볼트가 거기
// 있었기 때문이다.
//
// 그런데 macOS 는 `~/Documents` 를 보호한다. 앱은 권한이 있어 읽지만
// **터미널에서 도는 prior 와 훅은 못 읽는다** — 그러면 그 볼트에는 기록도 회수도
// 동기화도 안 되는데, 앱 화면은 멀쩡해 보인다.
//
// 폴더를 옮기려 해도 같은 권한이 막는다(`mv` 가 Operation not permitted). 그래서
// **설정에서 자리를 옮기는 길**이 필요했는데 그것이 없었다.

func TestSetVaultPathMovesTheEntry(t *testing.T) {
	out, err := AddVault([]byte(liveish), "회사", "~/Documents/회사")
	if err != nil {
		t.Fatal(err)
	}
	out, err = SetVaultPath(out, "회사", "~/vaults/회사")
	if err != nil {
		t.Fatal(err)
	}
	c := parseOrFail(t, out)

	var got string
	for _, v := range c.Vaults {
		if v.Name == "회사" {
			got = v.Path
		}
	}
	if got != "~/vaults/회사" {
		t.Errorf("경로가 %q — 새 자리여야 한다", got)
	}
	// **다른 볼트는 그대로다.** 하나를 고치다 나머지가 흔들리면 회수가 통째로 죽는다.
	if len(c.Vaults) != 2 {
		t.Errorf("볼트가 %d개 — 2개여야 한다", len(c.Vaults))
	}
	// 설정의 나머지도 안 깨져야 한다 (AddVault 시험과 같은 이유).
	if c.DefaultDomain != "common" {
		t.Errorf("default_domain 이 깨졌다: %q", c.DefaultDomain)
	}
}

// ★ 없는 볼트는 조용히 만들지 않는다 — 오타로 새 볼트가 생기면 기록이 갈린다.
func TestSetVaultPathRefusesUnknown(t *testing.T) {
	if _, err := SetVaultPath([]byte(liveish), "없는볼트", "~/x"); err == nil {
		t.Fatal("모르는 볼트인데 통과했다")
	}
}

// ★ 빈 경로는 거부한다. 빈 값이 들어가면 그 볼트가 어디인지 아무도 모른다.
func TestSetVaultPathRefusesEmpty(t *testing.T) {
	out, _ := AddVault([]byte(liveish), "회사", "~/Documents/회사")
	if _, err := SetVaultPath(out, "회사", "  "); err == nil {
		t.Fatal("빈 경로가 통과했다")
	}
}
