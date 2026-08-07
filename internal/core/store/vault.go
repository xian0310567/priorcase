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
	if l.marker == "" {
		return nil, errNoMarker(l.c.Naming.DecisionFile)
	}
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
			if !l.isDecisionFile(name) {
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

// errNoMarker 는 표식을 유도할 수 없는 설정에 대한 공통 에러다.
// 조용히 "결정이 하나도 없다" 로 처리하면 색인이 통째로 비어버린다.
func errNoMarker(template string) error {
	return fmt.Errorf("설정의 decision_file 템플릿에서 결정 표식을 유도할 수 없다: %q", template)
}

// isDecisionFile 은 파일명이 결정 노트 규약에 맞는지 본다. 이름은 NFC 여야 한다.
func (l *Layout) isDecisionFile(name string) bool {
	return strings.HasSuffix(name, ".md") && strings.Contains(name, l.marker)
}

// DecisionStems 는 한 도메인의 결정 폴더에 있는 노트 stem 을 준다.
// 파일을 파싱하지 않는다 — 파일명만 필요한 호출자(중복 검사)를 위한 것이다.
// 폴더가 아직 없으면 빈 목록이고 에러가 아니다.
func (l *Layout) DecisionStems(prefix string) ([]string, error) {
	if l.marker == "" {
		return nil, errNoMarker(l.c.Naming.DecisionFile)
	}
	dir, err := l.decisionsDir(prefix)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("결정 폴더를 읽을 수 없다 (%s): %w", dir, err)
	}
	var stems []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := NFC(e.Name())
		if !l.isDecisionFile(name) {
			continue
		}
		stems = append(stems, strings.TrimSuffix(name, ".md"))
	}
	return stems, nil
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
//
// WriteFileAtomic 을 쓴다 — os.WriteFile 은 기존 파일을 먼저 비운 뒤 쓰기
// 때문에 중간에 실패하면 결정 노트가 잘린 채로 남는다. 색인과 달리 결정
// 노트는 `cb index` 로 재생성할 원본이 없으므로 이 보장이 특히 중요하다.
func (l *Layout) Write(n Note) error {
	if err := os.MkdirAll(filepath.Dir(n.Path), 0o755); err != nil {
		return err
	}
	return WriteFileAtomic(n.Path, EmitNote(n.Meta, n.Body), 0o644)
}
