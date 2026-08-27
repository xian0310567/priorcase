package health

import (
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// **표 안의 `\|` 를 안 끊으면 멀쩡한 링크가 전부 깨진 것으로 보인다.**
//
// 실측 중에 실제로 이 실수를 했다 — `[[대상\|별칭]]` 이 `대상\` 로 잡혀 후보가
// 68개로 부풀었고, 고치니 28개가 됐다. 오탐 93%로 본문 검사를 포기했던 2026-08-15
// 의 판단도 일부는 이런 순진한 파싱 탓이었을 수 있다.
func TestLinkTargetHandlesEscapedAliasPipe(t *testing.T) {
	for raw, want := range map[string]string{
		`대상`:     "대상",
		`대상|별칭`:  "대상",
		`대상\|별칭`: "대상", // 표 안에서 이스케이프된 파이프
		`대상#헤딩`:  "대상",
		`00-도구-선택-가이드\|01 · 가이드`: "00-도구-선택-가이드",
		`  여백  `: "여백",
	} {
		if got := linkTarget(raw); got != want {
			t.Errorf("linkTarget(%q) = %q, want %q", raw, got, want)
		}
	}
}

// 자리표시자는 낱말 목록이 아니라 **모양**으로 거른다. 목록은 새 자리표시자가
// 생길 때마다 늘어나고, 늘리는 것을 잊으면 조용히 오탐이 된다.
func TestLooksLikeNoteFiltersPlaceholders(t *testing.T) {
	notes := []string{
		"draft00-결정-유통병목은가격이아니라무료표면-2026-08-12", // 표식 + 날짜
		"editup-decision-3줄요약-유실-우리로그-318대20",   // 옛 규약 표식
		"create-결정-서버먼저오픈-oneblock확정",           // 표식만 (날짜 누락)
		"00-결정-색인",                         // 번호 시작
		"14-market-reference-scan-2026-08", // 번호 시작
	}
	for _, v := range notes {
		if !looksLikeNote(v) {
			t.Errorf("노트 이름을 걸렀다: %q", v)
		}
	}
	// 실볼트에서 실제로 나온 자리표시자·문법이다.
	placeholders := []string{
		"X", "wikilink", "위키링크", "source", "basename", "..", "vault", "domain",
		"T-...", "K-...", "벧전 5:7", "옛이름", "새이름", "this", "old", "new",
		"next-dev-build-conflict",
	}
	for _, v := range placeholders {
		if looksLikeNote(v) {
			t.Errorf("자리표시자를 노트로 봤다: %q", v)
		}
	}
}

// 코드 펜스 안의 `[[domain]]` 은 TOML array-of-tables 문법이지 위키링크가 아니다.
func TestBodyLinksIgnoresCodeFences(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	writeDoc(t, l.Vault(), "alpha/샘플.md", "본문\n\n```toml\n[[domain]]\nprefix = \"x\"\n```\n\n[[00-없는-문서-2026-01-01]]\n")

	typo, orphan := scanBodyLinks(l.Vault(), mustStems(t, l))
	all := append(typo, orphan...)
	if len(all) != 1 || all[0].Target != "00-없는-문서-2026-01-01" {
		t.Fatalf("펜스 안을 셌거나 밖을 놓쳤다: %+v", all)
	}
}

// frontmatter 는 checkLinks 의 몫이다 — 두 검사가 같은 링크를 두 번 말하면
// 사람이 어느 쪽을 고칠지 헷갈린다.
func TestBodyLinksSkipsFrontmatter(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	writeDoc(t, l.Vault(), "alpha/샘플2.md",
		"---\nrelated: [\"[[00-없는-것-2026-01-01]]\"]\n---\n\n본문에는 링크가 없다.\n")

	typo, orphan := scanBodyLinks(l.Vault(), mustStems(t, l))
	if len(typo)+len(orphan) != 0 {
		t.Errorf("frontmatter 를 셌다: %+v %+v", typo, orphan)
	}
}

// **두 종류를 갈라야 한다.** 가까운 이름이 있으면 오타이고, 없으면 참조된 기록이
// 애초에 안 쓰인 것이다 — 고칠 대상이 다르다.
func TestBodyLinksSeparatesTypoFromOrphan(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	// alpha-결정-저장엔진-2026-08-01 이 픽스처에 있다.
	writeDoc(t, l.Vault(), "alpha/샘플3.md",
		"근거 = [[alpha-결정-저장엔지-2026-08-01]].\n\n"+ // 한 글자 오타
			"근거 = [[alpha-결정-완전히다른주제짜장면탕수육-2026-01-01]].\n") // 대상이 없다

	typo, orphan := scanBodyLinks(l.Vault(), mustStems(t, l))
	if len(typo) != 1 || !strings.Contains(typo[0].Suggest, "저장엔진") {
		t.Errorf("오타를 제안과 함께 못 잡았다: %+v", typo)
	}
	if len(orphan) != 1 {
		t.Errorf("고아를 못 갈랐다: %+v", orphan)
	}
}

// 깨끗한 볼트에서는 **사실로** 말한다 (경고가 아니다).
func TestBodyLinksQuietWhenClean(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	if got := find(t, Vault(c, l), "본문 링크"); got.Level != OK {
		t.Errorf("깨끗한데 경고를 냈다: %s", got.Detail)
	}
}

func mustStems(t *testing.T, l *store.Layout) map[string]bool {
	t.Helper()
	s, err := l.AllStems()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// 인라인 코드 안의 `[[X]]` 는 링크가 아니다 — 옵시디언도 그렇게 본다.
// 스키마·규약 문서가 자리표시자를 그렇게 적으므로, 안 거르면 그 문서가 영원히 짖는다.
func TestBodyLinksIgnoresInlineCode(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	writeDoc(t, l.Vault(), "alpha/스키마.md",
		"superseded-by:  # 뒤집혔을 때 `[[새-결정-노트]]` 를 넣는다\n\n진짜 [[alpha-결정-없는것-2026-01-01]]\n")

	typo, orphan := scanBodyLinks(l.Vault(), mustStems(t, l))
	all := append(typo, orphan...)
	if len(all) != 1 || all[0].Target != "alpha-결정-없는것-2026-01-01" {
		t.Fatalf("인라인 코드를 셌거나 진짜를 놓쳤다: %+v", all)
	}
}
