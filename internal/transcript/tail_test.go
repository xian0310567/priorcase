package transcript

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 상한보다 작으면 첫 줄이 진짜 첫 줄이다. 버리면 안 된다.
func TestTailKeepsWholeSmallFile(t *testing.T) {
	body := "첫째\n둘째\n셋째\n"
	r, err := Tail(write(t, body), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, r); got != body {
		t.Fatalf("통째로 와야 한다:\n%q", got)
	}
}

// ★ 임의의 자리에서 자르면 첫 줄이 반쪽이다. 그 반쪽을 넘기면 파서가 **깨진 줄**로
// 세는데, 그 수는 체크포인트 전진을 막는 신호이기도 하다 — 정상인 파일이 계속
// 깨진 것으로 보이게 된다.
func TestTailDropsPartialFirstLine(t *testing.T) {
	body := strings.Repeat("x", 100) + "\n" + "온전한줄\n" + "마지막줄\n"
	r, err := Tail(write(t, body), 40)
	if err != nil {
		t.Fatal(err)
	}
	got := read(t, r)
	if strings.Contains(got, "x") {
		t.Fatalf("반쪽 줄이 남았다: %q", got)
	}
	if !strings.Contains(got, "마지막줄") {
		t.Fatalf("끝부분이 없다: %q", got)
	}
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if line == "" {
			t.Fatalf("빈 줄이 생겼다: %q", got)
		}
	}
}

// 상한 안에 개행이 하나도 없으면 온전한 줄이 없다는 뜻이다. 반쪽을 주느니 빈다.
func TestTailGivesNothingWhenNoCompleteLine(t *testing.T) {
	body := strings.Repeat("y", 5000) + "\n"
	r, err := Tail(write(t, body), 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, r); got != "" {
		t.Fatalf("온전한 줄이 없으므로 비어야 한다: %q", got)
	}
}

func TestTailOnMissingFileErrors(t *testing.T) {
	if _, err := Tail(filepath.Join(t.TempDir(), "없다.jsonl"), 100); err == nil {
		t.Fatal("없는 파일인데 에러가 없다 — 호출부가 조용히 빈 맥락으로 돈다")
	}
}
