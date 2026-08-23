package cli_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/testutil"
)

// capture 는 prior capture 를 돌리고 stdout·stderr 를 준다.
func capture(t *testing.T, bin, cfgPath string, args ...string) (string, string) {
	t.Helper()
	full := append([]string{"--config", cfgPath, "capture"}, args...)
	cmd := exec.Command(bin, full...)
	cmd.Stdin = strings.NewReader("## 결정\n본문\n")
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("capture 실패: %v\nstdout=%s\nstderr=%s", err, out.String(), errb.String())
	}
	return out.String(), errb.String()
}

// ★ **회수에 아무것도 안 더하는 태그를 그 자리에서 알린다.**
//
// 태그의 낱말이 이미 제목이나 요약에 있으면 그 태그는 걸리는 질의를 하나도
// 안 늘린다. 적는 사람은 어휘를 넓혔다고 믿는데 아무 일도 안 일어난다 —
// 조용하다. 실볼트 278건 중 12건(4%)이 그 상태였고 새 낱말은 중앙값 2개였다.
//
// **막지는 않는다.** 노트는 이미 저장됐고, 무엇이 좋은 태그인지는 사람이 정한다.
func TestCaptureWarnsWhenTagsAddNothing(t *testing.T) {
	bin := buildCB(t)
	cfgPath, _ := testutil.VaultConfigFile(t)

	out, errOut := capture(t, bin, cfgPath,
		"--domain", "alpha", "--slug", "캐시전략",
		"--summary", "캐시 전략을 정한다",
		"--tag", "캐시", "--tag", "전략", // 둘 다 제목·요약에 이미 있다
		"--body", "-")

	if !strings.Contains(out, "기록됨") {
		t.Fatalf("기록이 안 됐다:\n%s", out)
	}
	if !strings.Contains(errOut, "회수") {
		t.Errorf("헛도는 태그를 안 알린다:\nstderr=%s", errOut)
	}
	// **stdout 은 오염되면 안 된다** — 다른 명령이 파이프로 읽는다.
	if strings.Contains(out, "회수 어휘") {
		t.Errorf("경고가 stdout 으로 샜다:\n%s", out)
	}
}

// 태그가 새 낱말을 더하면 조용하다.
func TestCaptureQuietWhenTagsWiden(t *testing.T) {
	bin := buildCB(t)
	cfgPath, _ := testutil.VaultConfigFile(t)

	_, errOut := capture(t, bin, cfgPath,
		"--domain", "alpha", "--slug", "캐시전략",
		"--summary", "캐시 전략을 정한다",
		"--tag", "무효화", "--tag", "만료", "--tag", "성능",
		"--body", "-")

	if strings.Contains(errOut, "회수 어휘") {
		t.Errorf("태그가 어휘를 넓혔는데 경고가 떴다:\n%s", errOut)
	}
}
