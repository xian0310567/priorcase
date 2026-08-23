package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/index"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// writeTagged 는 요약과 태그를 골라 결정 노트 하나를 심는다.
func writeTagged(t *testing.T, c *config.Config, name, summary, tags string) {
	t.Helper()
	p := filepath.Join(c.DefaultVaultPath(), "alpha", "decisions", name+".md")
	body := "---\ntype: decision\ndate: 2026-08-20\ndomain: [alpha]\n" +
		"summary: \"" + summary + "\"\nstatus: active\noutcome: pending\n" +
		"supersedes: \"\"\nrelated: []\ntags: [" + tags + "]\nsource_session: \"\"\n---\n\n## 결정\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ★ **회수에 아무것도 안 더하는 태그를 보이게 한다.**
//
// 회수는 stem + summary + tags 를 head 로 합쳐 본다. 태그의 낱말이 이미 제목이나
// 요약에 있으면 걸리는 질의가 똑같다 — 적는 사람은 어휘를 넓혔다고 믿는데 아무
// 일도 안 일어난다. 실볼트 278건 중 12건(4%)이 그 상태였고 새 낱말은 중앙값 2개였다.
func TestDoctorSeesTagsThatAddNothing(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	// 태그가 전부 제목·요약 안에 있다 → 회수 어휘가 하나도 안 넓어졌다.
	writeTagged(t, c, "alpha-결정-캐시전략-2026-08-20", "캐시 전략을 정한다", "캐시, 전략")
	if _, err := index.Write(l); err != nil {
		t.Fatal(err)
	}

	got := find(t, Vault(c, l), "회수 어휘")
	if got.Level == OK {
		t.Errorf("죽은 태그만 달린 노트가 있는데 정상이라고 한다: %s", got.Detail)
	}
	if !strings.Contains(got.Detail, "캐시전략") {
		t.Errorf("어느 노트인지 안 알려 준다: %s", got.Detail)
	}
}

// 태그가 새 낱말을 더하면 조용하다.
func TestDoctorQuietWhenTagsWiden(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	writeTagged(t, c, "alpha-결정-캐시전략-2026-08-20", "캐시 전략을 정한다",
		"무효화, 성능, 메모리, 만료")
	if _, err := index.Write(l); err != nil {
		t.Fatal(err)
	}

	if got := find(t, Vault(c, l), "회수 어휘"); got.Level != OK {
		t.Errorf("태그가 어휘를 넓혔는데 경고가 뜬다: %s", got.Detail)
	}
}

// 태그가 아예 없는 노트는 이 검사의 대상이 아니다 — 규약이 태그를 강제하지 않는다.
// 여기서 걸면 옛 노트 전부가 매번 뜨고, 늘 뜨는 경고는 무시를 가르친다.
func TestDoctorIgnoresNotesWithoutTags(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	writeTagged(t, c, "alpha-결정-태그없음-2026-08-20", "태그가 없는 결정", "")
	if _, err := index.Write(l); err != nil {
		t.Fatal(err)
	}

	if got := find(t, Vault(c, l), "회수 어휘"); got.Level != OK {
		t.Errorf("태그 없는 노트를 걸었다: %s", got.Detail)
	}
}
