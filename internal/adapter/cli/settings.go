package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
)

// 이 파일은 **설정을 보고 고치는 표면**이다. 데스크탑 앱의 화면 둘(호스트·볼트)이
// 여기만 부른다.
//
// # 왜 CLI 가 설정을 고치나
//
// 앱이 config.toml 을 직접 쓰면 쓰기 규칙이 두 벌이 된다 — 주석 보존, 스칼라
// 볼트 변환, 고친 뒤 검증. 한쪽만 고쳐진 채로 남으면 사람이 손으로 쓴 설정이
// 조용히 망가진다. 앱은 `prior` 명령만 부른다(앱 README 의 규칙).
//
// # 백업
//
// 고치기 전에 `config.toml.bak` 을 한 벌 남긴다. edit 가 결과를 검증하므로
// 망가진 설정이 나갈 일은 없지만, **이 파일에는 사람이 쓴 주석이 있고 그건
// 되살릴 수 없다.** 타임스탬프를 붙이지 않는 이유는 백업이 무한히 쌓이면
// 사람이 어느 것이 최근인지 못 고르기 때문이다 — 직전 판 하나면 충분하다.

// settingsOut 은 `prior settings --json` 의 출력이다. 앱의 계약이므로 필드를
// 뺄 때는 앱을 같이 고쳐야 한다.
type settingsOut struct {
	ConfigPath string      `json:"config_path"`
	Vaults     []vaultOut  `json:"vaults"`
	Domains    []domainOut `json:"domains"`
	Hosts      []hostOut   `json:"hosts"`
	Warnings   []string    `json:"warnings,omitempty"`
}

type vaultOut struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Exists    bool     `json:"exists"`
	Decisions int      `json:"decisions"`
	Domains   []string `json:"domains"`
}

type domainOut struct {
	Prefix string   `json:"prefix"`
	Folder string   `json:"folder"`
	Vault  string   `json:"vault"`
	Paths  []string `json:"paths"`
	Repos  []string `json:"repos"`
}

type hostOut struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Root    string `json:"root"`
	Exists  bool   `json:"exists"`
	// Files 는 그 자리에서 읽을 수 있는 대화 기록 수다. **0 은 두 가지 뜻이
	// 아니다** — Exists 가 거짓이면 자리가 없는 것이고, 참인데 0이면 정말 비었다.
	Files int `json:"files"`
}

// configFile 은 설정 파일의 경로와 원문을 준다.
func configFile(cmd *cobra.Command) (string, []byte, error) {
	flag, err := cmd.Flags().GetString("config")
	if err != nil {
		return "", nil, err
	}
	path, err := config.ResolvePath(flag)
	if err != nil {
		return "", nil, err
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("설정 파일을 열 수 없다 (%s): %w", path, err)
	}
	return path, src, nil
}

// writeConfig 는 고친 설정을 원자적으로 쓴다. 쓰기 전에 직전 판을 남긴다.
func writeConfig(path string, old, next []byte) error {
	if err := store.WriteFileAtomic(path+".bak", old, 0o600); err != nil {
		return fmt.Errorf("백업을 못 남겨서 쓰지 않았다: %w", err)
	}
	return store.WriteFileAtomic(path, next, 0o600)
}

// applyEdit 는 읽기 → 고치기 → 쓰기를 한 자리에 모은다.
func applyEdit(cmd *cobra.Command, fn func(src []byte) ([]byte, error)) error {
	path, src, err := configFile(cmd)
	if err != nil {
		return err
	}
	next, err := fn(src)
	if err != nil {
		return err
	}
	if err := writeConfig(path, src, next); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "고쳤다: %s (직전 판은 %s.bak)\n", path, filepath.Base(path))
	return nil
}

func newSettingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "지금 설정을 한 번에 낸다 (앱의 설정 화면용)",
		Long: "볼트·도메인·호스트를 한 번에 낸다.\n\n" +
			"데스크탑 앱의 호스트·볼트 화면이 이것만 읽는다 — 앱이 설정 파일을\n" +
			"직접 파싱하면 스칼라 볼트 변형·기본값 규칙이 두 벌이 된다.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			asJSON, _ := cmd.Flags().GetBool("json")
			out, err := collectSettings(cmd)
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}
			printSettings(cmd, out)
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "JSON 으로 낸다")
	return cmd
}

func collectSettings(cmd *cobra.Command) (settingsOut, error) {
	path, _, err := configFile(cmd)
	if err != nil {
		return settingsOut{}, err
	}
	c, err := config.Load(path)
	if err != nil {
		return settingsOut{}, err
	}
	out := settingsOut{ConfigPath: path}

	// 도메인 → 볼트. 볼트 줄에 "누가 이 볼트를 쓰는가" 를 달아 주면 사람이
	// 볼트를 지우기 전에 무엇이 딸려 가는지 안다.
	byVault := map[string][]string{}
	for _, d := range c.Domain {
		v := d.Vault
		if v == "" {
			v = config.DefaultVaultName
		}
		byVault[v] = append(byVault[v], d.Prefix)
		out.Domains = append(out.Domains, domainOut{
			Prefix: d.Prefix, Folder: d.Folder, Vault: d.Vault,
			Paths: d.Paths, Repos: d.Repos,
		})
	}

	l := store.NewLayout(c)
	for _, v := range c.Vaults {
		vo := vaultOut{Name: v.Name, Path: v.Path, Domains: byVault[v.Name]}
		if st, serr := os.Stat(v.Path); serr == nil && st.IsDir() {
			vo.Exists = true
			if ll, lerr := l.For(prefixUsing(c, v.Name)); lerr == nil {
				notes, skipped, nerr := ll.List()
				if nerr == nil {
					vo.Decisions = len(notes)
					if len(skipped) > 0 {
						out.Warnings = append(out.Warnings,
							fmt.Sprintf("볼트 %s 의 결정 노트 %d건을 읽지 못했다", v.Name, len(skipped)))
					}
				}
			}
		} else {
			// **자리가 없는 볼트는 조용히 넘기지 않는다.** 그 볼트로 엮인
			// 도메인의 기록이 통째로 안 써지는데 겉으로는 아무 일도 안 난다.
			out.Warnings = append(out.Warnings,
				fmt.Sprintf("볼트 %s 의 자리가 없다: %s", v.Name, v.Path))
		}
		sort.Strings(vo.Domains)
		out.Vaults = append(out.Vaults, vo)
	}

	// **레지스트리가 목록의 정본이다.** 설정에 적힌 것만 보이면 새 파서를
	// 붙였을 때 사람이 그 존재를 영영 모른다.
	for _, h := range hosts.All() {
		ho := hostOut{Name: h.Name, Enabled: c.HostOn(h.Name), Root: c.HostRoot(h.Name)}
		root := ho.Root
		if root == "" {
			if r, rerr := h.DefaultRoot(); rerr == nil {
				root = r
				ho.Root = r
			}
		}
		if root != "" {
			if st, serr := os.Stat(root); serr == nil && st.IsDir() {
				ho.Exists = true
				if files, _, lerr := h.List(root); lerr == nil {
					ho.Files = len(files)
				}
			} else if h.Required {
				out.Warnings = append(out.Warnings,
					fmt.Sprintf("%s 의 기록 자리가 없다: %s", h.Name, root))
			}
		}
		out.Hosts = append(out.Hosts, ho)
	}
	return out, nil
}

// prefixUsing 은 그 볼트를 쓰는 도메인 접두어 하나를 준다. Layout.For 는
// 도메인으로 볼트를 고르므로, 볼트를 직접 지목할 통로가 없다.
func prefixUsing(c *config.Config, vault string) string {
	for _, d := range c.Domain {
		if d.Vault == vault {
			return d.Prefix
		}
		if d.Vault == "" && vault == config.DefaultVaultName {
			return d.Prefix
		}
	}
	return ""
}

func printSettings(cmd *cobra.Command, s settingsOut) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "설정  %s\n\n", s.ConfigPath)
	fmt.Fprintln(w, "볼트")
	for _, v := range s.Vaults {
		mark := "✓"
		if !v.Exists {
			mark = "✗"
		}
		fmt.Fprintf(w, "  %s %-12s %s\n", mark, v.Name, v.Path)
		fmt.Fprintf(w, "      결정 %d건 · 도메인 %d개\n", v.Decisions, len(v.Domains))
	}
	fmt.Fprintln(w, "\n호스트")
	for _, h := range s.Hosts {
		mark := "○"
		if h.Enabled {
			mark = "●"
		}
		state := "자리 없음"
		if h.Exists {
			state = fmt.Sprintf("기록 %d개", h.Files)
		}
		fmt.Fprintf(w, "  %s %-14s %s  %s\n", mark, h.Name, state, h.Root)
	}
	fmt.Fprintln(w, "\n도메인")
	for _, d := range s.Domains {
		v := d.Vault
		if v == "" {
			v = config.DefaultVaultName + " (기본)"
		}
		fmt.Fprintf(w, "  %-12s → %-16s %s\n", d.Prefix, v, d.Folder)
	}
	for _, warn := range s.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "경고: %s\n", warn)
	}
}

func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "vault",
		Short:         "볼트를 보고 만든다",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	add := &cobra.Command{
		Use:   "add <이름> <경로>",
		Short: "볼트를 하나 더한다",
		Long: "볼트를 더한다. 자리가 없으면 만든다.\n\n" +
			"설정이 아직 `vault = \"...\"` 한 줄짜리면 [[vault]] 두 벌로 바꾼다 —\n" +
			"옛 볼트의 이름은 " + config.DefaultVaultName + " 이 된다.",
		Args:          cobra.ExactArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, path := args[0], args[1]
			abs, err := expandPath(path)
			if err != nil {
				return err
			}
			// **자리를 먼저 만든다.** 설정만 고치고 폴더가 없으면 그 볼트로 엮인
			// 도메인이 첫 기록에서 실패하는데, 그때는 설정을 고친 지 한참 뒤다.
			if err := os.MkdirAll(abs, 0o755); err != nil {
				return fmt.Errorf("볼트 자리를 만들 수 없다 (%s): %w", abs, err)
			}
			return applyEdit(cmd, func(src []byte) ([]byte, error) {
				return config.AddVault(src, name, path)
			})
		},
	}
	list := &cobra.Command{
		Use:           "list",
		Short:         "볼트를 낸다",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := collectSettings(cmd)
			if err != nil {
				return err
			}
			for _, v := range s.Vaults {
				mark := "✓"
				if !v.Exists {
					mark = "✗"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %-12s %s (결정 %d건)\n", mark, v.Name, v.Path, v.Decisions)
			}
			return nil
		},
	}
	cmd.AddCommand(add, list)
	return cmd
}

func newDomainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "domain",
		Short:         "도메인을 볼트에 엮는다",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	bind := &cobra.Command{
		Use:   "bind <도메인> [볼트]",
		Short: "도메인이 쓸 볼트를 정한다 (볼트를 비우면 기본 볼트로 되돌린다)",
		Long: "그 도메인의 결정이 어느 볼트에 쓰이고 어느 볼트에서 회수되는지를 정한다.\n\n" +
			"볼트를 비우면 기본 볼트로 되돌린다.",
		Args:          cobra.RangeArgs(1, 2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			vault := ""
			if len(args) == 2 {
				vault = args[1]
			}
			return applyEdit(cmd, func(src []byte) ([]byte, error) {
				return config.BindDomain(src, args[0], vault)
			})
		},
	}
	cmd.AddCommand(bind)
	return cmd
}

func newHostsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "hosts",
		Short:         "어느 도구의 대화 기록을 훑을지 정한다",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	list := &cobra.Command{
		Use:           "list",
		Short:         "호스트를 낸다",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := collectSettings(cmd)
			if err != nil {
				return err
			}
			for _, h := range s.Hosts {
				mark := "○"
				if h.Enabled {
					mark = "●"
				}
				state := "자리 없음"
				if h.Exists {
					state = fmt.Sprintf("기록 %d개", h.Files)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %-14s %-14s %s\n", mark, h.Name, state, h.Root)
			}
			return nil
		},
	}
	set := func(use, short string, on bool) *cobra.Command {
		return &cobra.Command{
			Use:           use + " <이름>",
			Short:         short,
			Args:          cobra.ExactArgs(1),
			SilenceUsage:  true,
			SilenceErrors: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				name, err := knownHost(args[0])
				if err != nil {
					return err
				}
				root, _ := cmd.Flags().GetString("root")
				return applyEdit(cmd, func(src []byte) ([]byte, error) {
					return config.SetHost(src, name, on, root)
				})
			},
		}
	}
	enable := set("enable", "그 호스트의 기록을 훑는다", true)
	enable.Flags().String("root", "", "기록이 쌓이는 자리를 덮어쓴다 (비우면 기본 자리)")
	disable := set("disable", "그 호스트의 기록을 안 훑는다", false)
	disable.Flags().String("root", "", "기록이 쌓이는 자리를 덮어쓴다 (비우면 기본 자리)")
	cmd.AddCommand(list, enable, disable)
	return cmd
}

// knownHost 는 이름이 레지스트리에 있는지 본다.
//
// **오타를 받아 주면 안 된다.** 설정에는 아무 이름이나 들어가고, 그 이름은
// 어느 호스트와도 안 맞으므로 아무 일도 안 일어난다 — 사람은 껐다고 믿는다.
func knownHost(name string) (string, error) {
	var names []string
	for _, h := range hosts.All() {
		if h.Name == name {
			return h.Name, nil
		}
		names = append(names, h.Name)
	}
	return "", fmt.Errorf("그런 호스트가 없다: %q (있는 것: %v)", name, names)
}

// expandPath 는 ~ 를 편다. 설정에는 원문(~ 포함)을 그대로 쓰고, 폴더를 만들
// 때만 편 경로를 쓴다 — 설정 파일이 기계 뒤에서도 그대로 읽히게 하려는 것이다.
func expandPath(p string) (string, error) {
	if p != "~" && !filepath.IsAbs(p) && len(p) > 1 && p[0] == '~' && p[1] == '/' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("홈 디렉토리를 확인할 수 없다: %w", err)
		}
		return filepath.Join(home, p[2:]), nil
	}
	if p == "~" {
		return os.UserHomeDir()
	}
	return filepath.Abs(p)
}
