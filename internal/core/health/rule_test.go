package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// writeRule 은 규칙 노트 하나를 심는다.
func writeRule(t *testing.T, c *config.Config, name, summary, related string) {
	t.Helper()
	dir := filepath.Join(c.DefaultVaultPath(), "_meta", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ntype: rule\ndate: 2026-08-27\n" +
		"summary: \"" + summary + "\"\nstatus: active\noutcome: pending\n" +
		"supersedes: \"\"\nrelated: [" + related + "]\ntags: [rule]\n---\n\n## 규칙\n"
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ★ **doctor 가 이 기능의 유일한 입구다.**
//
// 규칙은 폴더 하나를 만드는 것으로 켜진다. 설정 키도 명령도 없으니 없는 기능은
// 아무도 못 찾는다 — 동의어 표와 같은 문제이고 같은 해법이다. 폴더가 없는 것은
// 고장이 아니므로 경고가 아니라 **사실**로 적고, 경로를 같이 말해 준다.
func TestDoctorTellsWhereRulesGoWhenThereAreNone(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)

	got := find(t, Vault(c, l), "규칙")
	if got.Level != OK {
		t.Errorf("규칙이 없는 것은 고장이 아니다: [%s] %s", got.Level.Mark(), got.Detail)
	}
	if !strings.Contains(got.Detail, l.RulesDirRel()) {
		t.Errorf("어디에 두라는 말이 없다: %s", got.Detail)
	}
}

func TestDoctorCountsRules(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	writeRule(t, c, "규칙-손해가0이면-먼저넣는다", "한쪽 손해가 0 이면 검증보다 먼저 넣는다",
		"\"[[alpha-결정-저장엔진-2026-08-01]]\"")

	got := find(t, Vault(c, l), "규칙")
	if got.Level != OK {
		t.Errorf("정상 규칙인데 경고다: %s", got.Detail)
	}
	if !strings.Contains(got.Detail, "1건") {
		t.Errorf("건수가 안 나온다: %s", got.Detail)
	}
}

// ★★ **근거 없는 규칙은 지워지지도 않고 영원히 남는다.**
//
// 규칙은 결정에서 증류한 것이라 `related` 에 출처가 있어야 한다. 없으면 다음
// 사람이 그것을 신뢰할지 판단할 수 없다 — 동의어 표가 "고칠 때 이유를 남겨라" 로
// 막는 것과 같은 병이다. 규칙은 몇 건뿐이라 하나가 썩으면 비중이 크다.
func TestDoctorSeesRuleWithoutProvenance(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	writeRule(t, c, "규칙-근거없음", "무언가를 하라", "")

	got := find(t, Vault(c, l), "규칙")
	if got.Level == OK {
		t.Errorf("출처 없는 규칙이 있는데 정상이라고 한다: %s", got.Detail)
	}
	if !strings.Contains(got.Fix, "related") {
		t.Errorf("무엇을 하라는 말이 없다: %s", got.Fix)
	}
}

// ★★ **규칙의 값은 짧다는 것이다.**
//
// 요약이 이 길이를 넘으면 회수 점수식의 길이 정규화가 감점을 시작하고(search 의
// refHeadRunes 와 같은 수다), 무엇보다 사건 서술이 섞였다는 뜻이다 — 그러면 다른
// 프로젝트에서 안 걸린다. 규칙을 만든 이유가 그 자리에서 사라진다.
func TestDoctorSeesOverlongRuleSummary(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	writeRule(t, c, "규칙-너무길다",
		strings.Repeat("가", ruleSummaryRunes+1), "\"[[alpha-결정-저장엔진-2026-08-01]]\"")

	got := find(t, Vault(c, l), "규칙")
	if got.Level == OK {
		t.Errorf("요약이 %d자를 넘는 규칙이 있는데 정상이라고 한다: %s", ruleSummaryRunes, got.Detail)
	}
}

// 깨진 규칙은 조용히 빠지면 안 된다 — 규칙 몇 건 중 하나가 안 읽히면 그 판단
// 기준이 전 프로젝트에서 사라진다.
func TestDoctorSeesUnreadableRule(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	dir := filepath.Join(c.DefaultVaultPath(), "_meta", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "깨진규칙.md"),
		[]byte("---\ntype: rule\nsummary: [닫히지 않은\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := find(t, Vault(c, l), "규칙")
	if got.Level == OK {
		t.Errorf("읽지 못한 규칙이 있는데 정상이라고 한다: %s", got.Detail)
	}
}
