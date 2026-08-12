package search_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/search"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// ★★ **대조는 판정이 아니다.**
//
// 회수는 언제나 무언가를 돌려준다. 실측에서 **일치가 없는 발췌도 1위 54점**을 받았고,
// 진짜 일치인 다른 발췌는 65점이었다 — 절대 점수로는 못 가른다. 그래서 Similar 는
// "이미 기록됨" 같은 판정을 내리지 않고 점수 붙은 후보만 준다.
//
// 이 테스트는 그 계약을 지킨다: 관련 없는 글에도 결과가 나올 수 있고, 그것이 결함이
// 아니라는 것.
func TestSimilarReturnsCandidatesNotVerdicts(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)

	hits, _, err := search.Similar(l, c, "저장 엔진을 어느 것으로 할지 정했다. 임베디드 DB 로 간다.")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("맞는 노트가 있는데 아무것도 안 나왔다")
	}
	if !strings.Contains(hits[0].Note.Stem, "저장엔진") {
		t.Errorf("1위가 %q — 맞는 노트가 위로 안 왔다", hits[0].Note.Stem)
	}
	// **점수가 실려야 한다.** 앱과 사람이 상대 비교를 할 유일한 재료다.
	if hits[0].Score <= 0 {
		t.Errorf("점수가 %d — 상대 비교를 못 한다", hits[0].Score)
	}
}

// ★ 발췌가 비면 대조할 것이 없다. 볼트를 읽지도 말아야 한다.
func TestSimilarOnEmptyExcerpt(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	hits, _, err := search.Similar(l, c, "   ")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("빈 발췌에 %d건이 나왔다", len(hits))
	}
}

// ★★ **cwd 도메인에 가산점을 주면 안 된다.**
//
// 발췌는 그 구간의 것이지 지금 셸이 있는 자리의 것이 아니다. Cwd 를 넘기면 엉뚱한
// 프로젝트가 위로 올라온다 — 확인 큐는 여러 도메인의 구간을 한 화면에 놓으므로
// 그 오염이 특히 나쁘다.
func TestSimilarDoesNotFavorCwdDomain(t *testing.T) {
	if strings.Contains(readSrc(t, "similar.go"), "Cwd:") {
		t.Error("Similar 가 Cwd 를 넘긴다 — 셸 위치가 대조 순위를 바꾼다")
	}
	if !strings.Contains(readSrc(t, "similar.go"), "CrossProject: true") {
		t.Error("Similar 가 전체를 보지 않는다")
	}
}

func readSrc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ★ **상한을 지켜야 한다.**
//
// 상한이 풀리면 볼트 전체가 발췌마다 딸려 나온다. 확인 큐는 구간을 여러 개 보여
// 주므로, 한 구간이 노트 100건을 끌고 오면 화면이 통째로 못 쓰게 된다.
func TestSimilarRespectsLimit(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)

	// **상한보다 많이 걸려야 상한을 시험할 수 있다.** 픽스처만으로는 3건이 걸리는데
	// 상한도 3이라 어떤 위반도 안 보인다 — 변형 테스트가 그 공허함을 잡았다.
	dir := filepath.Join(c.Vault, "alpha", "decisions")
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("alpha-결정-저장엔진후보%d-2026-08-0%d.md", i, i+1)
		body := fmt.Sprintf("---\ntype: decision\ndate: 2026-08-0%d\ndomain: [alpha]\n"+
			"summary: \"저장 엔진 후보 %d 을 검토했다\"\nstatus: active\noutcome: pending\n---\n\n## 결정\n\nx\n",
			i+1, i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	hits, _, err := search.Similar(l, c, "저장 엔진을 어느 것으로 할지 정했다")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > search.SimilarLimit {
		t.Errorf("%d건이 나왔다 — 상한 %d 을 넘으면 화면이 넘친다",
			len(hits), search.SimilarLimit)
	}
}
