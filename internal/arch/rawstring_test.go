package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ★★ raw 문자열 안에 역따옴표를 넣으면 **문자열이 그 자리에서 끊긴다.**
//
// 오늘 두 번 겪었다 — 판별기 지시문과 스타터 설정. 둘 다 긴 한국어 산문이라 편집이
// 잦은데, 마크다운 습관으로 `이렇게` 쓰면 컴파일이 깨진다. 빌드가 잡아 주긴 하지만
// **한 번은 깨진 채로 커밋까지 갔다.**
//
// 이 테스트는 컴파일 오류를 대신 잡는 것이 아니라(그건 빌드가 한다) **긴 raw 문자열을
// 가진 파일을 표시해 두는 것**이다 — 그런 파일을 고칠 때 역따옴표를 조심해야 한다는
// 신호이고, 목록이 늘면 그 자체가 리뷰 대상이다.
func TestLongRawStringsAreKnown(t *testing.T) {
	root := repoRoot(t)
	// 긴 raw 문자열(산문 템플릿)을 가진 파일. 여기 있는 파일을 고칠 때는
	// 역따옴표를 쓰지 마라 — 마크다운 습관이 컴파일을 깬다.
	known := map[string]bool{
		"internal/core/judge/judge.go":         true, // 판별기 지시문
		"internal/adapter/hook/initcmd.go":     true, // 스타터 설정 (ko·en)
		"internal/core/rollup/rollup.go":       true, // 요약 파일 머리말
		"internal/adapter/mcp/instructions.go": true,
	}
	var found []string
	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}
		long := false
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if ok && lit.Kind == token.STRING && strings.HasPrefix(lit.Value, "`") && len(lit.Value) > 400 {
				long = true
			}
			return true
		})
		if long {
			rel, _ := filepath.Rel(root, path)
			found = append(found, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		if !known[f] {
			t.Errorf("%s 에 긴 raw 문자열이 새로 생겼다 — "+
				"그 안에 역따옴표를 쓰면 문자열이 끊긴다. 확인했으면 이 테스트의 known 에 추가하라", f)
		}
	}
}
