package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/i18n"
)

type Layout struct {
	c *config.Config
	// marker 는 파일명이 결정 노트임을 나타내는 표식이다. 설정의 decision_file
	// 템플릿에서 유도한 값(config.DecisionMarker)을 NFC 로 접어 캐시한다 —
	// 설정 파일이 NFD 로 저장돼 있어도 ReadDir 이 준 NFC 이름과 비교되게.
	marker string
}

// NewLayout 은 Layout 을 만든다.
func NewLayout(c *config.Config) *Layout {
	return &Layout{c: c, marker: NFC(c.DecisionMarker())}
}

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

// IndexPath 는 색인 파일의 절대 경로다.
func (l *Layout) IndexPath() string {
	return filepath.Join(l.c.Vault, l.c.Naming.Index)
}

// DecisionDirs 는 **설정에 선언된** 결정 폴더의 경로를 전부 준다.
//
// 존재 여부는 보지 않는다 — 폴더는 그 도메인의 첫 결정을 쓸 때 만들어지므로,
// 아직 없는 것이 정상이다. 존재를 알고 싶으면 호출자가 stat 한다.
func (l *Layout) DecisionDirs() []string {
	var out []string
	for _, d := range l.c.Domain {
		if p, err := l.decisionsDir(d.Prefix); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// RelPath 는 절대 경로를 볼트 상대 경로로 바꾼다.
func (l *Layout) RelPath(p string) string {
	if rel, err := filepath.Rel(l.c.Vault, p); err == nil {
		return rel
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
	matches, err := filepath.Glob(filepath.Join(l.c.Vault, pattern))
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
	return filepath.Join(l.c.Vault, folder, name), nil
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
