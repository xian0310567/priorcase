package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/index"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// find 는 이름으로 검사 하나를 꺼낸다.
func find(t *testing.T, r *Report, name string) Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("검사 %q 가 없다. 있는 것: %v", name, names(r))
	return Check{}
}

func names(r *Report) []string {
	var out []string
	for _, c := range r.Checks {
		out = append(out, c.Name)
	}
	return out
}

// 픽스처 볼트는 건강해야 한다. 이게 안 되면 나머지 테스트의 기준선이 없다.
func TestHealthyVault(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	if _, err := index.Write(l); err != nil {
		t.Fatal(err)
	}
	r := Vault(c, l)
	for _, ck := range r.Checks {
		if ck.Level != OK {
			t.Errorf("[%s] %s — %s", ck.Level.Mark(), ck.Name, ck.Detail)
		}
	}
	if r.Worst() != OK {
		t.Errorf("Worst = %v, OK 여야 한다", r.Worst())
	}
}

// ★ 이 패키지의 핵심 검사.
//
// 볼트에 결정 폴더가 있는데 설정에 없으면 그 프로젝트의 결정이 **전부** 색인과
// 회수에서 빠진다. 그런데 색인은 정상 생성되고 회수도 에러를 안 낸다 — 없는 것처럼 군다.
// 그래서 이건 Warn 이 아니라 Fail 이다.
func TestUndeclaredDomainIsFail(t *testing.T) {
	c := testutil.VaultConfig(t)
	dir := filepath.Join(c.Vault, "감마", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	note := "---\ntype: decision\ndate: 2026-08-08\ndomain: [감마]\nsummary: \"x\"\n" +
		"status: active\noutcome: pending\nsupersedes: \"\"\nrelated: []\ntags: []\n" +
		"source_session: \"\"\n---\n\n## 결정\n\nx\n"
	if err := os.WriteFile(filepath.Join(dir, "감마-결정-x-2026-08-08.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}

	got := find(t, Vault(c, store.NewLayout(c)), "미선언 도메인")
	if got.Level != Fail {
		t.Errorf("Level = %v, Fail 이어야 한다 — 결정이 통째로 빠지는데 경고로 그치면 안 된다", got.Level)
	}
	if !strings.Contains(got.Detail, "감마") {
		t.Errorf("어느 폴더인지 안 알려 준다: %s", got.Detail)
	}
	if got.Fix == "" {
		t.Error("고치는 법을 안 알려 준다 — 모르면 못 고친다")
	}
}

// 빈 폴더는 알리지 않는다. 결정이 없으면 잃을 것도 없고, 소음만 는다.
func TestEmptyUndeclaredFolderIsNotReported(t *testing.T) {
	c := testutil.VaultConfig(t)
	if err := os.MkdirAll(filepath.Join(c.Vault, "감마", "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := find(t, Vault(c, store.NewLayout(c)), "미선언 도메인"); got.Level != OK {
		t.Errorf("빈 폴더를 알렸다: %s", got.Detail)
	}
}

// 색인이 낡았으면 알아야 한다. **색인이 결정적이라서 가능한 검사다.**
func TestStaleIndexIsDetected(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	if _, err := index.Write(l); err != nil {
		t.Fatal(err)
	}
	// 노트를 하나 더 넣는다 — 색인은 그대로다.
	dir := filepath.Join(c.Vault, "alpha", "decisions")
	note := "---\ntype: decision\ndate: 2026-08-08\ndomain: [alpha]\nsummary: \"새 결정\"\n" +
		"status: active\noutcome: pending\nsupersedes: \"\"\nrelated: []\ntags: []\n" +
		"source_session: \"\"\n---\n\n## 결정\n\nx\n"
	if err := os.WriteFile(filepath.Join(dir, "alpha-결정-새것-2026-08-08.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}

	got := find(t, Vault(c, l), "색인")
	if got.Level != Warn {
		t.Errorf("Level = %v, Warn 이어야 한다 (%s)", got.Level, got.Detail)
	}
	if got.Fix != "prior index" {
		t.Errorf("Fix = %q, \"prior index\" 여야 한다", got.Fix)
	}
}

// 읽지 못한 노트는 Fail 이다 — 회수에서 빠지는 것은 조용한 손실이다.
func TestUnreadableNoteIsFail(t *testing.T) {
	c := testutil.VaultConfig(t)
	broken := filepath.Join(c.Vault, "alpha", "decisions", "alpha-결정-깨짐-2026-01-01.md")
	if err := os.WriteFile(broken, []byte("---\ntitle: 구 스키마\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := find(t, Vault(c, store.NewLayout(c)), "결정 노트")
	if got.Level != Fail {
		t.Errorf("Level = %v, Fail 이어야 한다", got.Level)
	}
	if !strings.Contains(got.Detail, "깨짐") {
		t.Errorf("어느 파일인지 안 알려 준다: %s", got.Detail)
	}
}

// 볼트가 없으면 나머지를 볼 것도 없다.
func TestMissingVaultIsFail(t *testing.T) {
	c := &config.Config{
		Vault:  filepath.Join(t.TempDir(), "없는볼트"),
		Naming: testutil.VaultConfig(t).Naming,
		Domain: []config.Domain{{Prefix: "alpha", Folder: "alpha"}},
	}
	r := Vault(c, store.NewLayout(c))
	if got := find(t, r, "볼트"); got.Level != Fail {
		t.Errorf("Level = %v, Fail 이어야 한다", got.Level)
	}
	if r.Worst() != Fail {
		t.Errorf("Worst = %v, Fail 이어야 한다", r.Worst())
	}
}

// ★ **파싱과 검증은 다르다.** List() 는 frontmatter 가 10키인지만 보고, 접두어와
// domain 첫 값이 같은지·status 가 허용값인지는 안 본다. `prior capture` 는 이걸 검증하지만
// **손으로 쓰면 통째로 우회된다** — 여기가 그 그물이다.
func TestSchemaViolationIsCaught(t *testing.T) {
	c := testutil.VaultConfig(t)
	// 접두어(alpha)와 domain 첫 값(beta)이 어긋난 노트를 손으로 심는다.
	bad := filepath.Join(c.Vault, "alpha", "decisions", "alpha-결정-어긋남-2026-08-08.md")
	body := "---\ntype: decision\ndate: 2026-08-08\ndomain: [beta]\nsummary: \"x\"\n" +
		"status: active\noutcome: pending\nsupersedes: \"\"\nrelated: []\ntags: []\n" +
		"source_session: \"\"\n---\n\n## 결정\n\nx\n"
	if err := os.WriteFile(bad, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	r := Vault(c, store.NewLayout(c))
	if got := find(t, r, "결정 노트"); got.Level != OK {
		t.Errorf("파싱은 됐어야 한다 (검증과 다르다): %s", got.Detail)
	}
	got := find(t, r, "스키마")
	if got.Level != Fail {
		t.Fatalf("Level = %v, Fail 이어야 한다 — 파싱만 보면 이 노트는 정상으로 보인다", got.Level)
	}
	if !strings.Contains(got.Detail, "어긋남") {
		t.Errorf("어느 노트인지 안 알려 준다: %s", got.Detail)
	}
}

// 감사 결함 4 — 하이픈·대소문자만 다른 중복. prior capture 는 거부하지만 손으로 쓰면 우회된다.
func TestSimilarSlugIsCaught(t *testing.T) {
	c := testutil.VaultConfig(t)
	dup := filepath.Join(c.Vault, "alpha", "decisions", "alpha-결정-저장-엔진-2026-08-01.md")
	body := "---\ntype: decision\ndate: 2026-08-01\ndomain: [alpha]\nsummary: \"x\"\n" +
		"status: active\noutcome: pending\nsupersedes: \"\"\nrelated: []\ntags: []\n" +
		"source_session: \"\"\n---\n\n## 결정\n\nx\n"
	if err := os.WriteFile(dup, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// 픽스처의 alpha-결정-저장엔진-2026-08-01 과 하이픈 하나 차이, 같은 날짜다.
	// (날짜가 다르면 capture 도 안 잡는다 — 후속 결정일 수 있기 때문이다.)
	got := find(t, Vault(c, store.NewLayout(c)), "유사 slug")
	if got.Level != Warn {
		t.Fatalf("Level = %v, Warn 이어야 한다 (%s)", got.Level, got.Detail)
	}
	if !strings.Contains(got.Detail, "↔") {
		t.Errorf("무엇과 무엇이 겹치는지 안 보여 준다: %s", got.Detail)
	}
}

func TestRecentDecisionsCounts(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	// 픽스처는 2026-08-01~04 다.
	if got := RecentDecisions(l, time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), 7); got != 4 {
		t.Errorf("RecentDecisions = %d, 4여야 한다", got)
	}
	if got := RecentDecisions(l, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), 7); got != 0 {
		t.Errorf("RecentDecisions = %d, 0이어야 한다 (전부 오래됐다)", got)
	}
}
