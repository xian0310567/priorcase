package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// realisticSettings 는 실제 ~/.claude/settings.json 의 형태다 — second-brain 5개와
// orca 12개가 한 파일에 산다. 이 공존이 이 태스크의 전부다.
const realisticSettings = `{
  "model": "opus",
  "statusLine": {"type": "command", "command": "~/.claude/statusline.sh"},
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "/Users/t/.claude/hooks/second-brain/session-start.sh"}]},
      {"hooks": [{"type": "command", "command": "if [ -f '/Users/t/.orca/agent-hooks/claude-hook.sh' ]; then /bin/sh '/Users/t/.orca/agent-hooks/claude-hook.sh'; fi"}]}
    ],
    "UserPromptSubmit": [
      {"hooks": [{"type": "command", "command": "/Users/t/.claude/hooks/second-brain/user-prompt-submit.sh"}]},
      {"hooks": [{"type": "command", "command": "if [ -f '/Users/t/.orca/agent-hooks/claude-hook.sh' ]; then /bin/sh '/Users/t/.orca/agent-hooks/claude-hook.sh'; fi"}]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "/Users/t/.claude/hooks/second-brain/stop.sh"}]},
      {"hooks": [{"type": "command", "command": "if [ -f '/Users/t/.orca/agent-hooks/claude-hook.sh' ]; then /bin/sh '/Users/t/.orca/agent-hooks/claude-hook.sh'; fi"}]}
    ],
    "PreCompact": [
      {"hooks": [{"type": "command", "command": "/Users/t/.claude/hooks/second-brain/pre-compact.sh"}]}
    ],
    "SessionEnd": [
      {"hooks": [{"type": "command", "command": "/Users/t/.claude/hooks/second-brain/session-end.sh"}]}
    ],
    "PreToolUse": [
      {"matcher": "*", "hooks": [{"type": "command", "command": "if [ -f '/Users/t/.orca/agent-hooks/claude-hook.sh' ]; then /bin/sh '/Users/t/.orca/agent-hooks/claude-hook.sh'; fi"}]}
    ],
    "PostToolUse": [
      {"matcher": "*", "hooks": [{"type": "command", "command": "if [ -f '/Users/t/.orca/agent-hooks/claude-hook.sh' ]; then /bin/sh '/Users/t/.orca/agent-hooks/claude-hook.sh'; fi"}]}
    ],
    "SubagentStop": [
      {"hooks": [{"type": "command", "command": "if [ -f '/Users/t/.orca/agent-hooks/claude-hook.sh' ]; then /bin/sh '/Users/t/.orca/agent-hooks/claude-hook.sh'; fi"}]}
    ]
  }
}`

func writeSettings(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func opts(t *testing.T, settings string) InitOptions {
	t.Helper()
	return InitOptions{
		SettingsPath: settings,
		ConfigPath:   filepath.Join(t.TempDir(), "config.toml"),
		Binary:       "/usr/local/bin/prior",
		Now:          time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
	}
}

// commands 는 결과 설정의 (이벤트 → 명령들) 을 준다.
func commands(t *testing.T, path string) map[string][]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Hooks map[string][]hookGroup `json:"hooks"`
	}
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatalf("결과가 JSON 이 아니다: %v", err)
	}
	out := map[string][]string{}
	for ev, groups := range root.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				out[ev] = append(out[ev], h.Command)
			}
		}
	}
	return out
}

func apply(t *testing.T, o InitOptions) *Plan {
	t.Helper()
	p, err := BuildPlan(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(ReadSettings(o.SettingsPath)); err != nil {
		t.Fatal(err)
	}
	return p
}

// ★ 이 태스크에서 가장 중요한 테스트다.
//
// `~/.claude/settings.json` 은 priorcase 만의 파일이 아니다. 실측으로 orca 훅 12개가
// 같이 산다. 하나라도 잘못 지우면 **사용자의 다른 시스템이 죽는다** — 그리고 그 사람은
// 무엇이 자기 훅을 지웠는지 모른다.
func TestInitPreservesOtherToolsHooks(t *testing.T) {
	sp := writeSettings(t, realisticSettings)
	before := commands(t, sp)

	apply(t, opts(t, sp))
	after := commands(t, sp)

	// orca 훅은 하나도 사라지면 안 된다.
	for ev, cmds := range before {
		for _, c := range cmds {
			if strings.Contains(c, "second-brain") {
				continue // 이건 걷어내는 게 맞다
			}
			found := false
			for _, a := range after[ev] {
				if a == c {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s 의 남의 훅이 사라졌다:\n  %s", ev, c)
			}
		}
	}

	// 우리와 무관한 이벤트는 통째로 그대로여야 한다.
	for _, ev := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		if len(after[ev]) != len(before[ev]) {
			t.Errorf("%s 의 훅 수가 %d → %d 로 바뀌었다", ev, len(before[ev]), len(after[ev]))
		}
	}
}

// 설정의 hooks 밖 키도 그대로여야 한다.
func TestInitKeepsUnrelatedSettings(t *testing.T) {
	sp := writeSettings(t, realisticSettings)
	apply(t, opts(t, sp))

	b, _ := os.ReadFile(sp)
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	if root["model"] != "opus" {
		t.Errorf("model 이 사라졌다: %v", root["model"])
	}
	if root["statusLine"] == nil {
		t.Error("statusLine 이 사라졌다")
	}
}

func TestInitRemovesOldHooksAndAddsOurs(t *testing.T) {
	sp := writeSettings(t, realisticSettings)
	apply(t, opts(t, sp))
	after := commands(t, sp)

	for ev, cmds := range after {
		for _, c := range cmds {
			if strings.Contains(c, "second-brain") {
				t.Errorf("%s 에 옛 훅이 남았다: %s", ev, c)
			}
		}
	}
	for _, ev := range Events {
		name := ev.claudeCodeName()
		found := false
		for _, c := range after[name] {
			if strings.Contains(c, hookMarker) && strings.HasSuffix(c, "hook "+string(ev)) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s 에 priorcase 훅이 안 심겼다: %v", name, after[name])
		}
	}
}

// 두 번 돌려도 중복 등록되지 않는다. 사용자는 init 을 여러 번 돌린다.
func TestInitIsIdempotent(t *testing.T) {
	sp := writeSettings(t, realisticSettings)
	apply(t, opts(t, sp))
	first := commands(t, sp)
	apply(t, opts(t, sp))
	second := commands(t, sp)

	for ev := range second {
		if len(second[ev]) != len(first[ev]) {
			t.Errorf("%s 훅 수가 %d → %d 로 늘었다 — 중복 등록됐다",
				ev, len(first[ev]), len(second[ev]))
		}
	}
}

// 백업은 **바이트 그대로**여야 하고, revert 는 그걸 그대로 되돌려야 한다.
// 우리가 다시 직렬화하면 키 순서·들여쓰기가 바뀌어 복원이 아니게 된다.
func TestBackupAndRevertRestoreBytes(t *testing.T) {
	sp := writeSettings(t, realisticSettings)
	original, _ := os.ReadFile(sp)

	p := apply(t, opts(t, sp))
	if p.BackupPath == "" {
		t.Fatal("백업 경로가 비었다")
	}
	bak, err := os.ReadFile(p.BackupPath)
	if err != nil {
		t.Fatalf("백업 파일이 없다: %v", err)
	}
	if string(bak) != string(original) {
		t.Error("백업이 원본과 다르다")
	}

	changed, _ := os.ReadFile(sp)
	if string(changed) == string(original) {
		t.Fatal("아무것도 안 바뀌었다")
	}

	if _, err := Revert(sp); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(sp)
	if string(restored) != string(original) {
		t.Error("revert 후 바이트가 원본과 다르다")
	}
}

// 설정 파일이 아예 없어도 동작한다 (새 설치).
func TestInitOnMissingSettings(t *testing.T) {
	sp := filepath.Join(t.TempDir(), "없던디렉토리", "settings.json")
	o := opts(t, sp)
	p := apply(t, o)
	if p.BackupPath != "" {
		t.Errorf("없는 파일의 백업을 만들려 했다: %s", p.BackupPath)
	}
	after := commands(t, sp)
	if len(after) != len(Events) {
		t.Errorf("이벤트 %d개, %d개여야 한다", len(after), len(Events))
	}
}

// **깨진 설정을 덮어쓰지 않는다.** JSON 이 아니면 손대기 전에 멈춘다 —
// 여기서 map 으로 못 읽은 채 진행하면 사용자의 설정이 통째로 날아간다.
func TestInitRefusesBrokenSettings(t *testing.T) {
	sp := writeSettings(t, `{이건 JSON 이 아니다`)
	before, _ := os.ReadFile(sp)

	if _, err := BuildPlan(opts(t, sp)); err == nil {
		t.Fatal("깨진 설정을 받아들였다")
	}
	after, _ := os.ReadFile(sp)
	if string(after) != string(before) {
		t.Error("실패했는데 파일이 바뀌었다")
	}
}

// --dry-run 이 보여 주는 계획은 실제로 일어날 일과 같아야 한다.
func TestPlanMatchesWhatHappens(t *testing.T) {
	sp := writeSettings(t, realisticSettings)
	p, err := BuildPlan(opts(t, sp))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Remove) != 5 {
		t.Errorf("걷어낼 훅 %d개, second-brain 5개여야 한다: %v", len(p.Remove), p.Remove)
	}
	if len(p.Add) != len(Events) {
		t.Errorf("심을 훅 %d개, %d개여야 한다", len(p.Add), len(Events))
	}
	if p.Keep != 6 {
		t.Errorf("손대지 않는 훅 %d개, orca 6개여야 한다", p.Keep)
	}

	// 계획만 만들고 파일은 안 건드렸는지
	before, _ := os.ReadFile(sp)
	if !strings.Contains(string(before), "second-brain") {
		t.Error("BuildPlan 이 파일을 건드렸다")
	}
}

// ★★ `--apply` 를 두 번 하면 `--revert` 가 "되돌렸다" 고 말하면서 아무것도 안 되돌렸다.
//
// 두 번째 apply 가 **이미 priorcase 훅이 든 파일**을 백업하고, revert 는 사전순
// 마지막(=그 백업)을 골랐다. 사용자는 지운 줄 아는데 훅 5개가 그대로 산다.
func TestRevertAfterDoubleApply(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "settings.json")
	const original = `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo 남의훅"}]}]}}`
	if err := os.WriteFile(sp, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		p, err := BuildPlan(InitOptions{SettingsPath: sp, Binary: "/bin/true",
			Now: base.Add(time.Duration(i) * time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Apply(ReadSettings(sp)); err != nil {
			t.Fatal(err)
		}
	}

	// 백업은 하나만 생겨야 한다 — 두 번째는 이미 우리 훅이 든 파일이라 백업 대상이 아니다.
	baks, _ := filepath.Glob(sp + ".priorcase-backup-*")
	if len(baks) != 1 {
		t.Errorf("백업 %d개, want 1 — 우리 훅이 든 파일까지 백업했다: %v", len(baks), baks)
	}

	if _, err := Revert(sp); err != nil {
		t.Fatalf("되돌리지 못했다: %v", err)
	}
	got, err := os.ReadFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), hookMarker) {
		t.Errorf("되돌렸다는데 priorcase 훅이 남아 있다:\n%s", got)
	}
	if !strings.Contains(string(got), "남의훅") {
		t.Errorf("남의 훅이 사라졌다:\n%s", got)
	}
}

// 옛 판이 만든 백업(우리 훅이 이미 든 것)이 디스크에 남아 있는 사용자가 여기로 온다.
// 그런 백업은 건너뛰고 더 옛것을 골라야 한다.
func TestRevertSkipsBackupsThatAlreadyContainOurHooks(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(sp, []byte(`{"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// 깨끗한 옛 백업 하나, 오염된 최근 백업 둘.
	clean := sp + ".priorcase-backup-20260101-000000"
	if err := os.WriteFile(clean, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo 원본"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, ts := range []string{"20260808-000000", "20260809-000000"} {
		dirty := sp + ".priorcase-backup-" + ts
		body := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"` + hookMarker + `=1 /bin/prior hook stop"}]}]}}`
		if err := os.WriteFile(dirty, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := LatestBackup(sp)
	if err != nil {
		t.Fatal(err)
	}
	if got != clean {
		t.Errorf("오염된 백업을 골랐다: %s (want %s)", filepath.Base(got), filepath.Base(clean))
	}
}

// 백업이 전부 오염됐으면 조용히 아무거나 고르지 말고 실패해야 한다.
func TestRevertFailsLoudlyWhenAllBackupsAreDirty(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(sp, []byte(`{"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	body := `{"x":"` + hookMarker + `"}`
	if err := os.WriteFile(sp+".priorcase-backup-20260809-000000", []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LatestBackup(sp)
	if err == nil {
		t.Fatal("전부 오염됐는데 성공했다 — 되돌린 척하게 된다")
	}
	if !strings.Contains(err.Error(), "우리 훅을 이미 담고") {
		t.Errorf("왜 못 되돌리는지 안 알려준다: %v", err)
	}
}

// ★ 스타터 설정이 **거짓 약속을 하면 안 된다.**
//
// 예전에 "회수는 계속 동작한다" 고 적혀 있었는데 거짓이었다. exclude 는 쓰기를
// 막는 것이고, 회수는 별개 이유로 막힌다 — prior 는 type: decision 인 문서만 읽는다.
// 실측으로 볼트 392개 중 회수 대상은 76개(19%)뿐이었다. 이 문장은 **새 사용자
// 전원에게** 나가므로 특히 비싸다.
func TestStarterConfigDoesNotPromiseRecallEverywhere(t *testing.T) {
	src, err := os.ReadFile("initcmd.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"회수는 계속 동작한다", "recall still works"} {
		if strings.Contains(string(src), banned) {
			t.Errorf("스타터 설정이 %q 라고 약속한다 — 거짓이다", banned)
		}
	}
	if strings.Count(string(src), "type: decision") < 2 {
		t.Error("회수 한계 설명이 ko·en 양쪽에 없다")
	}
}

// legacySettings 는 개명(2026-08-10) 직전의 실제 ~/.claude/settings.json 모양이다 —
// 우리 훅 5개가 CASEBOOK_HOOK=1 표시를 달고 있고, orca 훅이 같이 산다.
const legacySettings = `{
  "model": "opus",
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "CASEBOOK_HOOK=1 \"/Users/t/.local/bin/cb\" hook session-start"}]},
      {"hooks": [{"type": "command", "command": "if [ -f '/Users/t/.orca/agent-hooks/claude-hook.sh' ]; then /bin/sh '/Users/t/.orca/agent-hooks/claude-hook.sh'; fi"}]}
    ],
    "UserPromptSubmit": [
      {"hooks": [{"type": "command", "command": "CASEBOOK_HOOK=1 \"/Users/t/.local/bin/cb\" hook user-prompt-submit"}]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "CASEBOOK_HOOK=1 \"/Users/t/.local/bin/cb\" hook stop"}]}
    ],
    "PreCompact": [
      {"hooks": [{"type": "command", "command": "CASEBOOK_HOOK=1 \"/Users/t/.local/bin/cb\" hook pre-compact", "timeout": 120}]}
    ],
    "SessionEnd": [
      {"hooks": [{"type": "command", "command": "CASEBOOK_HOOK=1 \"/Users/t/.local/bin/cb\" hook session-end", "timeout": 120}]}
    ]
  }
}`

// ★ **개명 전 훅을 걷어내지 않으면 훅이 두 벌이 된다.**
//
// 걷어내기는 표시(환경변수)로만 우리 것과 남의 것을 가른다. 개명으로 표시가
// CASEBOOK_HOOK 에서 PRIORCASE_HOOK 으로 바뀌었으니, 옛 훅은 "남의 것" 으로 보여
// 그대로 남는다. 그 위에 새 훅 5개가 얹히면 매 이벤트가 두 번 발동한다.
//
// **그리고 조용하다.** 훅의 stdout 은 대화에 안 들어가서 두 번 도는 것과 한 번
// 도는 것이 겉으로 같다. 판별기가 같은 구간에 두 번 불리는 낭비만 남는다.
func TestInitRemovesPreRenameHooks(t *testing.T) {
	sp := writeSettings(t, legacySettings)
	apply(t, opts(t, sp))
	cmds := commands(t, sp)

	for ev, list := range cmds {
		for _, c := range list {
			if strings.Contains(c, "CASEBOOK_HOOK") {
				t.Errorf("%s: 개명 전 훅이 남았다 — 매 이벤트가 두 번 발동한다: %s", ev, c)
			}
		}
	}
	// 이벤트마다 우리 훅은 정확히 하나여야 한다.
	for _, ev := range Events {
		n := 0
		for _, c := range cmds[ev.claudeCodeName()] {
			if strings.Contains(c, hookMarker) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s 에 우리 훅이 %d개다, 1개여야 한다", ev.claudeCodeName(), n)
		}
	}
	// 남의 훅은 그대로 살아 있어야 한다.
	found := false
	for _, list := range cmds {
		for _, c := range list {
			if strings.Contains(c, "orca") {
				found = true
			}
		}
	}
	if !found {
		t.Error("남의 훅(orca)까지 지웠다")
	}
}

// ★ **개명 전 이름으로 뜬 백업도 찾아야 한다.**
//
// 백업 파일 이름에 제품명이 들어간다. 새 이름만 찾으면 `--revert` 가 "백업이
// 없다" 고 하는데, 정작 되돌려야 할 그 파일이 바로 옆에 있다. 실제로 디스크에
// settings.json.casebook-backup-20260808-012309 이 있었다.
func TestRevertFindsPreRenameBackup(t *testing.T) {
	sp := writeSettings(t, realisticSettings)
	clean, _ := os.ReadFile(sp)

	// **먼저 우리 훅을 심어 둔다.** 그래야 두 번째 apply 가 백업을 뜨지 않고
	// (BuildPlan 은 우리 훅이 이미 있으면 건너뛴다), 디스크에 남는 깨끗한 백업이
	// 개명 전 이름 하나뿐인 상황이 된다. 이 격리가 없으면 새 이름 백업이 대신
	// 걸려서, 옛 이름을 아예 안 찾아도 테스트가 통과한다.
	apply(t, opts(t, sp))
	for _, e := range mustGlob(t, sp+".priorcase-backup-*") {
		if err := os.Remove(e); err != nil {
			t.Fatal(err)
		}
	}

	bak := sp + ".casebook-backup-20260808-012309"
	if err := os.WriteFile(bak, clean, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LatestBackup(sp)
	if err != nil {
		t.Fatalf("개명 전 이름의 백업을 못 찾았다: %v", err)
	}
	if got != bak {
		t.Errorf("LatestBackup = %s, 개명 전 이름의 백업이어야 한다", filepath.Base(got))
	}
}

func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()
	m, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// ★ **개명 전 훅이 든 백업으로 되돌리면 안 된다.**
//
// 그 백업을 복원하면 이제는 없는 `cb` 를 가리키는 훅 5개가 살아난다. 호스트는
// 매 이벤트마다 없는 파일을 실행하려다 실패하고, 사용자는 "되돌렸다" 는 말만 듣는다.
func TestRevertRejectsBackupWithPreRenameHooks(t *testing.T) {
	sp := writeSettings(t, realisticSettings)
	// 있는 백업이 개명 전 훅을 담고 있는 상황.
	if err := os.WriteFile(sp+".casebook-backup-20260808-012309", []byte(legacySettings), 0o600); err != nil {
		t.Fatal(err)
	}
	apply(t, opts(t, sp))

	got, err := LatestBackup(sp)
	if err != nil {
		return // 되돌릴 것이 없다고 크게 실패하는 것도 옳다
	}
	b, _ := os.ReadFile(got)
	if strings.Contains(string(b), legacyHookMarker) {
		t.Errorf("개명 전 훅이 든 백업을 골랐다 — 되돌리면 없는 cb 를 가리키는 훅이 살아난다: %s", got)
	}
}

// ★ **백업 목록은 이름이 아니라 타임스탬프로 정렬해야 한다.**
//
// 접두어가 두 종류라 이름 전체로 정렬하면 casebook- 이 priorcase- 보다 앞선다.
// 그러면 개명 직전에 뜬 가장 최근 백업이 목록 맨 앞으로 밀려, "가장 최근" 을
// 뒤에서부터 찾는 코드가 훨씬 오래된 것을 먼저 만난다.
func TestBackupsSortByTimestampNotName(t *testing.T) {
	sp := writeSettings(t, realisticSettings)
	clean, _ := os.ReadFile(sp)

	// 옛 이름 = 더 최근, 새 이름 = 더 오래됨. 이름순이면 순서가 뒤집힌다.
	older := sp + ".priorcase-backup-20260101-000000"
	newer := sp + ".casebook-backup-20260808-012309"
	if err := os.WriteFile(older, []byte(`{"marker":"오래된 것"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, clean, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LatestBackup(sp)
	if err != nil {
		t.Fatal(err)
	}
	if got != newer {
		t.Errorf("LatestBackup = %s, 타임스탬프가 가장 늦은 %s 여야 한다", filepath.Base(got), filepath.Base(newer))
	}
}
