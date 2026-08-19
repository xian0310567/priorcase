package worklog

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/rollup"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

func layout(t *testing.T) *store.Layout {
	t.Helper()
	return store.NewLayout(testutil.VaultConfig(t))
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAppendCreatesFileWithHeaderAndEntry(t *testing.T) {
	l := layout(t)
	res, err := Append(l, Entry{
		Domain: "alpha", Date: "2026-08-19", Time: "14:20",
		Title: "옵션 A·B·C·D 비교 후 B 기각",
		Body:  "#### 고려한 대안\n- B: 스토어 심사 경로가 통제 밖",
		Tags:  []string{"프롬프트", "기각"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped {
		t.Error("첫 항목인데 건너뛰었다")
	}
	body := read(t, res.Path)
	for _, want := range []string{
		"type: worklog",
		"## 2026-08-19",
		"### 14:20 · 옵션 A·B·C·D 비교 후 B 기각",
		"스토어 심사 경로가 통제 밖",
		"#프롬프트",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("작업 로그에 %q 가 없다:\n%s", want, body)
		}
	}
}

// ★ **형식의 정본은 core/rollup 이다.**
//
// rollup 의 weekBlocks 는 `## YYYY-MM-DD` 를 경계로 주를 쪼개고 다음 `## ` 에서 끊는다.
// 이 형식을 벗어나면 `prior rollup` 이 그 주를 통째로 못 본다 — 그리고 그 사실은
// 몇 주 뒤 요약을 돌릴 때야 드러난다. 그래서 실제 rollup 을 태워서 확인한다.
//
// 항목 제목이 `###` 인 것도 여기 걸린다. `##` 로 쓰면 rollup 이 항목마다 주 블록을 끊는다.
func TestAppendedEntriesAreVisibleToRollup(t *testing.T) {
	c := testutil.VaultConfig(t)
	c.Naming.Rollup = "98-{project}-요약.md"
	l := store.NewLayout(c)

	for _, e := range []Entry{
		{Domain: "alpha", Date: "2026-08-17", Time: "09:00", Title: "첫날", Body: strings.Repeat("가", 80)},
		{Domain: "alpha", Date: "2026-08-19", Time: "14:20", Title: "사흘 뒤", Body: strings.Repeat("나", 80)},
	} {
		if _, err := Append(l, e); err != nil {
			t.Fatal(err)
		}
	}

	// 2026-08-17 은 월요일이라 두 날짜가 같은 ISO 주(2026-W34)에 든다.
	weeks, err := rollup.Scan(l, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var got *rollup.Week
	for i := range weeks {
		if weeks[i].Prefix == "alpha" {
			got = &weeks[i]
		}
	}
	if got == nil {
		t.Fatalf("rollup 이 작업 로그를 못 봤다 — 형식 계약이 깨졌다. weeks=%+v", weeks)
	}
	if !got.Todo() {
		t.Errorf("요약 대상이어야 한다: %+v", got)
	}

	block, err := rollup.Block(l, "alpha", got.Week)
	if err != nil {
		t.Fatal(err)
	}
	// 두 항목이 한 블록에 다 들어와야 한다. `###` 이 아니라 `##` 로 썼다면
	// 여기서 뒤 항목이 잘려 나간다.
	for _, want := range []string{"첫날", "사흘 뒤"} {
		if !strings.Contains(block, want) {
			t.Errorf("주 블록에 %q 가 없다:\n%s", want, block)
		}
	}
}

// ★ **본문의 `##` 는 rollup 의 주 블록을 끊는다.**
//
// 실측으로 물렸다: 스키마 설명과 판별기 지시문 양쪽에 "절 제목은 #### 이하" 를
// 적어 뒀는데도, 실제 판별기가 낸 첫 작업 로그 항목이 `## 검토한 대안` 을 썼다.
// 본문을 쓰는 것은 LLM 이고 우리는 그 출력을 통제하지 못한다 — 지시가 아니라
// 쓰는 자리에서 막아야 한다.
//
// 안 막으면 그 항목의 본문이 통째로 주 블록 밖으로 떨어져 `prior rollup` 이 못 본다.
func TestBodyHeadingsAreDemotedBelowEntry(t *testing.T) {
	c := testutil.VaultConfig(t)
	c.Naming.Rollup = "98-{project}-요약.md"
	l := store.NewLayout(c)

	res, err := Append(l, Entry{Domain: "alpha", Date: "2026-08-17", Time: "09:00",
		Title: "저장소 검토",
		Body: "# 큰 제목\n## 검토한 대안\nS3 와 R2.\n### 기각\nR2 는 보안팀 미승인.\n" +
			"#### 이미 깊다\n#해시태그는 헤딩이 아니다\n" +
			"```sh\n# 셸 주석은 건드리지 않는다\necho hi\n```\n"})
	if err != nil {
		t.Fatal(err)
	}
	body := read(t, res.Path)

	// 날짜 헤딩 말고 `## ` 가 하나라도 남으면 rollup 이 거기서 끊는다.
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## ") && line != "## 2026-08-17" {
			t.Errorf("본문에 `## ` 가 남았다 — rollup 이 여기서 주 블록을 끊는다: %q", line)
		}
	}
	for _, want := range []string{"#### 큰 제목", "#### 검토한 대안", "#### 기각", "#### 이미 깊다"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q 로 내려가지 않았다:\n%s", want, body)
		}
	}
	// 해시태그와 코드 펜스 안은 건드리지 않는다.
	if !strings.Contains(body, "\n#해시태그는 헤딩이 아니다") {
		t.Error("해시태그를 헤딩으로 오해했다")
	}
	if !strings.Contains(body, "\n# 셸 주석은 건드리지 않는다") {
		t.Error("코드 펜스 안의 주석을 헤딩으로 바꿨다 — 코드가 깨진다")
	}

	// 그리고 실제로 rollup 이 본문 전체를 본다.
	weeks, err := rollup.Scan(l, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(weeks) == 0 {
		t.Fatalf("rollup 이 못 봤다: %v %+v", err, weeks)
	}
	block, err := rollup.Block(l, "alpha", weeks[0].Week)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(block, "R2 는 보안팀 미승인") {
		t.Errorf("주 블록이 본문 도중에 끊겼다:\n%s", block)
	}
}

// 같은 날에 여러 번 쓰면 날짜 헤딩은 하나여야 한다. 매번 열면 옵시디언에서
// 하루가 여러 덩어리로 갈라져 읽기 나쁘다.
func TestSameDayReusesHeading(t *testing.T) {
	l := layout(t)
	var path string
	for i, title := range []string{"첫째", "둘째", "셋째"} {
		res, err := Append(l, Entry{Domain: "alpha", Date: "2026-08-19",
			Time: "0" + string(rune('1'+i)) + ":00", Title: title})
		if err != nil {
			t.Fatal(err)
		}
		path = res.Path
	}
	body := read(t, path)
	if n := strings.Count(body, "## 2026-08-19"); n != 1 {
		t.Errorf("날짜 헤딩이 %d개다, 1개여야 한다:\n%s", n, body)
	}
	if n := strings.Count(body, "### "); n != 3 {
		t.Errorf("항목이 %d개다, 3개여야 한다", n)
	}
}

// 날이 바뀌면 새 헤딩을 연다.
func TestNewDayOpensNewHeading(t *testing.T) {
	l := layout(t)
	if _, err := Append(l, Entry{Domain: "alpha", Date: "2026-08-18", Title: "어제"}); err != nil {
		t.Fatal(err)
	}
	res, err := Append(l, Entry{Domain: "alpha", Date: "2026-08-19", Title: "오늘"})
	if err != nil {
		t.Fatal(err)
	}
	body := read(t, res.Path)
	for _, want := range []string{"## 2026-08-18", "## 2026-08-19"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q 가 없다:\n%s", want, body)
		}
	}
}

// ★ **같은 구간을 두 번 쌓지 않는다.**
//
// 데몬은 같은 구간을 여러 번 훑을 수 있다 (재기동·재시도·훅과 데몬의 중복).
// 그때마다 항목이 붙으면 작업 로그가 같은 말을 반복하고, 그건 문턱을 낮춘 대가로
// 나올 수 있는 가장 흔한 실패다.
func TestSameSourceIsAppendedOnce(t *testing.T) {
	l := layout(t)
	e := Entry{Domain: "alpha", Date: "2026-08-19", Title: "한 번만",
		Source: "/t/a.jsonl@1234"}
	first, err := Append(l, e)
	if err != nil {
		t.Fatal(err)
	}
	if first.Skipped {
		t.Fatal("첫 번째가 건너뛰었다")
	}
	second, err := Append(l, e)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Skipped {
		t.Error("같은 구간을 두 번 쌓았다")
	}
	if n := strings.Count(read(t, first.Path), "한 번만"); n != 1 {
		t.Errorf("항목이 %d번 나온다", n)
	}
}

// Source 가 없으면 중복 검사를 건너뛴다 — 사람이 직접 쓰는 경우다.
func TestNoSourceMeansNoDedup(t *testing.T) {
	l := layout(t)
	e := Entry{Domain: "alpha", Date: "2026-08-19", Title: "손으로 두 번"}
	for i := 0; i < 2; i++ {
		res, err := Append(l, e)
		if err != nil {
			t.Fatal(err)
		}
		if res.Skipped {
			t.Fatal("Source 가 없는데 건너뛰었다")
		}
	}
}

// 제목은 `###` 헤딩이 되므로 줄바꿈이 들어가면 파일 구조가 깨진다.
func TestTitleRejectsNewlines(t *testing.T) {
	l := layout(t)
	if _, err := Append(l, Entry{Domain: "alpha", Title: "앞\n뒤"}); err == nil {
		t.Error("줄바꿈이 든 제목을 받아들였다 — 파일 구조가 깨진다")
	}
	if _, err := Append(l, Entry{Domain: "alpha", Title: "  "}); err == nil {
		t.Error("빈 제목을 받아들였다")
	}
}

func TestBadDateIsRejected(t *testing.T) {
	l := layout(t)
	if _, err := Append(l, Entry{Domain: "alpha", Date: "2026/08/19", Title: "x"}); err == nil {
		t.Error("YYYY-MM-DD 가 아닌 날짜를 받아들였다")
	}
}

// ★ 검색은 제목에 3점, 본문에 1점이다. core/search 의 weightHead·weightBody 와
// 같은 비율이라 두 계층의 결과를 나란히 읽을 수 있다.
//
// **다만 제목 히트가 0이어도 버리지 않는다** — 결정 노트와 다른 점이다.
// 작업 로그의 제목은 한 줄이고 근거는 본문에 있으므로, 본문만 걸리는 것이 정상이다.
func TestSearchFindsBodyOnlyHits(t *testing.T) {
	l := layout(t)
	if _, err := Append(l, Entry{Domain: "alpha", Date: "2026-08-19",
		Title: "배포 경로 검토", Body: "스토어 심사가 통제 밖이다"}); err != nil {
		t.Fatal(err)
	}
	hits, err := Search(l, []string{"심사"}, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("본문에만 있는 낱말로 못 찾았다: %+v", hits)
	}
	if hits[0].Title != "배포 경로 검토" || hits[0].Domain != "alpha" {
		t.Errorf("결과가 이상하다: %+v", hits[0])
	}

	// 제목 히트가 본문 히트보다 무겁다.
	if _, err := Append(l, Entry{Domain: "beta", Date: "2026-08-19",
		Title: "심사 대응", Body: "관계없는 본문"}); err != nil {
		t.Fatal(err)
	}
	hits, err = Search(l, []string{"심사"}, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("둘 다 걸려야 한다: %+v", hits)
	}
	if hits[0].Title != "심사 대응" {
		t.Errorf("제목 히트가 앞이어야 한다: %+v", hits)
	}
}

// ★ **`--cross-project=false` 는 작업 로그에도 걸려야 한다.**
//
// 처음에는 안 걸렸고, 그래서 그 플래그가 반쪽만 지켜졌다 — 결정 노트는
// core/search 의 filterByDomain 이 좁혔는데 작업 로그는 전 도메인이 나왔다.
// alpha 에서 좁혀 물었는데 beta 의 진행 중 메모가 딸려 나오는 것을 실제로 재현했다.
//
// 그리고 **core/search 와 달리 넓히지 않는다.** 좁힌 결과가 비어도 전체로 안 간다.
// 확정된 결정은 남의 도메인 것도 읽을 값어치가 있지만, 남의 프로젝트에서 진행
// 중인 검토는 이 대화에 끼어들 이유가 없다.
func TestSearchHonorsScope(t *testing.T) {
	l := layout(t)
	for _, e := range []Entry{
		{Domain: "alpha", Date: "2026-08-19", Title: "알파의 rocksdb 검토"},
		{Domain: "beta", Date: "2026-08-19", Title: "베타의 rocksdb 검토"},
	} {
		if _, err := Append(l, e); err != nil {
			t.Fatal(err)
		}
	}

	all, err := Search(l, []string{"rocksdb"}, "", 5)
	if err != nil || len(all) != 2 {
		t.Fatalf("scope 가 비면 전부 봐야 한다: %+v (%v)", all, err)
	}

	only, err := Search(l, []string{"rocksdb"}, "alpha", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || only[0].Domain != "alpha" {
		t.Errorf("alpha 로 좁혔는데 %d건이 나왔다: %+v", len(only), only)
	}

	// 좁힌 도메인에 아무것도 없으면 **빈 결과다.** 전체로 넓히지 않는다.
	none, err := Search(l, []string{"rocksdb"}, "common", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("좁힌 도메인이 비었는데 남의 것을 줬다: %+v", none)
	}
}

// Scope 는 어댑터가 되풀이하던 계산이다. cli 와 mcp 가 각자 하면 한쪽만 고쳐진다.
func TestScopeMapsFlagToDomain(t *testing.T) {
	c := testutil.VaultConfig(t)
	if got := Scope(c, "/tmp/proj/alpha", true); got != "" {
		t.Errorf("cross-project 인데 좁혔다: %q", got)
	}
	if got := Scope(c, "", false); got != "" {
		t.Errorf("cwd 를 모르는데 좁혔다: %q", got)
	}
	if got := Scope(c, "/tmp/proj/alpha", false); got != "alpha" {
		t.Errorf("Scope = %q, want alpha", got)
	}
	// 어느 paths 에도 안 걸리면 폴백 도메인이다 — config.DomainForCwd 의 계약이고,
	// 여기서 그것을 바꾸지 않는다.
	if got := Scope(c, "/tmp/somewhere-else", false); got != "common" {
		t.Errorf("폴백 도메인이 아니다: %q", got)
	}
}

func TestSearchWithoutKeywordsReturnsNothing(t *testing.T) {
	l := layout(t)
	if _, err := Append(l, Entry{Domain: "alpha", Title: "뭔가"}); err != nil {
		t.Fatal(err)
	}
	hits, err := Search(l, nil, "", 5)
	if err != nil || len(hits) != 0 {
		t.Errorf("키워드가 없는데 결과를 줬다: %+v (%v)", hits, err)
	}
}

// ★ 세션 끝 판정이 받는 축적분이다. 발췌 상한에 잘려 나간 앞부분이 여기 남아 있다.
func TestSessionTitlesFiltersBySession(t *testing.T) {
	l := layout(t)
	for _, e := range []Entry{
		{Domain: "alpha", Date: "2026-08-19", Title: "이 세션 하나", Session: "S1"},
		{Domain: "alpha", Date: "2026-08-19", Title: "남의 세션", Session: "S2"},
		{Domain: "alpha", Date: "2026-08-19", Title: "이 세션 둘", Session: "S1"},
	} {
		if _, err := Append(l, e); err != nil {
			t.Fatal(err)
		}
	}
	got := SessionTitles(l, "alpha", "S1", 10)
	want := []string{"이 세션 하나", "이 세션 둘"}
	if len(got) != len(want) {
		t.Fatalf("제목 %d개, %d개여야 한다: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if n := SessionTitles(l, "alpha", "", 10); n != nil {
		t.Errorf("세션 id 가 없으면 빈 결과여야 한다: %v", n)
	}
	// 넘치면 최근 것을 남긴다 — 진행 중인 것을 담는 계층이라 오래된 검토보다
	// 방금 것이 거의 언제나 더 쓸모 있다.
	if got := SessionTitles(l, "alpha", "S1", 1); len(got) != 1 || got[0] != "이 세션 둘" {
		t.Errorf("상한을 걸었을 때 최근 것이 남아야 한다: %v", got)
	}
}

// 작업 로그가 아직 없는 도메인은 조용히 빈 결과다 — 그 세션의 첫 판정이 정상이다.
func TestSessionTitlesOnMissingFileIsQuiet(t *testing.T) {
	l := layout(t)
	if got := SessionTitles(l, "alpha", "S1", 10); got != nil {
		t.Errorf("파일이 없는데 결과를 줬다: %v", got)
	}
	hits, err := Search(l, []string{"뭐든"}, "", 5)
	if err != nil || len(hits) != 0 {
		t.Errorf("파일이 없는데 실패하거나 결과를 줬다: %+v (%v)", hits, err)
	}
}
