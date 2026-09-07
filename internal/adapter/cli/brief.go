package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// ── 훅이 없는 호스트를 위한 파일 브리지 ────────────────────────────────
//
// **회수 주입은 호스트 훅으로만 된다** (hook/recall.go 의 §: MCP 에는 서버가 대화
// 중간에 텍스트를 밀어넣는 채널이 없다). 그래서 훅 API 가 없는 호스트에서는
// 자동 주입이 **0** 이다 — 지금 배선된 호스트는 Claude Code 와 Codex 둘뿐이고
// (adapter/hook/host.go), 나머지에서는 모델이 스스로 `prior recall` 을 불러야
// 하는데 작은 모델은 그걸 안 한다.
//
// 실측이 그것을 더 세게 말한다: 2026-08-31 재현에서 **Claude Code 조차** 회수를
// 부르고도(트랜스크립트에 `called priorcase 2 times`) 그 결과를 쓰지 못했다.
// 자발적 호출에 기대는 설계는 큰 모델에서도 새는데 작은 모델에서는 없는 것과 같다.
//
// # 파일이면 호스트를 안 가린다
//
// 프로젝트 지침 파일(CLAUDE.md·AGENTS.md·.cursorrules…)을 읽는 것은 거의 모든
// 하네스가 한다. 브리지를 파일로 내면 호스트마다 배선을 만들지 않고도 같은 바닥이
// 깔린다. 대신 **매 프롬프트가 아니라 세션 하나에 한 번**이라, 여기 실을 것은
// 주제와 무관하게 언제나 참인 것뿐이다.
//
// # 무엇을 싣는가 — 그리고 왜 결정은 안 싣는가
//
// ① **규칙** (`_meta/rules/`). 도메인이 없어 어디서든 유효하고, 증류물이라 줄당
//
//	값이 가장 크다. 지금 9건이다.
//
// ② **이 프로젝트의 실행 절차.** 본문에 셸 절차를 가진 결정의 명령 이름이다
//
//	(store.ProcedureCommands 의 §). 2026-08-31 고장이 정확히 이것이 없어서 났다 —
//	에이전트가 `orca` 라는 CLI 의 존재를 알 통로가 없어 "수단이 없다" 로 끝냈다.
//
// ③ **회수하는 법.** 나머지는 주제가 정해진 뒤에 꺼내야 한다.
//
// 결정 본문을 싣지 않는 이유: 볼트가 543건이고 주제와 무관한 것을 미리 실으면
// 그게 컨텍스트를 먹는다. 그 선별이 회수가 하는 일이고, 여기서 대신할 수 없다.
func newBriefCmd() *cobra.Command {
	var out, install string
	cmd := &cobra.Command{
		Use:   "brief",
		Short: "훅이 없는 호스트에 깔 지침을 낸다 (규칙 + 이 프로젝트의 실행 절차)",
		Long: "훅 API 가 없는 에이전트 호스트에서는 회수 자동 주입이 아예 없다.\n" +
			"그 호스트가 읽는 지침 파일에 넣을 내용을 만든다.\n\n" +
			"기본은 표준출력이다.\n" +
			"  --install <파일>  그 파일의 priorcase 구간만 갈아 끼운다 (**AGENTS.md 에는 이쪽을 써라**)\n" +
			"  --out <파일>      파일을 통째로 그 내용으로 만든다",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			wd, _ := os.Getwd()
			domain := c.DomainForCwd(wd)
			body, err := renderBrief(l, domain)
			if err != nil {
				return err
			}
			if install != "" {
				return installBrief(cmd, install, body)
			}
			if out == "" {
				fmt.Fprint(cmd.OutOrStdout(), body)
				return nil
			}
			if err := store.WriteFileAtomic(out, []byte(body), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "썼다: %s (%d바이트)\n", out, len(body))
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "",
		"이 경로를 **통째로** 이 내용으로 만든다 — 기존 파일이 있으면 사라진다")
	cmd.Flags().StringVar(&install, "install", "",
		"이 파일의 priorcase 구간만 갈아 끼운다 (없으면 끝에 붙인다). AGENTS.md·CLAUDE.md 용이다")
	return cmd
}

// installBrief 는 사용자의 지침 파일에 **우리 구간만** 넣는다 (store.SpliceBlock 의 §).
//
// **--out 과 다른 명령이다.** --out 은 파일을 통째로 만드는 것이고 이쪽은 남의
// 문서에 끼워 넣는 것이다. AGENTS.md 를 --out 으로 겨누면 그 문서가 사라진다 —
// 그래서 도움말이 이쪽을 가리킨다.
func installBrief(cmd *cobra.Command, path, body string) error {
	old := ""
	if b, err := os.ReadFile(path); err == nil {
		old = string(b)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("%s 를 읽을 수 없다: %w", path, err)
	}
	next, changed, err := store.SpliceBlock(old, body)
	if err != nil {
		return err
	}
	if !changed {
		// **안 바뀌었으면 안 쓴다.** 매번 "썼다" 라고 하면 사람이 무엇이 바뀐
		// 줄 알고, 파일 mtime 이 움직여 git 이 그 문서를 계속 건드린 것처럼 보인다.
		fmt.Fprintf(cmd.ErrOrStderr(), "이미 최신이다: %s\n", path)
		return nil
	}
	if err := store.WriteFileAtomic(path, []byte(next), 0o644); err != nil {
		return err
	}
	verb := "구간을 갈아 끼웠다"
	if !store.HasBlock(old) {
		verb = "구간을 붙였다"
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s\n", verb, path)
	return nil
}

func renderBrief(l *store.Layout, domain string) (string, error) {
	var b strings.Builder
	b.WriteString("<!-- prior brief — 이 절은 `prior brief` 가 만든다. 손으로 고치지 마라. -->\n")
	b.WriteString("## 과거 결정 (priorcase)\n\n")
	b.WriteString("이 작업 공간에는 과거 결정이 볼트에 쌓여 있다. " +
		"**새 주제로 넘어갈 때마다 먼저 부른다:**\n\n```bash\nprior recall \"<주제>\"\n```\n\n" +
		"부르지 않으면 이미 뒤집힌 결정을 다시 제안하게 된다.\n\n")

	rules, _, err := l.ListRules()
	if err != nil {
		return "", err
	}
	if len(rules) > 0 {
		sort.Slice(rules, func(i, j int) bool { return rules[i].Stem < rules[j].Stem })
		b.WriteString("### 규칙 — 프로젝트를 가리지 않는 제약\n\n" +
			"참고가 아니라 **지켜야 하는 것**이다.\n\n")
		for _, r := range rules {
			if r.Meta.Status == store.StatusSuperseded || r.Meta.Status == store.StatusRetracted {
				continue
			}
			s := strings.TrimSpace(r.Meta.Summary)
			if s == "" {
				s = r.Stem
			}
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\n")
	}

	// **이 프로젝트에서 쓸 수 있는 도구.** 요약에는 안 나오고 본문에만 있는 것이라
	// (store.ProcedureCommands 의 §) 회수가 없는 호스트에서는 여기가 유일한 통로다.
	notes, _, err := l.List()
	if err != nil {
		return "", err
	}
	type proc struct {
		stem string
		cmds []string
	}
	var procs []proc
	for _, n := range notes {
		if domain != "" && !containsFold(n.Meta.Domain, domain) {
			continue
		}
		if cmds := store.ProcedureCommands(n.Body); len(cmds) > 0 {
			procs = append(procs, proc{n.Stem, cmds})
		}
	}
	sort.Slice(procs, func(i, j int) bool { return procs[i].stem < procs[j].stem })
	if len(procs) > 0 {
		fmt.Fprintf(&b, "### 이 프로젝트에서 쓸 수 있는 것 (%s)\n\n"+
			"아래 명령은 **이 환경에 실제로 있다.** 절차는 그 노트의 본문에 있으니, "+
			"\"그런 수단이 없다\" 로 끝내기 전에 열어라.\n\n", domain)
		for _, p := range procs {
			fmt.Fprintf(&b, "- `%s` → `prior path %s` 로 경로를 얻어 본문을 읽어라\n",
				strings.Join(p.cmds, "`, `"), p.stem)
		}
		b.WriteString("\n")
	}
	b.WriteString("<!-- /prior brief -->\n")
	return b.String(), nil
}

func containsFold(ss []string, s string) bool {
	for _, v := range ss {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
