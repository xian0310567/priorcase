// Package config 는 casebook 의 유일한 설정 정본이다.
// 코드에 개인 설정 리터럴을 두지 않는다 — 볼트 경로·도메인 매핑·제외·키워드가 전부 여기서 온다.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/xian0310567/casebook/internal/core/xdgpath"
)

type Naming struct {
	DecisionFile string `toml:"decision_file"`
	DecisionsDir string `toml:"decisions_dir"`
	Worklog      string `toml:"worklog"`
	Index        string `toml:"index"`
}

type Capture struct {
	Signals        []string `toml:"signals"`
	MinTurns       int      `toml:"min_turns"`
	QuiesceSeconds int      `toml:"quiesce_seconds"`
}

type Domain struct {
	Prefix string   `toml:"prefix"`
	Folder string   `toml:"folder"`
	Paths  []string `toml:"paths"`
}

type Config struct {
	Vault   string   `toml:"vault"`
	Exclude []string `toml:"exclude"`
	Naming  Naming   `toml:"naming"`
	Capture Capture  `toml:"capture"`
	Domain  []Domain `toml:"domain"`
}

// DefaultPath 는 XDG 기준 설정 파일 경로다.
func DefaultPath() (string, error) {
	dir, err := xdgpath.ConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "casebook", "config.toml"), nil
}

// Load 는 설정을 읽는다. path 가 비면 DefaultPath 를 쓴다.
// CASEBOOK_VAULT 가 설정돼 있으면 vault 를 덮어쓴다 (테스트 볼트 격리용).
func Load(path string) (*Config, error) {
	if path == "" {
		var err error
		if path, err = DefaultPath(); err != nil {
			return nil, err
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("설정 파일을 열 수 없다 (%s): %w", path, err)
	}
	defer f.Close()

	dec := toml.NewDecoder(f)
	dec.DisallowUnknownFields() // 오타 키를 조용히 넘기지 않는다

	var c Config
	if err := dec.Decode(&c); err != nil {
		var se *toml.StrictMissingError
		if errors.As(err, &se) {
			return nil, fmt.Errorf("설정에 알 수 없는 키가 있다 (%s):\n%s", path, se.String())
		}
		return nil, fmt.Errorf("설정 파싱 실패 (%s): %w", path, err)
	}

	if v := os.Getenv("CASEBOOK_VAULT"); v != "" {
		c.Vault = v
	}
	if err := c.expand(); err != nil {
		return nil, fmt.Errorf("경로 확장 실패 (%s): %w", path, err)
	}
	return &c, c.validate()
}

// expand 는 ~ 를 홈 디렉토리로 편다. 경로 비교 전에 반드시 수행한다.
// 홈 디렉토리를 못 구하면 에러를 반환한다 — 조용히 넘어가면 ~ 가 문자
// 그대로 남아 filepath.Rel 비교에서 이상한 결과를 낸다.
func (c *Config) expand() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("홈 디렉토리를 확인할 수 없다: %w", err)
	}
	tilde := func(p string) string {
		if p == "~" {
			return home
		}
		if strings.HasPrefix(p, "~/") {
			return filepath.Join(home, p[2:])
		}
		return p
	}
	c.Vault = tilde(c.Vault)
	for i := range c.Exclude {
		c.Exclude[i] = tilde(c.Exclude[i])
	}
	for i := range c.Domain {
		for j := range c.Domain[i].Paths {
			c.Domain[i].Paths[j] = tilde(c.Domain[i].Paths[j])
		}
	}
	return nil
}

func (c *Config) validate() error {
	if c.Vault == "" {
		return fmt.Errorf("vault 가 비어 있다")
	}
	seen := map[string]bool{}
	for _, d := range c.Domain {
		if d.Prefix == "" || d.Folder == "" {
			return fmt.Errorf("domain 에 prefix 또는 folder 가 없다: %+v", d)
		}
		if seen[d.Prefix] {
			return fmt.Errorf("domain prefix 가 중복이다: %s", d.Prefix)
		}
		seen[d.Prefix] = true
	}
	return nil
}

// under 는 child 가 parent 이거나 그 하위인지 본다. 문자열 접두 비교의 오탐을 막는다.
func under(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

// DomainForCwd 는 cwd 에 해당하는 도메인 접두어를 준다. 없으면 빈 문자열.
// 제외 경로가 우선한다 — 제외이면서 동시에 도메인일 수 없다.
func (c *Config) DomainForCwd(cwd string) string {
	if c.IsExcluded(cwd) {
		return ""
	}
	for _, d := range c.Domain {
		for _, p := range d.Paths {
			if under(p, cwd) {
				return d.Prefix
			}
		}
	}
	return ""
}

func (c *Config) IsExcluded(cwd string) bool {
	for _, e := range c.Exclude {
		if under(e, cwd) {
			return true
		}
	}
	return false
}

// FolderFor 는 접두어에 대응하는 볼트 폴더명을 준다.
func (c *Config) FolderFor(prefix string) (string, bool) {
	for _, d := range c.Domain {
		if d.Prefix == prefix {
			return d.Folder, true
		}
	}
	return "", false
}
