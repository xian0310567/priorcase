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
		if isSubagent(d.Name()) {
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

// isSubagent 는 서브에이전트(sidechain) 기록인지 이름으로 판정한다.
//
// **왜 거르나.** 실측(2026-08-13)으로 이 기계의 Claude Code 기록 1,913개 중
// **1,417개(74% · 338MB)가 서브에이전트**이고, 어제 하루에만 278개가 늘었다.
// 안 거르면 체크포인트가 그만큼 쌓이는데, 상태 파일은 mutate 마다 통째로 다시
// 쓰므로 그 무게가 **모든 쓰기에** 실린다. 그리고 데몬이 도는 중에 생긴 파일은
// 0부터 훑혀서 한 대화가 파일 수만큼 중복 표시될 수 있다.
//
// **무엇을 잃나.** 서브에이전트 안에서만 내려지고 부모에게 보고되지 않은 결정.
// 그건 부모가 채택하지 않은 것이므로 프로젝트 결정으로 보기 어렵다 — 채택된
// 것은 보고서를 통해 부모 기록에 다시 들어온다.
//
// **왜 이름으로 보나.** 내용의 isSidechain 을 보려면 파일을 열어야 하고, 그건
// 목록을 만들 때마다 1,900번 여는 일이다. 실측에서 이름 판정이 **1,844건 전부**
// 내용과 일치했다 (불일치 0).
func isSubagent(name string) bool {
	return strings.HasPrefix(name, "agent-")
}
