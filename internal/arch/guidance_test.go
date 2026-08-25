package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 이 파일은 **안내 문구가 없는 명령을 가리키지 않게** 한다.
//
// # 왜 필요한가
//
// 2026-08-24 에 `prior index` 를 없앴다(색인 폐지). 명령과 패키지는 걷어냈는데
// **사용자에게 출력되는 안내 문구 네 곳이 그대로 남았다** — doctor 의 fix 줄이
// "prior index 를 다시 돌려라" 라고 말하고, 그대로 치면 `unknown command` 가 난다.
//
// 이게 특히 나쁜 이유: 이 프로젝트의 진단은 전부 "무엇을 하라" 로 끝난다.
// health.Check 의 Fix 필드 주석이 그 이유를 적어 뒀다 — "진단만 하고 무엇을 하라는
// 말이 없으면 사용자는 그 경고를 무시하는 법을 배운다." 그런데 하라는 것이
// **존재하지 않으면** 무시하는 법을 더 빨리 배운다.
//
// 주석으로는 못 막는다. 명령을 지우는 사람이 문구까지 뒤질 이유가 없다.

// useLine 은 cobra 의 `Use: "recall <query>"` 에서 명령 이름을 뽑는다.
var useLine = regexp.MustCompile(`Use:\s*"([a-z][a-z-]*)`)

// mention 은 안내 문구 안의 `prior <하위명령>` 을 찾는다.
//
// ASCII 소문자만 본다 — `prior 를`·`prior 가` 같은 한국어 조사가 붙은 자리는
// 명령 이름이 아니다.
var mention = regexp.MustCompile(`prior ([a-z][a-z-]+)`)

func repoFiles(t *testing.T, ext string) []string {
	t.Helper()
	root := repoRoot(t)
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ext) {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// commandNames 는 소스에 선언된 하위명령 이름을 모은다.
//
// 목록을 손으로 적지 않는다 — 손으로 적으면 그것이 다음번에 낡는 자리가 된다.
func commandNames(t *testing.T) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, f := range repoFiles(t, ".go") {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range useLine.FindAllStringSubmatch(string(b), -1) {
			names[m[1]] = true
		}
	}
	delete(names, "prior") // 루트 자신
	return names
}

// ★ 안내 문구가 가리키는 명령은 실제로 있어야 한다.
func TestGuidanceOnlyNamesRealCommands(t *testing.T) {
	cmds := commandNames(t)
	if len(cmds) < 5 {
		t.Fatalf("명령을 못 찾았다 — 스캔이 깨졌다: %v", cmds)
	}

	bad := map[string][]string{} // 명령 → 그것을 말하는 파일들
	for _, f := range repoFiles(t, ".go") {
		if strings.HasSuffix(f, "_test.go") {
			continue // 테스트는 사용자에게 안 보인다
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, lit := range stringLiterals(t, f, b) {
			for _, m := range mention.FindAllStringSubmatch(lit, -1) {
				name := m[1]
				if cmds[name] {
					continue
				}
				rel, _ := filepath.Rel(repoRoot(t), f)
				bad[name] = append(bad[name], rel)
			}
		}
	}
	if len(bad) == 0 {
		return
	}
	var keys []string
	for k := range bad {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sort.Strings(bad[k])
		t.Errorf("`prior %s` 는 없는 명령인데 안내 문구가 가리킨다 — %s",
			k, strings.Join(uniq(bad[k]), " · "))
	}
}

// stringLiterals 는 그 파일의 **문자열 리터럴만** 준다.
//
// **주석은 안 본다.** 주석은 개발자가 읽는 자리이고, 없앤 명령을 과거형으로 적는
// 것은 정당하다 — vault.go 가 "옛 prior index 는 아무 말 없이 색인을 만들었다" 로
// 그때의 사고를 기록한 것이 그렇다. 여기서 막고 싶은 것은 **사용자에게 출력되는
// 문구**가 없는 명령을 치라고 하는 것뿐이다.
func stringLiterals(t *testing.T, path string, src []byte) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
	if err != nil {
		return nil // 파싱 못 하면 빌드가 먼저 죽는다
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			out = append(out, lit.Value)
		}
		return true
	})
	return out
}

func uniq(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
