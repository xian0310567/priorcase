package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/xian0310567/priorcase/internal/testutil"
)

// ★★★ **`--json` 은 빈 목록을 절대 `null` 로 내지 않는다.**
//
// # 왜 이 시험이 따로 있나
//
// 같은 고장이 **두 번** 났고 둘 다 앱 화면을 통째로 지웠다.
//
//	2026-09-01 ①  show --json 의 summary_history 가 null → 속성 패널이 터짐
//	              → **검은 화면.** 결정을 누르면 아무것도 안 나왔다.
//	2026-09-01 ②  settings --json 의 domains 가 null → 볼트 카드가 터짐
//	              → 볼트를 만들었더니 **화면 절반이 사라짐.** 새로고침해도 그대로.
//
// 매번 그 필드 하나를 고쳤다. 그런데 원인은 필드가 아니라 **구조**다: Go 의 nil
// 슬라이스는 JSON `null` 로 나가고, 빈 목록은 예외가 아니라 **가장 흔한 첫 경험**이다
// (새 볼트에는 프로젝트가 없고, 새 결정에는 요약 이력이 없다). TS 쪽은 배열을
// 가정하므로 `.length` 에서 즉시 터지고, 그 예외는 렌더를 끊는다.
//
// 그래서 필드가 아니라 **계약을 잠근다.** 새 필드가 늘어도 여기서 잡힌다.
//
// # 왜 문자열이 아니라 파싱해서 보나
//
// 중첩된 자리(볼트 안의 domains, 노트 안의 tags)까지 봐야 하는데 문자열 검색으로는
// 어느 키인지 못 짚는다. 못 짚으면 고치는 사람이 어디를 볼지 모른다.

// nullOK 는 **null 이어도 되는 키**다. 배열이 아니라 "값이 없음" 을 뜻하는 자리만
// 여기 온다. 목록에 새로 넣을 때는 그것이 정말 목록이 아닌지 확인해라 —
// 넓히는 순간 이 시험이 무력해진다.
var nullOK = map[string]bool{}

// nullKeys 는 값이 null 인 키를 경로째 모은다.
func nullKeys(prefix string, v any, out *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			if val == nil {
				if !nullOK[k] {
					*out = append(*out, p)
				}
				continue
			}
			nullKeys(p, val, out)
		}
	case []any:
		for i, val := range t {
			nullKeys(fmt.Sprintf("%s[%d]", prefix, i), val, out)
		}
	}
}

func assertNoNulls(t *testing.T, what, raw string) {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("%s: JSON 이 아니다: %v\n%s", what, err, raw)
	}
	var bad []string
	nullKeys("", v, &bad)
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("%s 가 null 을 냈다 — 앱이 순회하다 터진다:\n  %s",
			what, strings.Join(bad, "\n  "))
	}
}

// ★ 새 볼트가 이 계약의 첫 시험대다 — 아직 아무 프로젝트도 안 쓴다.
func TestSettingsJSONHasNoNulls(t *testing.T) {
	cfg := settingsFixtureAt(t)
	if _, errb, err := runSettingsCmd(t, "vault", "add", "회사", "--config", cfg); err != nil {
		t.Fatalf("볼트를 못 만들었다: %v (%s)", err, errb)
	}
	out, errb, err := runSettingsCmd(t, "settings", "--json", "--config", cfg)
	if err != nil {
		t.Fatalf("%v (%s)", err, errb)
	}
	assertNoNulls(t, "settings --json", out)
}

// runJSON 은 임의의 명령을 픽스처 볼트에 대고 돌린다.
//
// **명령마다 따로 조립하지 않는다.** 이 계약은 `--json` 을 내는 **모든** 자리에
// 걸려야 하는데, 명령별로 하네스를 복제하면 새 명령이 늘 때 조용히 빠진다.
func runJSON(t *testing.T, cfg string, args ...string) string {
	t.Helper()
	root := &cobra.Command{Use: "prior"}
	root.PersistentFlags().String("config", "", "")
	root.AddCommand(newSettingsCmd(), newVaultCmd(), newDomainCmd(), newHostsCmd(),
		newListCmd(), newShowCmd())
	var out, errb strings.Builder
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(append(args, "--config", cfg))
	if err := root.Execute(); err != nil {
		t.Fatalf("%v (%s)", err, errb.String())
	}
	return out.String()
}

// ★ 목록도 같은 계약이다. 태그 없는 노트가 첫 시험대다.
func TestListJSONHasNoNulls(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)
	assertNoNulls(t, "list --json", runJSON(t, cfgPath, "list", "--json"))
}

// ★★ show 는 이 고장이 **처음 난 자리**다 (검은 화면). 못을 다시 박는다.
func TestShowJSONHasNoNulls(t *testing.T) {
	cfgPath, _ := testutil.VaultConfigFile(t)
	var rows []struct {
		Stem string `json:"stem"`
	}
	if err := json.Unmarshal([]byte(runJSON(t, cfgPath, "list", "--json")), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("픽스처에 결정이 없다")
	}
	for _, r := range rows {
		assertNoNulls(t, "show --json "+r.Stem, runJSON(t, cfgPath, "show", r.Stem, "--json"))
	}
}
