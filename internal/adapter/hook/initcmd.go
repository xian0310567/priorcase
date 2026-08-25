package hook

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// NewInitCommand 는 `prior init` 을 만든다.
//
// **기본은 계획만 보여 준다.** 쓰려면 --apply 가 필요하다. 이 명령은 priorcase 만의
// 파일이 아니라 호스트가 다른 도구들과 공유하는 설정을 수술하기 때문이다 — 실측으로
// 그 파일에는 다른 시스템의 훅이 6개 이상 같이 산다. 실수로 한 번 돌려서 남의 훅이
// 사라지면, 그 사람은 무엇이 지웠는지도 모른다.
func NewInitCommand() *cobra.Command {
	var apply, revert bool
	var settingsPath, binary, removeMatching, vault, host string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "에이전트 훅을 배선한다 (기본은 계획만 보여 준다)",
		Long: "Claude Code 또는 Codex 설정에 priorcase 훅을 심고 옛 셸 훅을 걷어낸다.\n\n" +
			"**기본은 계획만 보여 준다. 쓰려면 --apply 를 붙인다.** 이 설정 파일은 " +
			"다른 도구들과 공유하는 자리라, 실수로 한 번 돌려서 남의 훅이 사라지면 안 된다.\n\n" +
			"--apply 는 수정 전에 백업을 남기고, --revert 가 그 백업으로 되돌린다.\n\n" +
			"두 호스트를 다 쓰면 각각 한 번씩 돌린다 — 설정 파일이 서로 다르다.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			h, err := ParseHost(host)
			if err != nil {
				return err
			}
			if settingsPath == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				settingsPath = defaultSettingsPath(home, h)
			}

			if revert {
				bak, err := Revert(settingsPath)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "되돌렸다: %s ← %s\n", settingsPath, bak)
				return nil
			}

			cfgPath, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}
			if cfgPath == "" {
				if cfgPath, err = config.DefaultPath(); err != nil {
					return err
				}
			}

			p, err := BuildPlan(InitOptions{
				SettingsPath: settingsPath, ConfigPath: cfgPath,
				Binary: binary, RemoveMatching: removeMatching, Host: h,
			})
			if err != nil {
				return err
			}
			fmt.Fprint(out, p.String())

			if !apply {
				fmt.Fprintln(out, "\n계획만 보여 줬다. 실제로 바꾸려면 --apply 를 붙인다.")
				return nil
			}

			if p.CreateConfig {
				if err := writeStarterConfig(cfgPath, vault); err != nil {
					return err
				}
				fmt.Fprintf(out, "\n설정 파일을 만들었다: %s\n", cfgPath)
			}
			if err := p.Apply(ReadSettings(settingsPath)); err != nil {
				return err
			}
			fmt.Fprintf(out, "\n배선했다: %s\n", settingsPath)
			if p.BackupPath != "" {
				// **호스트를 빼면 엉뚱한 파일이 되돌아간다.** --revert 도 기본 호스트의
				// 설정 경로를 쓰므로, Codex 를 배선하고 이 문구를 그대로 따르면
				// 손대지도 않은 Claude Code 설정이 되돌아가고 Codex 배선은 남는다.
				hostArg := ""
				if h != HostClaudeCode {
					hostArg = "--host " + string(h) + " "
				}
				fmt.Fprintf(out, "되돌리려면: prior init %s--revert   (백업: %s)\n", hostArg, p.BackupPath)
			}
			// **Codex 에서는 데몬이 선택이 아니다.** Claude Code 는 SessionEnd 훅이
			// 세션 끝에 아크를 판정하는데(scan.go), Codex 에는 그 이벤트가 없다.
			// 그 자리를 데몬의 arcStale(20분 침묵)이 대신해야 한다 — 안 띄우면
			// 훑기는 되고 **승격이 압축될 때만 돈다.**
			fmt.Fprintln(out, "\n데몬은 자동 등록하지 않는다 — `prior watch` 를 직접 띄운다.")
			if h == HostCodex {
				fmt.Fprintln(out, "⚠️ **Codex 에서는 선택이 아니다.** Codex 에는 SessionEnd 이벤트가 없어서,")
				fmt.Fprintln(out, "   세션 끝에 자동 기록을 하는 자리를 데몬이 대신해야 한다 (20분 침묵 뒤 판정).")
				fmt.Fprintln(out, "   안 띄우면 훑기는 되지만 자동 기록은 압축될 때만 돈다.")
			} else {
				fmt.Fprintln(out, "안 띄워도 훅이 턴 경계마다 대신 훑으므로 안전망은 동작한다.")
			}
			if _, err := exec.LookPath("prior"); err != nil {
				// 훅은 절대 경로로 돌지만 사람은 못 친다. 배선한 직후가 알려 줄 자리다.
				fmt.Fprintf(out, "\n⚠️ `prior` 가 PATH 에 없다. 훅은 돌지만 명령을 직접 칠 수 없다:\n"+
					"   ln -s %s ~/.local/bin/prior   (또는 PATH 에 그 디렉토리를 추가)\n", binaryPath())
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&apply, "apply", false, "실제로 설정을 바꾼다 (없으면 계획만 보여 준다)")
	f.BoolVar(&revert, "revert", false, "가장 최근 백업으로 되돌린다")
	f.StringVar(&host, "host", "", "배선할 에이전트: claude-code (기본) · codex")
	f.StringVar(&settingsPath, "settings", "", "설정 경로 (기본: ~/.claude/settings.json · codex 는 ~/.codex/hooks.json)")
	f.StringVar(&binary, "binary", "", "훅이 실행할 prior 경로 (기본: 지금 실행 중인 것)")
	f.StringVar(&removeMatching, "remove-matching", "", "걷어낼 옛 훅을 알아보는 문자열 (기본: hooks/second-brain/)")
	f.StringVar(&vault, "vault", "", "새 설정 파일이 가리킬 볼트 경로")
	return cmd
}

// defaultSettingsPath 는 그 호스트가 훅을 읽는 파일이다.
//
// Codex 는 `~/.codex/config.toml` 의 `[hooks]` 절로도 읽지만 **hooks.json 쪽을 쓴다** —
// config.toml 은 사람이 손으로 관리하는 설정이 잔뜩 든 파일이고, TOML 을 다시 쓰면
// 주석이 전부 날아간다. JSON 파일은 우리가 이미 안전하게 수술할 줄 안다(BuildPlan).
func defaultSettingsPath(home string, h Host) string {
	if h == HostCodex {
		return filepath.Join(home, ".codex", "hooks.json")
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// writeStarterConfig 는 설정 파일이 없을 때만 만든다. **있으면 절대 안 건드린다** —
// 사용자의 도메인 매핑을 덮어쓰는 것은 되돌리기 어렵다.
//
// **바로 쓸 수 있는 설정을 만든다.** 예전에는 [[domain]] 을 전부 주석으로 두었는데,
// 그러면 새 사용자는 도메인이 0개인 채로 시작하고 아무것도 기록되지 않는다 —
// 훅은 돌고 안전망은 표시까지 하는데 승격에서 조용히 막힌다. 실제로 그 상태를
// 재현해 확인했다.
func writeStarterConfig(path, vault string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if vault == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		vault = filepath.Join(home, "Documents", "Obsidian Vault")
	}
	// 볼트가 없으면 만든다. 없는 경로를 가리키는 설정은 첫 실행부터 빨간불이다.
	if err := os.MkdirAll(filepath.Join(vault, "common", "decisions"), 0o755); err != nil {
		return fmt.Errorf("볼트를 만들 수 없다 (%s): %w", vault, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	home, _ := os.UserHomeDir()
	if localeIsKorean() {
		return store.WriteFileAtomic(path, []byte(starterKO(vault, home)), 0o600)
	}
	return store.WriteFileAtomic(path, []byte(starterEN(vault, home)), 0o600)
}

// localeIsKorean 은 환경변수로 한국어 사용자인지 본다.
//
// **틀려도 손해가 작은 판정이다.** 설정 파일은 사람이 읽고 고칠 수 있고, 첫 줄에
// 무엇을 바꾸면 되는지 적혀 있다. 물어보는 것보다 낫다 — prior init 은 훅 배선이
// 본업이고, 거기에 대화형 질문을 붙이면 스크립트에서 못 쓴다.
func localeIsKorean() bool {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			return strings.HasPrefix(strings.ToLower(v), "ko")
		}
	}
	return false
}

func starterKO(vault, home string) string {
	return fmt.Sprintf(`# priorcase 설정. 자세한 것은 README 를 보라.
# 영어로 쓰려면 lang = "en" 으로 바꾸고 아래 이름·시그널을 영어로 고친다.
vault = %q
lang  = "ko"

# 여기 적힌 경로에서는 결정을 **기록**하지 않는다.
#
# ⚠️ 회수도 그 경로의 문서를 대부분 못 본다 — prior 는 frontmatter 에
# type: decision 이 적힌 문서만 읽는다. 다른 도구가 만든 문서(위키·노트 앱
# 산출물)는 그 표식이 없어 회수에 안 잡힌다.
exclude = []

# 어느 [[domain]] 의 paths 에도 안 걸릴 때 쓸 도메인.
# **이걸 비우면 그런 자리에서는 아무것도 기록되지 않는다.**
default_domain = "common"

[naming]
decision_file = "{domain}-결정-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog       = "99-{project}-작업-로그.md"
# 색인은 결정 노트에서 다시 만들 수 있는 파생물이다. 볼트를 git 으로 공유하면
# 이 파일을 .gitignore 에 넣어라 — 안 그러면 두 사람이 각자 기록할 때마다 충돌한다.
# prior doctor 가 그 상태를 알려 준다.
index         = "_meta/00-결정-색인.md"
rollup        = "98-{project}-작업-로그-요약.md"

# 데몬(prior watch)과 훅이 쓰는 값이다.
# signals 가 비면 어떤 구간도 표시되지 않는다.
[capture]
signals = ["결정", "선택", "하기로", "채택", "대신", "전략", "포기", "변경"]
min_turns = 6
quiesce_seconds = 3

# 자동 기록 — 세션 끝에 판별기가 기록되지 않은 결정을 대신 남긴다.
# 비우면 자동으로 찾는다. 못 찾으면 표시만 남는다. **API 키는 쓰지 않는다.**
judge_path  = ""
judge_model = "claude-haiku-4-5"

# 무소속 결정이 쌓이는 곳. default_domain 이 이걸 가리킨다.
[[domain]]
prefix = "common"
folder = "common"

# 프로젝트마다 한 블록을 더한다.
#
# repos 를 적으면 **팀원이 어디에 체크아웃하든** 같은 도메인으로 잡힌다 —
# 팀에서 쓸 때는 이쪽이 낫다. paths 는 절대 경로라 사람마다 다르다.
# 둘 다 적으면 paths 가 먼저다 (모노레포의 하위 프로젝트를 가르기 위해서다).
# [[domain]]
# prefix = "myapp"
# folder = "myapp"
# repos  = ["myorg/myapp"]
# paths  = ["%s/project/myapp"]
`, vault, home)
}

func starterEN(vault, home string) string {
	return fmt.Sprintf(`# priorcase configuration. See the README for details.
# The naming templates and signals below are yours to change — priorcase only
# reads them, so any language or convention works.
vault = %q
lang  = "en"

# Decisions are never **recorded** under these paths.
#
# ⚠️ Recall also misses most documents there — priorcase only reads notes whose
# frontmatter says type: decision. Documents produced by other tools lack
# that marker.
exclude = []

# Domain used when the working directory matches no [[domain]] paths.
# **Leave this empty and nothing gets recorded in those places.**
default_domain = "common"

[naming]
decision_file = "{domain}-decision-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog       = "99-{project}-worklog.md"
# The index is derived from the decision notes. If you share the vault over git,
# add this file to .gitignore — otherwise every pair of concurrent captures
# conflicts. prior doctor tells you when that happens.
index         = "_meta/00-decision-index.md"
rollup        = "98-{project}-worklog-rollup.md"

# Used by the daemon (prior watch) and the hooks.
# If signals is empty, no segment is ever flagged.
[capture]
signals = ["decided", "chose", "instead of", "rejected", "trade-off",
           "we'll go with", "settled on", "ruled out"]
min_turns = 6
quiesce_seconds = 3

# Automatic recording — at the end of a session a judge records decisions the
# agent never captured. Leave judge_path empty to auto-detect. If nothing is
# found, segments are only flagged. **No API key is ever read.**
judge_path  = ""
judge_model = "claude-haiku-4-5"

# Where decisions that belong to no project go. default_domain points here.
[[domain]]
prefix = "common"
folder = "common"

# Add one block per project.
#
# Set repos and the domain resolves the same **wherever a teammate checks out** —
# prefer it on a team. paths are absolute and differ per machine.
# With both, paths win (so a monorepo's sub-projects stay apart).
# [[domain]]
# prefix = "myapp"
# folder = "myapp"
# repos  = ["myorg/myapp"]
# paths  = ["%s/project/myapp"]
`, vault, home)
}

// binaryPath 는 지금 도는 prior 의 경로다. 안내 문구에 쓴다.
func binaryPath() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "prior"
}
