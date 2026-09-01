package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/split"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/core/sync"
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
	ConfigPath string `json:"config_path"`
	// VaultParent 는 **새 볼트를 만들면 들어갈 부모 디렉토리**다.
	//
	// 앱이 이걸 알아야 "만들면 어디에 생기는지" 를 누르기 전에 보여 줄 수 있다.
	// 경로를 사람에게 묻지 않기로 했으므로(2026-08-14), 대신 어디에 생기는지는
	// 반드시 보여 줘야 한다 — 안 그러면 어디에 만들어졌는지 모르는 폴더가 생긴다.
	VaultParent string      `json:"vault_parent"`
	Vaults      []vaultOut  `json:"vaults"`
	Domains     []domainOut `json:"domains"`
	Hosts       []hostOut   `json:"hosts"`
	Warnings    []string    `json:"warnings,omitempty"`
}

type vaultOut struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Exists    bool     `json:"exists"`
	Decisions int      `json:"decisions"`
	Domains   []string `json:"domains"`
	// Remote 는 이 볼트가 동기화할 git origin 이다. 빈 값은 **이 머신에만 있다**
	// 는 뜻이고 고장이 아니다 — 앱이 그 상태를 빨갛게 그리면 안 된다.
	Remote string `json:"remote"`
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
	if parent, perr := c.VaultParent(); perr == nil {
		out.VaultParent = parent
	}

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
			Paths: list(d.Paths), Repos: list(d.Repos),
		})
	}

	for _, v := range c.Vaults {
		// **빈 목록은 [] 로 낸다. null 이면 앱이 순회하다 터진다.**
		//
		// 2026-09-01 사고: 새 볼트는 아직 아무 프로젝트도 안 쓰므로 이 키가 없고,
		// Go 의 nil 슬라이스는 JSON null 로 나간다. 앱이 `v.domains.length` 를 읽다
		// TypeError 를 냈고 그 예외가 렌더를 끊어 **볼트 화면이 통째로 사라졌다.**
		// 같은 고장이 show --json 의 summary_history 에서 먼저 났는데(검은 화면)
		// 그 교훈을 여기 안 옮겼다.
		//
		// "아직 아무도 안 쓰는 볼트" 는 흔한 정상 상태다 — 볼트를 만든 직후가
		// 언제나 그렇다. 즉 이 경로는 예외가 아니라 **첫 경험**이다.
		vo := vaultOut{Name: v.Name, Path: v.Path, Domains: list(byVault[v.Name])}
		if st, serr := os.Stat(v.Path); serr == nil && st.IsDir() {
			vo.Exists = true
			// **볼트에서 곧장 레이아웃을 만든다.**
			//
			// 예전에는 그 볼트를 쓰는 도메인 하나를 찾아(prefixUsing) 거기서
			// 레이아웃을 얻었다. 그런데 아직 아무 도메인도 안 엮인 볼트는 빈
			// 접두어가 나오고, 그러면 VaultFor 가 **기본 볼트로 떨어뜨린다** —
			// 방금 만든 빈 볼트가 기본 볼트의 결정 수를 그대로 달고 나왔다.
			// 사람은 그걸 보고 "이미 옮겨졌다" 로 읽는다.
			// 리모트를 못 읽는 것은 고장이 아니다 (sync.Remote 의 §).
			vo.Remote, _ = sync.Remote(v.Path)
			if notes, skipped, nerr := store.NewLayoutFor(c, v).List(); nerr == nil {
				vo.Decisions = len(notes)
				if len(skipped) > 0 {
					out.Warnings = append(out.Warnings,
						fmt.Sprintf("볼트 %s 의 결정 노트 %d건을 읽지 못했다", v.Name, len(skipped)))
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
		Use:   "add <이름> [경로]",
		Short: "볼트를 하나 더한다 (경로를 안 주면 지금 볼트 옆에 만든다)",
		Long: "볼트를 더한다. 자리가 없으면 만든다.\n\n" +
			"**경로는 안 줘도 된다.** 안 주면 지금 기본 볼트 옆에 이름 그대로 만든다\n" +
			"(볼트가 하나도 없으면 ~/Documents). 볼트를 여럿 두는 이유가 공유를 위한\n" +
			"분리라, 새 볼트도 사람이 Finder 에서 보고 다룰 수 있는 자리여야 한다.\n\n" +
			"설정이 아직 `vault = \"...\"` 한 줄짜리면 [[vault]] 두 벌로 바꾼다 —\n" +
			"옛 볼트의 이름은 " + config.DefaultVaultName + " 이 된다.",
		Args:          cobra.RangeArgs(1, 2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			c, _, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			// **경로를 안 주면 우리가 정한다.** 사람에게 경로를 묻는 것은
			// 답이 이미 정해져 있는 질문이다 — 지금 볼트 옆이다.
			path := ""
			if len(args) == 2 {
				path = args[1]
			} else {
				p, perr := c.NewVaultPath(name)
				if perr != nil {
					return perr
				}
				path = p
			}
			abs, err := expandPath(path)
			if err != nil {
				return err
			}
			// **자리를 먼저 만든다.** 설정만 고치고 폴더가 없으면 그 볼트로 엮인
			// 도메인이 첫 기록에서 실패하는데, 그때는 설정을 고친 지 한참 뒤다.
			if err := os.MkdirAll(abs, 0o755); err != nil {
				return fmt.Errorf("볼트 자리를 만들 수 없다 (%s): %w", abs, err)
			}
			if err := applyEdit(cmd, func(src []byte) ([]byte, error) {
				return config.AddVault(src, name, path)
			}); err != nil {
				return err
			}
			// **어디에 만들었는지 반드시 말한다.** 경로를 안 물어봤으므로
			// 이 줄이 없으면 어디에 생겼는지 모르는 폴더가 남는다.
			fmt.Fprintf(cmd.ErrOrStderr(), "볼트 %s → %s\n", name, abs)
			return nil
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
	cmd.AddCommand(add, list, newVaultRemoteCmd(), newVaultPathCmd())
	return cmd
}

// newVaultRemoteCmd 는 볼트의 git 리모트를 보고 정한다.
//
// **앱이 이걸 부른다.** 앱만 받은 사람에게 "터미널에서 git remote add 를 치세요"
// 라고 할 수는 없다 — 회사 볼트는 만들자마자 회사 리모트에 붙어야 그 결정이
// 개인 머신에만 남지 않는다 (sync.SetRemote 의 §).
// newVaultPathCmd 는 볼트가 사는 자리를 옮긴다.
//
// # 왜 필요한가
//
// `vault add` 는 경로를 안 묻고 **기존 볼트 옆**에 만든다. 그 전제는 기본 볼트가
// 읽을 수 있는 자리에 있다는 것인데, 2026-09-01 에 깨졌다 — 기본 볼트가
// `~/Documents` 에 있어서 새 볼트도 거기 생겼고, macOS 가 그 폴더를 보호한다.
// **앱은 권한이 있어 읽지만 터미널의 prior 와 훅은 못 읽는다.** 그러면 그 볼트에는
// 기록도 회수도 동기화도 안 되는데 앱 화면은 멀쩡해 보인다.
//
// 폴더를 옮기려 해도 같은 권한이 막는다. 그래서 설정에서 옮기는 길이 필요하다.
//
// **파일은 안 옮긴다.** 새 자리에 볼트를 만들고 내용을 옮기는 것은 사람이 정할
// 일이다 — 여기서 같이 하면 "설정만 고치고 싶다" 는 경우에 되돌릴 수 없다.
func newVaultPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path <볼트> <경로>",
		Short: "볼트가 사는 자리를 옮긴다 (설정만 바꾼다)",
		Long: "설정에서 그 볼트의 경로를 바꾼다. **파일은 안 옮긴다.**\n\n" +
			"보호된 폴더(macOS 의 ~/Documents 등)에 볼트가 생기면 앱은 읽어도\n" +
			"터미널의 prior 와 훅은 못 읽는다 — 그때 자리를 옮기는 자리다.",
		Args:          cobra.ExactArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := applyEdit(cmd, func(src []byte) ([]byte, error) {
				return config.SetVaultPath(src, args[0], args[1])
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "볼트 %s → %s\n", args[0], args[1])
			return nil
		},
	}
}

func newVaultRemoteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "remote <볼트> [URL]",
		Short: "볼트가 동기화할 git 리모트를 보고 정한다 (URL 을 비우면 지금 값만 낸다)",
		Long: "URL 을 주면 origin 을 그 주소로 정한다. 그 자리가 git 저장소가 아니면 만들어 준다.\n\n" +
			"URL 을 비우면 지금 붙어 있는 origin 을 낸다.",
		Args:          cobra.RangeArgs(1, 2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			v, err := c.VaultNamed(args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(args) == 1 {
				url, rerr := sync.Remote(v.Path)
				if rerr != nil {
					return rerr
				}
				if url == "" {
					fmt.Fprintf(out, "볼트 %s 에 리모트가 없다 — 이 볼트는 이 머신에만 있다\n", v.Name)
					return nil
				}
				fmt.Fprintf(out, "%s\n", url)
				return nil
			}
			// **저장하기 전에 실제로 닿아 본다.**
			//
			// 예전에는 주소를 그대로 박았다. 문법만 맞으면 저장되므로 오타나
			// 자격증명 문제가 **다음 동기화가 실패할 때까지 안 드러났다** — 그런데
			// 동기화는 세션 경계에서 조용히 돌고 훅은 무슨 일이 있어도 exit 0 이라,
			// 그 볼트의 결정이 아무 데도 안 가는 상태가 며칠씩 안 보인다.
			//
			// **못 닿으면 저장하지 않는다.** 저장해 두고 경고만 하면 그 경고는
			// 한 번 스쳐 지나가고, 남는 것은 틀린 주소다. 정말 강행해야 하는
			// 경우를 위해 --force 를 둔다 (지금 네트워크가 안 될 뿐인 때).
			if !force {
				if cerr := sync.CheckRemote(args[1], remoteCheckBudget); cerr != nil {
					return fmt.Errorf("%w\n\n주소·권한을 확인해라. 지금 네트워크가 안 될 뿐이면 --force 로 그대로 저장한다", cerr)
				}
			}
			if err := sync.SetRemote(v.Path, args[1]); err != nil {
				return err
			}
			fmt.Fprintf(out, "볼트 %s → %s\n", v.Name, args[1])
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false,
		"닿는지 확인하지 않고 그대로 저장한다 (지금 네트워크가 안 될 뿐일 때)")
	return cmd
}

// remoteCheckBudget 은 리모트 접근 확인에 주는 시간이다.
//
// **사람이 [저장]을 누르고 기다리는 자리다.** 회사망에서 git 이 매달리면 앱이
// 통째로 멈춘 것처럼 보이므로 짧게 끊는다. 못 끝내면 "못 닿는다" 로 답하는데,
// 그건 이 상황에서 사실상 맞는 말이다 — 매번 이만큼 걸리는 리모트라면 동기화도
// 매번 실패한다.
const remoteCheckBudget = 12 * time.Second

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
	cmd.AddCommand(bind, newDomainSplitCmd(), newDomainMoveCmd())
	return cmd
}

// newDomainMoveCmd 는 도메인의 파일을 **실제로** 다른 볼트로 옮긴다.
//
// `domain bind` 는 설정만 바꾼다. 그것만 하면 회수는 새 볼트의 빈 폴더를 보고
// 그 프로젝트의 결정이 통째로 사라진다 — 2026-09-01 에 editup 63건이 그렇게
// 안 보이게 됐고, 화면에는 "결정 0건" 이라고만 떴다. 둘은 짝이다.
func newDomainMoveCmd() *cobra.Command {
	var apply bool
	cmd := &cobra.Command{
		Use:   "move <도메인> <볼트>",
		Short: "도메인의 파일을 다른 볼트로 옮긴다 (기본은 계획만)",
		Long: "그 도메인의 폴더를 통째로 다른 볼트로 옮기고 설정도 함께 바꾼다.\n\n" +
			"**domain bind 는 설정만 바꾼다.** 파일이 안 따라가면 회수는 빈 폴더를 보고\n" +
			"그 프로젝트의 결정이 통째로 사라진다 — 이 명령이 그 뒤를 잇는다.\n\n" +
			"파일명도 frontmatter 도 안 바뀐다. 도메인 이름이 그대로이기 때문이다.\n" +
			"다만 **볼트를 넘는 위키링크는 깨진다** — 옵시디언은 볼트 안에서만 링크를 푼다.\n\n" +
			"**되돌리기는 git 이다.** 볼트에 커밋하지 않은 변경이 있으면 먼저 정리해라.",
		Args:          cobra.ExactArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			prefix, vaultName := args[0], args[1]
			c, _, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			dstVault, err := c.VaultNamed(vaultName)
			if err != nil {
				return err
			}
			// **설정이 아니라 디스크를 본다.** 이 명령이 고치려는 상태가 정확히
			// "설정은 바뀌었는데 파일이 안 따라간 것" 이라, 설정으로 원본을 찾으면
			// 목적지와 같아져서 고치려던 바로 그 상태에서만 안 돈다.
			srcPath, err := split.FindSource(c, prefix, dstVault.Path)
			if err != nil {
				return err
			}
			srcVault, err := c.VaultAtPath(srcPath)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			p, err := split.PlanMove(c, store.NewLayoutFor(c, srcVault),
				store.NewLayoutFor(c, dstVault), prefix)
			if err != nil {
				return err
			}

			fmt.Fprintf(out, "%s: %s\n  → %s\n\n파일 %d개\n", prefix, p.From, p.To, len(p.Moves))
			if !apply {
				// **기본은 계획만.** 파일을 옮기는 것은 되돌리기 어렵고, split 도
				// 같은 규약을 쓴다 — 두 명령이 다르게 굴면 하나는 반드시 오해받는다.
				fmt.Fprintln(out, "\n계획만 세웠다. 실행하려면 --apply 를 줘라.")
				return nil
			}
			if err := split.ApplyMove(p); err != nil {
				return err
			}
			// **설정도 같이 바꾼다.** 파일만 옮기고 설정이 옛 볼트를 가리키면
			// 방금 고친 고장의 거울상이 된다.
			if err := applyEdit(cmd, func(src []byte) ([]byte, error) {
				return config.BindDomain(src, prefix, vaultName)
			}); err != nil {
				return err
			}
			fmt.Fprintf(out, "\n옮겼다. %s 는 이제 볼트 %s 에 있다.\n", prefix, vaultName)
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "실제로 옮긴다")
	return cmd
}

// newDomainSplitCmd 는 폴백 도메인에 쌓인 프로젝트를 떼어낸다.
//
// `prior doctor` 의 **폴백 적체** 검사가 찾아낸 것을 실행하는 자리다. 그 검사가
// 이 명령을 가리키므로 둘은 짝이다 — 한쪽만 있으면 진단이 갈 곳을 잃는다.
func newDomainSplitCmd() *cobra.Command {
	var as, path, vault string
	var apply bool
	cmd := &cobra.Command{
		Use:   "split <낱말>...",
		Short: "폴백 도메인에 쌓인 프로젝트를 새 도메인으로 떼어낸다 (기본은 계획만)",
		Long: "`prior doctor` 의 폴백 적체 검사가 찾아낸 프로젝트를 자기 도메인으로 옮긴다.\n\n" +
			"결정 노트를 옮기고 파일명·frontmatter 의 domain 을 바꾸며, 그 노트를 가리키던\n" +
			"위키링크를 볼트 전체에서 고친다.\n\n" +
			"**되돌리기는 git 이다.** 볼트에 커밋하지 않은 변경이 있으면 먼저 정리해라.",
		Args:          cobra.MinimumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := loadFrom(cmd)
			if err != nil {
				return err
			}
			notes, skipped, err := l.List()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, sk := range skipped {
				fmt.Fprintf(cmd.ErrOrStderr(), "⚠ 읽지 못해 대상에서 빠졌다: %s\n", l.RelPath(sk.Path))
			}
			// **도착 볼트를 먼저 정한다.** 새 도메인은 그 볼트에 산다.
			dst := l
			if vault != "" {
				v, verr := c.VaultNamed(vault)
				if verr != nil {
					return verr
				}
				dst = store.NewLayoutFor(c, v)
			}
			p, err := split.Build(c, l, dst, notes, args, as)
			if err == nil {
				p.Vault = vault
			}
			if err != nil {
				return err
			}
			if len(p.Moves) == 0 {
				fmt.Fprintf(out, "%s/ 에서 %v 로 옮길 결정이 없다\n", c.DefaultDomain, args)
				return nil
			}
			where := ""
			if p.Vault != "" {
				where = fmt.Sprintf(" (볼트 %s)", p.Vault)
			}
			fmt.Fprintf(out, "도메인 %s%s ← %s/ 결정 %d건\n",
				p.Prefix, where, c.DefaultDomain, len(p.Moves))
			for _, m := range p.Moves {
				fmt.Fprintf(out, "  %s\n    → %s\n", m.OldStem, m.NewStem)
			}
			if n := len(p.Relinks); n > 0 {
				total := 0
				for _, r := range p.Relinks {
					total += r.Count
				}
				fmt.Fprintf(out, "위키링크 %d개를 문서 %d건에서 고친다\n", total, n)
			}
			for _, s := range p.Skipped {
				fmt.Fprintf(out, "건너뜀: %s\n", s)
			}
			if !apply {
				fmt.Fprintf(out, "\n계획만 냈다. 실행하려면 --apply 를 붙여라 (되돌리기는 git 이다).\n")
				return nil
			}
			// **설정을 먼저 고친다.** 파일을 옮겨 놓고 설정이 실패하면 그 폴더는
			// 미선언 도메인이 되어 회수에서 통째로 빠진다 — doctor 가 잡기는 하지만
			// 그 사이에 조용히 안 보인다. 순서를 뒤집으면 그 창이 없다.
			if err := applyEdit(cmd, func(src []byte) ([]byte, error) {
				var paths []string
				if path != "" {
					paths = []string{path}
				}
				return config.AddDomain(src, p.Prefix, p.Folder, p.Vault, paths)
			}); err != nil {
				return err
			}
			if err := split.Apply(p); err != nil {
				return err
			}
			fmt.Fprintf(out, "옮겼다: 결정 %d건 → %s\n", len(p.Moves), l.RelPath(p.Dir))
			return nil
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "새 도메인 접두어 (기본: 낱말 그대로)")
	cmd.Flags().StringVar(&path, "path", "", "이 프로젝트의 작업 경로 (설정의 paths 에 넣는다)")
	cmd.Flags().StringVar(&vault, "vault", "",
		"새 도메인이 살 볼트 (비우면 기본 볼트) — 회사 결정을 회사 볼트로 보낼 때 쓴다")
	cmd.Flags().BoolVar(&apply, "apply", false, "실제로 옮긴다")
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

// list 는 nil 슬라이스를 **빈 슬라이스**로 만든다.
//
// # 왜 필요한가
//
// Go 의 nil 슬라이스는 JSON `null` 로 나가는데, 그것을 받는 쪽은 배열을 가정하고
// `.length` 를 읽는다. 그러면 TypeError 가 나고 그 예외가 렌더를 끊는다 —
// 화면이 통째로 사라지고, 새로고침해도 같은 자리에서 또 터지니 영구적으로 보인다.
//
// **두 번 겪었다** (2026-09-01): show --json 의 summary_history 가 검은 화면을,
// settings --json 의 domains 가 볼트 화면 절반을 지웠다. 매번 그 필드 하나를
// 고쳤는데, 원인은 필드가 아니라 구조다 — 빈 목록은 예외가 아니라 **가장 흔한
// 첫 경험**이다. 새 볼트에는 프로젝트가 없고, 새 결정에는 요약 이력이 없다.
//
// jsoncontract_test.go 가 출력 전체를 훑어 이 계약을 잠근다.
func list(ss []string) []string {
	if ss == nil {
		return []string{}
	}
	return ss
}
