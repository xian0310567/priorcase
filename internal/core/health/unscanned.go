package health

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// checkUnscanned 는 **회수가 아예 훑지 않는 폴더**를 본다.
//
// # 왜 이 검사가 있나 — 같은 사고가 하루에 두 번 났다
//
// `store.ListReferences` 는 **선언된 도메인 폴더만** 돈다. 그래서 볼트에 문서가
// 있어도 그 폴더가 설정에 없으면 회수 후보에 아예 안 들어간다. 에러도 안 나고
// 그냥 없는 것처럼 군다.
//
// 기존 `checkUndeclared` 는 이것을 절반만 잡는다 — `decisions/` 하위 폴더가 있는
// 곳만 보기 때문이다. 결정 노트 없이 참고 문서만 있는 폴더는 **완전히 조용하다.**
//
// 2026-08-24 실측으로 같은 고장이 두 번 났다:
//
//	아침: 집에서 만든 novels·tutela 가 이 머신 설정에 없어 결정 23건이 안 보였다
//	      (decisions/ 가 있어서 checkUndeclared 가 잡았다)
//	오후: OCC 16건·영상제작 9건이 안 보였다 — summary 가 이미 다 있는데 선언이
//	      없어서였고, decisions/ 가 없으니 **아무 검사도 말하지 않았다**
//
// 그리고 이 침묵은 사람을 잘못된 결론으로 데려간다. 볼트 654건 중 회수 가능이
// 65% 라는 숫자를 보면 "참고 문서 기능이 부족하다" 로 읽히는데, 실제 원인은
// 대부분 **의도적으로 범위 밖인 폴더**(NOI 220건)이고 고칠 것은 설정 두 줄이었다.
//
// # 두 종류를 갈라 말한다
//
// **summary 가 있는 문서가 있는 폴더** → 선언 한 줄로 회수에 들어온다. 고칠 수 있는
// 손해이므로 경고다.
//
// **summary 가 하나도 없는 폴더** → 모양이 다른 시스템이다(NOI 는 자체 스키마를 쓴다:
// `id`·`owner`·`lease-until`·claim 파일). 선언해도 회수가 쓸 게 없고 claim·signal 같은
// 기계 파일이 후보로 들어온다. 이건 고장이 아니라 경계이므로 사실로만 적는다 —
// **고칠 수 없는 것을 경고로 내면 경고를 무시하는 법을 가르친다.**
func checkUnscanned(r *Report, c *config.Config, l *store.Layout) {
	declared := map[string]bool{}
	for _, d := range c.Domain {
		if f, ok := c.FolderFor(d.Prefix); ok && f != "" {
			declared[store.NFC(f)] = true
		}
	}

	entries, err := os.ReadDir(l.Vault())
	if err != nil {
		r.add("훑지 않는 폴더", Warn, "볼트를 읽을 수 없다: "+err.Error(), "")
		return
	}

	type folder struct {
		name       string
		docs, gist int // 문서 수, 그중 summary 가 있는 것
	}
	var actionable, outOfShape []folder
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := store.NFC(e.Name())
		if declared[name] || machineryDir(name) {
			continue
		}
		f := folder{name: name}
		_ = filepath.WalkDir(filepath.Join(l.Vault(), e.Name()), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(store.NFC(d.Name()), ".md") {
				return nil //nolint:nilerr // 못 읽는 하위는 건너뛴다 — 이 검사는 개수만 센다
			}
			f.docs++
			if hasGist(p) {
				f.gist++
			}
			return nil
		})
		if f.docs == 0 {
			continue
		}
		if f.gist > 0 {
			actionable = append(actionable, f)
		} else {
			outOfShape = append(outOfShape, f)
		}
	}

	if len(actionable) == 0 && len(outOfShape) == 0 {
		r.add("훑지 않는 폴더", OK, "없다 — 볼트의 모든 폴더가 선언돼 있다", "")
		return
	}
	// 고칠 수 있는 쪽이 있으면 그것을 말한다. 사실 보고에 묻히면 안 된다.
	if len(actionable) > 0 {
		sort.Slice(actionable, func(i, j int) bool { return actionable[i].gist > actionable[j].gist })
		var parts []string
		total := 0
		for _, f := range actionable {
			parts = append(parts, fmt.Sprintf("%s %d/%d건", f.name, f.gist, f.docs))
			total += f.gist
		}
		r.add("훑지 않는 폴더", Warn,
			fmt.Sprintf("선언만 하면 회수에 들어올 문서가 %d건 있다 — %s", total, strings.Join(parts, " · ")),
			"설정에 [[domain]] 블록을 추가하라 (prefix·folder 만 있으면 된다. paths 는 이 머신에 "+
				"작업 디렉토리가 있을 때만)")
		return
	}
	var parts []string
	total := 0
	for _, f := range outOfShape {
		parts = append(parts, fmt.Sprintf("%s %d건", f.name, f.docs))
		total += f.docs
	}
	r.add("훑지 않는 폴더", OK,
		fmt.Sprintf("문서 %d건이 범위 밖이다 (%s) — summary 가 없어 선언해도 회수가 쓸 것이 없다",
			total, strings.Join(parts, " · ")), "")
}

// machineryDir 은 볼트의 기계 폴더다 — 회수 대상이 아닌 것이 정상이다.
func machineryDir(name string) bool {
	switch name {
	case "_meta", "_templates", "_derived", "_rules", "_attachments", ".obsidian", ".git", ".trash":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// hasGist 는 그 문서가 회수에 참여할 수 있는지다.
//
// **`store.readReference` 와 같은 파서를 쓴다.** 처음에는 `^summary:\s*\S` 정규식으로
// 썼는데 `summary: ""` 가 통과했다 — 따옴표가 비공백이라서다. 참여 조건은 원문 한 줄이
// 아니라 **파싱된 값이 비었는지**이므로(readReference 는 `TrimSpace(m.Summary) == ""`),
// 조건을 두 번 구현하면 갈라진다. 테스트가 잡았다.
//
// 결정 노트는 세지 않는다 — 그건 `checkUndeclared` 의 몫이고, 여기서 같이 세면
// 같은 폴더를 두 검사가 각자 다르게 말한다.
func hasGist(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	m, _, err := store.ParseFrontmatter(b)
	if err != nil {
		return false // frontmatter 가 없거나 깨졌다 — 어느 쪽이든 참여하지 않는다
	}
	return m.Type != "decision" && strings.TrimSpace(m.Summary) != ""
}
