package daemon

import (
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
)

func hostNames(t *testing.T, c *config.Config, override string) []string {
	t.Helper()
	rs, err := ResolveHosts(c, override)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Host.Name)
	}
	return out
}

func has(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// ★★★ **끈 호스트는 훑지 않는다.**
//
// 설정에 값만 들어가고 훑기가 그것을 안 읽으면, 사람은 껐다고 믿는데 대화는
// 계속 읽힌다. 이 프로젝트가 경계하는 조용한 실패 그대로다.
func TestDisabledHostIsNotResolved(t *testing.T) {
	off := false
	c := &config.Config{Host: []config.Host{{Name: "Codex CLI", Enabled: &off}}}
	if names := hostNames(t, c, ""); has(names, "Codex CLI") {
		t.Errorf("끈 호스트가 목록에 있다: %v", names)
	}
}

// ★★★ **설정에 없는 호스트는 켜진 것이다.** 새 파서를 붙였을 때 기존 사용자의
// 설정에 그 이름이 없다고 꺼져 있으면, 그 사람은 새 기능이 도는 줄 알고 안
// 도는 상태로 남는다.
func TestUnlistedHostIsResolved(t *testing.T) {
	if names := hostNames(t, &config.Config{}, ""); !has(names, "Claude Code") {
		t.Errorf("설정에 없는 호스트가 빠졌다: %v", names)
	}
}

// ★★ **설정의 root 가 기본 자리를 덮는다.** 안 그러면 기록을 옮겨 둔 사람이
// 그 자리를 지정할 방법이 없다.
func TestConfiguredRootOverridesDefault(t *testing.T) {
	c := &config.Config{Host: []config.Host{{Name: "Codex CLI", Root: "/tmp/codex-elsewhere"}}}
	rs, err := ResolveHosts(c, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.Host.Name == "Codex CLI" && r.Root != "/tmp/codex-elsewhere" {
			t.Errorf("root %q — 설정 값이어야 한다", r.Root)
		}
	}
}

// ★★★ **--transcript-root 는 설정을 이긴다.**
//
// 사람이 방금 손으로 지목한 자리다. 설정이 그 호스트를 꺼 뒀다고 빈 목록을
// 주면, 왜 아무것도 안 읽히는지 알 방법이 없다.
func TestOverrideBeatsDisabled(t *testing.T) {
	off := false
	c := &config.Config{Host: []config.Host{{Name: "Claude Code", Enabled: &off}}}
	if names := hostNames(t, c, t.TempDir()); len(names) != 1 {
		t.Errorf("호스트 %v — 지목한 하나여야 한다", names)
	}
}

// ★★★ **전부 끄는 것은 에러가 아니다.**
//
// Claude Code 가 Required 인 것은 "자리를 못 찾으면 배선이 틀렸다" 는 뜻이지
// "끌 수 없다" 는 뜻이 아니다. 명시적으로 끈 것과 없어서 못 찾은 것은 다르다.
func TestDisablingEverythingIsAllowed(t *testing.T) {
	off := false
	c := &config.Config{Host: []config.Host{
		{Name: "Claude Code", Enabled: &off},
		{Name: "Codex CLI", Enabled: &off},
	}}
	rs, err := ResolveHosts(c, "")
	if err != nil {
		t.Fatalf("전부 끄는 것을 에러로 봤다: %v", err)
	}
	if len(rs) != 0 {
		t.Errorf("호스트 %d개 — 0개여야 한다", len(rs))
	}
}
