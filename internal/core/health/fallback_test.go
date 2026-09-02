package health

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

func writeFallback(t *testing.T, l *store.Layout, stem, domain, summary string) {
	t.Helper()
	p := filepath.Join(l.Vault(), domain, "decisions", stem+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ntype: decision\ndate: 2026-08-28\ndomain: [" + domain + "]\n" +
		"summary: \"" + summary + "\"\nstatus: active\ntags: [decision]\n---\n\n본문\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func clusters(t *testing.T, c *config.Config, l *store.Layout) []FallbackCluster {
	t.Helper()
	notes, _, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	return FallbackClusters(c, l, notes)
}

// ★ 실볼트에서 실제로 잡힌 것을 재현한다: `twincrew` 가 common/ 에만 13건 있었고
// 그 프로젝트에는 도메인이 없었다. 사용자는 이걸 손으로 찾고 있었다.
func TestFallbackClusterFindsProjectName(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	// **요약은 서로 달라야 한다.** 같은 문구를 다섯 번 쓰면 그 문구도 군집으로
	// 잡히는데, 그건 코드가 아니라 픽스처가 만든 것이다 — 실볼트에서는 요약이
	// 전부 다르고 실제로 twincrew·lg 만 잡혔다(오탐 0).
	summaries := []string{
		"twincrew 라우터를 버릴 수 없었다",
		"twincrew 오케스트레이터는 신규 저장소로 간다",
		"twincrew 클라이언트를 다시 만들었다",
		"twincrew 상품명 조회는 사이트맵으로 푼다",
		"twincrew 도구는 서비스가 아니라 능력 단위다",
	}
	if len(summaries) != minFallbackCluster {
		t.Fatalf("픽스처가 임계값(%d)과 안 맞는다", minFallbackCluster)
	}
	for i, sm := range summaries {
		writeFallback(t, l, "common-결정-twincrew-작업"+string(rune('가'+i))+"-2026-08-28",
			"common", sm)
	}
	got := clusters(t, c, l)
	if len(got) != 1 || got[0].Token != "twincrew" {
		t.Fatalf("찾은 것: %+v — twincrew 하나여야 한다", got)
	}
	if got[0].Count != minFallbackCluster {
		t.Errorf("건수 %d, want %d", got[0].Count, minFallbackCluster)
	}
}

// **밖에 한 건이라도 있으면 프로젝트 이름이 아니다.** 도구·개념은 여러 도메인을
// 가로지른다 — 실볼트에서 orca(안 7·밖 12)·shopify(6·16)가 그랬다.
func TestFallbackClusterIgnoresCrossCuttingWord(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	for i, sm := range []string{
		"오르카로 지라를 연다", "오르카로 슬랙을 읽는다", "오르카로 구글챗을 본다",
		"오르카 탭은 작업 뒤 닫는다", "오르카 세션은 로그인을 물려받는다",
		"오르카 다운로드 경로는 안 먹는다", "오르카 스크린샷은 base64 다",
		"오르카 워커는 자동으로 놓는다",
	} {
		writeFallback(t, l, "common-결정-도구사용"+string(rune('가'+i))+"-2026-08-28", "common", sm)
	}
	writeFallback(t, l, "alpha-결정-여기서도쓴다-2026-08-28", "alpha", "오르카로 연다")
	for _, x := range clusters(t, c, l) {
		if x.Token == "오르카" {
			t.Errorf("도구 이름을 프로젝트로 잡았다: %+v", x)
		}
	}
}

func TestFallbackClusterNeedsEnoughNotes(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	for i, sm := range []string{
		"트윈크루 라우터를 남긴다", "트윈크루 배포를 미룬다",
		"트윈크루 스키마를 나눈다", "트윈크루 지표를 컬럼으로 뺀다",
	} {
		writeFallback(t, l, "common-결정-갈래"+string(rune('가'+i))+"-2026-08-28", "common", sm)
	}
	if got := clusters(t, c, l); len(got) != 0 {
		t.Errorf("임계값 미만인데 %+v 를 잡았다 — 잡음이 섞이면 이 경고는 안 읽힌다", got)
	}
}

// 이미 선언된 도메인 이름은 후보가 아니다.
func TestFallbackClusterSkipsDeclaredPrefix(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	for i, sm := range []string{
		"alpha 저장소를 나눈다", "alpha 배포를 앞당긴다", "alpha 스키마를 고친다",
		"alpha 지표를 다시 잰다", "alpha 로그를 줄인다", "alpha 캐시를 끈다",
	} {
		writeFallback(t, l, "common-결정-갈래"+string(rune('가'+i))+"-2026-08-28", "common", sm)
	}
	for _, x := range clusters(t, c, l) {
		if x.Token == "alpha" {
			t.Errorf("이미 선언된 도메인을 후보로 냈다: %+v", x)
		}
	}
}

// ★★ **제안하는 이름이 조사에 잘리면 안 된다.**
//
// 2026-09-02 실볼트에서 물렸다. `젠틀파이`(회사 이름)가 common/ 에 9건 쌓였는데
// doctor 가 제안한 이름은 `젠틀파` 였고, `prior domain split 젠틀파` 를 그대로
// 실행하면 결정 9건이 `젠틀파-결정-…` 으로 개명된다 — **회사 이름 오타가 파일명에
// 영구히 박힌다.**
//
// 원인은 군집 키를 그대로 이름으로 쓴 것이다. 키는 `search.ExtractKeywords` 가 준
// 검색용 토큰이라 한국어 조사가 떨어져 있다(`젠틀파이` → `이`를 조사로 보고 뗀다).
// keywords.go 의 원형 복구 가드는 2글자 미만일 때만 도는데 `젠틀파`는 3글자라
// 빠져나간다 — 그 주석이 예로 든 `파이(→파)` 가 복합어 안에서 일어난 판이다.
//
// **세는 것은 그대로 토큰으로 한다.** 매칭 동작을 바꾸면 임계값 실측(5)이 무너진다.
// 이름만 본문에 실제로 나타난 표기로 고른다.
func TestFallbackClusterProposesSurfaceFormNotStrippedToken(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	for i, s := range []string{
		"젠틀파이 사내 VPN 인증서 한 장을 전 직원이 공유한다",
		"젠틀파이 지라는 에픽 스토리 태스크 3단계로만 쓴다",
		"젠틀파이 서버는 매일 21시 prune 이 돈다",
		"젠틀파이 제품화가 두 번 실패했고 구축운영 순환이 매출 모델이다",
		"젠틀파이 인수 기준일은 문서를 쓴 날이 아니다",
	} {
		writeFallback(t, l, "common-결정-사례"+string(rune('가'+i))+"-2026-08-2"+string(rune('0'+i)), "common", s)
	}

	got := clusters(t, c, l)
	var found *FallbackCluster
	for i := range got {
		if got[i].Count >= 5 {
			found = &got[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("군집을 못 찾았다: %+v", got)
	}
	if found.Name() != "젠틀파이" {
		t.Errorf("제안 이름이 %q — 조사가 떨어져 나갔다. 본문 표기 %q 를 써야 한다",
			found.Name(), "젠틀파이")
	}
}
