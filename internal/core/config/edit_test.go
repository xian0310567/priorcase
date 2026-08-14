package config

import (
	"strings"
	"testing"
)

// 실사용 설정 파일의 모양이다. **주석이 값보다 많다** — 그게 이 시험들의 요점이다.
const liveish = `# priorcase 설정. 2026-08-08 셸 훅에서 이관.

vault = "~/Documents/Obsidian Vault"

# NOI 는 자체 스키마 구역이다.
# ⚠️ 2026-08-09 정정: "회수는 계속 동작한다" 고 적어 뒀는데 거짓이었다.
exclude = ["~/project/NOI"]

default_domain = "common"

[naming]
decision_file = "{domain}-결정-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog       = "99-{project}-작업-로그.md"
index         = "_meta/00-결정-색인.md"

# 옛 stop.sh 의 SIGNALS 를 그대로 옮겼다.
[capture]
signals = ["결정", "선택"]
min_turns = 6

[[domain]]
prefix = "omni"
folder = "omni"
paths  = ["~/project/omni"]

# 아래는 cwd 매핑이 없다 — 볼트 폴더로만 존재한다.
[[domain]]
prefix = "영상제작"
folder = "영상제작"
`

func parseOrFail(t *testing.T, b []byte) *Config {
	t.Helper()
	c, err := parseBytes(b)
	if err != nil {
		t.Fatalf("고친 설정이 안 읽힌다: %v\n---\n%s", err, b)
	}
	return c
}

// ★★★ **손으로 쓴 주석이 살아남아야 한다.**
//
// 실사용 설정에는 왜 NOI 를 제외했는지, 무엇이 2026-08-09 에 거짓으로 판명됐는지가
// 주석으로 적혀 있다. 파싱 후 Marshal 로 다시 쓰면 한 줄이면 되지만 그것이 전부
// 사라지고, **사라진 것은 되살릴 수 없다.** 설정을 고치는 명령이 생기는 순간
// 이 손실은 사용자가 모르는 사이에 일어난다.
func TestEditKeepsComments(t *testing.T) {
	out, err := AddVault([]byte(liveish), "work", "~/vaults/work")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# priorcase 설정. 2026-08-08 셸 훅에서 이관.",
		"# ⚠️ 2026-08-09 정정:",
		"# 옛 stop.sh 의 SIGNALS 를 그대로 옮겼다.",
		"# 아래는 cwd 매핑이 없다 — 볼트 폴더로만 존재한다.",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("주석이 사라졌다: %q", want)
		}
	}
}

// ★★★ **스칼라 vault 를 테이블로 바꿀 때 뒤따르는 최상위 키를 삼키면 안 된다.**
//
// TOML 은 테이블 머리 뒤의 키를 그 테이블 것으로 읽는다. `vault = "..."` 자리에
// 그대로 `[[vault]]` 를 쓰면 그 아래 exclude·default_domain 이 전부 볼트 안으로
// 빨려들고, **파싱은 성공한다.** 그러면 제외 목록이 조용히 사라져 NOI 가 기록
// 대상이 된다 — 그 볼트는 git 이 아니라 되돌릴 수 없다.
func TestAddVaultDoesNotSwallowFollowingKeys(t *testing.T) {
	out, err := AddVault([]byte(liveish), "work", "~/vaults/work")
	if err != nil {
		t.Fatal(err)
	}
	c := parseOrFail(t, out)

	if len(c.Exclude) != 1 || c.Exclude[0] != "~/project/NOI" {
		t.Errorf("exclude 가 딸려 갔다: %v", c.Exclude)
	}
	if c.DefaultDomain != "common" {
		t.Errorf("default_domain 이 딸려 갔다: %q", c.DefaultDomain)
	}
	if len(c.Domain) != 2 {
		t.Errorf("도메인 %d개 — 2개여야 한다", len(c.Domain))
	}
	if c.Capture.MinTurns != 6 {
		t.Errorf("capture 가 망가졌다: %+v", c.Capture)
	}
}

// ★★★ 옛 단일 볼트는 **이름을 잃지 않고** 테이블 두 벌이 된다.
//
// 이름이 바뀌면 `[[domain]] vault = "personal"` 로 엮여 있던 도메인이 전부
// 갈 곳을 잃는다.
func TestAddVaultConvertsScalarFormKeepingDefaultName(t *testing.T) {
	out, err := AddVault([]byte(liveish), "work", "~/vaults/work")
	if err != nil {
		t.Fatal(err)
	}
	c := parseOrFail(t, out)
	if len(c.Vaults) != 2 {
		t.Fatalf("볼트 %d개 — 2개여야 한다: %+v", len(c.Vaults), c.Vaults)
	}
	if c.Vaults[0].Name != DefaultVaultName || c.Vaults[0].Path != "~/Documents/Obsidian Vault" {
		t.Errorf("옛 볼트가 바뀌었다: %+v", c.Vaults[0])
	}
	if c.Vaults[1].Name != "work" || c.Vaults[1].Path != "~/vaults/work" {
		t.Errorf("새 볼트가 틀렸다: %+v", c.Vaults[1])
	}
}

// ★★ 이미 테이블 형태면 한 벌만 덧붙인다.
func TestAddVaultAppendsToTableForm(t *testing.T) {
	src := "[[vault]]\nname = \"personal\"\npath = \"~/v1\"\n"
	out, err := AddVault([]byte(src), "work", "~/v2")
	if err != nil {
		t.Fatal(err)
	}
	c := parseOrFail(t, out)
	if len(c.Vaults) != 2 || c.Vaults[1].Name != "work" {
		t.Errorf("볼트 %+v", c.Vaults)
	}
}

// ★★★ **같은 이름을 두 번 만들면 안 된다.** 도메인이 이름으로 볼트를 고르므로
// 이름이 겹치면 어느 쪽으로 쓰이는지 사람이 알 방법이 없다.
func TestAddVaultRejectsDuplicateName(t *testing.T) {
	src := "[[vault]]\nname = \"personal\"\npath = \"~/v1\"\n"
	if _, err := AddVault([]byte(src), "personal", "~/other"); err == nil {
		t.Error("같은 이름을 받아 줬다")
	}
}

// ★★ 한글 이름을 \uXXXX 로 escape 하지 않는다 — 사람이 자기가 적은 이름을
// 못 알아보면 설정 파일을 손으로 못 고친다.
func TestAddVaultWritesReadableKorean(t *testing.T) {
	out, err := AddVault([]byte(liveish), "회사", "~/vaults/회사")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `name = "회사"`) {
		t.Errorf("한글이 escape 됐다:\n%s", out)
	}
}

// ★★★ 도메인을 볼트에 엮는다.
func TestBindDomainSetsVault(t *testing.T) {
	src, err := AddVault([]byte(liveish), "work", "~/vaults/work")
	if err != nil {
		t.Fatal(err)
	}
	out, err := BindDomain(src, "omni", "work")
	if err != nil {
		t.Fatal(err)
	}
	c := parseOrFail(t, out)
	if c.Domain[0].Vault != "work" {
		t.Errorf("도메인 omni 의 볼트 %q — work 여야 한다", c.Domain[0].Vault)
	}
	// 옆 도메인은 안 건드린다.
	if c.Domain[1].Vault != "" {
		t.Errorf("옆 도메인이 바뀌었다: %+v", c.Domain[1])
	}
}

// ★★ 이미 엮여 있으면 갈아 끼운다 — 줄이 둘 생기면 뒤엣것이 이기는데 사람은
// 앞엣것을 읽는다.
func TestBindDomainReplacesExisting(t *testing.T) {
	src := "[[vault]]\nname = \"a\"\npath = \"~/a\"\n\n[[vault]]\nname = \"b\"\npath = \"~/b\"\n\n" +
		"[[domain]]\nprefix = \"x\"\nvault  = \"a\"\nfolder = \"x\"\n"
	out, err := BindDomain([]byte(src), "x", "b")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(out), "vault  ="); n != 1 {
		t.Errorf("vault 줄이 %d개 — 1개여야 한다:\n%s", n, out)
	}
	c := parseOrFail(t, out)
	if c.Domain[0].Vault != "b" {
		t.Errorf("볼트 %q — b 여야 한다", c.Domain[0].Vault)
	}
}

// ★★★ **없는 볼트로 엮으면 그 도메인의 기록이 갈 곳을 잃는다.** 오타 하나로
// 한 프로젝트의 결정이 통째로 안 써지는데 겉으로는 아무 일도 안 난다.
func TestBindDomainRejectsUnknownVault(t *testing.T) {
	if _, err := BindDomain([]byte(liveish), "omni", "없는볼트"); err == nil {
		t.Error("없는 볼트를 받아 줬다")
	}
}

// ★★ 없는 도메인은 거부한다.
func TestBindDomainRejectsUnknownDomain(t *testing.T) {
	if _, err := BindDomain([]byte(liveish), "없는도메인", ""); err == nil {
		t.Error("없는 도메인을 받아 줬다")
	}
}

// ★★ 빈 볼트는 "기본 볼트로 되돌린다" 는 뜻이고, 그러면 줄이 사라져야 한다.
func TestBindDomainEmptyRemovesLine(t *testing.T) {
	src := "[[domain]]\nprefix = \"x\"\nvault  = \"a\"\nfolder = \"x\"\n"
	out, err := BindDomain([]byte(src), "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "vault") {
		t.Errorf("vault 줄이 남았다:\n%s", out)
	}
}

// ★★★ 호스트를 끄고 켠다.
func TestSetHostUpserts(t *testing.T) {
	out, err := SetHost([]byte(liveish), "Codex CLI", false, "")
	if err != nil {
		t.Fatal(err)
	}
	c := parseOrFail(t, out)
	if c.HostOn("Codex CLI") {
		t.Error("껐는데 켜져 있다")
	}
	// 같은 이름을 다시 쓰면 블록이 둘 생기면 안 된다.
	out2, err := SetHost(out, "Codex CLI", true, "~/.codex/sessions")
	if err != nil {
		t.Fatal(err)
	}
	c2 := parseOrFail(t, out2)
	if len(c2.Host) != 1 {
		t.Fatalf("호스트 블록 %d개 — 1개여야 한다:\n%s", len(c2.Host), out2)
	}
	if !c2.HostOn("Codex CLI") || c2.HostRoot("Codex CLI") != "~/.codex/sessions" {
		t.Errorf("호스트가 틀렸다: %+v", c2.Host[0])
	}
}

// ★★★ **설정에 없는 호스트는 켜진 것이다.**
//
// 새 파서를 더했을 때 기존 사용자의 설정에 그 이름이 없다고 꺼져 있으면, 그
// 사람은 새 기능이 도는 줄 알고 안 도는 상태로 남는다 — 조용한 실패다.
func TestUnlistedHostIsOn(t *testing.T) {
	c := parseOrFail(t, []byte(liveish))
	if !c.HostOn("Claude Code") {
		t.Error("설정에 없는 호스트가 꺼져 있다")
	}
}

// ★★★ **그물이 실제로 잡는가.**
//
// 줄 수술은 조용히 옆엣것을 건드릴 수 있다. edit 는 고치기 전후를 파싱해
// 의도한 변경 하나만 일어났는지 보고, 아니면 결과를 버린다. 그 판정이 없으면
// 위의 모든 시험은 "내가 상상한 사고" 만 막고 상상 못 한 사고는 통과시킨다.
func TestEditRefusesUnintendedChange(t *testing.T) {
	// exclude 를 몰래 지우는 수술.
	_, err := edit([]byte(liveish), func(lines []string) ([]string, error) {
		out := []string{}
		for _, l := range lines {
			if keyOf(l) == "exclude" {
				continue
			}
			out = append(out, l)
		}
		return out, nil
	}, func(c *Config) { /* 아무것도 의도하지 않았다 */ })
	if err == nil {
		t.Fatal("의도하지 않은 변경을 통과시켰다")
	}
	if !strings.Contains(err.Error(), "exclude") {
		t.Errorf("무엇이 어긋났는지 안 말한다: %v", err)
	}
}

// ★★★ 결과가 TOML 로 안 읽히면 쓰지 않는다.
func TestEditRefusesBrokenResult(t *testing.T) {
	_, err := edit([]byte(liveish), func(lines []string) ([]string, error) {
		return append(lines, `이건 = "닫히지 않은`), nil
	}, func(c *Config) {})
	if err == nil {
		t.Fatal("깨진 결과를 통과시켰다")
	}
	if !strings.Contains(err.Error(), "쓰지 않았다") {
		t.Errorf("안 썼다는 사실을 안 말한다: %v", err)
	}
}

// ★★ **파일은 개행으로 끝나야 한다.** 블록을 끝에 붙이면 마지막 줄의 개행이
// 사라지고, 다음 편집이 그 줄에 이어 붙어 두 키가 한 줄이 된다.
func TestEditKeepsTrailingNewline(t *testing.T) {
	out, err := AddVault([]byte(liveish), "work", "~/w")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(out); n == 0 || out[n-1] != '\n' {
		t.Errorf("개행으로 안 끝난다: %q", string(out[max(0, len(out)-20):]))
	}
	// 이어서 또 고쳐도 줄이 뭉치지 않는다.
	out2, err := SetHost(out, "Codex CLI", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out2), `path = "~/w"[[host]]`) {
		t.Errorf("줄이 뭉쳤다:\n%s", out2)
	}
	if n := len(out2); out2[n-1] != '\n' {
		t.Error("두 번째 편집 뒤에도 개행이 없다")
	}
}
