# casebook Plan 1 — 코어 + CLI 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 볼트에 대고 `capture` / `recall` / `review` / `index` 가 동작하는 `cb` CLI 를 만든다.

**Architecture:** 코어(`internal/core/*`)가 유일한 API 이고 CLI 어댑터가 그것만 부른다. 코어는 어댑터를 모른다. YAML 은 파싱에만 쓰고 frontmatter 방출은 10키를 리터럴 순서로 찍는 함수 하나로 통일한다 — 방출기 이원화를 코드로 불가능하게 만드는 것이 이 계획의 핵심 목표다.

**Tech Stack:** Go · cobra(CLI) · pelletier/go-toml/v2(설정) · go.yaml.in/yaml/v3(파싱) · golang.org/x/text(NFC) · gofrs/flock(Plan 3 용, 여기선 go.mod 에만)

**참조 스펙:** `docs/design/2026-08-07-casebook-cli-design.md`

## Global Constraints

- 모듈 경로: `github.com/xian0310567/casebook`
- **`GOTOOLCHAIN=auto` 를 저장소에 고정한다.** 개발 머신은 Homebrew Go 1.23.3 + `GOTOOLCHAIN=local` 이라 맨몸 `go mod tidy` 가 실패한다(최신 `x/text` 가 go>=1.25 요구). Task 0 에서 처리한다.
- 의존성 버전 고정: cobra v1.10.2 · go-toml/v2 v2.4.3 · go.yaml.in/yaml/v3 v3.0.5 · x/text v0.28.0 · flock v0.12.1 · fsnotify v1.10.1
- **`gopkg.in/yaml.v3` 를 쓰지 않는다** — 2025-04 아카이브됨. `go.yaml.in/yaml/v3` 를 쓴다.
- frontmatter 10키 순서(불변): `type` `date` `domain` `summary` `status` `outcome` `supersedes` `related` `tags` `source_session`
- **정본 배열 형식은 `[a, b, c]`** (콤마 뒤 공백 1). 실볼트에 두 형식이 섞여 있으므로 하나를 정한다.
- 파일명에서 온 문자열은 **프로세스 경계를 넘는 즉시 `norm.NFC.String()`** 을 통과시킨다. macOS APFS 는 NFD 를 보존해서 돌려주고 Linux ext4 는 바이트 정확 매칭이다.
- 문자열 절단은 항상 **rune 기준**. `s[:n]` 바이트 절단 금지.
- 테스트 볼트는 `t.TempDir()` 로 격리한다. 실볼트를 건드리는 테스트를 쓰지 않는다.
- `testdata/` 에 **실볼트 사본을 넣지 않는다** — 개인 내용이 공개된다. 합성 픽스처만.
- 커밋 메시지는 한국어, 본문에 "무엇을 왜"를 적는다.

---

## File Structure

| 파일 | 책임 |
|---|---|
| `go.mod`, `go.sum` | 모듈 정의, 의존성 핀 |
| `cmd/cb/main.go` | 진입점. cobra 루트에 위임만 |
| `internal/core/xdgpath/xdgpath.go` | `~/.config`, `~/.local/state` 해석 (XDG 문자 그대로) |
| `internal/core/config/config.go` | TOML 로드, 검증, cwd→domain 해석, exclude 판정 |
| `internal/core/store/frontmatter.go` | frontmatter 파싱 + **정본 방출(단일 함수)** |
| `internal/core/store/text.go` | NFC 정규화, rune 절단, slug 생성 |
| `internal/core/store/paths.go` | 파일명 조립, stem→경로 해석, 경로 순회 방어 |
| `internal/core/store/vault.go` | 볼트 스캔 — 결정 노트 열거·읽기·쓰기 |
| `internal/core/schema/schema.go` | 결정 노트 검증 (접두어=domain 첫값, 허용값, 날짜) |
| `internal/core/index/index.go` | 색인 표 생성 |
| `internal/core/search/keywords.go` | 키워드 추출 (조사 절단·불용어) |
| `internal/core/search/search.go` | 점수 계산, 정렬, 절단 |
| `internal/core/capture/capture.go` | 결정 생성 |
| `internal/core/capture/review.go` | outcome·회고·supersedes 갱신 |
| `internal/adapter/cli/root.go` | cobra 루트 + 공통 초기화 |
| `internal/adapter/cli/{index,recall,capture,review}.go` | 서브커맨드 |
| `testdata/vault/` | 합성 픽스처 |
| `.goreleaser.yaml`, `.github/workflows/` | 릴리스·CI |

---

## Task 0: 프로젝트 뼈대와 툴체인

**Files:**
- Create: `go.mod`, `cmd/cb/main.go`, `internal/adapter/cli/root.go`, `.github/workflows/ci.yml`, `Makefile`

**Interfaces:**
- Produces: `cli.Execute() error` — 모든 서브커맨드가 붙을 cobra 루트

- [ ] **Step 1: 툴체인 고정과 모듈 초기화**

```bash
cd ~/project/casebook
go env -w GOTOOLCHAIN=auto
go mod init github.com/xian0310567/casebook
go get github.com/spf13/cobra@v1.10.2
go get github.com/pelletier/go-toml/v2@v2.4.3
go get go.yaml.in/yaml/v3@v3.0.5
go get golang.org/x/text@v0.28.0
go get github.com/gofrs/flock@v0.12.1
go get github.com/fsnotify/fsnotify@v1.10.1
```

`go env -w` 는 사용자 전역 설정이므로, 저장소에도 명시하기 위해 Makefile 에 `export GOTOOLCHAIN=auto` 를 넣는다(Step 3).

- [ ] **Step 2: 루트 커맨드와 진입점**

`internal/adapter/cli/root.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version 은 릴리스 시 -ldflags 로 주입된다.
var Version = "dev"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cb",
		Short:         "casebook — 결정을 기록하고 회수한다",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}
	root.PersistentFlags().String("config", "", "설정 파일 경로 (기본: $XDG_CONFIG_HOME/casebook/config.toml)")
	return root
}

// Execute 는 CLI 를 실행한다. 에러는 호출자가 종료 코드로 옮긴다.
func Execute() error {
	if err := newRootCmd().Execute(); err != nil {
		return fmt.Errorf("cb: %w", err)
	}
	return nil
}
```

`cmd/cb/main.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/xian0310567/casebook/internal/adapter/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Makefile 과 CI**

`Makefile`:

```makefile
export GOTOOLCHAIN := auto

.PHONY: build test lint
build:
	go build -trimpath -ldflags="-s -w" -o cb ./cmd/cb
test:
	go test ./...
lint:
	go vet ./...
```

`.github/workflows/ci.yml`:

```yaml
name: ci
on:
  push:
    branches: [main]
  pull_request:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version: '1.25'
      - run: go vet ./...
      - run: go test -race ./...
```

- [ ] **Step 4: 빌드와 실행 확인**

```bash
make build && ./cb --version
```

기대: `cb version dev`

- [ ] **Step 5: 커밋**

```bash
git add -A
git commit -m "feat: 프로젝트 뼈대 — cobra 루트, 툴체인 고정, CI

Homebrew Go 1.23.3 은 GOTOOLCHAIN=local 로 배포돼 최신 x/text 를 못 가져온다.
Makefile 에서 auto 로 고정한다."
```

---

## Task 1: XDG 경로

**Files:**
- Create: `internal/core/xdgpath/xdgpath.go`, `internal/core/xdgpath/xdgpath_test.go`

**Interfaces:**
- Produces: `xdgpath.ConfigHome() (string, error)`, `xdgpath.StateHome() (string, error)` — 각각 `~/.config`, `~/.local/state` 또는 환경변수 값

`adrg/xdg` 와 `os.UserConfigDir()` 은 macOS 에서 `~/Library/Application Support` 를 반환한다. casebook 은 셸 훅 시절과 같은 자리를 요구하므로 직접 구현한다.

- [ ] **Step 1: 실패하는 테스트를 쓴다**

`internal/core/xdgpath/xdgpath_test.go`:

```go
package xdgpath

import (
	"path/filepath"
	"testing"
)

func TestConfigHome(t *testing.T) {
	home := "/home/tester"
	tests := []struct {
		name string
		env  string
		want string
	}{
		{"미설정이면 ~/.config", "", filepath.Join(home, ".config")},
		{"절대경로면 그대로", "/custom/cfg", "/custom/cfg"},
		{"상대경로는 무시하고 기본값", "relative/cfg", filepath.Join(home, ".config")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", tt.env)
			t.Setenv("HOME", home)
			got, err := ConfigHome()
			if err != nil {
				t.Fatalf("ConfigHome() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ConfigHome() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: 실패 확인**

```bash
go test ./internal/core/xdgpath/ -run TestConfigHome
```

기대: `undefined: ConfigHome` 컴파일 실패

- [ ] **Step 3: 구현**

`internal/core/xdgpath/xdgpath.go`:

```go
// Package xdgpath 는 XDG Base Directory 를 명세 문자 그대로 해석한다.
// os.UserConfigDir 은 macOS 에서 ~/Library/Application Support 를 주므로 쓰지 않는다.
package xdgpath

import (
	"os"
	"path/filepath"
)

func ConfigHome() (string, error) { return resolve("XDG_CONFIG_HOME", ".config") }
func StateHome() (string, error)  { return resolve("XDG_STATE_HOME", ".local", "state") }

// resolve 는 환경변수가 절대경로일 때만 채택한다 (XDG 명세).
func resolve(env string, fallback ...string) (string, error) {
	if v := os.Getenv(env); filepath.IsAbs(v) {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{home}, fallback...)...), nil
}
```

- [ ] **Step 4: 통과 확인**

```bash
go test ./internal/core/xdgpath/ -v
```

기대: 3개 서브테스트 모두 PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/core/xdgpath
git commit -m "feat: XDG 경로 해석

os.UserConfigDir 은 macOS 에서 ~/Library/Application Support 를 반환한다.
casebook 은 셸 훅 시절과 같은 ~/.config 를 요구하므로 직접 구현한다."
```

---

## Task 2: 설정 로더

**Files:**
- Create: `internal/core/config/config.go`, `internal/core/config/config_test.go`, `internal/core/config/testdata/valid.toml`
- Modify: `internal/adapter/cli/root.go`

**Interfaces:**
- Consumes: `xdgpath.ConfigHome()`
- Produces:
  - `type Config struct { Vault string; Exclude []string; Naming Naming; Capture Capture; Domain []Domain }`
  - `type Domain struct { Prefix, Folder string; Paths []string }`
  - `config.Load(path string) (*Config, error)` — path 가 빈 문자열이면 XDG 기본 경로
  - `(*Config) DomainForCwd(cwd string) string` — 접두어 또는 빈 문자열
  - `(*Config) IsExcluded(cwd string) bool`

- [ ] **Step 1: 실패하는 테스트를 쓴다**

`internal/core/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `
vault = "/tmp/vault"
exclude = ["/home/t/project/NOI"]

[naming]
decision_file = "{domain}-결정-{slug}-{date}.md"
decisions_dir = "{project}/decisions"
worklog = "99-{project}-작업-로그.md"
index = "_meta/00-결정-색인.md"

[capture]
signals = ["결정", "선택"]
min_turns = 6
quiesce_seconds = 3

[[domain]]
prefix = "omni"
folder = "omni"
paths = ["/home/t/project/omni"]

[[domain]]
prefix = "occ"
folder = "OCC"
paths = ["/home/t/Documents/automation-dropshipping"]
`

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad(t *testing.T) {
	c, err := Load(write(t, sample))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.Vault != "/tmp/vault" {
		t.Errorf("Vault = %q", c.Vault)
	}
	// exclude 가 top-level 로 붙었는지 — [[domain]] 뒤에 두면 조용히 domain 필드가 된다
	if len(c.Exclude) != 1 || c.Exclude[0] != "/home/t/project/NOI" {
		t.Errorf("Exclude = %v, want top-level 1건", c.Exclude)
	}
	if len(c.Domain) != 2 || c.Domain[1].Folder != "OCC" {
		t.Errorf("Domain = %+v", c.Domain)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	_, err := Load(write(t, sample+"\ntypo_key = 1\n"))
	if err == nil {
		t.Fatal("오타 키를 통과시켰다 — strict 모드가 꺼져 있다")
	}
}

func TestDomainForCwd(t *testing.T) {
	c, err := Load(write(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ cwd, want string }{
		{"/home/t/project/omni", "omni"},
		{"/home/t/project/omni/src/deep", "omni"},
		{"/home/t/Documents/automation-dropshipping", "occ"},
		{"/home/t/unmapped", ""},
		{"/home/t/project/omni-other", ""}, // 접두 문자열 오탐 방지
	}
	for _, tt := range tests {
		if got := c.DomainForCwd(tt.cwd); got != tt.want {
			t.Errorf("DomainForCwd(%q) = %q, want %q", tt.cwd, got, tt.want)
		}
	}
}

func TestIsExcluded(t *testing.T) {
	c, err := Load(write(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsExcluded("/home/t/project/NOI/docs") {
		t.Error("NOI 하위가 제외되지 않았다")
	}
	if c.IsExcluded("/home/t/project/omni") {
		t.Error("omni 가 제외됐다")
	}
}
```

- [ ] **Step 2: 실패 확인**

```bash
go test ./internal/core/config/
```

기대: `undefined: Load` 컴파일 실패

- [ ] **Step 3: 구현**

`internal/core/config/config.go`:

```go
// Package config 는 casebook 의 유일한 설정 정본이다.
// 코드에 개인 설정 리터럴을 두지 않는다 — 볼트 경로·도메인 매핑·제외·키워드가 전부 여기서 온다.
package config

import (
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
		if ok := asStrict(err, &se); ok {
			return nil, fmt.Errorf("설정에 알 수 없는 키가 있다 (%s):\n%s", path, se.String())
		}
		return nil, fmt.Errorf("설정 파싱 실패 (%s): %w", path, err)
	}

	if v := os.Getenv("CASEBOOK_VAULT"); v != "" {
		c.Vault = v
	}
	c.expand()
	return &c, c.validate()
}

func asStrict(err error, target **toml.StrictMissingError) bool {
	se, ok := err.(*toml.StrictMissingError)
	if ok {
		*target = se
	}
	return ok
}

// expand 는 ~ 를 홈 디렉토리로 편다. 경로 비교 전에 반드시 수행한다.
func (c *Config) expand() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
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
```

- [ ] **Step 4: 통과 확인**

```bash
go test ./internal/core/config/ -v
```

기대: `TestLoad`, `TestLoadRejectsUnknownKey`, `TestDomainForCwd`(5개 케이스), `TestIsExcluded` 전부 PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/core/config
git commit -m "feat: 설정 로더 — strict TOML, 도메인 매핑, 제외 판정

exclude 를 [[domain]] 뒤에 두면 TOML 규칙상 마지막 domain 의 필드가 되는데
파서 에러가 안 난다. DisallowUnknownFields 로 이 조용한 실패를 잡는다.
경로 비교는 filepath.Rel 로 한다 — 문자열 접두 비교는 omni-other 를 오탐한다."
```

---

## Task 3: 텍스트 정규화 — NFC, rune 절단, slug

**Files:**
- Create: `internal/core/store/text.go`, `internal/core/store/text_test.go`

**Interfaces:**
- Produces:
  - `store.NFC(s string) string`
  - `store.TruncateRunes(s string, n int) string`
  - `store.Slugify(s string) string` — 파일명에 쓸 수 있게 다듬고 80 rune 으로 자른다

감사에서 확인된 것: `cut -c1-80` 은 GNU coreutils 에서 바이트 기준이라 리눅스에서 한글 slug 를 문자 중간에서 잘라 깨진 UTF-8 파일명을 만든다.

- [ ] **Step 1: 실패하는 테스트를 쓴다**

`internal/core/store/text_test.go`:

```go
package store

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateRunesKeepsValidUTF8(t *testing.T) {
	s := strings.Repeat("한글결정", 50) // 200 rune / 600 byte
	for n := 0; n <= 210; n++ {
		got := TruncateRunes(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("n=%d 에서 깨진 UTF-8", n)
		}
		want := n
		if want > 200 {
			want = 200
		}
		if c := utf8.RuneCountInString(got); c != want {
			t.Fatalf("n=%d: rune 수 %d, want %d", n, c, want)
		}
	}
}

func TestNFC(t *testing.T) {
	nfd := "한"  // ᄒ + ᅡ + ᆫ (분리)
	nfc := "한"              // 한 (완성)
	if got := NFC(nfd); got != nfc {
		t.Errorf("NFC(NFD) = %q, want %q", got, nfc)
	}
	if got := NFC(nfc); got != nfc {
		t.Errorf("NFC 는 멱등이어야 한다")
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"저장엔진 OPFS", "저장엔진-OPFS"},
		{"a/b:c*d?e", "a-b-c-d-e"},
		{"  앞뒤 공백  ", "앞뒤-공백"},
		{"--앞뒤 하이픈--", "앞뒤-하이픈"},
		{"연속   공백", "연속-공백"},
	}
	for _, tt := range tests {
		if got := Slugify(tt.in); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	long := Slugify(strings.Repeat("한", 140))
	if utf8.RuneCountInString(long) != 80 {
		t.Errorf("긴 slug 가 80 rune 으로 안 잘렸다: %d", utf8.RuneCountInString(long))
	}
	if !utf8.ValidString(long) {
		t.Error("긴 slug 가 깨진 UTF-8")
	}
}
```

- [ ] **Step 2: 실패 확인**

```bash
go test ./internal/core/store/ -run 'TestTruncateRunes|TestNFC|TestSlugify'
```

기대: `undefined: TruncateRunes` 컴파일 실패

- [ ] **Step 3: 구현**

`internal/core/store/text.go`:

```go
package store

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// slugMaxRunes 는 파일명 slug 의 최대 길이다. 바이트가 아니라 rune 이다 —
// 바이트로 자르면 한글 문자 중간에서 잘려 깨진 UTF-8 파일명이 생긴다.
const slugMaxRunes = 80

// NFC 는 문자열을 NFC 로 정규화한다.
// macOS APFS 는 NFD 이름을 그대로 보존해 돌려주고 Linux ext4 는 바이트 정확 매칭이므로,
// 프로세스 경계를 넘어온 파일명(ReadDir·argv·tar·훅 JSON)은 반드시 통과시킨다.
func NFC(s string) string {
	if norm.NFC.IsNormalString(s) {
		return s // 흔한 경우에 할당을 피한다
	}
	return norm.NFC.String(s)
}

// TruncateRunes 는 앞에서부터 n 개 rune 만 남긴다.
// range 로 문자열을 돌면 인덱스가 항상 rune 경계 바이트 오프셋이다.
func TruncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// slugBad 는 파일명에 쓸 수 없는 문자다. 허용목록이 아니라 거부목록으로 간다 —
// 허용목록은 한글·이모지·확장 문자를 매번 빠뜨린다.
const slugBad = `/\:*?"<>|`

// Slugify 는 임의 문자열을 파일명 조각으로 바꾼다.
func Slugify(s string) string {
	s = NFC(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case strings.ContainsRune(slugBad, r), r == ' ', r == '\t', r == '\n':
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	out = strings.Trim(out, "-.")
	return TruncateRunes(out, slugMaxRunes)
}
```

- [ ] **Step 4: 통과 확인**

```bash
go test ./internal/core/store/ -run 'TestTruncateRunes|TestNFC|TestSlugify' -v
```

기대: 전부 PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/core/store/text.go internal/core/store/text_test.go
git commit -m "feat: NFC 정규화·rune 절단·slug 생성

cut -c1-80 은 GNU coreutils 에서 바이트 기준이라 리눅스에서 한글 slug 를
문자 중간에서 잘라 깨진 파일명을 만든다. rune 경계로 자른다.
APFS 는 NFD 를 보존해 돌려주므로 디렉토리 리스팅 비교 전에 정규화한다."
```

---

## Task 4: frontmatter 파싱과 정본 방출

**Files:**
- Create: `internal/core/store/frontmatter.go`, `internal/core/store/frontmatter_test.go`

**Interfaces:**
- Produces:
  - `type Meta struct { Type, Date string; Domain []string; Summary, Status, Outcome, Supersedes string; Related, Tags []string; SourceSession string }`
  - `store.ParseFrontmatter(data []byte) (Meta, []byte, error)` — meta 와 본문
  - `store.EmitFrontmatter(m Meta) []byte` — **유일한 방출기**
  - `store.EmitNote(m Meta, body []byte) []byte`

**이 태스크가 계획의 핵심이다.** 방출 순서가 함수 본문의 리터럴이 되면 "방출기 이원화"가 구조적으로 불가능해진다.

- [ ] **Step 1: 실패하는 테스트를 쓴다**

`internal/core/store/frontmatter_test.go`:

```go
package store

import (
	"bytes"
	"strings"
	"testing"
)

const noteA = `---
type: decision
date: 2026-08-01
domain: [omni]
summary: "저장 엔진은 OPFS + SQLite 로 간다"
status: active
outcome: pending
supersedes: ""
related: ["[[omni-결정-데이터모델-2026-08-01]]"]
tags: [decision,omni,저장엔진]
source_session: ""
---

## 결정

OPFS 위의 SQLite 를 쓴다.
`

func TestParseFrontmatter(t *testing.T) {
	m, body, err := ParseFrontmatter([]byte(noteA))
	if err != nil {
		t.Fatalf("ParseFrontmatter() error = %v", err)
	}
	if m.Type != "decision" || m.Date != "2026-08-01" {
		t.Errorf("type/date = %q/%q", m.Type, m.Date)
	}
	if len(m.Domain) != 1 || m.Domain[0] != "omni" {
		t.Errorf("domain = %v", m.Domain)
	}
	if m.Summary != "저장 엔진은 OPFS + SQLite 로 간다" {
		t.Errorf("summary = %q", m.Summary)
	}
	if len(m.Tags) != 3 || m.Tags[2] != "저장엔진" {
		t.Errorf("tags = %v — 공백 없는 콤마 형식을 못 읽었다", m.Tags)
	}
	if !bytes.Contains(body, []byte("OPFS 위의 SQLite")) {
		t.Errorf("body 가 잘못 잘렸다: %q", body)
	}
}

func TestParseRejectsMissingFrontmatter(t *testing.T) {
	if _, _, err := ParseFrontmatter([]byte("# 그냥 마크다운\n")); err == nil {
		t.Fatal("frontmatter 없는 문서를 통과시켰다")
	}
}

// 정본형 멱등성: 기존 볼트와의 바이트 동일은 불가능하지만(볼트에 두 형식이 섞여 있다),
// 정본형으로 한 번 방출한 뒤로는 반드시 안정적이어야 한다.
func TestEmitIsIdempotent(t *testing.T) {
	m1, body1, err := ParseFrontmatter([]byte(noteA))
	if err != nil {
		t.Fatal(err)
	}
	once := EmitNote(m1, body1)

	m2, body2, err := ParseFrontmatter(once)
	if err != nil {
		t.Fatalf("자기 출력을 다시 못 읽는다: %v", err)
	}
	twice := EmitNote(m2, body2)

	if !bytes.Equal(once, twice) {
		t.Errorf("멱등하지 않다\n--- 1회 ---\n%s\n--- 2회 ---\n%s", once, twice)
	}
}

func TestEmitKeyOrderAndFormat(t *testing.T) {
	m := Meta{
		Type: "decision", Date: "2026-08-07", Domain: []string{"casebook"},
		Summary: `따옴표 " 와 백슬래시 \ 가 든 요약`, Status: "active", Outcome: "pending",
		Related: []string{"[[a]]", "[[b]]"}, Tags: []string{"decision", "go"},
	}
	got := string(EmitFrontmatter(m))

	wantOrder := []string{"type:", "date:", "domain:", "summary:", "status:",
		"outcome:", "supersedes:", "related:", "tags:", "source_session:"}
	pos := -1
	for _, k := range wantOrder {
		i := strings.Index(got, "\n"+k)
		if i < 0 {
			t.Fatalf("키 %q 가 없다:\n%s", k, got)
		}
		if i <= pos {
			t.Fatalf("키 순서가 틀렸다 (%q):\n%s", k, got)
		}
		pos = i
	}
	if !strings.Contains(got, "tags: [decision, go]") {
		t.Errorf("정본 배열 형식(콤마+공백)이 아니다:\n%s", got)
	}
	if !strings.Contains(got, `supersedes: ""`) {
		t.Errorf("빈 supersedes 가 \"\" 로 안 나왔다:\n%s", got)
	}
	// 이스케이프는 yaml 에 맡긴다 — 손으로 짜면 반드시 틀린다
	rt, _, err := ParseFrontmatter(append(EmitFrontmatter(m), []byte("\n본문\n")...))
	if err != nil {
		t.Fatal(err)
	}
	if rt.Summary != m.Summary {
		t.Errorf("이스케이프 왕복 실패: %q != %q", rt.Summary, m.Summary)
	}
}

func TestEmitEmptyRelated(t *testing.T) {
	got := string(EmitFrontmatter(Meta{Type: "decision"}))
	if !strings.Contains(got, "related: []") {
		t.Errorf("빈 배열이 [] 로 안 나왔다:\n%s", got)
	}
}
```

- [ ] **Step 2: 실패 확인**

```bash
go test ./internal/core/store/ -run 'TestParse|TestEmit'
```

기대: `undefined: ParseFrontmatter` 컴파일 실패

- [ ] **Step 3: 구현**

`internal/core/store/frontmatter.go`:

```go
package store

import (
	"bytes"
	"fmt"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// Meta 는 결정 노트의 frontmatter 10키다. 순서는 EmitFrontmatter 가 정한다.
type Meta struct {
	Type          string   `yaml:"type"`
	Date          string   `yaml:"date"`
	Domain        []string `yaml:"domain"`
	Summary       string   `yaml:"summary"`
	Status        string   `yaml:"status"`
	Outcome       string   `yaml:"outcome"`
	Supersedes    string   `yaml:"supersedes"`
	Related       []string `yaml:"related"`
	Tags          []string `yaml:"tags"`
	SourceSession string   `yaml:"source_session"`
}

var fence = []byte("---")

// ParseFrontmatter 는 --- 로 감싼 YAML 블록과 그 뒤 본문을 나눈다.
func ParseFrontmatter(data []byte) (Meta, []byte, error) {
	var m Meta
	if !bytes.HasPrefix(data, fence) {
		return m, nil, fmt.Errorf("frontmatter 가 없다 (--- 로 시작하지 않는다)")
	}
	rest := data[len(fence):]
	if i := bytes.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	} else {
		return m, nil, fmt.Errorf("frontmatter 가 닫히지 않았다")
	}
	end := bytes.Index(rest, append([]byte("\n"), fence...))
	if end < 0 {
		return m, nil, fmt.Errorf("frontmatter 가 닫히지 않았다")
	}
	head := rest[:end+1]
	body := rest[end+1+len(fence):]
	if len(body) > 0 && body[0] == '\n' {
		body = body[1:]
	}

	dec := yaml.NewDecoder(bytes.NewReader(head))
	dec.KnownFields(true) // 10키 외의 잉여 키를 조용히 버리지 않는다
	if err := dec.Decode(&m); err != nil {
		return m, nil, fmt.Errorf("frontmatter 파싱 실패: %w", err)
	}
	return m, body, nil
}

// quote 는 문자열을 YAML 큰따옴표 스칼라로 만든다.
// 손으로 이스케이프하지 않는다 — yaml 에 맡긴다.
func quote(s string) string {
	n := yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: s}
	out, err := yaml.Marshal(&n)
	if err != nil {
		panic(fmt.Sprintf("casebook: YAML 스칼라 마샬 실패: %v", err))
	}
	q := strings.TrimRight(string(out), "\n")
	if strings.ContainsAny(q, "\n") {
		// emitter 가 긴 스칼라를 접으면 frontmatter 가 깨진다. 조용히 깨지느니 죽는다.
		panic("casebook: YAML 스칼라가 여러 줄로 접혔다 — 방출기를 고쳐야 한다")
	}
	return q
}

// bare 는 따옴표 없이 인라인 배열에 넣는다.
func bare(items []string) string  { return "[" + strings.Join(items, ", ") + "]" }
func quoted(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = quote(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// EmitFrontmatter 는 casebook 의 유일한 frontmatter 방출기다.
// 키 순서가 이 함수 본문의 리터럴이므로 방출기가 둘이 될 수 없다.
func EmitFrontmatter(m Meta) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("type: " + m.Type + "\n")
	b.WriteString("date: " + m.Date + "\n")
	b.WriteString("domain: " + bare(m.Domain) + "\n")
	b.WriteString("summary: " + quote(m.Summary) + "\n")
	b.WriteString("status: " + m.Status + "\n")
	b.WriteString("outcome: " + m.Outcome + "\n")
	b.WriteString("supersedes: " + quote(m.Supersedes) + "\n")
	b.WriteString("related: " + quoted(m.Related) + "\n")
	b.WriteString("tags: " + bare(m.Tags) + "\n")
	b.WriteString("source_session: " + quote(m.SourceSession) + "\n")
	b.WriteString("---\n")
	return []byte(b.String())
}

// EmitNote 는 frontmatter 와 본문을 합친다. 사이에 빈 줄 하나.
func EmitNote(m Meta, body []byte) []byte {
	out := EmitFrontmatter(m)
	out = append(out, '\n')
	return append(out, body...)
}
```

- [ ] **Step 4: 통과 확인**

```bash
go test ./internal/core/store/ -v
```

기대: `TestParseFrontmatter`, `TestParseRejectsMissingFrontmatter`, `TestEmitIsIdempotent`, `TestEmitKeyOrderAndFormat`, `TestEmitEmptyRelated` 전부 PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/core/store/frontmatter.go internal/core/store/frontmatter_test.go
git commit -m "feat: frontmatter 파싱과 단일 방출기

10키 순서를 EmitFrontmatter 본문의 리터럴로 박는다. 방출기가 둘이 될 수 없게
하는 것이 목적이다 — 현행 셸은 sb_rewrite_decision 과 scribe 두 벌이 있어
볼트에 [a,b,c] 39건과 [a, b, c] 7건이 섞여 있다.

바이트 동일 라운드트립은 기존 볼트 대상으로 불가능하므로(볼트가 비정본)
정본형 멱등성으로 테스트한다. 이스케이프는 yaml.Node 에 맡긴다."
```

---

## Task 5: 파일명 규약과 경로 방어

**Files:**
- Create: `internal/core/store/paths.go`, `internal/core/store/paths_test.go`

**Interfaces:**
- Consumes: `config.Config`, `store.Slugify`, `store.NFC`
- Produces:
  - `type Layout struct { ... }`; `store.NewLayout(c *config.Config) *Layout`
  - `(*Layout) DecisionPath(prefix, slug, date string) (string, error)` — 절대 경로
  - `(*Layout) ResolveStem(stem string) (string, error)` — stem→절대 경로. 순회 거부
  - `(*Layout) PrefixOf(stem string) string`

감사에서 `../CLAUDE` 같은 값이 볼트의 지침 문서에 도달 가능함이 확인됐다. stem 은 LLM 이 만든 문자열이다.

- [ ] **Step 1: 실패하는 테스트를 쓴다**

`internal/core/store/paths_test.go`:

```go
package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/core/config"
)

func testLayout(t *testing.T) (*Layout, string) {
	t.Helper()
	vault := t.TempDir()
	c := &config.Config{
		Vault: vault,
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Index:        "_meta/00-결정-색인.md",
		},
		Domain: []config.Domain{
			{Prefix: "omni", Folder: "omni"},
			{Prefix: "common", Folder: "common"},
		},
	}
	return NewLayout(c), vault
}

func TestDecisionPath(t *testing.T) {
	l, vault := testLayout(t)
	got, err := l.DecisionPath("omni", "저장엔진 OPFS", "2026-08-07")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(vault, "omni", "decisions", "omni-결정-저장엔진-OPFS-2026-08-07.md")
	if got != want {
		t.Errorf("DecisionPath() = %q,\nwant %q", got, want)
	}
}

func TestDecisionPathRejectsUnknownPrefix(t *testing.T) {
	l, _ := testLayout(t)
	if _, err := l.DecisionPath("없는도메인", "x", "2026-08-07"); err == nil {
		t.Fatal("알 수 없는 접두어를 통과시켰다")
	}
}

func TestResolveStemRejectsTraversal(t *testing.T) {
	l, _ := testLayout(t)
	bad := []string{
		"../CLAUDE",
		"omni-결정-a-2026-08-01/../../../CLAUDE",
		"/etc/passwd",
		"..",
		"",
		"규약에-맞지-않는-이름",         // -결정- 이 없다
		"없는도메인-결정-x-2026-08-01", // 알 수 없는 접두어
	}
	for _, s := range bad {
		if p, err := l.ResolveStem(s); err == nil {
			t.Errorf("ResolveStem(%q) 가 통과했다 → %q", s, p)
		}
	}
}

func TestResolveStemNFD(t *testing.T) {
	l, vault := testLayout(t)
	// NFD 로 들어온 stem 도 NFC 경로로 해석돼야 한다 (tar 복원 시나리오)
	nfdStem := "omni-결정-한-2026-08-01"
	// 위 stem 은 "omni-결정-한-2026-08-01" 의 NFD 형태
	got, err := l.ResolveStem(nfdStem)
	if err != nil {
		t.Fatalf("NFD stem 을 거부했다: %v", err)
	}
	want := filepath.Join(vault, "omni", "decisions", "omni-결정-한-2026-08-01.md")
	if got != want {
		t.Errorf("ResolveStem(NFD) = %q,\nwant %q", got, want)
	}
	if strings.Contains(got, "ᄀ") {
		t.Error("경로에 NFD 자모가 남아 있다")
	}
}

func TestPrefixOf(t *testing.T) {
	l, _ := testLayout(t)
	if got := l.PrefixOf("omni-결정-저장엔진-2026-08-01"); got != "omni" {
		t.Errorf("PrefixOf() = %q", got)
	}
	if got := l.PrefixOf("규약없음"); got != "" {
		t.Errorf("PrefixOf(비규약) = %q, want 빈 문자열", got)
	}
}
```

- [ ] **Step 2: 실패 확인**

```bash
go test ./internal/core/store/ -run 'TestDecisionPath|TestResolveStem|TestPrefixOf'
```

기대: `undefined: NewLayout` 컴파일 실패

- [ ] **Step 3: 구현**

`internal/core/store/paths.go`:

```go
package store

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xian0310567/casebook/internal/core/config"
)

// decisionMarker 는 파일명이 결정 노트임을 나타내는 표식이다.
// 설정의 decision_file 템플릿에서 유도한다.
const decisionMarker = "-결정-"

type Layout struct{ c *config.Config }

func NewLayout(c *config.Config) *Layout { return &Layout{c: c} }

// decisionsDir 는 접두어에 대응하는 결정 폴더의 절대 경로다.
func (l *Layout) decisionsDir(prefix string) (string, error) {
	folder, ok := l.c.FolderFor(prefix)
	if !ok {
		return "", fmt.Errorf("알 수 없는 도메인 접두어: %q", prefix)
	}
	rel := strings.ReplaceAll(l.c.Naming.DecisionsDir, "{project}", folder)
	return filepath.Join(l.c.Vault, rel), nil
}

// DecisionPath 는 새 결정 노트가 놓일 절대 경로를 만든다.
func (l *Layout) DecisionPath(prefix, slug, date string) (string, error) {
	dir, err := l.decisionsDir(prefix)
	if err != nil {
		return "", err
	}
	name := l.c.Naming.DecisionFile
	name = strings.ReplaceAll(name, "{domain}", prefix)
	name = strings.ReplaceAll(name, "{slug}", Slugify(slug))
	name = strings.ReplaceAll(name, "{date}", date)
	return filepath.Join(dir, NFC(name)), nil
}

// PrefixOf 는 stem 에서 도메인 접두어를 뽑는다. 규약에 안 맞으면 빈 문자열.
func (l *Layout) PrefixOf(stem string) string {
	stem = NFC(stem)
	i := strings.Index(stem, decisionMarker)
	if i <= 0 {
		return ""
	}
	prefix := stem[:i]
	if _, ok := l.c.FolderFor(prefix); !ok {
		return ""
	}
	return prefix
}

// ResolveStem 은 위키링크 stem 을 절대 경로로 바꾼다.
//
// ⚠️ 여기로 오는 문자열은 LLM 이나 외부 입력에서 온다. 검사 없이 이어붙이면
// ../CLAUDE 같은 값이 볼트의 지침 문서를 가리킨다 — 감사에서 도달 가능함이 확인됐다.
// 그래서 (1) 모양 검사 (2) 접두어 화이트리스트 (3) 최종 경로가 볼트 안인지, 셋을 다 본다.
func (l *Layout) ResolveStem(stem string) (string, error) {
	stem = NFC(stem)
	if stem == "" || strings.ContainsAny(stem, `/\`) || strings.Contains(stem, "..") {
		return "", fmt.Errorf("허용되지 않는 stem: %q", stem)
	}
	prefix := l.PrefixOf(stem)
	if prefix == "" {
		return "", fmt.Errorf("규약에 맞지 않는 stem: %q", stem)
	}
	dir, err := l.decisionsDir(prefix)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, stem+".md")
	rel, err := filepath.Rel(l.c.Vault, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("볼트 밖을 가리킨다: %q", stem)
	}
	return p, nil
}

// IndexPath 는 색인 파일의 절대 경로다.
func (l *Layout) IndexPath() string {
	return filepath.Join(l.c.Vault, l.c.Naming.Index)
}

// DecisionDirs 는 실제로 존재하는 결정 폴더를 전부 준다.
func (l *Layout) DecisionDirs() []string {
	var out []string
	for _, d := range l.c.Domain {
		if p, err := l.decisionsDir(d.Prefix); err == nil {
			out = append(out, p)
		}
	}
	return out
}
```

- [ ] **Step 4: 통과 확인**

```bash
go test ./internal/core/store/ -v
```

기대: 순회 거부 7케이스 포함 전부 PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/core/store/paths.go internal/core/store/paths_test.go
git commit -m "feat: 파일명 규약과 경로 순회 방어

stem 은 LLM 이 만든 문자열이다. 검사 없이 이어붙이면 ../CLAUDE 가 볼트
지침 문서를 덮어쓴다 — 2026-08-04 감사에서 도달 가능함이 확인됐다.
모양 검사 + 접두어 화이트리스트 + 볼트 경계 확인 셋을 모두 건다."
```

---

## Task 6: 볼트 스캔과 스키마 검증

**Files:**
- Create: `internal/core/store/vault.go`, `internal/core/store/vault_test.go`, `internal/core/schema/schema.go`, `internal/core/schema/schema_test.go`
- Create: `testdata/vault/` (합성 픽스처)

**Interfaces:**
- Consumes: `Layout`, `ParseFrontmatter`, `EmitNote`
- Produces:
  - `type Note struct { Path, Stem string; Meta Meta; Body []byte }`
  - `(*Layout) List() ([]Note, error)` — 모든 결정 노트, 경로 오름차순
  - `(*Layout) Read(path string) (Note, error)`
  - `(*Layout) Write(n Note) error`
  - `schema.Validate(stem string, m Meta) error`

- [ ] **Step 1: 합성 픽스처를 만든다**

**실볼트 사본을 넣지 않는다** — 개인 내용이 공개된다. 구조만 재현하고 엣지 케이스를 심는다.

```bash
mkdir -p testdata/vault/{alpha,beta,common}/decisions testdata/vault/_meta

cat > testdata/vault/alpha/decisions/alpha-결정-저장엔진-2026-08-01.md <<'EOF'
---
type: decision
date: 2026-08-01
domain: [alpha]
summary: "저장 엔진을 임베디드 DB 로 고른다"
status: active
outcome: pending
supersedes: ""
related: []
tags: [decision,alpha,저장엔진]
source_session: ""
---

## 결정

임베디드 DB 를 쓴다.
EOF

cat > testdata/vault/alpha/decisions/alpha-결정-스키마-2026-08-02.md <<'EOF'
---
type: decision
date: 2026-08-02
domain: [alpha]
summary: "스키마를 단일 테이블로 유지한다"
status: superseded
outcome: pending
supersedes: ""
related: ["[[alpha-결정-저장엔진-2026-08-01]]"]
tags: [decision, alpha, 스키마]
source_session: ""
---

## 결정

단일 테이블.
EOF

cat > testdata/vault/beta/decisions/beta-결정-배포전략-2026-08-03.md <<'EOF'
---
type: decision
date: 2026-08-03
domain: [beta]
summary: "배포는 정적 바이너리 한 개로 한다"
status: active
outcome: good
supersedes: ""
related: []
tags: [decision, beta, 배포]
source_session: ""
---

## 결정

정적 바이너리.
EOF

cat > testdata/vault/common/decisions/common-결정-로케일함정-2026-08-04.md <<'EOF'
---
type: decision
date: 2026-08-04
domain: [common]
summary: "로케일 의존 도구는 바이트 기준으로 고정한다"
status: active
outcome: bad
supersedes: ""
related: []
tags: [decision, common, lesson, 로케일]
source_session: ""
---

## 결정

바이트 기준 고정.
EOF
```

배열 형식 두 가지(`[a,b,c]` / `[a, b, c]`), `related: []`, `status: superseded`, `outcome: good`/`bad` 가 모두 들어 있다.

- [ ] **Step 2: 실패하는 테스트를 쓴다**

`internal/core/schema/schema_test.go`:

```go
package schema

import "testing"

import "github.com/xian0310567/casebook/internal/core/store"

func base() store.Meta {
	return store.Meta{
		Type: "decision", Date: "2026-08-07", Domain: []string{"alpha"},
		Summary: "요약", Status: "active", Outcome: "pending",
	}
}

func TestValidateAccepts(t *testing.T) {
	if err := Validate("alpha-결정-x-2026-08-07", base()); err != nil {
		t.Fatalf("정상 노트를 거부했다: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	tests := []struct {
		name string
		stem string
		mut  func(*store.Meta)
	}{
		{"접두어와 domain 첫값 불일치", "beta-결정-x-2026-08-07", func(m *store.Meta) {}},
		{"domain 이 비었다", "alpha-결정-x-2026-08-07", func(m *store.Meta) { m.Domain = nil }},
		{"summary 가 비었다", "alpha-결정-x-2026-08-07", func(m *store.Meta) { m.Summary = "" }},
		{"status 허용값 밖", "alpha-결정-x-2026-08-07", func(m *store.Meta) { m.Status = "unknown" }},
		{"outcome 허용값 밖", "alpha-결정-x-2026-08-07", func(m *store.Meta) { m.Outcome = "maybe" }},
		{"date 형식 오류", "alpha-결정-x-2026-08-07", func(m *store.Meta) { m.Date = "2026/08/07" }},
		{"type 이 decision 이 아니다", "alpha-결정-x-2026-08-07", func(m *store.Meta) { m.Type = "note" }},
		{"파일명 날짜와 date 불일치", "alpha-결정-x-2026-08-01", func(m *store.Meta) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := base()
			tt.mut(&m)
			if err := Validate(tt.stem, m); err == nil {
				t.Errorf("통과했다: stem=%q meta=%+v", tt.stem, m)
			}
		})
	}
}
```

- [ ] **Step 3: 실패 확인**

```bash
go test ./internal/core/schema/
```

기대: `undefined: Validate` 컴파일 실패

- [ ] **Step 4: schema 구현**

`internal/core/schema/schema.go`:

```go
// Package schema 는 결정 노트의 불변식을 강제한다.
// 쓰기 경로가 하나뿐이므로 여기를 통과하지 않은 노트는 볼트에 들어갈 수 없다.
package schema

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/xian0310567/casebook/internal/core/store"
)

var (
	dateRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	statuses = map[string]bool{"active": true, "superseded": true, "regretted": true}
	outcomes = map[string]bool{"pending": true, "good": true, "bad": true}
)

// Validate 는 stem 과 meta 가 서로 정합한지 본다.
func Validate(stem string, m store.Meta) error {
	if m.Type != "decision" {
		return fmt.Errorf("type 은 decision 이어야 한다: %q", m.Type)
	}
	if !dateRe.MatchString(m.Date) {
		return fmt.Errorf("date 는 YYYY-MM-DD 여야 한다: %q", m.Date)
	}
	if len(m.Domain) == 0 {
		return fmt.Errorf("domain 이 비었다")
	}
	if strings.TrimSpace(m.Summary) == "" {
		return fmt.Errorf("summary 가 비었다 — 회수 시 이것만 주입되므로 필수다")
	}
	if !statuses[m.Status] {
		return fmt.Errorf("status 가 허용값(active/superseded/regretted) 밖이다: %q", m.Status)
	}
	if !outcomes[m.Outcome] {
		return fmt.Errorf("outcome 이 허용값(pending/good/bad) 밖이다: %q", m.Outcome)
	}
	stem = store.NFC(stem)
	i := strings.Index(stem, "-결정-")
	if i <= 0 {
		return fmt.Errorf("stem 이 규약에 맞지 않는다: %q", stem)
	}
	if prefix := stem[:i]; prefix != m.Domain[0] {
		return fmt.Errorf("파일명 접두어(%q)와 domain 첫 값(%q)이 다르다", prefix, m.Domain[0])
	}
	if !strings.HasSuffix(stem, "-"+m.Date) {
		return fmt.Errorf("파일명 날짜와 date(%q)가 다르다: %q", m.Date, stem)
	}
	return nil
}
```

- [ ] **Step 5: vault 구현**

`internal/core/store/vault.go`:

```go
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Note 는 디스크의 결정 노트 하나다.
type Note struct {
	Path string
	Stem string
	Meta Meta
	Body []byte
}

// List 는 모든 결정 폴더의 노트를 경로 오름차순으로 준다.
// 읽기 실패한 파일은 건너뛰고 계속한다 — 한 건 때문에 전체가 죽으면 안 된다.
func (l *Layout) List() ([]Note, error) {
	var notes []Note
	for _, dir := range l.DecisionDirs() {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("결정 폴더를 읽을 수 없다 (%s): %w", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			// ReadDir 결과는 macOS 에서 NFD 일 수 있다. 즉시 정규화한다.
			name := NFC(e.Name())
			if !strings.HasSuffix(name, ".md") || !strings.Contains(name, decisionMarker) {
				continue
			}
			n, err := l.Read(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			n.Stem = strings.TrimSuffix(name, ".md")
			notes = append(notes, n)
		}
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Path < notes[j].Path })
	return notes, nil
}

func (l *Layout) Read(path string) (Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Note{}, err
	}
	m, body, err := ParseFrontmatter(data)
	if err != nil {
		return Note{}, fmt.Errorf("%s: %w", path, err)
	}
	stem := strings.TrimSuffix(NFC(filepath.Base(path)), ".md")
	return Note{Path: path, Stem: stem, Meta: m, Body: body}, nil
}

// Write 는 노트를 정본 형식으로 쓴다. 부모 디렉토리를 만든다.
func (l *Layout) Write(n Note) error {
	if err := os.MkdirAll(filepath.Dir(n.Path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(n.Path, EmitNote(n.Meta, n.Body), 0o644)
}
```

- [ ] **Step 6: vault 테스트**

`internal/core/store/vault_test.go`:

```go
package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xian0310567/casebook/internal/core/config"
)

// fixtureLayout 은 testdata/vault 를 임시 디렉토리로 복사해 Layout 을 만든다.
func fixtureLayout(t *testing.T) *Layout {
	t.Helper()
	dst := t.TempDir()
	src := filepath.Join("..", "..", "..", "testdata", "vault")
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	return NewLayout(&config.Config{
		Vault: dst,
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Index:        "_meta/00-결정-색인.md",
		},
		Domain: []config.Domain{
			{Prefix: "alpha", Folder: "alpha"},
			{Prefix: "beta", Folder: "beta"},
			{Prefix: "common", Folder: "common"},
		},
	})
}

func TestList(t *testing.T) {
	l := fixtureLayout(t)
	notes, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 4 {
		t.Fatalf("노트 %d건, want 4", len(notes))
	}
	// 두 배열 형식을 모두 읽는지
	for _, n := range notes {
		if len(n.Meta.Tags) == 0 {
			t.Errorf("%s: tags 를 못 읽었다", n.Stem)
		}
	}
}

func TestWriteThenRead(t *testing.T) {
	l := fixtureLayout(t)
	notes, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	n := notes[0]
	if err := l.Write(n); err != nil {
		t.Fatal(err)
	}
	again, err := l.Read(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Meta.Summary != n.Meta.Summary {
		t.Errorf("왕복 후 summary 가 변했다")
	}
	// 정본형으로 재기록된 뒤에는 바이트 동일이어야 한다
	before, _ := os.ReadFile(n.Path)
	if err := l.Write(again); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(n.Path)
	if string(before) != string(after) {
		t.Errorf("정본형 재기록이 멱등하지 않다")
	}
}
```

- [ ] **Step 7: 통과 확인**

```bash
go test ./internal/core/... -v
```

기대: schema 8개 거부 케이스 + vault 2개 전부 PASS

- [ ] **Step 8: 커밋**

```bash
git add internal/core/schema internal/core/store/vault.go internal/core/store/vault_test.go testdata
git commit -m "feat: 볼트 스캔과 스키마 검증

쓰기 경로가 하나뿐이므로 schema.Validate 를 통과하지 않은 노트는 볼트에
들어갈 수 없다. 현행 셸은 LLM 경로에만 검증이 있고 수동 경로에는 없다.

testdata 는 실볼트 사본이 아니라 합성 픽스처다 — 실볼트에는 개인 내용이
있어 공개 저장소에 넣을 수 없다. 두 배열 형식·superseded·good/bad 를 심었다."
```

---

## Task 7: 색인 생성 (`cb index`)

**Files:**
- Create: `internal/core/index/index.go`, `internal/core/index/index_test.go`, `internal/adapter/cli/index.go`
- Modify: `internal/adapter/cli/root.go`

**Interfaces:**
- Consumes: `(*Layout).List()`, `(*Layout).IndexPath()`
- Produces: `index.Build(l *store.Layout) ([]byte, error)`, `index.Write(l *store.Layout) (int, error)`

- [ ] **Step 1: 실패하는 테스트를 쓴다**

`internal/core/index/index_test.go`:

```go
package index

import (
	"strings"
	"testing"
)

func TestBuild(t *testing.T) {
	l := fixtureLayout(t) // store 패키지의 헬퍼를 index 용으로 복제 (아래 Step 3)
	out, err := Build(l)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "| 날짜 | domain | summary | status | outcome | 링크 |") {
		t.Errorf("헤더가 없다:\n%s", s)
	}
	rows := strings.Count(s, "\n| 2026-")
	if rows != 4 {
		t.Errorf("행 %d개, want 4:\n%s", rows, s)
	}
	// 최신순 정렬
	iNew := strings.Index(s, "2026-08-04")
	iOld := strings.Index(s, "2026-08-01")
	if iNew < 0 || iOld < 0 || iNew > iOld {
		t.Errorf("최신순 정렬이 아니다:\n%s", s)
	}
	// 파이프 이스케이프
	if strings.Contains(s, "임베디드 DB | 로") {
		t.Error("summary 의 파이프가 표를 깨뜨렸다")
	}
}
```

- [ ] **Step 2: 실패 확인**

```bash
go test ./internal/core/index/
```

기대: `undefined: Build` 컴파일 실패

- [ ] **Step 3: 구현**

`internal/core/index/index.go`:

```go
// Package index 는 전 프로젝트 결정을 한 표로 만든다.
package index

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xian0310567/casebook/internal/core/store"
)

const header = "| 날짜 | domain | summary | status | outcome | 링크 |\n" +
	"| --- | --- | --- | --- | --- | --- |\n"

// escapeCell 은 표 셀 안에서 파이프가 열을 쪼개지 않게 한다.
func escapeCell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}

// Build 는 색인 문서 전체를 만든다. 최신 날짜가 위로 온다.
func Build(l *store.Layout) ([]byte, error) {
	notes, err := l.List()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(notes, func(i, j int) bool {
		if notes[i].Meta.Date != notes[j].Meta.Date {
			return notes[i].Meta.Date > notes[j].Meta.Date
		}
		return notes[i].Stem < notes[j].Stem
	})

	var b strings.Builder
	b.WriteString("---\ntitle: 결정 색인\ntags: [index, decision]\n---\n\n")
	b.WriteString("# 결정 색인\n\n> 자동 생성된다. 직접 편집하지 마라 — `cb index` 가 덮어쓴다.\n\n")
	b.WriteString(header)
	for _, n := range notes {
		domain := "-"
		if len(n.Meta.Domain) > 0 {
			domain = n.Meta.Domain[0]
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s | %s | [[%s]] |\n",
			n.Meta.Date, domain, escapeCell(n.Meta.Summary),
			n.Meta.Status, n.Meta.Outcome, n.Stem)
	}
	return []byte(b.String()), nil
}

// Write 는 색인을 디스크에 쓰고 행 수를 준다.
func Write(l *store.Layout) (int, error) {
	out, err := Build(l)
	if err != nil {
		return 0, err
	}
	p := l.IndexPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return 0, err
	}
	if err := os.WriteFile(p, out, 0o644); err != nil {
		return 0, err
	}
	return strings.Count(string(out), "\n| 2"), nil
}
```

`internal/core/index/fixture_test.go` — `store` 의 헬퍼를 여기서도 쓰기 위해 복제한다:

```go
package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
)

func fixtureLayout(t *testing.T) *store.Layout {
	t.Helper()
	dst := t.TempDir()
	src := filepath.Join("..", "..", "..", "testdata", "vault")
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	return store.NewLayout(&config.Config{
		Vault: dst,
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Index:        "_meta/00-결정-색인.md",
		},
		Domain: []config.Domain{
			{Prefix: "alpha", Folder: "alpha"},
			{Prefix: "beta", Folder: "beta"},
			{Prefix: "common", Folder: "common"},
		},
	})
}
```

- [ ] **Step 4: CLI 배선**

`internal/adapter/cli/index.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/index"
)

func newIndexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "index",
		Short: "결정 색인을 재생성한다",
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := layoutFrom(cmd)
			if err != nil {
				return err
			}
			n, err := index.Write(l)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "색인 %d행 생성\n", n)
			return nil
		},
	}
}
```

`internal/adapter/cli/root.go` 에 공통 헬퍼와 등록을 추가한다:

```go
// layoutFrom 은 --config 플래그로 설정을 읽어 Layout 을 만든다.
func layoutFrom(cmd *cobra.Command) (*store.Layout, error) {
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return nil, err
	}
	c, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	return store.NewLayout(c), nil
}
```

`newRootCmd()` 의 `return root` 앞에 추가:

```go
	root.AddCommand(newIndexCmd())
```

import 에 `"github.com/xian0310567/casebook/internal/core/config"` 와 `"github.com/xian0310567/casebook/internal/core/store"` 를 넣는다.

- [ ] **Step 5: 통과 확인**

```bash
go test ./internal/core/index/ -v && make build
```

기대: `TestBuild` PASS, 빌드 성공

- [ ] **Step 6: 커밋**

```bash
git add internal/core/index internal/adapter/cli
git commit -m "feat: 결정 색인 생성과 cb index

summary 안의 파이프를 이스케이프한다 — 안 하면 표의 열이 밀린다.
정렬은 날짜 내림차순, 동일 날짜는 stem 오름차순으로 안정 정렬한다."
```

---

## Task 8: 키워드 추출

**Files:**
- Create: `internal/core/search/keywords.go`, `internal/core/search/keywords_test.go`, `internal/core/search/stopwords.go`

**Interfaces:**
- Produces: `search.ExtractKeywords(prompt string) []string`

현행 셸 동작을 그대로 옮긴다. 특히 **길이 필터가 바이트 기준**이라는 점이 중요하다 — macOS awk 의 `length()` 가 바이트를 세서 한글 1글자(3바이트)는 통과하고 ASCII 1글자는 탈락하는 비대칭이 현재 동작이다. `utf8.RuneCountInString` 으로 바꾸면 회수 결과가 달라진다.

- [ ] **Step 1: 실패하는 테스트를 쓴다**

`internal/core/search/keywords_test.go`:

```go
package search

import (
	"reflect"
	"testing"
)

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"조사 절단", "테스트이나 저장엔진을 골랐다", []string{"골랐다", "저장엔진", "테스트"}},
		{"구두점 분리", "a/b, c.d", []string{}}, // 전부 1바이트라 길이 필터 탈락
		{"하이픈 분리", "저장-엔진", []string{"엔진", "저장"}},
		{"길이 필터는 바이트 기준", "셸 a", []string{"셸"}}, // 셸=3바이트 통과, a=1바이트 탈락
		{"중복 제거와 정렬", "엔진 엔진 저장", []string{"엔진", "저장"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractKeywords(tt.in)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractKeywords(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestStopwordsRemoveExactLines(t *testing.T) {
	// "결정" 은 불용어다 — 없으면 모든 결정 문서와 매칭된다
	for _, k := range ExtractKeywords("결정 저장엔진") {
		if k == "결정" {
			t.Fatal("불용어 '결정' 이 남았다")
		}
	}
	// 부분 일치가 아니라 정확히 한 줄 전체 일치만 제거한다
	found := false
	for _, k := range ExtractKeywords("결정사항 검토") {
		if k == "결정사항" {
			found = true
		}
	}
	if !found {
		t.Fatal("'결정사항' 이 부분 일치로 제거됐다 — 정확 일치만 제거해야 한다")
	}
}
```

- [ ] **Step 2: 실패 확인**

```bash
go test ./internal/core/search/
```

기대: `undefined: ExtractKeywords` 컴파일 실패

- [ ] **Step 3: 구현**

`internal/core/search/stopwords.go`:

```go
package search

// stopwords 는 회수 키워드에서 제거할 단어다. 정확히 한 줄 전체가 일치할 때만 제거한다.
// "결정" 이 반드시 들어 있어야 한다 — 없으면 모든 결정 문서가 매칭된다.
var stopwords = map[string]bool{
	"결정": true, "그리고": true, "그래서": true, "하지만": true, "그런데": true,
	"이것": true, "저것": true, "그것": true, "여기": true, "거기": true,
	"어떻게": true, "무엇": true, "언제": true, "어디": true, "누가": true,
	"하다": true, "한다": true, "했다": true, "있다": true, "없다": true,
	"같다": true, "된다": true, "됐다": true, "보다": true, "주다": true,
	"확인": true, "진행": true, "작업": true, "파일": true, "내용": true,
	"때문": true, "위해": true, "대해": true, "관련": true, "정도": true,
	"경우": true, "부분": true, "방법": true, "문제": true, "상태": true,
	"the": true, "and": true, "for": true, "with": true, "this": true,
	"that": true, "from": true, "have": true, "not": true, "you": true,
}
```

`internal/core/search/keywords.go`:

```go
// Package search 는 결정 회수를 담당한다.
// 점수 함수는 나중에 임베딩으로 교체할 수 있게 이 패키지 안에 격리한다.
package search

import (
	"regexp"
	"sort"
	"strings"

	"github.com/xian0310567/casebook/internal/core/store"
)

// punct 는 토큰 분리자다. 하이픈·em/en dash 도 분리자로 친다.
var punct = regexp.MustCompile(`[\[\](){}<>"'` + "`" + `,.;:!?/\\|=+*&^%$#@~\-—–\s]+`)

// josa 는 한국어 조사다. 끝에 붙은 것만 자른다.
var josa = regexp.MustCompile(`(을|를|이|가|은|는|에서|에게|부터|까지|으로|로서|로써|로|와|과|의|도|만|이나|나|라도|처럼|보다|한테|께서|이라|라고|든지|하고|해서)$`)

// minTokenBytes 는 토큰 최소 길이다. **바이트** 기준이다 —
// 현행 셸의 awk length() 가 바이트를 세므로, rune 으로 바꾸면 회수 결과가 달라진다.
// 한글 1글자(3바이트)는 통과하고 ASCII 1글자는 탈락하는 비대칭이 의도된 동작이다.
const minTokenBytes = 2

// ExtractKeywords 는 프롬프트에서 검색 키워드를 뽑는다.
// 결과는 바이트 순 정렬 + 중복 제거된다.
func ExtractKeywords(prompt string) []string {
	seen := map[string]bool{}
	var out []string
	for _, tok := range punct.Split(store.NFC(prompt), -1) {
		tok = josa.ReplaceAllString(tok, "")
		if len(tok) < minTokenBytes {
			continue
		}
		tok = strings.ToLower(tok)
		if stopwords[tok] || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	sort.Strings(out) // LC_ALL=C sort -u 와 동일한 바이트 순
	return out
}
```

- [ ] **Step 4: 통과 확인**

```bash
go test ./internal/core/search/ -v
```

기대: 5개 서브테스트 + 불용어 테스트 PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/core/search
git commit -m "feat: 회수 키워드 추출

길이 필터를 바이트 기준으로 유지한다. 현행 셸의 awk length() 가 바이트를
세서 한글 1글자는 통과하고 ASCII 1글자는 탈락하는 비대칭이 현재 동작이고,
rune 으로 바꾸면 회수 결과가 달라진다. 동작 보존이 우선이다."
```

---

## Task 9: 회수 검색 (`cb recall`)

**Files:**
- Create: `internal/core/search/search.go`, `internal/core/search/search_test.go`, `internal/adapter/cli/recall.go`
- Modify: `internal/adapter/cli/root.go`

**Interfaces:**
- Consumes: `ExtractKeywords`, `(*Layout).List()`
- Produces:
  - `type Hit struct { Note store.Note; Score int }`
  - `type Options struct { Cwd string; CrossProject bool; Limit int; MinScore int }`
  - `search.Recall(l *store.Layout, c *config.Config, prompt string, o Options) []Hit`
  - `search.RenderInject(l *store.Layout, hits []Hit) string`

**★ 이 태스크에 열린 결정이 하나 있다.** 현행 셸은 cwd 도메인 폴더에 결정이 1건이라도 있으면 **절대 전체로 넓히지 않는다**. 주석은 "교차 프로젝트 회상이 존재 이유"라고 쓰여 있는데 코드는 반대다. 실측: `cwd=~/project/casebook` 에서 *"macOS 셸 한글 로케일 지뢰"* → `no-match`, 같은 프롬프트를 `cwd=/tmp` 로 주면 common 문서가 1위. 계획은 **`Options.CrossProject` 플래그로 양쪽을 다 테스트로 고정**하고, 기본값을 켜는 것을 권한다.

- [ ] **Step 1: 실패하는 테스트를 쓴다**

`internal/core/search/search_test.go`:

```go
package search

import (
	"strings"
	"testing"
)

func TestRecallScoring(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	hits := Recall(l, c, "저장 엔진을 무엇으로 골랐지", Options{Limit: 3, MinScore: 1})
	if len(hits) == 0 {
		t.Fatal("매칭이 없다")
	}
	if !strings.Contains(hits[0].Note.Stem, "저장엔진") {
		t.Errorf("1위가 저장엔진이 아니다: %s (score=%d)", hits[0].Note.Stem, hits[0].Score)
	}
}

func TestRecallNoMatchReturnsEmpty(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	if hits := Recall(l, c, "완전히 무관한 주제 짜장면", Options{Limit: 3, MinScore: 1}); len(hits) != 0 {
		t.Errorf("무관한 프롬프트에 %d건 매칭: %+v", len(hits), hits)
	}
}

func TestSupersededPenalty(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	hits := Recall(l, c, "스키마 단일 테이블", Options{Limit: 5, MinScore: 1})
	for _, h := range hits {
		if h.Note.Meta.Status == "superseded" {
			// 감점이 적용됐는지: 같은 매칭 수라면 active 보다 낮아야 한다
			t.Logf("superseded %s score=%d", h.Note.Stem, h.Score)
		}
	}
}

// ★ 교차 프로젝트 회상 — 현행 셸의 결함을 테스트로 고정한다
func TestCrossProjectRecall(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	// cwd 가 alpha 인데 찾는 건 common 문서다
	prompt := "로케일 의존 도구 바이트"
	cwd := "/tmp/proj/alpha"

	off := Recall(l, c, prompt, Options{Cwd: cwd, CrossProject: false, Limit: 3, MinScore: 1})
	on := Recall(l, c, prompt, Options{Cwd: cwd, CrossProject: true, Limit: 3, MinScore: 1})

	if len(on) == 0 {
		t.Fatal("CrossProject=true 인데 common 문서를 못 찾았다")
	}
	if len(off) >= len(on) {
		t.Errorf("CrossProject=false 가 더 많이 찾았다 — 필터가 동작하지 않는다 (off=%d on=%d)",
			len(off), len(on))
	}
}

func TestRenderInject(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	hits := Recall(l, c, "로케일 의존 도구 바이트", Options{CrossProject: true, Limit: 3, MinScore: 1})
	out := RenderInject(l, hits)
	if !strings.HasPrefix(out, "[과거 결정 참조]\n") {
		t.Errorf("헤더가 없다:\n%s", out)
	}
	if !strings.Contains(out, "(active/bad)") {
		t.Errorf("status/outcome 표기가 없다:\n%s", out)
	}
	// outcome: bad 가 있으면 경고 줄이 붙는다
	if !strings.Contains(out, "아쉬운 결과로 기록된 건이 있음") {
		t.Errorf("bad outcome 경고가 없다:\n%s", out)
	}
}

func TestRenderInjectEmpty(t *testing.T) {
	if out := RenderInject(nil, nil); out != "" {
		t.Errorf("매칭 없을 때 출력이 있다: %q", out)
	}
}
```

`internal/core/search/fixture_test.go`:

```go
package search

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
)

func fixtureLayoutConfig(t *testing.T) (*store.Layout, *config.Config) {
	t.Helper()
	dst := t.TempDir()
	src := filepath.Join("..", "..", "..", "testdata", "vault")
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	c := &config.Config{
		Vault: dst,
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Index:        "_meta/00-결정-색인.md",
		},
		Domain: []config.Domain{
			{Prefix: "alpha", Folder: "alpha", Paths: []string{"/tmp/proj/alpha"}},
			{Prefix: "beta", Folder: "beta", Paths: []string{"/tmp/proj/beta"}},
			{Prefix: "common", Folder: "common"},
		},
	}
	return store.NewLayout(c), c
}
```

- [ ] **Step 2: 실패 확인**

```bash
go test ./internal/core/search/ -run TestRecall
```

기대: `undefined: Recall` 컴파일 실패

- [ ] **Step 3: 구현**

`internal/core/search/search.go`:

```go
package search

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
)

// 점수 가중치 — 현행 셸 sb_search 에서 이식했다.
const (
	weightCwdDomain  = 4  // cwd 도메인이 노트 domain 에 있다
	weightMention    = 6  // 프롬프트가 도메인 접두어를 직접 언급했다 (도메인마다 누적)
	weightHead       = 3  // stem+summary+tags+domain 에서 키워드 히트
	weightBody       = 1  // 본문에서 키워드 히트
	penaltySuperseded = 5 // 뒤집힌 결정
)

type Hit struct {
	Note  store.Note
	Score int
}

type Options struct {
	Cwd string
	// CrossProject 가 false 면 cwd 도메인 폴더 결과가 완전히 빌 때만 전체로 넓힌다
	// (현행 셸 동작). true 면 항상 전체를 본다.
	CrossProject bool
	Limit        int
	MinScore     int
}

// Recall 은 프롬프트에 관련된 결정을 점수순으로 준다.
func Recall(l *store.Layout, c *config.Config, prompt string, o Options) []Hit {
	keywords := ExtractKeywords(prompt)
	if len(keywords) == 0 {
		return nil
	}
	notes, err := l.List()
	if err != nil {
		return nil
	}

	cwdDomain := ""
	if o.Cwd != "" {
		cwdDomain = c.DomainForCwd(o.Cwd)
	}
	mentioned := mentionedDomains(c, keywords)

	hits := scoreAll(notes, keywords, cwdDomain, mentioned)

	if !o.CrossProject && cwdDomain != "" {
		scoped := filterByDomain(hits, cwdDomain)
		if len(scoped) > 0 {
			hits = scoped // 결과가 있으면 넓히지 않는다 — 현행 셸 동작
		}
	}

	hits = trim(hits, o)
	return hits
}

func mentionedDomains(c *config.Config, keywords []string) map[string]bool {
	m := map[string]bool{}
	for _, d := range c.Domain {
		for _, k := range keywords {
			if k == strings.ToLower(d.Prefix) {
				m[d.Prefix] = true
			}
		}
	}
	return m
}

func scoreAll(notes []store.Note, keywords []string, cwdDomain string, mentioned map[string]bool) []Hit {
	var hits []Hit
	for _, n := range notes {
		head := strings.ToLower(strings.Join([]string{
			n.Stem, n.Meta.Summary,
			strings.Join(n.Meta.Tags, " "),
			strings.Join(n.Meta.Domain, " "),
		}, " "))
		body := strings.ToLower(string(n.Body))

		headHits, bodyHits := 0, 0
		for _, k := range keywords {
			if strings.Contains(head, k) {
				headHits++
			}
			if strings.Contains(body, k) {
				bodyHits++
			}
		}
		// head 히트가 없으면 점수 0 — 본문만 스치는 문서를 버린다
		if headHits == 0 {
			continue
		}

		score := weightHead*headHits + weightBody*bodyHits
		if cwdDomain != "" && contains(n.Meta.Domain, cwdDomain) {
			score += weightCwdDomain
		}
		for d := range mentioned {
			if contains(n.Meta.Domain, d) {
				score += weightMention
			}
		}
		if n.Meta.Status == "superseded" {
			score -= penaltySuperseded
		}
		if score > 0 {
			hits = append(hits, Hit{Note: n, Score: score})
		}
	}
	return hits
}

func filterByDomain(hits []Hit, domain string) []Hit {
	var out []Hit
	for _, h := range hits {
		if contains(h.Note.Meta.Domain, domain) {
			out = append(out, h)
		}
	}
	return out
}

func trim(hits []Hit, o Options) []Hit {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Note.Path > hits[j].Note.Path // 셸의 sort -rn 동점 처리와 동일
	})
	if o.Limit > 0 && len(hits) > o.Limit {
		hits = hits[:o.Limit]
	}
	if o.MinScore > 0 {
		var out []Hit
		for _, h := range hits {
			if h.Score >= o.MinScore {
				out = append(out, h)
			}
		}
		hits = out
	}
	return hits
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

const warnLine = "위 결정 중 아쉬운 결과로 기록된 건이 있음. " +
	"유사 선택 시 회고를 먼저 읽고 수정안을 제안할 것."

// RenderInject 는 훅 주입용 형식으로 렌더링한다. 매칭이 없으면 빈 문자열.
func RenderInject(l *store.Layout, hits []Hit) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[과거 결정 참조]\n")
	warn := false
	for _, h := range hits {
		m := h.Note.Meta
		date, status, outcome := m.Date, m.Status, m.Outcome
		if date == "" {
			date = "-"
		}
		if status == "" {
			status = "active"
		}
		if outcome == "" {
			outcome = "pending"
		}
		summary := m.Summary
		if summary == "" {
			summary = h.Note.Stem
		}
		fmt.Fprintf(&b, "- %s %s (%s/%s) → %s\n",
			date, summary, status, outcome, l.RelPath(h.Note.Path))
		if status == "regretted" || outcome == "bad" {
			warn = true
		}
	}
	if warn {
		b.WriteString(warnLine + "\n")
	}
	return b.String()
}
```

`internal/core/store/paths.go` 에 `RelPath` 를 추가한다:

```go
// RelPath 는 절대 경로를 볼트 상대 경로로 바꾼다.
func (l *Layout) RelPath(p string) string {
	if rel, err := filepath.Rel(l.c.Vault, p); err == nil {
		return rel
	}
	return p
}
```

- [ ] **Step 4: CLI 배선**

`internal/adapter/cli/recall.go`:

```go
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/search"
	"github.com/xian0310567/casebook/internal/core/store"
)

func newRecallCmd() *cobra.Command {
	var format string
	var crossProject bool
	var limit int

	cmd := &cobra.Command{
		Use:   "recall <query>",
		Short: "관련 과거 결정을 찾는다",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := cmd.Flags().GetString("config")
			c, err := config.Load(path)
			if err != nil {
				return err
			}
			l := store.NewLayout(c)
			cwd, _ := os.Getwd()

			query := args[0]
			for _, a := range args[1:] {
				query += " " + a
			}
			hits := search.Recall(l, c, query, search.Options{
				Cwd: cwd, CrossProject: crossProject, Limit: limit, MinScore: 1,
			})
			out := cmd.OutOrStdout()
			if format == "inject" {
				fmt.Fprint(out, search.RenderInject(l, hits))
				return nil
			}
			for _, h := range hits {
				fmt.Fprintf(out, "%3d  %s\n     %s\n", h.Score, h.Note.Stem, h.Note.Meta.Summary)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "human", "출력 형식: human | inject")
	cmd.Flags().BoolVar(&crossProject, "cross-project", true, "cwd 도메인 밖의 결정도 찾는다")
	cmd.Flags().IntVar(&limit, "limit", 3, "최대 결과 수")
	return cmd
}
```

`root.go` 에 `root.AddCommand(newRecallCmd())` 추가.

- [ ] **Step 5: 통과 확인**

```bash
go test ./internal/core/search/ -v && make build
```

기대: 교차 프로젝트 테스트 포함 전부 PASS

- [ ] **Step 6: 커밋**

```bash
git add internal/core/search internal/core/store/paths.go internal/adapter/cli
git commit -m "feat: 회수 검색과 cb recall

현행 셸의 점수 함수를 이식하되, 교차 프로젝트 회상을 Options.CrossProject
플래그로 노출하고 기본값을 켠다. 셸은 cwd 도메인에 결정이 1건이라도 있으면
절대 넓히지 않아서, 주석이 말하는 '교차 프로젝트 회상'이 실제로는 막혀 있었다.
양쪽 동작을 테스트로 고정했다."
```

---

## Task 10: 결정 기록 (`cb capture`)

**Files:**
- Create: `internal/core/capture/capture.go`, `internal/core/capture/capture_test.go`, `internal/adapter/cli/capture.go`
- Modify: `internal/adapter/cli/root.go`

**Interfaces:**
- Consumes: `Layout`, `schema.Validate`, `index.Write`, `search.Recall`
- Produces:
  - `type Request struct { Domain, Slug, Summary, Date, Supersedes, SourceSession string; Tags, Related []string; Body []byte }`
  - `type Result struct { Path string; Related []search.Hit }`
  - `capture.Do(l *store.Layout, c *config.Config, r Request) (Result, error)`

- [ ] **Step 1: 실패하는 테스트를 쓴다**

`internal/core/capture/capture_test.go`:

```go
package capture

import (
	"os"
	"strings"
	"testing"
)

func TestDoCreatesNote(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "새 결정", Summary: "새 결정을 내렸다",
		Date: "2026-08-07", Tags: []string{"decision", "alpha"},
		Body: []byte("## 결정\n\n내용.\n"),
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if !strings.HasSuffix(res.Path, "alpha-결정-새-결정-2026-08-07.md") {
		t.Errorf("경로가 규약과 다르다: %s", res.Path)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `summary: "새 결정을 내렸다"`) {
		t.Errorf("frontmatter 가 정본 형식이 아니다:\n%s", data)
	}
}

func TestDoRejectsSchemaViolation(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	// summary 가 비면 거부
	if _, err := Do(l, c, Request{Domain: "alpha", Slug: "x", Date: "2026-08-07"}); err == nil {
		t.Fatal("summary 없는 요청을 통과시켰다")
	}
	// 알 수 없는 도메인은 거부
	if _, err := Do(l, c, Request{Domain: "없음", Slug: "x", Summary: "s", Date: "2026-08-07"}); err == nil {
		t.Fatal("알 수 없는 도메인을 통과시켰다")
	}
}

func TestDoRejectsDuplicate(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	r := Request{Domain: "alpha", Slug: "중복", Summary: "s", Date: "2026-08-07",
		Body: []byte("## 결정\n")}
	if _, err := Do(l, c, r); err != nil {
		t.Fatal(err)
	}
	if _, err := Do(l, c, r); err == nil {
		t.Fatal("같은 경로에 두 번 썼다")
	}
}

func TestDoReturnsRelated(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	res, err := Do(l, c, Request{
		Domain: "alpha", Slug: "저장 엔진 재검토", Summary: "저장 엔진을 다시 본다",
		Date: "2026-08-07", Body: []byte("## 결정\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// 편승: 기록하면 관련 과거 결정이 딸려 나온다
	if len(res.Related) == 0 {
		t.Error("관련 결정이 반환되지 않았다 — 편승이 동작하지 않는다")
	}
}

func TestDoUpdatesIndex(t *testing.T) {
	l, c := fixtureLayoutConfig(t)
	if _, err := Do(l, c, Request{
		Domain: "alpha", Slug: "색인 확인", Summary: "색인이 갱신되는지 본다",
		Date: "2026-08-07", Body: []byte("## 결정\n"),
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(l.IndexPath())
	if err != nil {
		t.Fatalf("색인이 없다: %v", err)
	}
	if !strings.Contains(string(data), "색인이 갱신되는지 본다") {
		t.Error("새 결정이 색인에 없다")
	}
}
```

`internal/core/capture/fixture_test.go` — Task 9 의 `fixtureLayoutConfig` 와 동일한 내용을 `package capture` 로 복제한다 (import 는 `config`, `store` 만).

- [ ] **Step 2: 실패 확인**

```bash
go test ./internal/core/capture/
```

기대: `undefined: Do` 컴파일 실패

- [ ] **Step 3: 구현**

`internal/core/capture/capture.go`:

```go
// Package capture 는 결정 노트를 만들고 갱신한다.
// 볼트에 쓰는 유일한 경로이므로 스키마 검증이 여기를 통과해야만 한다.
package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/index"
	"github.com/xian0310567/casebook/internal/core/schema"
	"github.com/xian0310567/casebook/internal/core/search"
	"github.com/xian0310567/casebook/internal/core/store"
)

type Request struct {
	Domain        string
	Slug          string
	Summary       string
	Date          string // 비면 오늘
	Supersedes    string
	SourceSession string
	Tags          []string
	Related       []string
	Body          []byte
}

type Result struct {
	Path    string
	Related []search.Hit
}

// Do 는 결정 노트를 만들고 색인을 갱신한 뒤, 관련 과거 결정을 함께 준다.
// 관련 결정을 돌려주는 것이 "편승" 이다 — 기록하는 순간이 곧 결정하는 순간이므로
// 그때 과거 결정이 따라 나오는 것이 가장 정확한 타이밍이다.
func Do(l *store.Layout, c *config.Config, r Request) (Result, error) {
	if r.Date == "" {
		r.Date = time.Now().Format("2006-01-02")
	}
	path, err := l.DecisionPath(r.Domain, r.Slug, r.Date)
	if err != nil {
		return Result{}, err
	}
	stem := strings.TrimSuffix(filepath.Base(path), ".md")

	m := store.Meta{
		Type: "decision", Date: r.Date, Domain: []string{r.Domain},
		Summary: r.Summary, Status: "active", Outcome: "pending",
		Supersedes: r.Supersedes, Related: r.Related,
		Tags: ensureDecisionTag(r.Tags), SourceSession: r.SourceSession,
	}
	if err := schema.Validate(stem, m); err != nil {
		return Result{}, fmt.Errorf("스키마 검증 실패: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return Result{}, fmt.Errorf("같은 경로에 이미 결정이 있다: %s", l.RelPath(path))
	}

	// 편승: 쓰기 **전에** 검색한다 — 자기 자신이 결과에 끼지 않게
	related := search.Recall(l, c, r.Summary+" "+r.Slug,
		search.Options{CrossProject: true, Limit: 3, MinScore: 1})

	body := r.Body
	if len(body) == 0 {
		body = []byte("## 결정\n\n## 근거\n\n## 고려한 대안\n\n## 예상 리스크\n\n## 회고\n")
	}
	if err := l.Write(store.Note{Path: path, Stem: stem, Meta: m, Body: body}); err != nil {
		return Result{}, err
	}
	if _, err := index.Write(l); err != nil {
		return Result{}, fmt.Errorf("노트는 썼으나 색인 갱신에 실패했다: %w", err)
	}
	return Result{Path: path, Related: related}, nil
}

// ensureDecisionTag 는 decision 태그를 보장한다. 회수의 1차 구분자다.
func ensureDecisionTag(tags []string) []string {
	for _, t := range tags {
		if t == "decision" {
			return tags
		}
	}
	return append([]string{"decision"}, tags...)
}
```

- [ ] **Step 4: CLI 배선**

`internal/adapter/cli/capture.go`:

```go
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/capture"
	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
)

func newCaptureCmd() *cobra.Command {
	var r capture.Request
	var bodyFile string

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "결정을 기록한다",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := cmd.Flags().GetString("config")
			c, err := config.Load(path)
			if err != nil {
				return err
			}
			if bodyFile == "-" {
				if r.Body, err = io.ReadAll(cmd.InOrStdin()); err != nil {
					return err
				}
			} else if bodyFile != "" {
				if r.Body, err = os.ReadFile(bodyFile); err != nil {
					return err
				}
			}
			l := store.NewLayout(c)
			res, err := capture.Do(l, c, r)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "기록됨: %s\n", l.RelPath(res.Path))
			if len(res.Related) > 0 {
				fmt.Fprintln(out, "\n관련 과거 결정:")
				for _, h := range res.Related {
					fmt.Fprintf(out, "  - %s %s\n", h.Note.Meta.Date, h.Note.Meta.Summary)
				}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&r.Domain, "domain", "", "도메인 접두어 (필수)")
	f.StringVar(&r.Slug, "slug", "", "파일명 slug (필수)")
	f.StringVar(&r.Summary, "summary", "", "한 줄 요약 (필수)")
	f.StringVar(&r.Date, "date", "", "YYYY-MM-DD (기본: 오늘)")
	f.StringVar(&r.Supersedes, "supersedes", "", "뒤집는 결정의 stem")
	f.StringVar(&r.SourceSession, "session", "", "출처 세션 ID")
	f.StringSliceVar(&r.Tags, "tag", nil, "태그 (반복 가능)")
	f.StringSliceVar(&r.Related, "related", nil, "관련 결정 위키링크 (반복 가능)")
	f.StringVar(&bodyFile, "body", "", "본문 파일 경로. - 이면 표준입력")
	_ = cmd.MarkFlagRequired("domain")
	_ = cmd.MarkFlagRequired("slug")
	_ = cmd.MarkFlagRequired("summary")
	return cmd
}
```

`root.go` 에 `root.AddCommand(newCaptureCmd())` 추가.

- [ ] **Step 5: 통과 확인**

```bash
go test ./internal/core/... -v && make build
```

기대: capture 5개 테스트 전부 PASS

- [ ] **Step 6: 커밋**

```bash
git add internal/core/capture internal/adapter/cli
git commit -m "feat: 결정 기록과 cb capture

스키마 검증을 통과하지 않으면 볼트에 쓰지 않는다. 현행 셸은 LLM 경로에만
검증이 있고 에이전트 수동 작성 경로에는 없어서 규약 위반이 새어 들어왔다.

편승: 기록 결과에 관련 과거 결정을 함께 돌려준다. 검색은 쓰기 전에 해서
자기 자신이 결과에 끼지 않게 한다."
```

---

## Task 11: 결정 갱신 (`cb review`)

**Files:**
- Create: `internal/core/capture/review.go`, `internal/core/capture/review_test.go`, `internal/adapter/cli/review.go`
- Modify: `internal/adapter/cli/root.go`

**Interfaces:**
- Consumes: `(*Layout).ResolveStem`, `(*Layout).Read`, `(*Layout).Write`, `schema.Validate`
- Produces:
  - `type ReviewRequest struct { Stem, Outcome, Status, Retrospective, Supersedes string }`
  - `capture.Review(l *store.Layout, r ReviewRequest) error`

`supersedes` 를 설정하면 **양방향**으로 연결한다 — 새 노트의 `supersedes` 와 옛 노트의 `status: superseded` + `related`.

- [ ] **Step 1: 실패하는 테스트를 쓴다**

`internal/core/capture/review_test.go`:

```go
package capture

import (
	"os"
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/core/store"
)

func TestReviewUpdatesOutcome(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	stem := "alpha-결정-저장엔진-2026-08-01"
	if err := Review(l, ReviewRequest{
		Stem: stem, Outcome: "good", Retrospective: "잘 됐다.",
	}); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	p, err := l.ResolveStem(stem)
	if err != nil {
		t.Fatal(err)
	}
	n, err := l.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if n.Meta.Outcome != "good" {
		t.Errorf("outcome = %q, want good", n.Meta.Outcome)
	}
	if !strings.Contains(string(n.Body), "잘 됐다.") {
		t.Errorf("회고가 본문에 안 들어갔다:\n%s", n.Body)
	}
}

func TestReviewRejectsMissingTarget(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	err := Review(l, ReviewRequest{Stem: "alpha-결정-없는것-2026-01-01", Outcome: "good"})
	if err == nil {
		t.Fatal("없는 대상을 통과시켰다")
	}
	if !strings.Contains(err.Error(), "대상 없음") {
		t.Errorf("에러 메시지가 진단에 도움이 안 된다: %v", err)
	}
}

func TestReviewRejectsTraversal(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	if err := Review(l, ReviewRequest{Stem: "../CLAUDE", Outcome: "good"}); err == nil {
		t.Fatal("경로 순회를 통과시켰다")
	}
}

func TestReviewSupersedesBothSides(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	newStem := "alpha-결정-스키마-2026-08-02"
	oldStem := "alpha-결정-저장엔진-2026-08-01"
	if err := Review(l, ReviewRequest{Stem: newStem, Supersedes: oldStem}); err != nil {
		t.Fatal(err)
	}
	read := func(stem string) store.Note {
		p, err := l.ResolveStem(stem)
		if err != nil {
			t.Fatal(err)
		}
		n, err := l.Read(p)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := read(newStem).Meta.Supersedes; got != "[["+oldStem+"]]" {
		t.Errorf("새 노트 supersedes = %q", got)
	}
	old := read(oldStem)
	if old.Meta.Status != "superseded" {
		t.Errorf("옛 노트 status = %q, want superseded", old.Meta.Status)
	}
	found := false
	for _, r := range old.Meta.Related {
		if r == "[["+newStem+"]]" {
			found = true
		}
	}
	if !found {
		t.Errorf("옛 노트 related 에 후속 문서가 없다: %v", old.Meta.Related)
	}
}

func TestReviewRejectsBadValues(t *testing.T) {
	l, _ := fixtureLayoutConfig(t)
	stem := "alpha-결정-저장엔진-2026-08-01"
	if err := Review(l, ReviewRequest{Stem: stem, Outcome: "maybe"}); err == nil {
		t.Fatal("허용값 밖 outcome 을 통과시켰다")
	}
	if err := Review(l, ReviewRequest{Stem: stem, Status: "unknown"}); err == nil {
		t.Fatal("허용값 밖 status 를 통과시켰다")
	}
	// 파일이 안 망가졌는지
	p, _ := l.ResolveStem(stem)
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "outcome: pending") {
		t.Errorf("거부됐는데 파일이 변했다:\n%s", data)
	}
}
```

- [ ] **Step 2: 실패 확인**

```bash
go test ./internal/core/capture/ -run TestReview
```

기대: `undefined: Review` 컴파일 실패

- [ ] **Step 3: 구현**

`internal/core/capture/review.go`:

```go
package capture

import (
	"fmt"
	"strings"

	"github.com/xian0310567/casebook/internal/core/index"
	"github.com/xian0310567/casebook/internal/core/schema"
	"github.com/xian0310567/casebook/internal/core/store"
)

type ReviewRequest struct {
	Stem          string
	Outcome       string // 빈 문자열이면 변경 없음
	Status        string
	Retrospective string
	Supersedes    string // 뒤집는 대상의 stem
}

// Review 는 기존 결정의 outcome·status·회고·supersedes 를 갱신한다.
// supersedes 는 양방향으로 연결한다 — 옛 노트도 superseded 로 바꾸고 related 를 채운다.
func Review(l *store.Layout, r ReviewRequest) error {
	path, err := l.ResolveStem(r.Stem)
	if err != nil {
		return err
	}
	n, err := l.Read(path)
	if err != nil {
		return fmt.Errorf("대상 없음: %s (%w)", r.Stem, err)
	}

	if r.Outcome != "" {
		n.Meta.Outcome = r.Outcome
	}
	if r.Status != "" {
		n.Meta.Status = r.Status
	}
	if r.Supersedes != "" {
		oldPath, err := l.ResolveStem(r.Supersedes)
		if err != nil {
			return fmt.Errorf("supersedes 대상이 잘못됐다: %w", err)
		}
		old, err := l.Read(oldPath)
		if err != nil {
			return fmt.Errorf("대상 없음: %s (%w)", r.Supersedes, err)
		}
		n.Meta.Supersedes = "[[" + r.Supersedes + "]]"
		old.Meta.Status = "superseded"
		old.Meta.Related = appendUnique(old.Meta.Related, "[["+r.Stem+"]]")
		if err := schema.Validate(old.Stem, old.Meta); err != nil {
			return fmt.Errorf("옛 노트 검증 실패: %w", err)
		}
		if err := l.Write(old); err != nil {
			return err
		}
	}
	if r.Retrospective != "" {
		n.Body = appendRetrospective(n.Body, r.Retrospective)
	}

	if err := schema.Validate(n.Stem, n.Meta); err != nil {
		return fmt.Errorf("검증 실패: %w", err)
	}
	if err := l.Write(n); err != nil {
		return err
	}
	_, err = index.Write(l)
	return err
}

func appendUnique(ss []string, v string) []string {
	for _, s := range ss {
		if s == v {
			return ss
		}
	}
	return append(ss, v)
}

// appendRetrospective 는 ## 회고 절에 내용을 붙인다. 절이 없으면 만든다.
func appendRetrospective(body []byte, text string) []byte {
	s := string(body)
	const head = "## 회고"
	if i := strings.Index(s, head); i >= 0 {
		return []byte(strings.TrimRight(s, "\n") + "\n\n" + text + "\n")
	}
	return []byte(strings.TrimRight(s, "\n") + "\n\n" + head + "\n\n" + text + "\n")
}
```

- [ ] **Step 4: CLI 배선**

`internal/adapter/cli/review.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xian0310567/casebook/internal/core/capture"
	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
)

func newReviewCmd() *cobra.Command {
	var r capture.ReviewRequest
	cmd := &cobra.Command{
		Use:   "review <stem>",
		Short: "기존 결정의 outcome·회고·supersedes 를 갱신한다",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r.Stem = args[0]
			path, _ := cmd.Flags().GetString("config")
			c, err := config.Load(path)
			if err != nil {
				return err
			}
			l := store.NewLayout(c)
			if err := capture.Review(l, r); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "갱신됨: %s\n", r.Stem)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&r.Outcome, "outcome", "", "pending | good | bad")
	f.StringVar(&r.Status, "status", "", "active | superseded | regretted")
	f.StringVar(&r.Retrospective, "retro", "", "## 회고 에 붙일 내용")
	f.StringVar(&r.Supersedes, "supersedes", "", "이 결정이 뒤집는 결정의 stem")
	return cmd
}
```

`root.go` 에 `root.AddCommand(newReviewCmd())` 추가.

- [ ] **Step 5: 통과 확인**

```bash
go test ./... -race && make build
```

기대: 전체 테스트 PASS

- [ ] **Step 6: 커밋**

```bash
git add internal/core/capture internal/adapter/cli
git commit -m "feat: 결정 갱신과 cb review

supersedes 를 양방향으로 연결한다 — 새 노트의 supersedes 와 옛 노트의
status: superseded + related 를 함께 쓴다. 한쪽만 쓰면 회수 시 옛 결정이
계속 active 로 잡힌다.

대상이 없으면 '대상 없음' 으로 명시적으로 실패한다. 현행 셸은 이 실패에서만
체크포인트를 전진시켜 갱신 의도를 영구 소실했다 — Plan 3 데몬에서 다룬다."
```

---

## Task 12: 릴리스 파이프라인

**Files:**
- Create: `.goreleaser.yaml`, `.github/workflows/release.yml`, `README.md`, `LICENSE`

- [ ] **Step 1: goreleaser 설정**

`.goreleaser.yaml`:

```yaml
version: 2

before:
  hooks:
    - go mod tidy

builds:
  - id: cb
    main: ./cmd/cb
    binary: cb
    env:
      - CGO_ENABLED=0
    goos: [darwin, linux]
    goarch: [amd64, arm64]
    flags: [-trimpath]
    ldflags:
      - -s -w -X github.com/xian0310567/casebook/internal/adapter/cli.Version={{.Version}}

archives:
  - formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: checksums.txt

changelog:
  sort: asc
  filters:
    exclude: ['^docs:', '^test:', '^chore:']
```

- [ ] **Step 2: 릴리스 워크플로**

`.github/workflows/release.yml`:

```yaml
name: release
on:
  push:
    tags: ['v*']
permissions:
  contents: write
jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v6
        with:
          go-version: '1.25'
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: '~> v2'
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- [ ] **Step 3: 설정 검증**

```bash
brew install goreleaser 2>/dev/null || true
goreleaser check
goreleaser build --snapshot --clean --single-target
ls -la dist/
```

기대: `goreleaser check` 가 `1 configuration file(s) validated`, `dist/` 에 `cb` 바이너리

- [ ] **Step 4: README 작성**

`README.md` 에 다음을 넣는다 — 스펙 §9 의 **호스트별 보장 수준 표를 그대로 싣는다**. 과장하지 않는 것이 이 프로젝트의 원칙이다.

```markdown
# casebook

에이전트가 내린 결정을 사람이 시키지 않아도 남기고, 다음 판단 시점에 알아서 꺼내 주는 층.

## 설치

    brew install xian0310567/tap/casebook
    # 또는
    go install github.com/xian0310567/casebook/cmd/cb@latest

런타임 의존이 없는 단일 정적 바이너리다.

## 보장 수준

| | Claude Code | MCP 전용 호스트 |
|---|---|---|
| 결정 순간 기록 | 에이전트 `cb capture` | 동일 |
| 놓친 기록 줍기 | 데몬 | 동일 |
| 세션 진입 컨텍스트 | 훅 (보장) | `initialize.instructions` (사실상 동등) |
| 주제 전환 시 회수 | 훅 (강제) | 계약 + 편승 (유도) |

MCP 에는 서버가 대화 중간에 텍스트를 밀어넣는 채널이 없다. 마지막 줄이 유일한 차이다.
```

- [ ] **Step 5: 커밋과 태그**

```bash
git add .goreleaser.yaml .github README.md LICENSE
git commit -m "chore: 릴리스 파이프라인과 README

goreleaser 로 darwin/linux × amd64/arm64 정적 바이너리를 만든다.
README 에 호스트별 보장 수준 표를 그대로 싣는다 — MCP 전용 호스트에서
주제 전환 회수가 강제가 아니라는 걸 숨기지 않는다."
git tag v0.1.0
```

---

## Self-Review 결과

**스펙 커버리지**

| 스펙 절 | 담당 태스크 |
|---|---|
| §4.1 코어/어댑터 경계 | Task 0·2·7·9·10·11 (CLI 는 core 만 부른다) |
| §4.2 서브커맨드 | index(7) · recall(9) · capture(10) · review(11). watch/mcp/hook/init 은 Plan 2~4 |
| §5 설정 | Task 2 |
| §6 저장 포맷 | Task 4 |
| §7.1 에이전트 주도 기록 | Task 10 |
| §7.2 데몬 | **Plan 3** |
| §8 회수 (편승 포함) | Task 9·10 |
| §11 테스트 | Task 3(slug·NFD) · 4(멱등성) · 5(경로 순회) · 6(스키마) · 10(중복 거부) |
| §13 배포 | Task 12 |

**Plan 1 범위 밖 (의도적)** — §7.2 데몬, §9 MCP 어댑터, §12 컷오버. 각각 Plan 3·2·4.

**미해결로 남긴 것** — 스펙 §15 의 "교차 프로젝트 회상" 은 Task 9 에서 `Options.CrossProject` 로 양쪽을 테스트로 고정하고 기본값을 켜는 것으로 처리했다. 실사용 후 되돌릴 수 있다.
