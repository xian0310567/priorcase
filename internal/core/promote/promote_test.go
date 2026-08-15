package promote

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/judge"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// fake 는 실제 LLM 없이 판정을 흉내 낸다. 진짜를 부르는 테스트는 느리고 결정적이지 않다.
type fake struct {
	v   judge.Verdict
	err error
	got judge.Request
}

func (f *fake) Decide(_ context.Context, r judge.Request) (judge.Verdict, error) {
	f.got = r
	return f.v, f.err
}

func seg() Segment {
	return Segment{ID: "/t/a.jsonl@0", Domain: "alpha", Date: "2026-08-08",
		Excerpt: "에이전트: SQLite 로 하기로 결정했다", Session: "S1"}
}

// ★ **쓰기는 capture.Do 를 거친다.** 판별기가 준 값을 직접 파일로 쓰면 스키마 검증과
// 유사 slug 거부를 우회하게 되고, 그게 "쓰기 경로가 둘로 갈라진다" 그 자체다.
func TestPromoteWritesThroughCapture(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	f := &fake{v: judge.Verdict{Record: true, Slug: "저장엔진선택", Summary: "SQLite 로 간다",
		Body: "## 결정\n\nSQLite.\n", Tags: []string{"저장"}}}

	r := One(context.Background(), f, l, c, seg())
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if !r.Recorded {
		t.Fatalf("기록되지 않았다: %+v", r)
	}
	body, err := os.ReadFile(r.Path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	// capture.Do 를 거쳤다는 증거들 — 손으로 썼다면 없을 것들이다.
	if !strings.Contains(s, "type: decision") {
		t.Error("frontmatter 가 정본이 아니다")
	}
	if !strings.Contains(s, `source_session: "S1"`) {
		t.Error("세션이 안 박혔다")
	}
	if !strings.Contains(s, "decision") || !strings.Contains(s, "저장") {
		t.Errorf("태그가 안 붙었다:\n%s", s)
	}
	// 색인도 갱신됐어야 한다.
	idx, _ := os.ReadFile(filepath.Join(c.DefaultVaultPath(), "_meta", "00-결정-색인.md"))
	if !strings.Contains(string(idx), "SQLite 로 간다") {
		t.Error("색인이 갱신되지 않았다")
	}
}

// record=false 는 **성공이다.** 기록할 결정이 아니라는 판정이 나온 것이고,
// 호출자는 그 구간을 지워도 된다. 에러로 만들면 지울 수 없게 된다.
func TestNotRecordedIsNotAnError(t *testing.T) {
	c := testutil.VaultConfig(t)
	f := &fake{v: judge.Verdict{Record: false, Reason: "진행 보고다"}}
	r := One(context.Background(), f, store.NewLayout(c), c, seg())
	if r.Err != nil {
		t.Fatalf("에러가 났다: %v", r.Err)
	}
	if r.Recorded {
		t.Error("기록하면 안 된다")
	}
	if r.Reason != "진행 보고다" {
		t.Errorf("이유를 안 넘겼다: %q", r.Reason)
	}
}

// 유사 slug 거부도 "이미 있다" 는 뜻이라 이유로 옮긴다 — 실패가 아니다.
func TestSimilarSlugBecomesReason(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	// 픽스처에 alpha-결정-저장엔진-2026-08-01 이 있다. 같은 날짜로 유사 slug 를 낸다.
	s := seg()
	s.Date = "2026-08-01"
	f := &fake{v: judge.Verdict{Record: true, Slug: "저장-엔진", Summary: "겹친다", Body: "## 결정\n\nx\n"}}

	r := One(context.Background(), f, l, c, s)
	if r.Err != nil {
		t.Fatalf("에러가 났다 — 이건 실패가 아니라 '이미 있다' 다: %v", r.Err)
	}
	if r.Recorded {
		t.Error("중복을 기록했다")
	}
	if !strings.Contains(r.Reason, "유사한 결정") {
		t.Errorf("이유가 이상하다: %q", r.Reason)
	}
}

// 판별기에 **기존 결정 요약**을 넘겨야 중복을 거를 수 있다.
func TestExistingSummariesArePassedToJudge(t *testing.T) {
	c := testutil.VaultConfig(t)
	f := &fake{v: judge.Verdict{Record: false, Reason: "중복"}}
	One(context.Background(), f, store.NewLayout(c), c, seg())

	if len(f.got.Existing) == 0 {
		t.Fatal("기존 결정을 안 넘겼다 — 판별기가 중복을 알 방법이 없다")
	}
	found := false
	for _, e := range f.got.Existing {
		if strings.Contains(e.Summary, "저장 엔진을 임베디드 DB") {
			found = true
		}
	}
	if !found {
		t.Errorf("같은 도메인의 결정이 안 넘어갔다: %v", f.got.Existing)
	}
}

func TestNoJudgeIsAnError(t *testing.T) {
	c := testutil.VaultConfig(t)
	if r := One(context.Background(), nil, store.NewLayout(c), c, seg()); r.Err == nil {
		t.Error("판별기가 없는데 조용히 성공했다")
	}
}

func TestEmptyExcerptIsSkipped(t *testing.T) {
	c := testutil.VaultConfig(t)
	s := seg()
	s.Excerpt = "  "
	f := &fake{v: judge.Verdict{Record: true, Slug: "x", Summary: "y"}}
	r := One(context.Background(), f, store.NewLayout(c), c, s)
	if r.Recorded || r.Err != nil {
		t.Errorf("빈 발췌로 무언가 했다: %+v", r)
	}
}

// ★ 자동 경로가 **뒤집기를 표현할 수 있어야 한다.**
//
// 지금까지는 없었다 — 프롬프트가 "이미 있는 결정의 반복이면 record=false" 라고만
// 지시했고, 기존 결정을 뒤집는 대화는 그 결정과 주제·어휘가 거의 같아서 중복으로
// 판정되기 가장 좋은 형태다. record=false 가 나오면 그 구간은 조용히 지워진다.
// 그래서 기록이 각 주제의 **첫 결정 쪽으로 계통적으로 편향**됐다.
func TestPromoteCarriesSupersedes(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	f := &fake{v: judge.Verdict{
		Record: true, Slug: "저장엔진뒤집기", Summary: "임베디드 DB 를 접고 파일로 간다",
		Body: "## 결정\n\n파일.\n", Tags: []string{"저장"},
		Supersedes: []string{"alpha-결정-저장엔진-2026-08-01"},
	}}

	r := One(context.Background(), f, l, c, seg())
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if !r.Recorded {
		t.Fatalf("기록되지 않았다: %+v", r)
	}
	old, err := l.Read(filepath.Join(c.DefaultVaultPath(), "alpha", "decisions",
		"alpha-결정-저장엔진-2026-08-01.md"))
	if err != nil {
		t.Fatal(err)
	}
	if old.Meta.Status != "superseded" {
		t.Errorf("옛 노트 status = %q, want superseded — 자동 경로가 뒤집기를 못 옮겼다", old.Meta.Status)
	}
}

// 판별기가 없는 stem 을 지어내도 구간을 통째로 잃으면 안 된다.
// capture.Do 는 대상이 없으면 에러를 내므로, 걸러 내지 않으면 결정 자체가 버려진다.
func TestPromoteDropsUnknownSupersedes(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	f := &fake{v: judge.Verdict{
		Record: true, Slug: "지어낸것", Summary: "요약", Body: "## 결정\n\nx\n",
		Supersedes: []string{"alpha-결정-존재하지않음-2026-01-01"},
	}}

	r := One(context.Background(), f, l, c, seg())
	if r.Err != nil {
		t.Fatalf("없는 stem 하나 때문에 구간이 실패했다: %v", r.Err)
	}
	if !r.Recorded {
		t.Fatalf("기록되지 않았다: %+v", r)
	}
}

// 판별기가 뒤집을 대상을 이름으로 지목하려면 **stem 을 받아야 한다.**
// 요약만 주면 "그 결정" 이라고 가리킬 이름이 없다.
func TestExistingCarriesStems(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	f := &fake{v: judge.Verdict{Record: false, Reason: "x"}}
	One(context.Background(), f, l, c, seg())

	if len(f.got.Existing) == 0 {
		t.Fatal("기존 결정이 안 넘어갔다")
	}
	found := false
	for _, e := range f.got.Existing {
		if e.Stem == "alpha-결정-저장엔진-2026-08-01" && e.Summary != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("stem 이 안 넘어갔다: %+v", f.got.Existing)
	}
}
