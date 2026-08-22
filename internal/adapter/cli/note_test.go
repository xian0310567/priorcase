package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/testutil"
)

// worklogPath 는 픽스처 볼트에서 그 도메인의 작업 로그 경로다.
// naming.worklog 는 "99-{project}-작업-로그.md" 이고 project 는 폴더명이다.
func worklogPath(vault, domain string) string {
	return filepath.Join(vault, domain, "99-"+domain+"-작업-로그.md")
}

// TestNoteCmdAppendsToWorklog 는 `prior note` 가 작업 로그에 항목을 덧붙이고,
// **결정 노트는 만들지 않는지** 확인한다. 후자가 이 명령의 존재 이유다 — 두 계층이
// 섞이면 회수의 고정 슬롯을 확정 전의 것이 놓고 다투게 된다.
func TestNoteCmdAppendsToWorklog(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{
		"note", "--config", cfgPath, "--domain", "alpha",
		"--summary", "인덱스 자료구조 셋을 재 봤다", "--date", "2026-08-09",
		"--tag", "측정",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("prior note 실행 실패: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "작업 로그에 남겼다") {
		t.Errorf("어디에 남겼는지 말하지 않는다:\n%s", got)
	}

	path := worklogPath(c.DefaultVaultPath(), "alpha")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("작업 로그가 생기지 않았다 (%s): %v", path, err)
	}
	if !strings.Contains(string(body), "인덱스 자료구조 셋을 재 봤다") {
		t.Errorf("항목이 없다:\n%s", body)
	}
	// 날짜 헤딩이 없으면 `prior rollup` 이 그 주를 통째로 못 본다 (core/rollup 의 weekBlocks).
	if !strings.Contains(string(body), "## 2026-08-09") {
		t.Errorf("날짜 헤딩이 없다:\n%s", body)
	}
	if !strings.Contains(string(body), "#측정") {
		t.Errorf("태그가 안 붙었다:\n%s", body)
	}

	ents, err := os.ReadDir(filepath.Join(c.DefaultVaultPath(), "alpha", "decisions"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.Contains(e.Name(), "인덱스") {
			t.Errorf("note 가 결정 노트를 만들었다: %s", e.Name())
		}
	}
}

// 본문은 capture 와 같은 규약이다 — 파일 경로, `-` 이면 표준입력.
// 두 명령이 같은 플래그를 다르게 읽으면 파일 경로를 본문으로 쓰는 사고가 난다.
func TestNoteCmdReadsBodyFromStdin(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetIn(strings.NewReader("#### 대안\n하드코딩은 배포가 필요해서 기각.\n"))
	root.SetArgs([]string{
		"note", "--config", cfgPath, "--domain", "alpha",
		"--summary", "졸업요건 판정 위치를 검토중", "--body", "-", "--date", "2026-08-09",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("prior note 실행 실패: %v", err)
	}

	body, err := os.ReadFile(worklogPath(c.DefaultVaultPath(), "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "하드코딩은 배포가 필요해서 기각") {
		t.Errorf("표준입력 본문이 안 들어갔다:\n%s", body)
	}
}

// --summary 는 필수다. 제목 없는 항목은 worklog.Append 가 거부하지만, 그 전에
// 플래그 단계에서 막아야 사용자가 무엇을 빠뜨렸는지 바로 안다.
func TestNoteCmdRequiresSummary(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"note", "--config", cfgPath, "--domain", "alpha"})
	if err := root.Execute(); err == nil {
		t.Fatal("--summary 없이 prior note 가 성공했다")
	}
}

// **--domain 은 필수가 아니다.** 셸에서 한 줄 남기는 것이 주 용도인데 매번 도메인을
// 치게 하면 그만큼 안 치게 된다. 비면 cwd 로 판정하고, 픽스처는 폴백 도메인
// common 을 갖고 있으므로 어느 디렉토리에서 돌려도 거기로 간다.
func TestNoteCmdFallsBackToCwdDomain(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{
		"note", "--config", cfgPath,
		"--summary", "도메인 없이 남긴다", "--date", "2026-08-09",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("--domain 없이 prior note 가 실패했다: %v", err)
	}

	body, err := os.ReadFile(worklogPath(c.DefaultVaultPath(), "common"))
	if err != nil {
		t.Fatalf("폴백 도메인의 작업 로그가 없다: %v", err)
	}
	if !strings.Contains(string(body), "도메인 없이 남긴다") {
		t.Errorf("항목이 없다:\n%s", body)
	}
}

// `--tags` 를 먼저 치는 것도 자연스럽다. 거기서 "unknown flag" 로 죽으면 한 줄
// 남기려던 사람이 그만둔다 — 문턱을 낮추려고 만든 명령에서 가장 나쁜 실패다.
func TestNoteCmdAcceptsPluralTagsAlias(t *testing.T) {
	cfgPath, c := testutil.VaultConfigFile(t)

	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{
		"note", "--config", cfgPath, "--domain", "alpha",
		"--summary", "별칭도 받는다", "--tags", "측정,대안", "--date", "2026-08-09",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("--tags 를 못 받았다: %v", err)
	}

	body, err := os.ReadFile(worklogPath(c.DefaultVaultPath(), "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#측정", "#대안"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("%s 가 없다:\n%s", want, body)
		}
	}
}
