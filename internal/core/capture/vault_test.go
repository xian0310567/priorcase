package capture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/capture"
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// twoVaults 는 도메인 둘이 서로 다른 볼트를 가리키는 설정을 만든다.
func twoVaults(t *testing.T) (*config.Config, string, string) {
	t.Helper()
	a, b := t.TempDir(), t.TempDir()
	c := &config.Config{
		Vaults: []config.Vault{
			{Name: "personal", Path: a},
			{Name: "work", Path: b},
		},
		DefaultDomain: "alpha",
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-작업-로그.md",
			Index:        "_meta/00-결정-색인.md",
			Rollup:       "98-{project}-작업-로그-요약.md",
		},
		Domain: []config.Domain{
			{Prefix: "alpha", Folder: "alpha", Vault: "personal", Paths: []string{"/tmp/proj/alpha"}},
			{Prefix: "beta", Folder: "beta", Vault: "work", Paths: []string{"/tmp/proj/beta"}},
		},
	}
	return c, a, b
}

// ★★★ **프로젝트마다 지정된 볼트에 쓰여야 한다.**
//
// 이것이 다중 볼트의 전부다. 그리고 **호출부가 어느 Layout 을 넘기든** 같아야
// 한다 — capture 가 도메인에서 볼트를 스스로 정하기 때문이다. 호출부에 맡기면
// 훅·CLI·MCP 가 서로 다른 볼트에 쓰고, 그 어긋남은 파일이 엉뚱한 자리에 생긴
// 뒤에야 드러난다.
func TestCaptureWritesToDomainVault(t *testing.T) {
	c, personal, work := twoVaults(t)

	// **일부러 틀린 Layout 을 넘긴다.** personal 볼트의 Layout 으로 beta(=work)
	// 결정을 쓴다 — capture 가 바로잡아야 한다.
	wrong := store.NewLayoutFor(c, config.Vault{Name: "personal", Path: personal})

	for _, tc := range []struct{ domain, wantVault, name string }{
		{"alpha", personal, "personal"},
		{"beta", work, "work"},
	} {
		res, err := capture.Do(wrong, c, capture.Request{
			Domain: tc.domain, Slug: "저장엔진", Summary: "SQLite 로 간다",
			Date: "2026-08-13", Body: []byte("## 결정\n\nx\n"),
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.domain, err)
		}
		if !strings.HasPrefix(res.Path, tc.wantVault) {
			t.Errorf("%s 결정이 %s 볼트에 안 갔다:\n  실제: %s\n  기대: %s 아래",
				tc.domain, tc.name, res.Path, tc.wantVault)
		}
		if _, err := os.Stat(res.Path); err != nil {
			t.Errorf("%s: 파일이 없다: %v", tc.domain, err)
		}
	}

	// 서로 다른 볼트에 하나씩 있어야 한다 — 한쪽에 둘 다 가면 안 된다.
	for vault, want := range map[string]string{personal: "alpha", work: "beta"} {
		m, _ := filepath.Glob(filepath.Join(vault, "*", "decisions", "*.md"))
		if len(m) != 1 {
			t.Errorf("%s 볼트에 노트가 %d개다 (1개여야 한다): %v", want, len(m), m)
		}
	}
}

// ★★ **없는 볼트를 가리키면 쓰지 않고 실패해야 한다.**
//
// 기본 볼트로 조용히 떨어지면 그 결정은 사람이 찾을 수 없는 자리에 남는다.
func TestCaptureRefusesUnknownVault(t *testing.T) {
	c, personal, _ := twoVaults(t)
	c.Domain = append(c.Domain, config.Domain{
		Prefix: "gamma", Folder: "gamma", Vault: "없는볼트"})
	l := store.NewLayoutFor(c, config.Vault{Name: "personal", Path: personal})

	_, err := capture.Do(l, c, capture.Request{
		Domain: "gamma", Slug: "x", Summary: "y", Date: "2026-08-13", Body: []byte("## 결정\n\nx\n")})
	if err == nil {
		t.Fatal("없는 볼트를 가리키는데 기록됐다")
	}
	if !strings.Contains(err.Error(), "없는볼트") {
		t.Errorf("어느 볼트가 문제인지 안 알려 준다: %v", err)
	}
	// 기본 볼트로 새지 않았는지 본다.
	if m, _ := filepath.Glob(filepath.Join(personal, "*", "decisions", "*.md")); len(m) != 0 {
		t.Errorf("기본 볼트로 샜다: %v", m)
	}
}
