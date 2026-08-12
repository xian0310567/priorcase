package arch_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 제품명은 2026-08-10 에 casebook 에서 priorcase 로, 명령어는 cb 에서 prior 로 바뀌었다.
//
// **512군데를 손으로 지킬 수는 없다.** 옛 이름은 조용히 되돌아온다 — 옛 커밋에서
// 코드를 복사하거나, 문서를 옛 README 에서 베끼거나, 모델이 학습한 이름을 쓰거나.
// 그리고 되돌아온 옛 이름은 대개 빌드를 깨지 않는다: 문서의 `cb capture` 는 컴파일
// 되지 않고, `CASEBOOK_VAULT` 는 그냥 안 읽히는 환경변수이고, `~/.config/casebook`
// 은 빈 디렉토리를 새로 만든다. 전부 **조용히 틀린다.**
//
// 그래서 검사로 세운다.
//
// 옛 이름을 적어야 하는 곳이 딱 두 종류 있다.
//   - 이관 코드 — 옛 경로에서 새 경로로 옮기려면 옛 이름을 알아야 한다
//   - 이 검사 자신과, 왜 바뀌었는지 설명하는 주석
//
// 그 둘은 아래 allowOldName 에 적는다. 목록에 없는 파일에서 옛 이름이 나오면 실패다.
func TestOldNameDoesNotReturn(t *testing.T) {
	root := repoRoot(t)

	// 옛 이름을 적어도 되는 파일. **경로가 정확히 같아야 한다** — 접두어 일치로
	// 하면 디렉토리 하나가 통째로 뚫린다.
	allowOldName := map[string]string{
		"internal/arch/oldname_test.go":      "이 검사 자신",
		"internal/arch/npm_test.go":          "옛 이름의 조직이 막혔던 사실을 적는다",
		"npm/priorcase/bin/prior.js":         "옛 이름의 조직이 막혔던 사실을 적는다",
		"internal/adapter/hook/init.go":      "개명 전 훅 표시와 백업 이름을 알아본다",
		"internal/adapter/hook/init_test.go": "개명 전 훅·백업을 재현한다",
		"internal/daemon/promote.go":         "개명 때 옮기지 않은 옛 도메인 때문에 이 검사가 생겼다",
		"internal/daemon/promote_test.go":    "개명 때 옮기지 않은 옛 도메인이 남은 사실을 적는다",
	}

	// `cb` 는 흔한 두 글자라 낱말 경계로만 잡는다. 그래도 base64 조각 안에서는
	// 안 걸린다 — 앞뒤가 낱말 문자면 경계가 아니다.
	patterns := []struct {
		re   *regexp.Regexp
		what string
	}{
		{regexp.MustCompile(`casebook`), "옛 제품명 casebook"},
		{regexp.MustCompile(`CASEBOOK_`), "옛 환경변수 접두어 CASEBOOK_"},
		{regexp.MustCompile(`\bcb\b`), "옛 명령어 cb"},
	}

	out, err := exec.Command("git", "-C", root, "ls-files").Output()
	if err != nil {
		t.Skipf("git ls-files 를 못 돌렸다 (레포 밖에서 도는 중일 수 있다): %v", err)
	}

	tracked := map[string]bool{}
	for _, rel := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if rel != "" {
			tracked[rel] = true
		}
	}
	// **없는 파일이 목록에 남으면 그 자리가 조용히 뚫린다.** 파일을 지우거나 옮긴 뒤
	// 나중에 같은 경로가 되살아나면, 검사는 그것을 "허용된 것" 으로 통과시킨다.
	for rel, why := range allowOldName {
		if !tracked[rel] {
			t.Errorf("허용 목록의 %s (%s) 가 레포에 없다 — 목록에서 빼라", rel, why)
		}
	}

	for rel := range tracked {
		if why, ok := allowOldName[rel]; ok {
			// 허용 목록이 낡지 않게 지킨다. 옛 이름이 사라진 파일이 목록에 남아
			// 있으면, 나중에 진짜로 되돌아왔을 때 그 파일만 조용히 통과한다.
			if !hasAny(t, filepath.Join(root, rel), patterns) {
				t.Errorf("%s 는 허용 목록에 있는데(%s) 옛 이름이 없다 — 목록에서 빼라", rel, why)
			}
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue // 심링크나 방금 지운 파일
		}
		for _, p := range patterns {
			if loc := p.re.FindIndex(b); loc != nil {
				t.Errorf("%s: %s 가 돌아왔다 — %q\n"+
					"  옛 이름은 빌드를 깨지 않고 조용히 틀린다. 새 이름(priorcase·PRIORCASE_·prior)을 써라.\n"+
					"  이관 코드처럼 정말 옛 이름이 필요하면 이 검사의 allowOldName 에 이유와 함께 적어라.",
					rel, p.what, snippet(b, loc[0]))
				break // 파일당 한 번만 — 문서 하나가 수십 줄을 쏟지 않게
			}
		}
	}
}

func hasAny(t *testing.T, path string, patterns []struct {
	re   *regexp.Regexp
	what string
}) bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, p := range patterns {
		if p.re.Find(b) != nil {
			return true
		}
	}
	return false
}

// snippet 은 걸린 자리 앞뒤를 조금 보여 준다. 줄 번호만으로는 문서에서 찾기 어렵다.
func snippet(b []byte, at int) string {
	lo := at - 30
	if lo < 0 {
		lo = 0
	}
	hi := at + 40
	if hi > len(b) {
		hi = len(b)
	}
	return strings.ReplaceAll(string(b[lo:hi]), "\n", " ")
}
