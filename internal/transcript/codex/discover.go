package codex

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultRoot 는 Codex CLI 가 세션 기록을 쌓는 자리다.
//
// `~/.codex/sessions` 아래에 `YYYY/MM/DD/` 로 날짜 계층이 하나 더 있다.
// XDG 를 따르지 않는다 — 호스트가 정한 자리라 우리가 고를 수 없다.
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "sessions"), nil
}

// List 는 root 아래의 세션 파일을 경로순으로 준다.
//
// 읽을 수 없는 디렉토리를 만나도 멈추지 않는다 — 다른 프로젝트의 권한 문제 하나로
// 전체 감시가 죽는 것이 더 나쁘다. 다만 **몇 개를 못 봤는지 돌려준다.** 조용히 줄어든
// 목록은 "그 세션에는 결정이 없었다" 와 구별되지 않는다.
//
// **루트가 없으면 에러가 아니라 빈 목록이다.** Claude Code 와 다른 점이다. Codex 를
// 안 쓰는 사람이 훨씬 많고, 그 사람들에게 "디렉토리가 없다" 는 에러를 매번 보이면
// 진짜 문제를 가린다. 반면 claudecode 는 이 도구의 전제라서 없으면 알려야 한다.
func List(root string) (paths []string, unreadable int, err error) {
	if _, statErr := os.Stat(root); os.IsNotExist(statErr) {
		return nil, 0, nil
	}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			unreadable++
			return fs.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		return nil, unreadable, err
	}
	sort.Strings(paths)
	return paths, unreadable, nil
}
