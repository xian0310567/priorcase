package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/testutil"
)

// instructions 는 최근 결정 덤프가 아니라 행동 계약이다 (스펙 §8). 그래서 검사하는
// 것은 "요약이 실려 있는가" 가 아니라 "언제 무엇을 불러야 하는지가 적혀 있는가" 다.
func TestBuildInstructionsCarriesContract(t *testing.T) {
	c := testutil.VaultConfig(t)
	got, skipped := buildInstructions(store.NewLayout(c))

	if len(skipped) != 0 {
		t.Fatalf("픽스처 볼트에서 건너뛴 노트가 나왔다: %v", skipped)
	}
	for _, want := range []string{"casebook_recall", "casebook_capture", "4건"} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions 에 %q 가 없다:\n%s", want, got)
		}
	}
}

// 결정이 하나도 없어도 계약은 남아야 한다. 첫 결정을 기록하게 만드는 것이
// 빈 볼트에서 이 서버가 할 수 있는 유일한 일이기 때문이다.
func TestBuildInstructionsOnEmptyVault(t *testing.T) {
	c := &config.Config{
		Vault:  t.TempDir(),
		Naming: testutil.VaultConfig(t).Naming,
		Domain: []config.Domain{{Prefix: "alpha", Folder: "alpha"}},
	}
	got, _ := buildInstructions(store.NewLayout(c))

	if !strings.Contains(got, "casebook_capture") {
		t.Errorf("빈 볼트에서 기록 계약이 빠졌다:\n%s", got)
	}
	if strings.Contains(got, "casebook_recall") {
		t.Errorf("결정이 0건인데 회수를 요구한다 — 부를 것이 없다:\n%s", got)
	}
}

// 읽지 못한 노트는 instructions 에서도 알려야 한다. stderr 로 보내면 호스트
// 로그로 가고 에이전트는 영영 모른다 — 세션 진입은 그 사실을 알릴 첫 기회다.
func TestBuildInstructionsReportsSkipped(t *testing.T) {
	c := testutil.VaultConfig(t)
	broken := filepath.Join(c.Vault, "alpha", "decisions", "alpha-결정-깨짐-2026-01-01.md")
	if err := os.WriteFile(broken, []byte("---\ntitle: 구 스키마\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, skipped := buildInstructions(store.NewLayout(c))

	if len(skipped) != 1 {
		t.Fatalf("건너뛴 노트 %d건, 1건이어야 한다", len(skipped))
	}
	if !strings.Contains(got, "1건") {
		t.Errorf("instructions 가 건너뛴 노트를 알리지 않는다:\n%s", got)
	}
}
