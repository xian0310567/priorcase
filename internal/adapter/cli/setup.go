package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// ★★★ **`prior setup` 은 "이 기계에서 지금 무엇이 되고 있나" 를 한 번에 말한다.**
//
// # 왜 필요한가 — 배선이 명령마다 흩어져 있다
//
// 지금 배선은 이렇게 나뉘어 있다:
//
//	prior init --host claude-code --apply   Claude Code 훅
//	prior init --host codex --apply         Codex 훅 (따로 한 번 더)
//	prior brief --install AGENTS.md         훅 없는 호스트용 지침
//	prior doctor                            그래서 지금 어떤 상태인가
//
// 넷을 다 알아야 설치가 끝난다. 하나를 빠뜨리면 **그 통로만 조용히 죽어 있고**,
// 사람은 priorcase 가 도는 줄 안다 — 이 프로젝트가 죄목으로 드는 고장 그대로다.
//
// # doctor 와 무엇이 다른가
//
//	doctor  이미 배선된 것이 성한가        (진단 — 고치라고 말한다)
//	setup   무엇을 배선해야 하고 됐는가    (배선 — 계획을 보여 주고 실행한다)
//
// # 기본이 계획인 이유
//
// `prior init` 이 이미 그 규율을 갖고 있다 — 설정 파일은 다른 도구와 공유하는
// 자리라 실수로 한 번 돌려서 남의 훅이 사라지면 안 된다. setup 은 그 위에 서므로
// 같은 규율을 그대로 잇는다.
//
// # 마지막에 실제로 돌려 본다
//
// 배선했다는 것과 **된다**는 것은 다른 문장이다 (`규칙-한쪽-손해가-0이면-…` 의
// 두 번째 절이 같은 말을 한다: "넣었습니다" 는 참이지만 "됐습니다" 는 아직 모른다).
// 그래서 마지막에 회수를 실제로 한 번 돌려 본다.
func newSetupCmd() *cobra.Command {
	var apply bool
	var agentsFile string

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "이 기계의 배선을 한 번에 점검하고 맞춘다",
		Long: strings.Join([]string{
			"환경을 살피고 · 무엇을 배선할지 보여 주고 · 실제로 되는지 확인한다.",
			"",
			"기본은 계획만 보여 준다. 실제로 바꾸려면 --apply 를 붙인다.",
			"설정 파일은 다른 도구와 공유하는 자리라, 실수로 한 번 돌려서 남의 훅이 사라지면 안 된다.",
		}, "\n"),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			return runSetup(cmd, c, l, apply, agentsFile)
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "실제로 배선한다 (없으면 계획만)")
	cmd.Flags().StringVar(&agentsFile, "agents", "",
		"훅 없는 호스트용 지침을 넣을 파일 (기본: 현재 폴더의 AGENTS.md 가 있으면 그것)")
	return cmd
}

// step 은 화면 한 줄이다. mark 가 상태를 말한다.
func step(w io.Writer, mark, label, detail string) {
	if detail == "" {
		fmt.Fprintf(w, "  %s %s\n", mark, label)
		return
	}
	fmt.Fprintf(w, "  %s %-22s %s\n", mark, label, detail)
}

func runSetup(cmd *cobra.Command, c *config.Config, l *store.Layout, apply bool, agentsFile string) error {
	out := cmd.OutOrStdout()
	wd, _ := os.Getwd()

	// ── 1. 환경 ────────────────────────────────────────────────────────
	fmt.Fprintln(out, "환경")
	notes, skipped, lerr := l.List()
	switch {
	case lerr != nil:
		step(out, "✗", "볼트", fmt.Sprintf("%s — 못 읽는다: %v", l.Vault(), lerr))
		// 볼트를 못 읽으면 나머지는 볼 것이 없다. **여기서 멈춘다** —
		// 배선을 해 봐야 회수가 아무것도 못 꺼낸다.
		return fmt.Errorf("볼트를 못 읽어 배선을 멈춘다")
	default:
		d := fmt.Sprintf("%s · 결정 %d건", l.Vault(), len(notes))
		if len(skipped) > 0 {
			d += fmt.Sprintf(" (못 읽음 %d건 — prior doctor 가 이유를 준다)", len(skipped))
		}
		step(out, "✓", "볼트", d)
	}
	step(out, mark(c.DomainForCwdDeclared(wd) != ""), "이 폴더의 도메인",
		domainDetail(c, wd))

	hosts := detectHosts()
	for _, h := range hosts {
		step(out, mark(h.found), h.name, h.detail)
	}

	agents := pickAgentsFile(wd, agentsFile)
	if agents != "" {
		step(out, "✓", "지침 파일", agents)
	} else {
		step(out, "○", "지침 파일", "없다 — 훅 없는 호스트를 쓰면 --agents 로 지목해라")
	}

	// ── 2. 배선 ────────────────────────────────────────────────────────
	fmt.Fprintln(out, "\n배선")
	if !apply {
		fmt.Fprintln(out, "  (계획만 보여 준다. 실제로 하려면 --apply)")
	}
	for _, h := range hosts {
		if !h.found {
			step(out, "○", h.name+" 훅", "그 호스트가 없다 — 건너뛴다")
			continue
		}
		plan := fmt.Sprintf("prior init --host %s%s", h.id, applySuffix(apply))
		step(out, todo(apply), h.name+" 훅", plan)
	}
	if agents != "" {
		step(out, todo(apply), "지침 구간", fmt.Sprintf("prior brief --install %s", agents))
	}

	if apply {
		if err := applySetup(cmd, hosts, agents); err != nil {
			return err
		}
	}

	// ── 3. 검증 ────────────────────────────────────────────────────────
	//
	// **배선했다는 것과 된다는 것은 다른 문장이다** (이 파일 머리말의 §).
	fmt.Fprintln(out, "\n검증")
	hits, _, rerr := search.Recall(l, c, verifyQuery(notes), search.Options{
		Cwd: wd, CrossProject: true, Limit: 3, MinScore: 1,
	})
	switch {
	case rerr != nil:
		step(out, "✗", "회수", fmt.Sprintf("실패: %v", rerr))
	case len(notes) == 0:
		step(out, "○", "회수", "볼트가 비어 있어 확인할 것이 없다 — 첫 결정을 남겨 보라")
	case len(hits) == 0:
		step(out, "⚠", "회수", "돌긴 하는데 아무것도 못 꺼냈다 — prior eval 로 재 봐라")
	default:
		step(out, "✓", "회수", fmt.Sprintf("%d건 꺼냈다 (1위 %s)", len(hits), hits[0].Note.Stem))
	}

	fmt.Fprintln(out, "\n남은 것은 `prior doctor` 가 말한다 — 그쪽은 배선이 아니라 무결성을 본다.")
	return nil
}

type hostInfo struct {
	id, name, detail string
	found            bool
}

// detectHosts 는 이 기계에 어떤 에이전트 호스트가 있는지 본다.
//
// **설정 자리의 존재로 판정한다.** CLI 가 PATH 에 있는지가 아니라 — 그 호스트를
// 실제로 쓰는 사람은 설정 폴더를 갖고 있고, CLI 는 다른 이름으로 깔릴 수 있다.
func detectHosts() []hostInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	out := []hostInfo{
		{id: "claude-code", name: "Claude Code"},
		{id: "codex", name: "Codex"},
	}
	dirs := map[string]string{
		"claude-code": filepath.Join(home, ".claude"),
		"codex":       filepath.Join(home, ".codex"),
	}
	for i := range out {
		d := dirs[out[i].id]
		if fi, serr := os.Stat(d); serr == nil && fi.IsDir() {
			out[i].found, out[i].detail = true, d
		} else {
			out[i].detail = "설정 폴더가 없다 (" + d + ")"
		}
	}
	return out
}

// pickAgentsFile 은 지침을 넣을 파일을 고른다.
//
// **없는 파일을 만들지 않는다.** 사람이 안 쓰는 호스트의 지침 파일을 우리가
// 만들어 두면 그 파일이 무엇인지 모르는 채로 저장소에 커밋된다.
func pickAgentsFile(wd, override string) string {
	if override != "" {
		return override
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		p := filepath.Join(wd, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func applySetup(cmd *cobra.Command, hosts []hostInfo, agents string) error {
	out := cmd.OutOrStdout()
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("지금 도는 prior 의 경로를 못 찾는다: %w", err)
	}
	fmt.Fprintln(out)
	for _, h := range hosts {
		if !h.found {
			continue
		}
		// **자기 자신을 다시 부른다.** init 의 백업·되돌리기·옛 훅 걷어내기 규칙을
		// 여기서 흉내 내면 두 벌이 되고, 한쪽만 고쳐지면 조용히 갈린다.
		c := exec.Command(self, "init", "--host", h.id, "--apply")
		c.Stdout, c.Stderr = cmd.ErrOrStderr(), cmd.ErrOrStderr()
		if err := c.Run(); err != nil {
			step(out, "✗", h.name+" 훅", fmt.Sprintf("실패: %v", err))
			continue
		}
		step(out, "✓", h.name+" 훅", "배선됨")
	}
	if agents != "" {
		c := exec.Command(self, "brief", "--install", agents)
		c.Stdout, c.Stderr = cmd.ErrOrStderr(), cmd.ErrOrStderr()
		if err := c.Run(); err != nil {
			step(out, "✗", "지침 구간", fmt.Sprintf("실패: %v", err))
		} else {
			step(out, "✓", "지침 구간", agents)
		}
	}
	return nil
}

// verifyQuery 는 검증에 쓸 질의다. **볼트에 실제로 있는 낱말**이어야 한다 —
// 고정 문자열을 쓰면 그 낱말이 없는 볼트에서 늘 "못 꺼냈다" 가 나온다.
func verifyQuery(notes []store.Note) string {
	if len(notes) == 0 {
		return "결정"
	}
	// 가장 최근 결정의 요약을 쓴다. 그 노트 자신이 나오는 것이 정상이고,
	// 그것조차 안 나오면 회수가 통째로 죽은 것이다.
	newest := notes[0]
	for _, n := range notes {
		if n.Meta.Date > newest.Meta.Date {
			newest = n
		}
	}
	if s := strings.TrimSpace(newest.Meta.Summary); s != "" {
		return s
	}
	return newest.Stem
}

func mark(ok bool) string {
	if ok {
		return "✓"
	}
	return "○"
}

func todo(applied bool) string {
	if applied {
		return "→"
	}
	return "·"
}

func applySuffix(apply bool) string {
	if apply {
		return " --apply"
	}
	return ""
}

func domainDetail(c *config.Config, wd string) string {
	if d := c.DomainForCwdDeclared(wd); d != "" {
		return d
	}
	return fmt.Sprintf("선언 안 됨 — 기록이 폴백(%s)으로 간다. prior domain 으로 붙여라",
		c.DefaultDomain)
}
