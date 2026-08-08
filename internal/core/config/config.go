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
	// Rollup 은 주간 요약 파일 이름이다 (예: `98-{project}-작업-로그-요약.md`).
	//
	// **선택 키다.** 없으면 `cb rollup` 이 무엇을 적으라고 알려 주고 멈춘다.
	// 코드에 기본값을 숨기지 않는 이유는 스펙 §5 다 — 개인 설정 리터럴을 0으로 둔다.
	// 여기 한국어 파일명을 박으면 영어권 사용자에게 조용히 이상한 파일이 생긴다.
	Rollup string `toml:"rollup"`
}

type Capture struct {
	Signals        []string `toml:"signals"`
	MinTurns       int      `toml:"min_turns"`
	QuiesceSeconds int      `toml:"quiesce_seconds"`

	// JudgePath 는 자동 승격에 쓸 판별기 명령이다. 비면 자동으로 찾는다
	// (~/.local/bin/claude → PATH 의 claude). 못 찾으면 자동 승격이 꺼진다.
	//
	// **API 키를 직접 읽지 않는다.** 호스트 CLI 만 쓴다 — 키 등록은 진짜 장벽이고,
	// 사용자가 모르는 사이에 과금되는 경로를 만들지 않기 위해서다.
	JudgePath string `toml:"judge_path"`
	// JudgeModel 은 판별에 쓸 모델이다. 비면 haiku.
	JudgeModel string `toml:"judge_model"`
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

// PathEnv 는 설정 파일 경로를 덮어쓰는 환경변수 이름이다.
const PathEnv = "CASEBOOK_CONFIG"

// ResolvePath 는 실제로 열 설정 파일 경로를 정한다.
// 우선순위는 인자(--config 플래그) > CASEBOOK_CONFIG > XDG 기본 경로다.
//
// 환경변수가 필요한 이유: casebook 은 CLI 로만 불리는 물건이 아니다. Claude Code
// 훅과 데몬 어댑터(Plan 3~4)는 실행 명령줄을 우리가 못 정하는 자리가 있어서
// --config 를 붙일 수 없다. 그 경로들이 기본 XDG 경로가 아닌 설정을 가리키려면
// 환경변수가 유일한 통로다. 스펙 §5 도 이 변수를 전제로 쓰여 있다.
func ResolvePath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	if p := os.Getenv(PathEnv); p != "" {
		return p, nil
	}
	return DefaultPath()
}

// Load 는 설정을 읽는다. path 가 비면 ResolvePath 가 정한 경로를 쓴다
// (CASEBOOK_CONFIG → XDG 기본 경로).
// CASEBOOK_VAULT 가 설정돼 있으면 vault 를 덮어쓴다 (테스트 볼트 격리용).
func Load(path string) (*Config, error) {
	path, err := ResolvePath(path)
	if err != nil {
		return nil, err
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
// 홈 디렉토리 조회는 지연시킨다 — ~ 를 실제로 만났을 때만 os.UserHomeDir 를
// 부른다. casebook 은 훅·데몬으로 뜨는 물건이라 $HOME 이 항상 보장되지
// 않는데(launchd·cron·컨테이너), 설정에 ~ 가 하나도 없으면 $HOME 이 없어도
// Load 가 성공해야 한다. ~ 를 만났는데 홈 디렉토리를 못 구하면 에러를
// 반환한다 — 조용히 넘어가면 ~ 가 문자 그대로 남아 filepath.Rel 비교에서
// 이상한 결과를 낸다.
func (c *Config) expand() error {
	var home string
	var homeErr error
	homeLoaded := false
	getHome := func() (string, error) {
		if !homeLoaded {
			home, homeErr = os.UserHomeDir()
			if homeErr != nil {
				homeErr = fmt.Errorf("홈 디렉토리를 확인할 수 없다: %w", homeErr)
			}
			homeLoaded = true
		}
		return home, homeErr
	}
	tilde := func(p string) (string, error) {
		if p == "~" {
			return getHome()
		}
		if strings.HasPrefix(p, "~/") {
			h, err := getHome()
			if err != nil {
				return "", err
			}
			return filepath.Join(h, p[2:]), nil
		}
		return p, nil
	}
	var err error
	if c.Vault, err = tilde(c.Vault); err != nil {
		return err
	}
	for i := range c.Exclude {
		if c.Exclude[i], err = tilde(c.Exclude[i]); err != nil {
			return err
		}
	}
	for i := range c.Domain {
		for j := range c.Domain[i].Paths {
			if c.Domain[i].Paths[j], err = tilde(c.Domain[i].Paths[j]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Config) validate() error {
	if c.Vault == "" {
		return fmt.Errorf("vault 가 비어 있다")
	}
	if err := c.validateNaming(); err != nil {
		return err
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

// validateNaming 은 [naming] 절을 검사한다.
//
// 이 절이 통째로 빠져도 예전에는 Load 가 통과했다. 그러면 decisions_dir 이 빈
// 문자열이라 결정 폴더가 볼트 루트 자체가 되고, index 도 빈 문자열이라 색인이
// 볼트 디렉터리를 덮어쓰려 든다 — 설정 오류가 config 층이 아니라 store·schema
// 층에서 "stem 이 규약에 맞지 않는다: \"vault\"" 같은 엉뚱한 메시지로 터진다.
// 설정의 함정은 설정을 읽는 자리에서 잡는다.
func (c *Config) validateNaming() error {
	n := c.Naming
	for _, f := range []struct{ key, val string }{
		{"decision_file", n.DecisionFile},
		{"decisions_dir", n.DecisionsDir},
		{"worklog", n.Worklog},
		{"index", n.Index},
	} {
		if strings.TrimSpace(f.val) == "" {
			return fmt.Errorf("[naming] 의 %s 항목이 비어 있다 — 설정에 [naming] 절이 통째로 빠졌는지 확인하라", f.key)
		}
	}
	for _, ph := range []string{domainPH, slugPH, datePH} {
		if !strings.Contains(n.DecisionFile, ph) {
			return fmt.Errorf("[naming] 의 decision_file 에 %s 자리표시자가 없다: %q", ph, n.DecisionFile)
		}
	}
	// 결정 표식은 {domain} 과 {slug} 사이에서 유도된다. 순서가 뒤집혔거나 둘이
	// 붙어 있으면 표식이 없어져 파일명이 결정 노트인지 판정할 수 없다.
	if c.DecisionMarker() == "" {
		return fmt.Errorf("[naming] 의 decision_file 은 %s 뒤에 %s 가 오고 그 사이에 결정 표식이 있어야 한다"+
			" (예: \"{domain}-결정-{slug}-{date}.md\"): %q", domainPH, slugPH, n.DecisionFile)
	}
	// schema 는 stem 이 "-{date}" 로 끝나기를 요구하고 store 는 ".md" 만 결정
	// 노트로 본다. 템플릿이 그 모양이 아니면 capture 가 만든 파일을 schema 가
	// 거부한다 — 그 어긋남도 여기서 잡는다.
	if !strings.HasSuffix(n.DecisionFile, "-"+datePH+".md") {
		return fmt.Errorf("[naming] 의 decision_file 은 \"-%s.md\" 로 끝나야 한다: %q", datePH, n.DecisionFile)
	}
	if !strings.Contains(n.DecisionsDir, "{project}") {
		return fmt.Errorf("[naming] 의 decisions_dir 에 {project} 가 없다: %q", n.DecisionsDir)
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

// domainPH·slugPH 는 decision_file 템플릿의 자리표시자다.
const (
	domainPH = "{domain}"
	slugPH   = "{slug}"
	datePH   = "{date}"
)

// DecisionMarker 는 결정 노트 파일명의 표식을 decision_file 템플릿에서 유도한다.
// {domain} 과 {slug} 사이의 문자열이 표식이다 — 기본 한국어 템플릿에서는 "-결정-",
// 영어 템플릿 "{domain}-decision-{slug}-{date}.md" 에서는 "-decision-" 이 된다.
//
// 이것이 표식의 유일한 정본이다. store 의 파일 필터·접두어 추출과 schema 의 stem
// 검증이 전부 이 값을 쓴다 — 어느 한 곳에 리터럴을 두면 템플릿을 바꿨을 때 그
// 한 곳만 어긋나 국제화가 조용히 깨진다.
//
// 유도할 수 없으면(자리표시자가 없거나 {slug} 가 {domain} 보다 앞이거나 둘이
// 붙어 있으면) 빈 문자열을 준다. validate() 가 Load 경로에서 이 경우를 막지만,
// 설정 구조체를 직접 만든 호출자를 위해 소비자 쪽도 빈 표식에 실패로 대응한다.
func (c *Config) DecisionMarker() string {
	t := c.Naming.DecisionFile
	i := strings.Index(t, domainPH)
	j := strings.Index(t, slugPH)
	if i < 0 || j <= i+len(domainPH) {
		return ""
	}
	return t[i+len(domainPH) : j]
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
