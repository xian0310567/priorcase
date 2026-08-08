package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xian0310567/casebook/internal/core/store"
)

// hookMarker 는 casebook 이 심은 훅을 알아보는 표시다.
//
// 명령 문자열에 이 환경변수를 붙여 둔다. 해가 없고, 오탐이 불가능하며, 설정 파일을
// 사람이 열어 봤을 때 무엇인지 바로 보인다. JSON 에는 주석이 없어서 이 방법이 필요하다.
// 멱등성(두 번 돌려도 중복 등록 안 됨)과 `--revert` 가 전부 이 표시에 기댄다.
const hookMarker = "CASEBOOK_HOOK=1"

// Plan 은 cb init 이 무엇을 할지 미리 계산한 것이다. --dry-run 이 이걸 보여 준다.
//
// 계획을 먼저 만들고 나중에 적용하는 구조로 둔 이유: 이 명령은 사용자의 다른 시스템
// (orca 등)과 한 파일을 공유한다. 무엇을 지울지 **보여 준 뒤에** 지울 수 있어야 한다.
type Plan struct {
	SettingsPath string
	BackupPath   string
	ConfigPath   string
	CreateConfig bool
	Remove       []string // 지울 훅 명령 (요약)
	Add          []string // 심을 훅 명령
	Keep         int      // 손대지 않는 훅 수
	after        []byte
}

// InitOptions 는 배선에 필요한 값이다.
type InitOptions struct {
	SettingsPath string
	ConfigPath   string
	// Binary 는 훅이 실행할 cb 경로다. 비면 os.Executable().
	Binary string
	// RemoveMatching 은 걷어낼 옛 훅을 알아보는 문자열이다. 명령에 이게 들어 있으면
	// 지운다. 기본값은 셸 시절 경로 조각이다.
	RemoveMatching string
	// Vault 는 새로 만들 설정 파일이 가리킬 볼트다.
	Vault string
	Now   time.Time
}

type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

type hookGroup struct {
	Matcher string      `json:"matcher,omitempty"`
	Hooks   []hookEntry `json:"hooks"`
}

// BuildPlan 은 설정 파일을 읽어 무엇을 바꿀지 계산한다. **파일을 쓰지 않는다.**
func BuildPlan(o InitOptions) (*Plan, error) {
	if o.Binary == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("cb 경로를 알 수 없다: %w", err)
		}
		o.Binary = exe
	}
	if o.RemoveMatching == "" {
		o.RemoveMatching = "hooks/second-brain/"
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}

	p := &Plan{
		SettingsPath: o.SettingsPath,
		ConfigPath:   o.ConfigPath,
		BackupPath: fmt.Sprintf("%s.casebook-backup-%s",
			o.SettingsPath, o.Now.Format("20060102-150405")),
	}
	if o.ConfigPath != "" {
		if _, err := os.Stat(o.ConfigPath); os.IsNotExist(err) {
			p.CreateConfig = true
		}
	}

	// 설정 전체를 map 으로 읽는다. 우리가 모르는 키를 하나도 잃지 않기 위해서다 —
	// 구조체로 받으면 모르는 키가 조용히 사라지고, 그건 사용자의 다른 설정을 지우는 것이다.
	root := map[string]any{}
	raw, err := os.ReadFile(o.SettingsPath)
	switch {
	case os.IsNotExist(err):
		p.BackupPath = "" // 백업할 것이 없다
	case err != nil:
		return nil, fmt.Errorf("설정을 읽을 수 없다 (%s): %w", o.SettingsPath, err)
	default:
		if err := json.Unmarshal(raw, &root); err != nil {
			return nil, fmt.Errorf("설정이 JSON 이 아니다 (%s): %w", o.SettingsPath, err)
		}
		// **이미 우리 훅이 들어 있으면 백업하지 않는다.**
		//
		// 백업의 목적은 "casebook 을 깔기 전 상태" 를 붙잡아 두는 것이다. 이미 배선된
		// 파일을 또 백업하면 그 백업에도 우리 훅이 들어 있고, --revert 가 사전순
		// 마지막을 고르므로 **되돌려도 훅이 그대로 남는다.** 실측으로 재현했다 —
		// `--apply` 를 두 번 한 뒤 `--revert` 하면 "되돌렸다" 고 말하면서 훅 5개가
		// 살아 있었다. 사용자는 지운 줄 안다.
		if bytes.Contains(raw, []byte(hookMarker)) {
			p.BackupPath = ""
		}
	}

	hooks := map[string][]hookGroup{}
	if v, ok := root["hooks"]; ok {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &hooks); err != nil {
			return nil, fmt.Errorf("hooks 절의 형태를 모르겠다: %w", err)
		}
	}

	// ① 걷어내기 — 옛 훅과 casebook 이 전에 심은 것. **그 밖에는 손대지 않는다.**
	for event, groups := range hooks {
		kept := groups[:0]
		for _, g := range groups {
			keptHooks := g.Hooks[:0]
			for _, h := range g.Hooks {
				drop := strings.Contains(h.Command, hookMarker) ||
					(o.RemoveMatching != "" && strings.Contains(h.Command, o.RemoveMatching))
				if drop {
					p.Remove = append(p.Remove, event+": "+summarize(h.Command))
					continue
				}
				keptHooks = append(keptHooks, h)
				p.Keep++
			}
			g.Hooks = keptHooks
			if len(g.Hooks) > 0 {
				kept = append(kept, g)
			}
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}

	// ② 심기
	for _, ev := range Events {
		name := ev.claudeCodeName()
		cmd := fmt.Sprintf("%s %q hook %s", hookMarker, o.Binary, ev)
		hooks[name] = append(hooks[name], hookGroup{
			Hooks: []hookEntry{{Type: "command", Command: cmd, Timeout: hookTimeout(ev)}},
		})
		p.Add = append(p.Add, name+": "+summarize(cmd))
	}

	root["hooks"] = hooks
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	p.after = append(out, '\n')
	sort.Strings(p.Remove)
	return p, nil
}

// Apply 는 계획을 적용한다. **백업을 먼저 쓴다.**
func (p *Plan) Apply(raw []byte) error {
	if p.BackupPath != "" {
		if err := store.WriteFileAtomic(p.BackupPath, raw, 0o600); err != nil {
			return fmt.Errorf("백업에 실패했다 — 아무것도 바꾸지 않았다: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(p.SettingsPath), 0o755); err != nil {
		return err
	}
	return store.WriteFileAtomic(p.SettingsPath, p.after, 0o600)
}

// ReadSettings 는 Apply 에 넘길 원본 바이트를 준다 (없으면 nil).
func ReadSettings(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return b
}

// LatestBackup 은 가장 최근 백업 경로를 준다.
func LatestBackup(settingsPath string) (string, error) {
	dir := filepath.Dir(settingsPath)
	base := filepath.Base(settingsPath) + ".casebook-backup-"
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var found []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), base) {
			found = append(found, e.Name())
		}
	}
	if len(found) == 0 {
		return "", fmt.Errorf("백업이 없다 (%s*)", filepath.Join(dir, base))
	}
	sort.Strings(found) // 이름이 타임스탬프라 사전순 = 시간순

	// **casebook 훅이 들어 있지 않은 가장 최근 백업**을 고른다.
	//
	// 무조건 마지막을 고르면, 우리 훅이 이미 든 백업이 섞여 있을 때 되돌려도 훅이
	// 남는다. BuildPlan 이 이제 그런 백업을 만들지 않지만, **옛 판으로 이미 만들어진
	// 백업이 사용자 디스크에 남아 있다** — 그 사람들이 여기로 온다.
	for i := len(found) - 1; i >= 0; i-- {
		full := filepath.Join(dir, found[i])
		b, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		if !bytes.Contains(b, []byte(hookMarker)) {
			return full, nil
		}
	}
	return "", fmt.Errorf("되돌릴 백업이 없다 — 있는 백업 %d개가 전부 casebook 훅을 이미 담고 있다 (%s*)",
		len(found), filepath.Join(dir, base))
}

// Revert 는 가장 최근 백업으로 되돌린다. **바이트 그대로 복원한다** — 우리가 다시
// 직렬화하면 원본의 키 순서·들여쓰기가 바뀌고, 그건 복원이 아니다.
func Revert(settingsPath string) (string, error) {
	bak, err := LatestBackup(settingsPath)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(bak)
	if err != nil {
		return "", err
	}
	if err := store.WriteFileAtomic(settingsPath, b, 0o600); err != nil {
		return "", err
	}
	return bak, nil
}

// summarize 는 긴 셸 명령을 한눈에 보이게 줄인다.
func summarize(cmd string) string {
	cmd = strings.Join(strings.Fields(cmd), " ")
	if len([]rune(cmd)) > 90 {
		return string([]rune(cmd)[:90]) + "…"
	}
	return cmd
}

// String 은 --dry-run 출력이다.
func (p *Plan) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "설정 파일: %s\n", p.SettingsPath)
	if p.BackupPath != "" {
		fmt.Fprintf(&b, "백업:      %s\n", p.BackupPath)
	} else {
		b.WriteString("백업:      (설정 파일이 아직 없다)\n")
	}
	if p.CreateConfig {
		fmt.Fprintf(&b, "새로 만듦: %s\n", p.ConfigPath)
	}
	b.WriteString("\n걷어낼 훅:\n")
	if len(p.Remove) == 0 {
		b.WriteString("  (없음)\n")
	}
	for _, r := range p.Remove {
		fmt.Fprintf(&b, "  - %s\n", r)
	}
	b.WriteString("\n심을 훅:\n")
	for _, a := range p.Add {
		fmt.Fprintf(&b, "  + %s\n", a)
	}
	fmt.Fprintf(&b, "\n손대지 않는 훅: %d개\n", p.Keep)
	return b.String()
}

// **승격하는 훅에는 시간을 명시한다.**
//
// SessionEnd·PreCompact 만 판별기를 부른다. 판별기는 호스트 CLI 를 띄우는 것이라
// 초 단위로 걸리고, 미확인 구간이 여럿이면 그만큼 늘어난다. 훅에 timeout 을 안
// 적으면 호스트 기본값이 걸리는데 — 그 값이 얼마인지 우리가 모른다. 승격이 그
// 안에 못 끝나면 호스트가 훅을 죽이고, **자동 기록은 조용히 0건이 된다.**
//
// 컷오버 1일차부터 ③층이 한 번도 발동하지 않았고, 이것이 유력한 원인 중 하나다.
// 모르는 기본값에 기대지 않는다.
//
// 나머지 훅은 볼트를 읽고 문자열을 만드는 것뿐이라 밀리초 단위다 — 시간을 적어
// 봐야 호스트 기본값과 다를 이유가 없으므로 비워 둔다(0 이면 필드가 생략된다).
func hookTimeout(ev Event) int {
	if ev == EventSessionEnd || ev == EventPreCompact {
		return int(promoteHookTimeout / time.Second)
	}
	return 0
}
