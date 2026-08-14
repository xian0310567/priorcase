package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// userHome·osStat 은 시험이 갈아 끼울 수 있게 변수로 둔다. 홈 디렉토리를 실제로
// 건드리는 시험은 기계마다 다른 결과를 내고, 그런 시험은 신호를 잃는다.
var (
	userHome = os.UserHomeDir
	osStat   = func(p string) (fs.FileInfo, error) { return os.Stat(p) }
)

// DefaultVaultName 은 이름 없는 볼트에 붙는 이름이다.
//
// 레거시 설정(`vault = "경로"`)에는 이름을 적을 자리가 없다. 그때도 볼트가 하나
// 있는 것이고, 도메인이 볼트를 지정하지 않으면 그것으로 간다.
const DefaultVaultName = "default"

// Vault 는 결정 노트가 사는 자리 하나다.
//
// **볼트는 쓰기와 회수의 경계다.** 프로젝트가 어느 볼트에 속하는지가 곧 "그
// 프로젝트에서 일할 때 무엇이 기록되고 무엇이 회수되는가" 를 정한다.
type Vault struct {
	Name string `toml:"name"`
	Path string `toml:"path"`
}

// VaultFor 는 도메인 접두어가 속한 볼트를 준다.
//
// 도메인이 볼트를 지정하지 않았으면 기본 볼트다. 도메인을 모르면(빈 문자열)
// 역시 기본 볼트다 — 그 경우에도 쓸 자리는 있어야 한다.
//
// **못 찾으면 에러다. 기본 볼트로 조용히 떨어지지 않는다.** 설정에 없는 볼트
// 이름을 적은 것은 오타이고, 그때 엉뚱한 볼트에 쓰면 그 결정은 사람이 찾을 수
// 없는 자리에 남는다.
func (c *Config) VaultFor(prefix string) (Vault, error) {
	want := ""
	for _, d := range c.Domain {
		if d.Prefix == prefix {
			want = d.Vault
			break
		}
	}
	if want == "" {
		return c.DefaultVault()
	}
	for _, v := range c.Vaults {
		if v.Name == want {
			return v, nil
		}
	}
	var names []string
	for _, v := range c.Vaults {
		names = append(names, v.Name)
	}
	return Vault{}, fmt.Errorf("도메인 %q 가 가리키는 볼트 %q 가 설정에 없다 (있는 것: %s)",
		prefix, want, strings.Join(names, ", "))
}

// DefaultVault 는 볼트를 지정하지 않은 도메인이 쓸 볼트다.
//
// 이름이 DefaultVaultName 인 것이 있으면 그것, 없으면 **첫 번째**다. 순서는
// 설정 파일에 적힌 순서이므로 사람이 보는 것과 같다.
func (c *Config) DefaultVault() (Vault, error) {
	if len(c.Vaults) == 0 {
		return Vault{}, fmt.Errorf("볼트가 하나도 없다 — vault = \"경로\" 또는 [[vault]] 를 적어라")
	}
	for _, v := range c.Vaults {
		if v.Name == DefaultVaultName {
			return v, nil
		}
	}
	return c.Vaults[0], nil
}

// VaultForCwd 는 그 자리에서 일할 때 쓸 볼트를 준다.
//
// cwd → 도메인 → 볼트 순이다. 도메인 해석은 기존 규칙 그대로다.
func (c *Config) VaultForCwd(dir string) (Vault, error) {
	return c.VaultFor(c.DomainForCwd(dir))
}

// validateVaults 는 볼트 선언을 검사한다.
func (c *Config) validateVaults() error {
	if len(c.Vaults) == 0 {
		return fmt.Errorf("vault 가 비어 있다")
	}
	seen := map[string]bool{}
	for _, v := range c.Vaults {
		if v.Path == "" {
			return fmt.Errorf("볼트 %q 에 path 가 없다", v.Name)
		}
		if !filepath.IsAbs(v.Path) {
			return fmt.Errorf("볼트 %q 의 path 가 절대 경로가 아니다: %s", v.Name, v.Path)
		}
		// **이름이 겹치면 도메인이 어느 쪽을 가리키는지 알 수 없다.**
		// 하나를 조용히 고르면 결정이 엉뚱한 볼트에 쌓인다.
		if v.Name != "" && seen[v.Name] {
			return fmt.Errorf("볼트 이름이 중복이다: %s", v.Name)
		}
		seen[v.Name] = true
	}
	// 볼트가 둘 이상이면 이름이 있어야 도메인이 가리킬 수 있다.
	if len(c.Vaults) > 1 {
		for _, v := range c.Vaults {
			if v.Name == "" {
				return fmt.Errorf("볼트가 여럿인데 이름 없는 것이 있다 (path=%s) — "+
					"도메인이 name 으로 가리킨다", v.Path)
			}
		}
	}
	// 도메인이 가리키는 볼트가 실제로 있어야 한다. 없으면 기록이 조용히
	// 기본 볼트로 새거나 실패한다 — 둘 다 나중에야 드러난다.
	for _, d := range c.Domain {
		if d.Vault == "" {
			continue
		}
		if !seen[d.Vault] {
			return fmt.Errorf("도메인 %q 가 없는 볼트 %q 를 가리킨다", d.Prefix, d.Vault)
		}
	}
	return nil
}

// DefaultVaultPath 는 기본 볼트의 경로다. 볼트가 없으면 빈 문자열.
//
// **볼트가 하나인 경로에서만 써라.** 어느 볼트인지가 중요한 자리에서는
// VaultFor / VaultForCwd 로 명시해야 한다 — 여기로 오면 프로젝트별 볼트가
// 조용히 무시된다.
func (c *Config) DefaultVaultPath() string {
	v, err := c.DefaultVault()
	if err != nil {
		return ""
	}
	return v.Path
}

// folderBad 는 볼트 폴더 이름에 쓸 수 없는 문자다.
//
// **store.Slugify 의 목록과 일부러 따로 둔다.** 그쪽은 파일명 slug 라 공백을
// `-` 로 바꾸지만, 볼트 폴더는 사람이 Finder 에서 보는 이름이라 공백이 정상이다
// (실측: `Obsidian Vault`). 규칙이 같아 보여도 다른 규칙이므로 한 곳으로 묶으면
// 한쪽을 고칠 때 다른 쪽이 조용히 따라 바뀐다.
const folderBad = `/\:*?"<>|`

// VaultParent 는 **새 볼트를 만들 부모 디렉토리**다.
//
// 지금 기본 볼트가 있는 자리 옆이다. 볼트를 여럿 두는 이유가 공유를 위한 분리이므로
// (프로젝트 폴더 단위로 git 에 올리거나 동기화한다), 새 볼트도 **사람이 Finder 에서
// 보고 다룰 수 있는 자리**여야 한다. `~/.local/share` 같은 데 숨기면 그 목적을 못 갚는다.
//
// 볼트가 하나도 없으면 `~/Documents` 다 — Obsidian 이 기본으로 여는 자리다.
// 그것도 없으면 홈이다.
func (c *Config) VaultParent() (string, error) {
	if v, err := c.DefaultVault(); err == nil && filepath.IsAbs(v.Path) {
		return filepath.Dir(v.Path), nil
	}
	home, err := userHome()
	if err != nil {
		return "", err
	}
	docs := filepath.Join(home, "Documents")
	if fi, serr := osStat(docs); serr == nil && fi.IsDir() {
		return docs, nil
	}
	return home, nil
}

// NewVaultPath 는 이름만 받았을 때 볼트를 만들 자리다.
//
// **이름을 조용히 고치지 않는다.** 못 쓰는 문자가 있으면 거부한다 — 슬그머니
// 바꾸면 사람이 자기가 적은 이름으로 폴더를 찾다가 못 찾는다.
//
// 경로 구분자와 `..` 를 막는 것은 **부모 디렉토리 밖으로 나가지 못하게** 하기
// 위해서다. 이 값은 그대로 os.MkdirAll 로 간다.
func (c *Config) NewVaultPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("볼트 이름이 비었다")
	}
	if strings.ContainsAny(name, folderBad) {
		return "", fmt.Errorf("볼트 이름에 쓸 수 없는 문자가 있다 (%s): %q", folderBad, name)
	}
	if name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("볼트 이름은 점으로 시작할 수 없다: %q", name)
	}
	parent, err := c.VaultParent()
	if err != nil {
		return "", err
	}
	p := filepath.Join(parent, name)
	// 조합한 뒤에도 확인한다 — 위 검사를 빠져나가는 조합이 있으면 여기서 걸린다.
	if filepath.Dir(p) != parent {
		return "", fmt.Errorf("볼트 이름이 폴더를 벗어난다: %q", name)
	}
	return p, nil
}
