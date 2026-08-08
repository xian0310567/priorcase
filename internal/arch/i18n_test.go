package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ★★ `go vet` 은 **비상수 포맷 문자열을 검사하지 않는다.**
//
// `fmt.Fprintf(w, lang.T(ko, en), args...)` 로 옮기는 순간, printf 분석기가
// 그 자리를 건너뛴다. 즉 "ko 와 en 의 포맷 동사 개수·순서를 같게 유지하라" 는
// 규약을 지켜 주던 안전망이 **i18n 사이트 전부에서 꺼진다.** 한쪽만 고치면
// 런타임에 `%!d(MISSING)` 이 에이전트 컨텍스트로 나간다.
//
// 이 테스트가 그 자리를 대신한다. 국제화를 하면서 만든 구멍이므로 국제화와
// 같은 무게로 지킨다.
func TestTranslationPairsHaveMatchingVerbs(t *testing.T) {
	root := repoRoot(t)
	var checked int

	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil // 파싱 못 하는 파일은 다른 테스트가 잡는다
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "T" || len(call.Args) != 2 {
				return true
			}
			ko, kok := constString(call.Args[0])
			en, enk := constString(call.Args[1])
			if !kok || !enk {
				// 상수로 못 접는 T 호출은 이 검사가 못 본다. 그런 게 생기면
				// 알려야 한다 — 조용히 넘어가면 검사가 있다고 착각한다.
				t.Errorf("%s: T() 인자가 상수 문자열이 아니라 검사할 수 없다",
					fset.Position(call.Pos()))
				return true
			}
			checked++
			kv, ev := verbs(ko), verbs(en)
			if strings.Join(kv, "") != strings.Join(ev, "") {
				t.Errorf("%s: 포맷 동사가 어긋났다\n  ko %v: %q\n  en %v: %q",
					fset.Position(call.Pos()), kv, trunc(ko), ev, trunc(en))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 검사기가 아무것도 못 찾으면 통과해 버린다 — 그 자체가 회귀다.
	if checked < 30 {
		t.Errorf("T() 쌍을 %d개만 찾았다 — 검사기가 대상을 놓치고 있다", checked)
	}
	t.Logf("T() 쌍 %d개 검사", checked)
}

// verbRe 는 printf 동사다. %% 는 리터럴 퍼센트라 제외한다.
var verbRe = regexp.MustCompile(`%[-+# 0-9.*\[\]]*[a-zA-Z]`)

func verbs(s string) []string {
	s = strings.ReplaceAll(s, "%%", "")
	out := verbRe.FindAllString(s, -1)
	if out == nil {
		return []string{}
	}
	return out
}

// constString 은 문자열 리터럴과 그것들의 + 연결을 접는다.
func constString(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok1 := constString(v.X)
		r, ok2 := constString(v.Y)
		return l + r, ok1 && ok2
	}
	return "", false
}

func trunc(s string) string {
	r := []rune(strings.ReplaceAll(s, "\n", "\\n"))
	if len(r) > 70 {
		return string(r[:70]) + "…"
	}
	return string(r)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for d := wd; d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	t.Fatal("go.mod 를 못 찾았다")
	return ""
}
