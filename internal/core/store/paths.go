package store

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xian0310567/casebook/internal/core/config"
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

// RelPath 는 절대 경로를 볼트 상대 경로로 바꾼다.
func (l *Layout) RelPath(p string) string {
	if rel, err := filepath.Rel(l.c.Vault, p); err == nil {
		return rel
	}
	return p
}
