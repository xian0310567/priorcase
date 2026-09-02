package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// ── 콜드스타트: 볼트 이력이 없는 프로젝트 ────────────────────────────────
//
// # 고치려는 고장 (2026-09-02 실측)
//
// 새 프로젝트 `~/project/cosbot` 에서 "지라·슬랙·구글챗에서 내용을 확인해줘" 라고
// 물었을 때 어시스턴트가 **"지라·슬랙·구글챗 MCP 도구가 이 세션에 붙어 있지
// 않습니다"** 라고 답했다. 볼트에는 "orca 브라우저로 접근한다" 가 결정 8건으로
// 들어 있었는데도 그랬다.
//
// 그때 SessionStart 가 실은 것을 그대로 재 봤다:
//
//	고정 계약부      545자
//	최근 결정 5건  1,057자  ← EWS·식약처·novels·VPN. **cosbot 관련 0건**
//
// 그리고 규칙은 **한 건도 안 실렸다.** "회수가 `[규칙]` 로 주는 11건은 지켜야 하는
// 제약이다" 라고 **설명만 하고 그 11건을 안 준다.**
//
// 이력이 있는 프로젝트는 UserPromptSubmit 회수가 받쳐 준다. 콜드스타트는 받쳐 주는
// 것이 없다 — 걸릴 어휘 자체가 아직 볼트에 없기 때문이다. 랭킹을 고쳐도 안 닿는다.
//
// # 왜 최근 결정을 규칙으로 바꾸는가
//
// **최신성은 관련성과 무관하다.** 위 실측이 그것이다(5건 중 0건 관련). 반면 규칙은
// 도메인이 없어 **정의상 어느 프로젝트에서나 유효하다** — 콜드스타트에 실을 것이
// 있다면 그것뿐이다.
//
// 예산도 맞는다: 실볼트 규칙 11건 요약 합계가 1,103자로 최근 결정 5건(1,057자)과
// 거의 같다. **교체는 예산 중립이다.**

func coldCfg(t *testing.T) *config.Config {
	t.Helper()
	c := cfg(t)
	l := store.NewLayout(c)
	// 길이가 다른 규칙 셋. 짧은 것부터 채우는지 보려고 일부러 길이를 벌린다.
	writeHookRule(t, l, "규칙-짧다", "브라우저는 오르카로 연다", "active")
	writeHookRule(t, l, "규칙-중간", "밖으로 나가는 행동은 매번 문안을 보여주고 승인을 받는다", "active")
	writeHookRule(t, l, "규칙-길다", strings.Repeat("가", 300), "active")
	// 뒤집힌 규칙은 실리면 안 된다 — 세션당 한 번 실리고 갱신되지 않아 오래 산다.
	writeHookRule(t, l, "규칙-뒤집힘", "이건 뒤집혔다", "superseded")
	return c
}

func writeHookRule(t *testing.T, l *store.Layout, stem, summary, status string) {
	t.Helper()
	p := filepath.Join(l.RulesDir(), stem+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ntype: rule\nsummary: \"" + summary + "\"\nstatus: " + status + "\n---\n\n본문\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// **콜드스타트에는 규칙을 싣는다.** 이것이 cosbot 사고의 직접 수정이다.
func TestSessionStartColdProjectShipsRules(t *testing.T) {
	// /tmp/proj/새것 은 어느 domain paths 에도 없다 = 볼트 이력이 없다.
	r := runHook(t, coldCfg(t), t.TempDir(), EventSessionStart, Input{Cwd: "/tmp/proj/새것"})

	if !strings.Contains(r.out, "브라우저는 오르카로 연다") {
		t.Errorf("콜드스타트인데 규칙을 안 실었다:\n%s", r.out)
	}
	if !strings.Contains(r.out, "밖으로 나가는 행동은 매번") {
		t.Errorf("콜드스타트인데 규칙을 다 안 실었다:\n%s", r.out)
	}
	// 최근 결정 **블록**은 자리를 내준다 — 관련성이 0으로 실측된 자리다.
	// 머리글로 본다: 꼬리 안내문에 "최근 결정" 이라는 말 자체는 나온다.
	if strings.Contains(r.out, "### 최근 결정") {
		t.Errorf("콜드스타트에 최근 결정 블록이 아직 실린다:\n%s", r.out)
	}
	if strings.Contains(r.out, "이건 뒤집혔다") {
		t.Errorf("뒤집힌 규칙이 실렸다:\n%s", r.out)
	}
}

// **이력이 있는 프로젝트는 지금 그대로다.** 최근 결정 5건이 자리별로 고르게 값을
// 하는 것이 실측(1.00·1.00·0.81·0.88·0.75)이라 바꿀 근거가 없다. 그리고 웜스타트는
// UserPromptSubmit 이 규칙 슬롯 2칸을 이미 주므로 여기서 또 실으면 중복이다.
func TestSessionStartWarmProjectKeepsRecentDecisions(t *testing.T) {
	r := runHook(t, coldCfg(t), t.TempDir(), EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})

	if !strings.Contains(r.out, "최근 결정") {
		t.Errorf("이력이 있는데 최근 결정이 빠졌다:\n%s", r.out)
	}
	if strings.Contains(r.out, "브라우저는 오르카로 연다") {
		t.Errorf("웜스타트에 규칙 본문이 실렸다 — UPS 규칙 슬롯과 중복이다:\n%s", r.out)
	}
}

// **예산은 글자 수로 잡는다.** 건수 상한은 요약이 길어지면 조용히 드리프트한다 —
// BM25 길이 정규화(search.go)가 이미 같은 문제를 발견했다.
//
// 그리고 넘칠 때는 **짧은 것부터 채운다.** 최신순은 콜드스타트에서 값이 0인 것이
// 실측됐고(위 §), 긴 요약은 사건 서술이 섞였다는 뜻이다(rules README 의 200자 상한).
// 같은 예산에 더 많이 실리는 쪽이 낫다.
func TestSessionStartColdRulesStayInBudgetShortestFirst(t *testing.T) {
	c := cfg(t)
	l := store.NewLayout(c)
	// 예산을 확실히 넘긴다: 400자짜리 넷 = 1,600자 > coldVariableBudget
	for _, n := range []string{"가", "나", "다", "라"} {
		writeHookRule(t, l, "규칙-"+n+"길다", strings.Repeat(n, 400), "active")
	}
	writeHookRule(t, l, "규칙-짧다", "짧은 규칙은 먼저 실린다", "active")

	r := runHook(t, c, t.TempDir(), EventSessionStart, Input{Cwd: "/tmp/proj/새것"})

	if !strings.Contains(r.out, "짧은 규칙은 먼저 실린다") {
		t.Errorf("짧은 규칙이 긴 것에 밀렸다:\n%s", r.out)
	}
	if n := len([]rune(r.out)); n > coldBudget {
		t.Errorf("콜드스타트가 예산 %d자를 넘었다: %d자\n%s", coldBudget, n, r.out)
	}
	// 다 못 실었으면 그 사실을 말해야 한다 — 조용히 자르면 "규칙이 이게 전부" 로 읽힌다.
	if !strings.Contains(r.out, "prior recall") {
		t.Errorf("잘린 규칙을 어디서 보는지 안 알린다:\n%s", r.out)
	}
}

// **실볼트 규모에서는 규칙이 한 건도 안 잘려야 한다.**
//
// 이 테스트는 실제로 난 고장을 박아 둔다. 예산을 1,800자로 뒀을 때 실볼트 규칙 11건
// 중 10건이 실리고 **가장 긴 한 건이 잘렸는데, 그게 하필 이 블록을 만든 이유였다** —
// `규칙-브라우저가-필요하면-orca-CLI-지라슬랙구글챗은-이미-로그인돼있다`(184자).
// cosbot 사고가 그 규칙이 안 실려서 났는데 고치려고 만든 블록이 그것부터 버린 것이다.
//
// 길이는 중요도의 대리 지표가 아니다. 여기서는 **가장 구체적인 규칙이 가장 길었다.**
func TestSessionStartColdShipsLongestRuleAtRealVaultScale(t *testing.T) {
	c := cfg(t)
	l := store.NewLayout(c)
	// 실볼트 분포를 흉내낸다: 요약 중앙 88자 · 11건 · 그중 하나가 상한(184자)에 가깝다.
	for i, n := range []int{78, 78, 85, 87, 87, 88, 90, 91, 108, 127} {
		writeHookRule(t, l, "규칙-보통"+string(rune('가'+i)), strings.Repeat("가", n), "active")
	}
	writeHookRule(t, l, "규칙-가장긺", strings.Repeat("나", 184), "active")

	r := runHook(t, c, t.TempDir(), EventSessionStart, Input{Cwd: "/tmp/proj/새것"})

	if !strings.Contains(r.out, strings.Repeat("나", 184)) {
		t.Errorf("가장 긴 규칙이 잘렸다 — 길이로 중요도를 재면 안 된다:\n%s", r.out)
	}
	if strings.Contains(r.out, "그 밖") {
		t.Errorf("실볼트 규모(11건)에서 잘림이 났다:\n%s", r.out)
	}
	if n := len([]rune(r.out)); n > coldBudget {
		t.Errorf("콜드스타트가 예산 %d자를 넘었다: %d자", coldBudget, n)
	}
}

// 제외 구역에서는 콜드스타트여도 규칙을 안 싣는다 — 그 저장소의 규약을 건드리지 않는다.
func TestSessionStartExcludedColdDirShipsNoRules(t *testing.T) {
	r := runHook(t, coldCfg(t), t.TempDir(), EventSessionStart, Input{Cwd: "/tmp/proj/secret"})
	if strings.Contains(r.out, "브라우저는 오르카로 연다") {
		t.Errorf("제외 구역에 규칙을 실었다:\n%s", r.out)
	}
}

// 규칙이 없는 볼트는 지금과 한 바이트도 달라지지 않는다 — 켜는 것은 파일을 만드는
// 행위 하나다(동의어 표와 같은 설계).
func TestSessionStartColdWithoutRulesFallsBackToRecent(t *testing.T) {
	r := runHook(t, cfg(t), t.TempDir(), EventSessionStart, Input{Cwd: "/tmp/proj/새것"})
	if !strings.Contains(r.out, "최근 결정") {
		t.Errorf("규칙이 없으면 최근 결정으로 돌아가야 한다:\n%s", r.out)
	}
}

// ── 폴백 적체를 세션 진입에서 알린다 ──────────────────────────────────
//
// # 왜 doctor 로는 안 되는가 (2026-09-02 실측)
//
// 폴백 적체 탐지는 2026-08-31 에 들어갔는데 **호출처가 `prior doctor` 하나뿐이고,
// doctor 는 업무 중에 한 번도 안 돈다.** 트랜스크립트 전수로 셌더니 실행 116회가
// 전부 priorcase 저장소 안이었고 editup·novels·cosbot 같은 실제 작업에서는 0회다.
//
// 아무도 안 보는 자리에 있는 탐지는 없는 것과 같다. 사업주가 이걸 그대로 지적했다 —
// "네가 수동으로 도메인을 분리하는건 의미가 없지 않나?"
//
// # 왜 자동으로 옮기지는 않는가
//
// **이름은 기계가 정할 수 없다.** 탐지기는 "이 낱말이 폴백에만 5건 이상 있다" 까지만
// 알고, 그 프로젝트를 사람이 뭐라고 부르는지는 모른다 — 오늘 실제로 `젠틀파이` 를
// `젠틀파` 로 제안했고(조사 절단), 그대로 옮겼으면 회사 이름 오타가 파일명 9개에
// 박혔다. 개명은 볼트 전체의 위키링크까지 건드리고 여러 머신으로 동기화된다.
//
// 그래서 **찾는 것과 들이미는 것은 자동, 이름을 정하고 옮기는 것은 승인**이다.
// 세션 진입에 한 줄이 뜨면 에이전트가 그 자리에서 물어볼 수 있으므로, 사람이
// 기억해서 쳐야 하는 명령은 사라진다.
func TestSessionStartSurfacesFallbackCluster(t *testing.T) {
	c := cfg(t)
	l := store.NewLayout(c)
	// common/ 에만 다섯 건 — 임계값(5)을 넘긴다. 밖에는 이 낱말이 없어야 한다.
	for i, s := range []string{
		"트윈크루 배포 파이프라인을 젠킨스에서 옮긴다",
		"트윈크루 인증 토큰 만료를 서버에서 갱신한다",
		"트윈크루 로그 적재 경로를 바꾼다",
		"트윈크루 스키마 마이그레이션 순서를 고정한다",
		"트윈크루 알림 채널을 하나로 합친다",
	} {
		writeCommonNote(t, l, "common-결정-트윈"+string(rune('가'+i))+"-2026-08-2"+string(rune('0'+i)), s)
	}

	r := runHook(t, c, t.TempDir(), EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})

	if !strings.Contains(r.out, "트윈크루") {
		t.Errorf("폴백에 갇힌 프로젝트를 세션 진입에서 안 알린다:\n%s", r.out)
	}
	if !strings.Contains(r.out, "prior domain split") {
		t.Errorf("옮기는 방법을 안 알려준다:\n%s", r.out)
	}
}

// **쌓인 것이 없으면 한 줄도 안 쓴다.** 이 블록은 매 세션 실리므로, 할 일이 없을 때
// 조용하지 않으면 그 자체가 예산이 되고 며칠이면 안 읽힌다.
func TestSessionStartSaysNothingWithoutFallbackCluster(t *testing.T) {
	r := runHook(t, cfg(t), t.TempDir(), EventSessionStart, Input{Cwd: "/tmp/proj/alpha"})
	if strings.Contains(r.out, "prior domain split") {
		t.Errorf("쌓인 것이 없는데 도메인 분리를 권한다:\n%s", r.out)
	}
}

func writeCommonNote(t *testing.T, l *store.Layout, stem, summary string) {
	t.Helper()
	p := filepath.Join(l.Vault(), "common", "decisions", stem+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ntype: decision\ndate: 2026-08-28\ndomain: [common]\n" +
		"summary: \"" + summary + "\"\nstatus: active\ntags: [decision]\n---\n\n본문\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
