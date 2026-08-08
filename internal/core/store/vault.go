package store

import (
	"errors"
	"fmt"
	"io/fs"
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

// SkippedNote 는 List() 가 읽지 못해 건너뛴 결정 노트 하나다.
//
// Reason 은 경로를 붙이지 않은 원인 그대로다 — 경로는 Path 로 따로 들고 있으므로
// 호출자가 볼트 상대 경로로 바꿔 보여줄 수 있다.
type SkippedNote struct {
	Path   string
	Reason error
}

// List 는 모든 결정 폴더의 노트를 경로 오름차순으로 주고, 읽기·파싱에 실패해
// 건너뛴 파일을 두 번째 값으로 함께 준다.
//
// 건너뛰는 동작 자체는 의도된 것이다 — 노트 한 건이 깨졌다고 색인 전체가
// 죽으면 안 된다(TestListSkipsBrokenFile 이 이걸 고정한다). 문제는 예전에
// 건너뛴 것을 **아무에게도 알리지 않았다**는 점이었다: 실볼트 53건 중 6건이
// 구 스키마(title/project/created/superseded-by)로 쓰여 있어 KnownFields(true)
// 파싱에서 거부됐고, `cb index` 는 아무 말 없이 47행짜리 색인을 만들었다.
// 스펙 §1.3 이 셸의 죄목으로 든 "조용히 데이터를 잃는다" 가 그대로 재현된 것이다.
//
// 반환값을 구조체 하나로 묶지 않고 셋으로 늘린 이유: 구조체면 호출자가
// res.Notes 만 쓰고 res.Skipped 를 안 보는 것이 문법적으로 티가 나지 않는다.
// 세 번째 반환값은 무시하려면 `_` 를 명시적으로 적어야 한다 — 침묵이 의식적인
// 선택이 되고, 그 자리가 grep 으로 잡힌다.
func (l *Layout) List() ([]Note, []SkippedNote, error) {
	if l.marker == "" {
		return nil, nil, errNoMarker(l.c.Naming.DecisionFile)
	}
	var notes []Note
	var skipped []SkippedNote
	for _, dir := range l.DecisionDirs() {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("결정 폴더를 읽을 수 없다 (%s): %w", dir, err)
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
			path := filepath.Join(dir, e.Name())
			n, err := l.readNote(path)
			if err != nil {
				skipped = append(skipped, SkippedNote{Path: path, Reason: err})
				continue
			}
			n.Stem = strings.TrimSuffix(name, ".md")
			notes = append(notes, n)
		}
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Path < notes[j].Path })
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Path < skipped[j].Path })
	return notes, skipped, nil
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

// Read 는 노트 하나를 읽는다. 파싱 실패에는 경로를 붙인다 — 단건 호출자는
// 에러 문자열만으로 어느 파일이 문제인지 알아야 한다.
func (l *Layout) Read(path string) (Note, error) {
	n, err := l.readNote(path)
	if err != nil {
		var pe *fs.PathError
		if errors.As(err, &pe) {
			return Note{}, err // *PathError 는 이미 경로를 담고 있다
		}
		return Note{}, fmt.Errorf("%s: %w", path, err)
	}
	return n, nil
}

// readNote 는 Read 의 알맹이다. 에러에 경로를 덧붙이지 않는다 — List 는 경로를
// SkippedNote.Path 로 따로 들고 있어서, 여기서 붙이면 사용자에게 나가는 경고에
// 경로가 두 번(절대 경로 + 볼트 상대 경로) 찍힌다.
func (l *Layout) readNote(path string) (Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Note{}, err
	}
	m, body, err := ParseFrontmatter(data)
	if err != nil {
		return Note{}, err
	}
	// **우리 노트인지 여기서 가른다.**
	//
	// 예전에는 `KnownFields(true)` 가 이 일을 겸했다 — 10키 밖의 키가 하나라도 있으면
	// 파싱이 실패했으므로 옛 도구가 남긴 `title/project/created` 노트가 자연히 걸러졌다.
	// 그런데 같은 규칙이 **사용자가 Obsidian 에서 넣은 `aliases:` 한 줄**도 걸러서,
	// 멀쩡한 결정을 색인·회수·review 에서 통째로 지워 버렸다.
	//
	// 이제 잉여 키는 Extra 로 받는다. 그러면 옛 스키마 노트가 **빈 결정 노트로** 통과해
	// summary 가 빈 채 색인에 들어가므로, 대신 여기서 표식을 본다.
	// `type: decision` 이 우리 노트의 유일한 표식이다(schema.Validate 와 같은 기준).
	if m.Type != "decision" {
		return Note{}, fmt.Errorf("결정 노트가 아니다 (type: %q) — 다른 도구가 남긴 형식이거나 frontmatter 가 옛 스키마다", m.Type)
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
