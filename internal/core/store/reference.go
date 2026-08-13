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

// ListReferences 는 **회수에만 쓰이는 참고 문서**를 준다.
//
// # 왜 있나
//
// 실측(2026-08-13): 볼트 245건 중 결정이 147건이고 나머지 98건 — 설계 문서 ·
// 리서치 · 기획 · 작업 로그 — 은 회수에 영영 안 걸렸다. 실제 질의 51개로 재 보니
// 그것들을 넣었을 때 **1위가 바뀐 것이 9건이고 결정이 상위 3에서 밀려난 것은
// 1건**뿐이었다. 채우는 것이지 밀어내는 것이 아니다.
//
// 그중에는 결정만 봐서는 답이 없던 질의가 있었다 — "한국에서 해외취업을 원하는
// 사람들" 에 결정 1위는 무관한 타이포그래피 결정이었고, 참고를 넣으니 그 주제의
// 기획 문서가 18점으로 1위였다.
//
// # 무엇을 넣나
//
// **`summary` 가 있는 것만.** 그것이 참여 신호다. 통째로 넣으면 초안·메모·생성물
// 까지 들어와 소음이 되고, 무엇보다 회수기는 head(stem·summary·태그)에 아무것도
// 안 걸리면 그 문서를 버리므로 summary 없이는 **실제로 아무 일도 안 일어난다.**
//
// **설정된 도메인 폴더만 훑는다.** 볼트에는 그 밖의 것이 있다 — 지침 문서 ·
// 생성 색인 · 이 시스템이 손대면 안 되는 구역(NOI). 특히 생성 색인은 전 노트의
// 요약을 모아 둔 파일이라 회수에 들어오면 어떤 질의에도 걸려 언제나 1위가 된다.
// 도메인 폴더만 보면 그것들이 자동으로 빠진다 — 제외 목록을 따로 관리하면 새
// 자리가 생길 때마다 빠뜨린다.
//
// # 무엇이 아닌가
//
// 참고는 **쓰기·검증·색인·회고의 대상이 아니다.** capture 는 결정만 쓰고,
// doctor 의 스키마 검사와 `prior index` 는 결정만 본다. 참고에 그 규칙을 걸면
// 98건이 전부 위반으로 잡히고, 그건 사람이 쓰는 문서에 우리 규약을 강요하는 것이다.
func (l *Layout) ListReferences() ([]Note, []SkippedNote, error) {
	var notes []Note
	var skipped []SkippedNote

	for _, d := range l.c.Domain {
		folder, ok := l.c.FolderFor(d.Prefix)
		if !ok || folder == "" {
			continue
		}
		root := filepath.Join(l.vault, folder)
		// 그 도메인의 결정 폴더는 건너뛴다 — 결정은 List() 가 준다.
		decDir, _ := l.decisionsDir(d.Prefix)

		err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return fs.SkipDir // 폴더가 아직 없는 도메인은 정상이다
				}
				return nil // 못 읽는 하위는 건너뛴다 — 아래에서 skipped 로 안 잡히지만
			}
			if e.IsDir() {
				if decDir != "" && sameDir(p, decDir) {
					return fs.SkipDir
				}
				return nil
			}
			name := NFC(e.Name())
			if !strings.HasSuffix(name, ".md") {
				return nil
			}
			n, rerr := l.readReference(p, d.Prefix)
			if rerr != nil {
				skipped = append(skipped, SkippedNote{Path: p, Reason: rerr})
				return nil
			}
			if n.Stem == "" {
				return nil // 참여하지 않는 문서 (summary 없음 · 결정 노트)
			}
			notes = append(notes, n)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("참고 문서를 훑을 수 없다 (%s): %w", root, err)
		}
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Path < notes[j].Path })
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Path < skipped[j].Path })
	return notes, skipped, nil
}

// readReference 는 참고 문서 하나를 읽는다.
//
// 참여하지 않는 문서(summary 없음 · type: decision)는 **에러가 아니라 빈 Note**
// 로 준다 — 그건 정상이고, 에러로 세면 건너뛴 목록이 볼트 전체가 된다.
//
// frontmatter 가 깨진 것은 에러다. 조용히 빠지면 "그 문서에 그런 말이 없다" 와
// 구별되지 않는다.
func (l *Layout) readReference(path, prefix string) (Note, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Note{}, err
	}
	m, body, err := ParseFrontmatter(b)
	if err != nil {
		// **frontmatter 가 없는 것은 고장이 아니다.** 볼트에는 평범한 마크다운이
		// 있고(실측 98건 중 2건), 그건 참여하지 않는 문서일 뿐이다. 읽기 실패로
		// 세면 매 프롬프트마다 "결정 노트를 읽지 못했다" 경고가 뜨고, 늘 뜨는
		// 경고는 무시하는 법을 가르쳐 진짜 깨진 노트까지 묻는다.
		//
		// **깨진 frontmatter 는 여전히 실패다** — 그건 사람이 고쳐야 한다.
		if errors.Is(err, ErrNoFrontmatter) {
			return Note{}, nil
		}
		return Note{}, err
	}
	// 결정은 List() 의 몫이다. 여기서 같이 주면 회수 상위가 같은 노트로 채워진다.
	if m.Type == "decision" {
		return Note{}, nil
	}
	if strings.TrimSpace(m.Summary) == "" {
		return Note{}, nil
	}
	// **도메인은 폴더에서 온다.** 참고 문서에 domain 을 요구하면 98건을 손으로
	// 고쳐야 하고, 폴더가 이미 그 사실을 말해 준다.
	m.Domain = []string{prefix}
	return Note{
		Path: path,
		Stem: strings.TrimSuffix(NFC(filepath.Base(path)), ".md"),
		Meta: m,
		Body: body,
	}, nil
}

// sameDir 는 두 경로가 같은 디렉토리인지 본다 (심볼릭 링크는 안 푼다).
func sameDir(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

// IsReference 는 이 노트가 참고 문서인지 본다.
//
// 회수 결과에서 **결정과 참고를 반드시 구분해 보여 줘야 한다.** 안 그러면
// 에이전트가 기획 초안을 확정된 결정으로 읽는다.
func (n Note) IsReference() bool { return n.Meta.Type != "decision" }
