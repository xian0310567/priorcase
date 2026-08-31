package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/i18n"
)

type Layout struct {
	c *config.Config
	// vault 는 이 Layout 이 다루는 볼트의 절대 경로다.
	//
	// **Layout 하나는 볼트 하나다.** 볼트가 쓰기와 회수의 경계이므로, 한 Layout 이
	// 여러 볼트를 오가면 그 경계가 흐려진다 — 어느 볼트에 쓸지가 호출부마다
	// 달라지고, 그 차이는 파일이 엉뚱한 자리에 생긴 뒤에야 드러난다.
	vault string
	// marker 는 파일명이 결정 노트임을 나타내는 표식이다. 설정의 decision_file
	// 템플릿에서 유도한 값(config.DecisionMarker)을 NFC 로 접어 캐시한다 —
	// 설정 파일이 NFD 로 저장돼 있어도 ReadDir 이 준 NFC 이름과 비교되게.
	marker string
}

// NewLayout 은 **기본 볼트**의 Layout 을 만든다.
//
// 볼트가 하나인 설정에서는 그것이고, 여럿이면 이름이 "default" 인 것 또는 첫 번째다.
// 프로젝트별 볼트가 필요하면 NewLayoutFor 를 쓴다.
func NewLayout(c *config.Config) *Layout {
	v, _ := c.DefaultVault() // 볼트가 없으면 빈 경로 — 경로 연산이 시끄럽게 실패한다
	return NewLayoutFor(c, v)
}

// NewLayoutFor 는 지정한 볼트의 Layout 을 만든다.
func NewLayoutFor(c *config.Config, v config.Vault) *Layout {
	return &Layout{c: c, vault: v.Path, marker: NFC(c.DecisionMarker())}
}

// LayoutForCwd 는 그 자리에서 일할 때 쓸 Layout 을 준다 (cwd → 도메인 → 볼트).
//
// **이것이 "A 프로젝트에서 일하면 A 의 볼트에 쓴다" 를 실현하는 자리다.**
func LayoutForCwd(c *config.Config, dir string) (*Layout, error) {
	v, err := c.VaultForCwd(dir)
	if err != nil {
		return nil, err
	}
	return NewLayoutFor(c, v), nil
}

// Vault 는 이 Layout 이 다루는 볼트의 절대 경로다.
func (l *Layout) Vault() string { return l.vault }

// DecisionMarker 는 결정 노트 파일명의 표식을 준다. 정본은 설정의 decision_file
// 템플릿이고, 여기서는 그걸 유도한 값을 그대로 내보낸다 — schema 는 config 를
// 모르므로 표식을 인자로 받아야 하는데, 그 인자의 출처가 여기다.
func (l *Layout) DecisionMarker() string { return l.marker }

// decisionsDir 는 접두어에 대응하는 결정 폴더의 절대 경로다.
func (l *Layout) decisionsDir(prefix string) (string, error) {
	folder, ok := l.c.FolderFor(prefix)
	if !ok {
		return "", fmt.Errorf("알 수 없는 도메인 접두어: %q", prefix)
	}
	rel := strings.ReplaceAll(l.c.Naming.DecisionsDir, "{project}", folder)
	return filepath.Join(l.vault, rel), nil
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

// DecisionPathIn 은 **아직 설정에 없는 도메인**의 결정 경로를 만든다.
//
// `DecisionPath` 는 `FolderFor` 로 폴더를 찾으므로 선언되지 않은 접두어에서는
// 에러가 난다. 그게 맞다 — 기록은 선언된 곳에만 해야 한다. 하지만 `prior domain
// split` 은 **도메인을 만들기 전에 계획을 보여 줘야 해서**(설정을 고치기 전에
// 사람이 옮길 목록을 봐야 한다) 그 순서가 뒤집힌다. 폴더를 인자로 받는다.
func (l *Layout) DecisionPathIn(folder, prefix, slug, date string) (string, error) {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return "", fmt.Errorf("폴더 이름이 비었다")
	}
	rel := strings.ReplaceAll(l.c.Naming.DecisionsDir, "{project}", folder)
	name := l.c.Naming.DecisionFile
	name = strings.ReplaceAll(name, "{domain}", prefix)
	name = strings.ReplaceAll(name, "{slug}", Slugify(slug))
	name = strings.ReplaceAll(name, "{date}", date)
	return filepath.Join(l.vault, rel, NFC(name)), nil
}

// PrefixOf 는 stem 에서 도메인 접두어를 뽑는다. 규약에 안 맞으면 빈 문자열.
func (l *Layout) PrefixOf(stem string) string {
	if l.marker == "" {
		return "" // 표식을 유도할 수 없는 설정 — 규약을 판정할 수 없으니 거부한다
	}
	stem = NFC(stem)
	i := strings.Index(stem, l.marker)
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
// 그래서 (1) 모양 검사 (2) 접두어 화이트리스트 (3) 최종 경로가 해당 도메인의
// decisions 디렉터리 안인지, 셋을 다 본다. 볼트 전체가 아니라 decisions 디렉터리로
// 좁히는 이유: 볼트 루트에는 CLAUDE.md 같은 지침 문서가 바로 있어서, "볼트 안"만
// 보면 dir/../../../CLAUDE 같은 경로가 통과한다.
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
	rel, err := filepath.Rel(dir, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("결정 폴더 밖을 가리킨다: %q", stem)
	}
	return p, nil
}

// DecisionDirs 는 **설정에 선언된** 결정 폴더의 경로를 전부 준다.
//
// 존재 여부는 보지 않는다 — 폴더는 그 도메인의 첫 결정을 쓸 때 만들어지므로,
// 아직 없는 것이 정상이다. 존재를 알고 싶으면 호출자가 stat 한다.
// DecisionDirs 는 **이 볼트가 가진 도메인의** 결정 폴더다.
//
// # 왜 볼트로 거르는가
//
// `decisionsDir` 는 도메인의 볼트를 안 보고 `{project}/decisions` 를 지금 볼트에
// 그대로 이어 붙인다. 볼트가 하나일 때는 그게 맞았다 — 전부 같은 볼트니까.
//
// 볼트를 가르면(코드주권 결정 2026-08-31: 개인 볼트 + 회사 볼트) 걸러야 한다.
// 안 걸면 개인 볼트에서 `개인볼트/editup/decisions` 를 훑는데, 그건 존재하지 않는
// 자리라 조용히 건너뛴다. 결과는 셋이다:
//
//	① `prior doctor` 의 "도메인 폴더" 가 남의 볼트 도메인을 "아직 없음" 으로 센다
//	② 볼트마다 도는 검사가 남의 도메인까지 자기 것으로 셈해 판정이 흐려진다
//	③ 읽을 자리가 아닌 곳을 매 회수마다 stat 한다
//
// **쓰기 경로는 이 함수를 안 쓴다.** `Layout.For(prefix)` 가 도메인의 볼트를 직접
// 찾아가므로(그 함수의 §) 회사 결정은 cwd 와 무관하게 회사 볼트에 쓰인다.
// 여기는 읽기 쪽 — "이 볼트에 무엇이 있는가" 다.
//
// 볼트가 하나면 전 도메인이 그 볼트로 풀리므로 결과가 예전과 같다.
func (l *Layout) DecisionDirs() []string {
	var out []string
	for _, d := range l.c.Domain {
		if !l.owns(d.Prefix) {
			continue
		}
		if p, err := l.decisionsDir(d.Prefix); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// owns 는 그 도메인이 이 볼트에 사는지다.
//
// 볼트를 못 풀면 **가진 것으로 본다** — 설정이 깨졌을 때 조용히 사라지는 것보다
// 한 번 더 훑고 doctor 가 말하게 하는 편이 낫다.
func (l *Layout) owns(prefix string) bool {
	v, err := l.c.VaultFor(prefix)
	if err != nil {
		return true
	}
	return v.Path == l.vault
}

// RelPath 는 절대 경로를 볼트 상대 경로로 바꾼다.
func (l *Layout) RelPath(p string) string {
	if rel, err := filepath.Rel(l.vault, p); err == nil {
		// **다른 볼트의 파일은 상대 경로로 그리지 않는다.**
		//
		// 볼트가 여럿이면 `../work/acme/decisions/…` 같은 문자열이 나오는데,
		// 그건 "이 볼트 기준 어딘가" 라는 뜻이라 어느 볼트인지 읽을 수가 없다.
		// 볼트를 넘으면 절대 경로가 정직하다.
		if !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			return rel
		}
		return p
	}
	return p
}

// UndeclaredDecisionDirs 는 볼트에 있는데 **설정에 없는** 결정 폴더를 준다.
//
// 이게 조용한 데이터 손실의 입구다. 색인과 회수는 설정에 선언된 도메인만 훑으므로,
// 볼트에 폴더를 만들고 설정에 안 적으면 그 프로젝트의 결정이 **전부** 빠진다.
// 그런데 색인은 정상적으로 생성되고 회수도 에러를 내지 않는다 — 그냥 없는 것처럼 군다.
//
// 탐색은 decisions_dir 템플릿에서 유도한다(`{project}/decisions` → `*/decisions`).
// 템플릿을 바꿔 써도 따라온다.
func (l *Layout) UndeclaredDecisionDirs() ([]string, error) {
	pattern := strings.ReplaceAll(l.c.Naming.DecisionsDir, "{project}", "*")
	matches, err := filepath.Glob(filepath.Join(l.vault, pattern))
	if err != nil {
		return nil, err
	}

	declared := map[string]bool{}
	for _, d := range l.DecisionDirs() {
		declared[NFC(d)] = true
	}

	var out []string
	for _, m := range matches {
		fi, err := os.Stat(m)
		if err != nil || !fi.IsDir() {
			continue
		}
		if declared[NFC(m)] {
			continue
		}
		// 결정 노트가 실제로 들어 있는 폴더만 알린다. 빈 폴더는 소음이다.
		ents, err := os.ReadDir(m)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				out = append(out, m)
				break
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// AllStems 는 **볼트 전역** 마크다운 파일명(확장자 뺀 basename)의 집합이다.
//
// 위키링크가 걸리는지 판정하는 유일한 근거다. 옵시디언이 `[[x]]` 를 푸는 방식이
// 정확히 이것이라 — 경로가 아니라 basename 으로, 볼트 전체에서 — 그대로 흉내낸다.
//
// # ResolveStem 을 쓰지 않는 이유
//
// ResolveStem(위 §)은 (1) 모양 (2) 접두어 화이트리스트 (3) decisions 디렉터리 안,
// 셋을 강제한다. 그건 **쓰기 경로의 보안 검증**으로 옳지만 판정에는 너무 좁다 —
// 실볼트 실측(2026-08-15)으로 frontmatter 링크 214개 중 **49개(23%)가 결정이 아닌
// 문서**를 가리킨다(`[[00-omni-프로젝트-개요]]`, `_meta/00-볼트-네이밍-규약`).
// ResolveStem 으로 판정하면 그 49건이 전부 "끊어진 링크" 로 잡힌다.
//
// # DecisionDirs 를 쓰지 않는 이유
//
// 그쪽은 설정의 [[domain]] 목록만 돈다. 설정에 없는 폴더는 통째로 안 보이는데,
// 실볼트에서 그 사각지대에 bard 14건이 들어앉아 한 세션 내내 회수되지 않았다.
// **검사기가 색인의 사각지대를 상속하면 그 사각지대를 영영 못 본다.**
//
// 파싱은 하지 않는다 — 파일명만 본다. 실측 2.4~2.7ms / 477파일.
func (l *Layout) AllStems() (map[string]bool, error) {
	out := map[string]bool{}
	err := filepath.WalkDir(l.vault, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			// 못 읽는 하위는 건너뛴다. 볼트 루트 자체가 없으면 아래에서 에러가 난다.
			if p == l.vault {
				return err
			}
			return nil
		}
		name := NFC(e.Name())
		if e.IsDir() {
			// 점으로 시작하는 폴더는 도구의 것이다(.obsidian·.git·.trash).
			// 옵시디언도 그 안을 링크 대상으로 세지 않는다.
			if p != l.vault && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".md") {
			out[strings.TrimSuffix(name, ".md")] = true
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("볼트를 훑을 수 없다 (%s): %w", l.vault, err)
	}
	return out, nil
}

// WorklogPath 는 도메인의 작업 로그 경로다.
func (l *Layout) WorklogPath(prefix string) (string, error) {
	return l.projectFile(prefix, l.c.Naming.Worklog)
}

// RollupPath 는 도메인의 주간 요약 파일 경로다.
//
// naming.rollup 은 선택 키라 비어 있을 수 있다. 그때는 조용히 기본값을 쓰지 않고
// 에러를 낸다 — 코드에 파일명 리터럴을 숨기면 설정이 정본이라는 전제가 깨진다.
func (l *Layout) RollupPath(prefix string) (string, error) {
	if strings.TrimSpace(l.c.Naming.Rollup) == "" {
		return "", fmt.Errorf("[naming] 에 rollup 이 없다 — 설정에 한 줄 넣어라 " +
			`(예: rollup = "98-{project}-작업-로그-요약.md")`)
	}
	return l.projectFile(prefix, l.c.Naming.Rollup)
}

func (l *Layout) projectFile(prefix, template string) (string, error) {
	folder, ok := l.c.FolderFor(prefix)
	if !ok {
		return "", fmt.Errorf("알 수 없는 도메인 접두어: %q", prefix)
	}
	name := strings.ReplaceAll(template, "{project}", folder)
	return filepath.Join(l.vault, folder, name), nil
}

// Prefixes 는 설정에 선언된 도메인 접두어를 순서대로 준다.
func (l *Layout) Prefixes() []string {
	out := make([]string, 0, len(l.c.Domain))
	for _, d := range l.c.Domain {
		out = append(out, d.Prefix)
	}
	return out
}

// Lang 은 볼트 산출물의 언어다. index·search 가 Config 를 직접 못 보므로 여기서 준다.
func (l *Layout) Lang() i18n.Lang { return i18n.Of(l.c.Lang) }

// For 는 그 도메인의 볼트를 다루는 Layout 을 준다.
//
// **쓰기 경로는 이것을 스스로 부른다.** 호출부가 볼트를 고르게 하면 어댑터마다
// 다른 답을 얻는다 — 훅은 훅의 cwd, CLI 는 셸의 cwd, MCP 는 또 다른 것. 도메인이
// 볼트를 정하는 규칙이 하나면 어디서 불러도 같은 자리에 쓴다.
//
// 이미 그 볼트면 자기 자신을 준다 (흔한 경우라 할당을 아낀다).
func (l *Layout) For(prefix string) (*Layout, error) {
	v, err := l.c.VaultFor(prefix)
	if err != nil {
		return nil, err
	}
	if v.Path == l.vault {
		return l, nil
	}
	return NewLayoutFor(l.c, v), nil
}
