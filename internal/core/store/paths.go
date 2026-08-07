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

// NewLayout 은 Layout 을 만든다.
//
// decisionMarker 상수는 설정의 decision_file 템플릿에서 유도한다는 전제로
// PrefixOf·ResolveStem 이 동작한다. 둘이 어긋나면 모든 결정 노트가 규약 밖으로
// 잘못 판정돼 나중에 조용히 깨진다 — 그러느니 생성 시점에 패닉으로 죽는다.
func NewLayout(c *config.Config) *Layout {
	if !strings.Contains(c.Naming.DecisionFile, decisionMarker) {
		panic(fmt.Sprintf("casebook: decision_file 템플릿에 %q 표식이 없다: %q", decisionMarker, c.Naming.DecisionFile))
	}
	return &Layout{c: c}
}

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
