package claudecode

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultRoot 는 Claude Code 가 transcript 를 쌓는 자리다.
//
// XDG 를 따르지 않고 ~/.claude 에 고정돼 있다 — 우리 상태 파일은 XDG 를 쓰지만
// (스펙 §5) 이건 호스트가 정한 자리라 우리가 고를 수 없다.
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// List 는 root 아래의 transcript 파일을 경로순으로 준다.
//
// 읽을 수 없는 디렉토리를 만나도 멈추지 않는다. 다른 프로젝트의 권한 문제 하나로
// 전체 감시가 죽는 것이 더 나쁘다 — 다만 **몇 개를 못 봤는지 돌려준다.** 조용히
// 줄어든 목록은 "그 세션에는 결정이 없었다" 와 구별되지 않는다.
func List(root string) (paths []string, unreadable int, err error) {
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err // 루트 자체가 없으면 알린다
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
