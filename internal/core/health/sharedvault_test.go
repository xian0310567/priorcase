package health

import (
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
)

// 공유 볼트는 **리모트가 없으면 고장이다.**
//
// 개인 볼트에서 리모트가 없는 것은 정상이다 — 그 머신에서만 쓰는 볼트다.
// 그런데 공유 볼트에 리모트가 없으면 **그 사람의 결정이 아무에게도 안 간다.**
// 본인 화면에는 다 잘 보이므로 알아챌 방법이 없다. 이 패키지가 존재하는 이유
// (조용한 무동작)의 교과서적인 사례다.

func vaults(vs ...config.Vault) *config.Config { return &config.Config{Vaults: vs} }

func check(t *testing.T, c *config.Config, name string) Check {
	t.Helper()
	r := &Report{}
	checkSharedRemote(r, c)
	for _, ck := range r.Checks {
		if ck.Name == name {
			return ck
		}
	}
	t.Fatalf("%q 검사가 없다 (%d개: %+v)", name, len(r.Checks), r.Checks)
	return Check{}
}

// ★★★ 공유인데 리모트가 없으면 Fail 이다. 경고가 아니라 실패다 — 동작하는데
// 손해가 있는 것이 아니라, 그 볼트의 목적 자체가 이루어지지 않고 있다.
func TestSharedVaultWithoutRemoteFails(t *testing.T) {
	c := vaults(config.Vault{Name: "회사", Path: t.TempDir(), Shared: true})
	ck := check(t, c, "공유 볼트")
	if ck.Level != Fail {
		t.Fatalf("등급이 %v — Fail 이어야 한다: %s", ck.Level, ck.Detail)
	}
	if !strings.Contains(ck.Detail, "회사") {
		t.Errorf("어느 볼트인지 안 말한다: %q", ck.Detail)
	}
	if ck.Fix == "" {
		t.Error("고칠 방법이 없다")
	}
}

// ★ 개인 볼트는 리모트가 없어도 조용하다. 여기서 경고하면 혼자 쓰는 사람이
// 매번 그것을 보고, 그러면 진짜 경고까지 같이 무시당한다.
func TestPrivateVaultWithoutRemoteIsSilent(t *testing.T) {
	c := vaults(config.Vault{Name: "default", Path: t.TempDir()})
	r := &Report{}
	checkSharedRemote(r, c)
	if len(r.Checks) != 0 {
		t.Fatalf("개인 볼트에 대해 말했다: %+v", r.Checks)
	}
}

// ★ 공유 볼트가 없으면 아무 말도 안 한다 — 아직 안 쓰는 기능이다.
func TestNoSharedVaultIsSilent(t *testing.T) {
	r := &Report{}
	checkSharedRemote(r, vaults(config.Vault{Name: "a", Path: t.TempDir()}))
	if len(r.Checks) != 0 {
		t.Fatalf("쓰지도 않는 기능을 켜라고 조른다: %+v", r.Checks)
	}
}
