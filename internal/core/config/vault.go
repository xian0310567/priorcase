package config

import (
	"fmt"
	"path/filepath"
	"strings"
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
