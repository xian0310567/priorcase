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
