package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// runCLI 는 루트 명령을 한 번 돌리고 (stdout, stderr, err) 를 준다.
func runCLI(t *testing.T, stdin io.Reader, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	if stdin != nil {
		root.SetIn(stdin)
	}
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errb.String(), err
}

// writeEditFixture 는 픽스처 볼트와 그것을 가리키는 설정 파일을 준다.
func writeEditFixture(t *testing.T) (string, *config.Config) {
	t.Helper()
	return testutil.VaultConfigFile(t)
}

// ★ **frontmatter 는 바이트 그대로 남아야 한다.**
//
// 거기가 스키마다 — 10키가 회수·색인·번복 사슬을 지탱하고 doctor 가 그것을
// 지키는 검사를 여럿 갖고 있다. 파싱해서 다시 쓰면 키 순서·따옴표·줄바꿈이
// 미묘하게 바뀌고, 볼트가 git 으로 오가므로 그 차이가 전부 diff 로 나와
// **무엇이 진짜 바뀌었는지**를 가린다.
func TestEditPreservesFrontmatterBytes(t *testing.T) {
	cfg, c := writeEditFixture(t)
	path := filepath.Join(c.Vaults[0].Path, "alpha", "decisions", "alpha-결정-저장엔진-2026-08-01.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	head := headOfFile(t, before)

	if _, errb, err := runCLI(t, strings.NewReader("## 새 본문\n\n완전히 다른 글이다.\n"),
		"edit", "alpha-결정-저장엔진-2026-08-01", "--config", cfg); err != nil {
		t.Fatalf("edit: %v\n%s", err, errb)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := headOfFile(t, after); got != head {
		t.Errorf("frontmatter 가 바뀌었다:\n전:\n%s\n후:\n%s", head, got)
	}
	// **본문만 본다.** "임베디드" 는 frontmatter 의 summary 에도 있는데 그건
	// 보존되는 것이 맞다 — 파일 전체에서 찾으면 정상 동작을 실패로 읽는다.
	_, body, err := splitFrontmatter(after)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "완전히 다른 글이다") {
		t.Errorf("본문이 안 바뀌었다:\n%s", body)
	}
	if strings.Contains(string(body), "임베디드") {
		t.Errorf("옛 본문이 남았다:\n%s", body)
	}
}

// **빈 본문은 거부한다.** 파이프가 끊기거나 앱이 빈 문자열을 보내면 결정문이
// 통째로 사라지는데, 그건 되돌릴 수 없다.
func TestEditRefusesEmptyBody(t *testing.T) {
	cfg, _ := writeEditFixture(t)
	for _, in := range []string{"", "   \n\n  "} {
		if _, _, err := runCLI(t, strings.NewReader(in),
			"edit", "alpha-결정-저장엔진-2026-08-01", "--config", cfg); err == nil {
			t.Errorf("빈 본문(%q)인데 통과했다 — 결정문이 지워진다", in)
		}
	}
}

func TestEditRejectsUnknownStem(t *testing.T) {
	cfg, _ := writeEditFixture(t)
	if _, _, err := runCLI(t, strings.NewReader("본문"),
		"edit", "없는-결정-2026-01-01", "--config", cfg); err == nil {
		t.Error("없는 stem 인데 조용히 통과했다")
	}
}

// 고친 뒤에도 그 노트가 그대로 읽혀야 한다 — 파싱이 깨지면 회수에서 빠진다.
func TestEditKeepsNoteReadable(t *testing.T) {
	cfg, _ := writeEditFixture(t)
	if _, errb, err := runCLI(t, strings.NewReader("## 고침\n\n본문을 갈았다.\n"),
		"edit", "alpha-결정-저장엔진-2026-08-01", "--config", cfg); err != nil {
		t.Fatalf("edit: %v\n%s", err, errb)
	}
	out, errb, err := runCLI(t, nil, "show", "alpha-결정-저장엔진-2026-08-01", "--json", "--config", cfg)
	if err != nil {
		t.Fatalf("show: %v\n%s", err, errb)
	}
	// 메타는 살아 있고 본문은 바뀌어 있어야 한다.
	for _, want := range []string{`"status":"active"`, `본문을 갈았다`} {
		if !strings.Contains(out, want) {
			t.Errorf("show 결과에 %q 가 없다:\n%s", want, out)
		}
	}
}

func headOfFile(t *testing.T, raw []byte) string {
	t.Helper()
	h, _, err := splitFrontmatter(raw)
	if err != nil {
		t.Fatalf("frontmatter 를 못 갈랐다: %v", err)
	}
	return string(h)
}
