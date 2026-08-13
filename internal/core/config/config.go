// Package config 는 priorcase 의 유일한 설정 정본이다.
// 코드에 개인 설정 리터럴을 두지 않는다 — 볼트 경로·도메인 매핑·제외·키워드가 전부 여기서 온다.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/xian0310567/priorcase/internal/core/xdgpath"
)

type Naming struct {
	DecisionFile string `toml:"decision_file"`
	DecisionsDir string `toml:"decisions_dir"`
	Worklog      string `toml:"worklog"`
	Index        string `toml:"index"`
	// Rollup 은 주간 요약 파일 이름이다 (예: `98-{project}-작업-로그-요약.md`).
	//
	// **선택 키다.** 없으면 `prior rollup` 이 무엇을 적으라고 알려 주고 멈춘다.
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
	// Repos 는 이 도메인에 해당하는 git 저장소다 (`owner/repo`, 호스트 없이).
	//
	// **paths 만으로는 팀에서 못 쓴다.** 같은 저장소를 사람마다 다른 자리에
	// 체크아웃하므로, 새 팀원이 설정을 손으로 고쳐야 도메인이 잡힌다.
	// `owner/repo` 는 누구 기계에서든 같다.
	Repos []string `toml:"repos"`
	// Vault 는 이 도메인의 결정이 사는 볼트 이름이다. 비면 기본 볼트.
	//
	// **프로젝트가 볼트를 고른다.** cwd 로 프로젝트가 정해지면 그 프로젝트의
	// 볼트에 쓰고 그 볼트에서만 회수한다 — 볼트가 쓰기와 회수의 경계다.
	Vault string `toml:"vault"`
}

type Config struct {
	// Vaults 는 결정 노트가 사는 자리들이다.
	//
	// 설정에서는 두 모양을 다 받는다 (Load 의 2패스 디코딩):
	//
	//	vault = "~/Documents/Obsidian Vault"     # 하나뿐일 때
	//
	//	[[vault]]                                 # 여럿일 때
	//	name = "personal"
	//	path = "~/Documents/Obsidian Vault"
	//
	// TOML 은 같은 키를 문자열과 테이블 배열로 겸할 수 없어서, 파일을 먼저 훑어
	// 어느 모양인지 보고 그에 맞는 구조체로 읽는다. 옛 설정을 그대로 쓰게 하려면
	// 이 방법뿐이다 — 키 이름을 바꾸면 기존 사용자의 설정이 깨진다.
	Vaults  []Vault  `toml:"-"`
	Exclude []string `toml:"exclude"`
	// DefaultDomain 은 어느 [[domain]] 의 paths 에도 안 걸릴 때 쓸 도메인이다.
	//
	// **이게 없으면 새 사용자는 아무것도 기록하지 못한다.** 설정에 프로젝트를 하나도
	// 안 적은 상태가 정상 출발점인데, 그때 DomainForCwd 가 빈 문자열을 주면 기록 경로가
	// 통째로 막힌다 — 그런데 겉으로는 아무 에러도 안 난다.
	//
	// 옛 셸 구현에는 "그 외 → common" 폴백이 있었는데 이관하면서 빠졌다.
	DefaultDomain string `toml:"default_domain"`
	// Lang 은 **볼트에 남는 문자열**의 언어다 (색인 머리말, 회수 주입 라벨 등).
	// CLI 진단 출력은 해당하지 않는다 — 그건 사람이 터미널에서 한 번 보고 마는 것이고,
	// 볼트 산출물은 남아서 회수되고 다른 사람이 읽는다.
	//
	// 비면 한국어다. 기존 볼트가 전부 한국어라 그쪽이 안전한 기본값이다.
	//
	// **결정 노트의 본문 언어는 여기가 정하지 않는다.** 판별기가 대화의 언어를
	// 따라간다 — 한 볼트에 여러 언어의 대화가 섞일 수 있기 때문이다.
	Lang string `toml:"lang"`
	// Author 는 결정 노트에 박을 사람 이름이다. 비면 git 신원을 쓴다.
	//
	// 명시 키를 두는 이유: git 을 안 쓰는 볼트가 있고, git 신원과 팀에서 부르는
	// 이름이 다른 경우도 있다. 둘 다 없으면 author 는 안 쓰인다 — 혼자 쓰는
	// 볼트에서 그건 아무 손해가 아니다.
	Author  string   `toml:"author"`
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
	return filepath.Join(dir, "priorcase", "config.toml"), nil
}

// PathEnv 는 설정 파일 경로를 덮어쓰는 환경변수 이름이다.
const PathEnv = "PRIORCASE_CONFIG"

// ResolvePath 는 실제로 열 설정 파일 경로를 정한다.
// 우선순위는 인자(--config 플래그) > PRIORCASE_CONFIG > XDG 기본 경로다.
//
// 환경변수가 필요한 이유: priorcase 는 CLI 로만 불리는 물건이 아니다. Claude Code
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
// (PRIORCASE_CONFIG → XDG 기본 경로).
// PRIORCASE_VAULT 가 설정돼 있으면 vault 를 덮어쓴다 (테스트 볼트 격리용).
// vaultIsTable 은 설정의 vault 키가 [[vault]] 형태인지 본다.
//
// TOML 은 같은 키를 문자열과 테이블 배열로 겸할 수 없고, Go 구조체도 필드 하나에
// 두 타입을 받을 수 없다. 그래서 **엄격 검사 없이 한 번 가볍게 훑어** 모양을 정하고,
// 그에 맞는 구조체로 다시 읽는다.
func vaultIsTable(src []byte) (bool, error) {
	var raw map[string]any
	if err := toml.Unmarshal(src, &raw); err != nil {
		return false, err
	}
	_, ok := raw["vault"].([]any)
	return ok, nil
}

// tomlBase 는 vault 를 뺀 나머지 설정이다.
//
// **두 변형이 이 구조체를 공유해야 한다.** 각자 필드를 들면 한쪽에만 키를 더했을 때
// 다른 쪽에서 그 키가 조용히 무시되고, DisallowUnknownFields 도 못 잡는다
// (필드가 있는 변형으로 읽히면 통과하니까).
type tomlBase struct {
	Exclude       []string `toml:"exclude"`
	DefaultDomain string   `toml:"default_domain"`
	Lang          string   `toml:"lang"`
	Author        string   `toml:"author"`
	Naming        Naming   `toml:"naming"`
	Capture       Capture  `toml:"capture"`
	Domain        []Domain `toml:"domain"`
}

func (b tomlBase) into(c *Config) {
	c.Exclude, c.DefaultDomain, c.Lang, c.Author = b.Exclude, b.DefaultDomain, b.Lang, b.Author
	c.Naming, c.Capture, c.Domain = b.Naming, b.Capture, b.Domain
}

func Load(path string) (*Config, error) {
	path, err := ResolvePath(path)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("설정 파일을 열 수 없다 (%s): %w", path, err)
	}

	table, err := vaultIsTable(src)
	if err != nil {
		return nil, fmt.Errorf("설정 파싱 실패 (%s): %w", path, err)
	}

	var c Config
	decode := func(v any) error {
		dec := toml.NewDecoder(bytes.NewReader(src))
		dec.DisallowUnknownFields() // 오타 키를 조용히 넘기지 않는다
		if err := dec.Decode(v); err != nil {
			var se *toml.StrictMissingError
			if errors.As(err, &se) {
				return fmt.Errorf("설정에 알 수 없는 키가 있다 (%s):\n%s", path, se.String())
			}
			return fmt.Errorf("설정 파싱 실패 (%s): %w", path, err)
		}
		return nil
	}

	if table {
		var f struct {
			tomlBase
			Vault []Vault `toml:"vault"`
		}
		if err := decode(&f); err != nil {
			return nil, err
		}
		f.tomlBase.into(&c)
		c.Vaults = f.Vault
	} else {
		var f struct {
			tomlBase
			Vault string `toml:"vault"`
		}
		if err := decode(&f); err != nil {
			return nil, err
		}
		f.tomlBase.into(&c)
		// 레거시 단일 볼트. 이름이 없으므로 기본 이름을 붙인다 —
		// 도메인이 vault 를 안 적으면 어차피 이것으로 온다.
		if f.Vault != "" {
			c.Vaults = []Vault{{Name: DefaultVaultName, Path: f.Vault}}
		}
	}

	// **환경변수는 기본 볼트만 덮는다.** 여러 볼트를 환경변수 하나로 표현할 방법이
	// 없고, 이건 테스트와 특수 배치를 위한 문이다.
	if v := os.Getenv("PRIORCASE_VAULT"); v != "" {
		c.Vaults = []Vault{{Name: DefaultVaultName, Path: v}}
	}
	if err := c.expand(); err != nil {
		return nil, fmt.Errorf("경로 확장 실패 (%s): %w", path, err)
	}
	return &c, c.validate()
}

// expand 는 ~ 를 홈 디렉토리로 편다. 경로 비교 전에 반드시 수행한다.
// 홈 디렉토리 조회는 지연시킨다 — ~ 를 실제로 만났을 때만 os.UserHomeDir 를
// 부른다. priorcase 는 훅·데몬으로 뜨는 물건이라 $HOME 이 항상 보장되지
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
	for i := range c.Vaults {
		if c.Vaults[i].Path, err = tilde(c.Vaults[i].Path); err != nil {
			return err
		}
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
	if err := c.validateVaults(); err != nil {
		return err
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
//
// 판정 순서는 **경로 → 저장소 → 폴백**이다.
//
// 경로를 먼저 보는 이유가 둘이다. 하나, 이미 경로로 설정한 사람의 동작이 그대로
// 유지된다 — 저장소를 먼저 보면 조용히 달라진다. 둘, **모노레포**에서는 경로만이
// 하위 프로젝트를 가른다 (한 저장소 안의 `apps/a`·`apps/b`). 저장소를 먼저 보면
// 그 구분이 통째로 사라진다.
//
// 저장소는 그 다음이다. 새 팀원은 경로가 하나도 안 맞으므로 여기서 잡힌다 —
// 설정을 손대지 않아도 된다. 그것이 이 순서로 얻는 것이다.
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
	// 경로가 안 맞으면 저장소로 본다. 파일을 읽으므로 경로 판정이 실패한 뒤에만 한다.
	if repo := RepoFor(cwd); repo != "" {
		if p := c.DomainForRepo(repo); p != "" {
			return p
		}
	}
	// 어디에도 안 걸리면 폴백이다. 없으면 빈 문자열 — 그건 기록이 막힌다는 뜻이고
	// prior doctor 가 그 상태를 알린다.
	return c.DefaultDomain
}

// DomainForRepo 는 `owner/repo` 에 해당하는 도메인 접두어를 준다. 없으면 빈 문자열.
//
// 설정에 적힌 값도 정규화해서 비교한다 — 사람이 전체 URL 을 붙여 넣거나
// 대소문자를 섞어 적어도 걸리게 한다. 설정 오타로 조용히 안 걸리는 것이
// 이 기능에서 가장 알아채기 어려운 실패다.
func (c *Config) DomainForRepo(repo string) string {
	want := NormalizeRemote(repo)
	if want == "" {
		want = strings.ToLower(strings.TrimSpace(repo))
	}
	if want == "" {
		return ""
	}
	for _, d := range c.Domain {
		for _, r := range d.Repos {
			got := NormalizeRemote(r)
			if got == "" {
				got = strings.ToLower(strings.TrimSpace(r))
			}
			if got != "" && got == want {
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

// AuthorFor 는 dir 에서 결정 노트에 박을 사람 이름을 정한다. 없으면 빈 문자열.
//
// 순서는 **설정 → git 신원**이다. 설정을 먼저 보는 이유: git 신원은 자동으로
// 잡히는 편의값이고, 사람이 굳이 적었다면 그쪽이 의도다.
func (c *Config) AuthorFor(dir string) string {
	if a := strings.TrimSpace(c.Author); a != "" {
		return a
	}
	return GitUser(dir)
}
