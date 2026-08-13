package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
)

func testLayout(t *testing.T) (*Layout, string) {
	t.Helper()
	vault := t.TempDir()
	c := &config.Config{
		Vaults: []config.Vault{{Name: config.DefaultVaultName, Path: vault}},
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Index:        "_meta/00-결정-색인.md",
		},
		Domain: []config.Domain{
			{Prefix: "omni", Folder: "omni"},
			{Prefix: "common", Folder: "common"},
		},
	}
	return NewLayout(c), vault
}

func TestDecisionPath(t *testing.T) {
	l, vault := testLayout(t)
	got, err := l.DecisionPath("omni", "저장엔진 OPFS", "2026-08-07")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(vault, "omni", "decisions", "omni-결정-저장엔진-OPFS-2026-08-07.md")
	if got != want {
		t.Errorf("DecisionPath() = %q,\nwant %q", got, want)
	}
}

func TestDecisionPathRejectsUnknownPrefix(t *testing.T) {
	l, _ := testLayout(t)
	if _, err := l.DecisionPath("없는도메인", "x", "2026-08-07"); err == nil {
		t.Fatal("알 수 없는 접두어를 통과시켰다")
	}
}

func TestResolveStemRejectsTraversal(t *testing.T) {
	l, _ := testLayout(t)
	bad := []string{
		"../CLAUDE",
		"omni-결정-a-2026-08-01/../../../CLAUDE",
		"/etc/passwd",
		"..",
		"",
		"규약에-맞지-않는-이름",          // -결정- 이 없다
		"없는도메인-결정-x-2026-08-01", // 알 수 없는 접두어
	}
	for _, s := range bad {
		if p, err := l.ResolveStem(s); err == nil {
			t.Errorf("ResolveStem(%q) 가 통과했다 → %q", s, p)
		}
	}
}

func TestResolveStemNFD(t *testing.T) {
	l, vault := testLayout(t)
	// NFD 로 들어온 stem 도 NFC 경로로 해석돼야 한다 (tar 복원 시나리오).
	// NFD 부분은 코드포인트 이스케이프로 쓴다 — 직접 쓰면 도구가 NFC 로 정규화한다.
	nfdStem := "omni-\u1100\u1167\u11af\u110c\u1165\u11bc-\u1112\u1161\u11ab-2026-08-01"
	nfcStem := "omni-결정-한-2026-08-01"
	if nfdStem == nfcStem {
		t.Fatal("NFD 리터럴이 NFC 와 같다 — 테스트가 무의미하다")
	}
	got, err := l.ResolveStem(nfdStem)
	if err != nil {
		t.Fatalf("NFD stem 을 거부했다: %v", err)
	}
	want := filepath.Join(vault, "omni", "decisions", nfcStem+".md")
	if got != want {
		t.Errorf("ResolveStem(NFD) = %q,\nwant %q", got, want)
	}
	if strings.ContainsRune(got, '\u1100') {
		t.Error("경로에 NFD 자모가 남아 있다")
	}
}

// TestDecisionMarkerRoundTrip 은 decisionMarker 상수("-결정-")가 설정의
// decision_file 템플릿과 어긋나지 않는지를 블랙박스로 확인한다: DecisionPath 로
// 만든 파일명의 stem 을 PrefixOf 로 되읽었을 때 원래 접두어가 복원돼야 한다.
// 템플릿에 마커가 없으면 이 왕복이 깨진다.
func TestDecisionMarkerRoundTrip(t *testing.T) {
	l, _ := testLayout(t)
	got, err := l.DecisionPath("omni", "저장엔진 OPFS", "2026-08-07")
	if err != nil {
		t.Fatal(err)
	}
	stem := strings.TrimSuffix(filepath.Base(got), ".md")
	if prefix := l.PrefixOf(stem); prefix != "omni" {
		t.Errorf("PrefixOf(%q) = %q, want %q", stem, prefix, "omni")
	}
}

func TestPrefixOf(t *testing.T) {
	l, _ := testLayout(t)
	if got := l.PrefixOf("omni-결정-저장엔진-2026-08-01"); got != "omni" {
		t.Errorf("PrefixOf() = %q", got)
	}
	if got := l.PrefixOf("규약없음"); got != "" {
		t.Errorf("PrefixOf(비규약) = %q, want 빈 문자열", got)
	}
}
