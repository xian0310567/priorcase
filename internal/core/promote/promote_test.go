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

// endSeg 는 **세션 끝** 판정에서 온 구간이다.
//
// seg() 의 Scope 를 비워 두면 judge.ScopeMid — 도중 판정이고, 그때는 decision 등급이
// 작업 로그로 강등된다(One 의 주석). 결정 노트가 나오는 것을 보는 테스트는 그래서
// 반드시 이쪽을 써야 한다. 예전에 이 구분이 없어서 두 테스트가 "결정 노트가 안 생긴다"
// 로 깨졌는데, 그건 회귀가 아니라 **의도한 동작을 테스트가 몰랐던 것**이다.
func endSeg() Segment {
	s := seg()
	s.Scope = judge.ScopeEnd
	return s
}

// ★ **쓰기는 capture.Do 를 거친다.** 판별기가 준 값을 직접 파일로 쓰면 스키마 검증과
// 유사 slug 거부를 우회하게 되고, 그게 "쓰기 경로가 둘로 갈라진다" 그 자체다.
func TestPromoteWritesThroughCapture(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	f := &fake{v: judge.Verdict{Record: true, Slug: "저장엔진선택", Summary: "SQLite 로 간다",
		Body: "## 결정\n\nSQLite.\n", Tags: []string{"저장"}}}

	r := One(context.Background(), f, l, c, endSeg())
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if !r.Recorded {
		t.Fatalf("기록되지 않았다: %+v", r)
	}
	if r.Tier != judge.TierDecision {
		t.Fatalf("결정 노트가 아니다: tier=%q path=%s", r.Tier, r.Path)
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
	// 세션 끝 판정이어야 결정 노트 경로로 들어가고, 그래야 유사 slug 거부를 만난다.
	s := endSeg()
	s.Date = "2026-08-01"
	f := &fake{v: judge.Verdict{Record: true, Slug: "저장-엔진", Summary: "겹친다", Body: "## 결정\n\nx\n"}}

	r := One(context.Background(), f, l, c, s)
	if r.Err != nil {
		t.Fatalf("에러가 났다 — 이건 실패가 아니라 '이미 있다' 다: %v", r.Err)
	}
	if !strings.Contains(r.Reason, "유사한 결정") {
		t.Errorf("이유가 이상하다: %q", r.Reason)
	}
	// **결정 노트는 안 생긴다.** 그게 유사 slug 거부의 전부다.
	if r.Tier == judge.TierDecision {
		t.Errorf("중복인데 결정 노트를 만들었다: %s", r.Path)
	}
	// **그런데 버리지도 않는다.** 판별기가 결정 등급을 줬다는 것은 새 내용이 있다고
	// 봤다는 뜻이고, 유사 slug 는 파일명이 겹친다는 것일 뿐이다. 예전에는 여기서
	// 조용히 사라졌다 — 작업 로그로 내려 두면 사람이 나중에 둘을 합칠 수 있다.
	if !r.Recorded || r.Tier != judge.TierWorklog {
		t.Fatalf("유사 slug 라고 통째로 버렸다: %+v", r)
	}
	body, err := os.ReadFile(r.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "겹친다") {
		t.Errorf("작업 로그에 내용이 안 남았다:\n%s", body)
	}
}

// ★★★ **도중 판정에서 온 decision 은 작업 로그로 내려간다 — 그리고 버려지지 않는다.**
//
// 이번 설계의 핵심 안전장치다. 지시문은 도중 판정에 decision 형식을 아예 안 보여
// 주지만 **모델 출력은 우리가 통제하지 못한다.** 여기서 막지 않으면 아크가 끝나지도
// 않은 창에서 결정 노트가 나오고, 그건 옛 실패의 거울상이다 — 예전엔 아무것도 안
// 남았고(원장 23건 · 자동 기록 0건) 이번엔 파편이 잔뜩 남는다. 회수는 슬롯 3개라
// 파편이 쌓이면 확정된 결정이 밀려난다.
//
// 강등이지 폐기가 아니라는 쪽이 나머지 절반이다. 판별기가 남길 값어치가 있다고 본
// 것을 등급이 안 맞는다고 버리면, 그게 바로 "확정 안 됐으니 나중에" 로 미루다 전부
// 잃던 옛 동작이다. 작업 로그는 회수에 자동 주입되지 않으므로 내려도 아무것도
// 오염되지 않는다.
func TestMidScopeDecisionIsDemotedNotDropped(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	f := &fake{v: judge.Verdict{
		Tier: judge.TierDecision, Record: true,
		Slug: "저장엔진선택", Summary: "SQLite 로 간다",
		Body: "## 결정\n\nSQLite.\n", Tags: []string{"저장"},
	}}

	s := seg() // Scope 를 안 준다 = judge.ScopeMid (도중 판정)
	r := One(context.Background(), f, l, c, s)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.Tier != judge.TierWorklog {
		t.Fatalf("도중 판정의 decision 이 강등되지 않았다: tier=%q path=%s", r.Tier, r.Path)
	}
	if !r.Recorded {
		t.Fatalf("강등하면서 버렸다 — 판별기가 남길 값어치가 있다고 본 것이다: %+v", r)
	}

	// 내려간 자리가 작업 로그이고, 내용이 온전히 실렸는지 본다.
	if filepath.Base(filepath.Dir(r.Path)) == "decisions" {
		t.Fatalf("결정 폴더에 썼다: %s", r.Path)
	}
	body, err := os.ReadFile(r.Path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	for _, want := range []string{"SQLite 로 간다", "SQLite.", "S1"} {
		if !strings.Contains(got, want) {
			t.Errorf("작업 로그에 %q 가 없다:\n%s", want, got)
		}
	}
	// 결정 노트는 한 건도 안 늘었어야 한다.
	notes, _, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range notes {
		if strings.Contains(n.Meta.Summary, "SQLite 로 간다") {
			t.Fatalf("도중 판정인데 결정 노트가 생겼다: %s", n.Path)
		}
	}
	// 왜 내려갔는지가 호출자에게 남아야 한다 — 원장에 이유가 없으면 나중에
	// "판별기가 결정이라고 했는데 왜 작업 로그에 있지" 를 코드로만 풀어야 한다.
	if !strings.Contains(r.Reason, "도중 판정") {
		t.Errorf("강등 이유가 안 남았다: %q", r.Reason)
	}
}

// TestScopeEndKeepsDecisionTier 는 **강등이 세션 끝까지 번지지 않는지** 본다.
// 위 가드가 Scope 를 안 보고 무조건 내리면 결정 노트가 영영 안 생기는데, 그건
// 고치려던 고장 그 자체다(원장 23건 · 자동 기록 0건).
func TestScopeEndKeepsDecisionTier(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	f := &fake{v: judge.Verdict{
		Tier: judge.TierDecision, Slug: "인덱스전략", Summary: "복합 인덱스로 간다",
		Body: "## 결정\n\n복합 인덱스.\n",
	}}
	r := One(context.Background(), f, l, c, endSeg())
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.Tier != judge.TierDecision || !r.Recorded {
		t.Fatalf("세션 끝 판정인데 결정 노트가 안 나왔다: %+v", r)
	}
	if filepath.Base(filepath.Dir(r.Path)) != "decisions" {
		t.Errorf("결정 폴더가 아니다: %s", r.Path)
	}
}

// TestScopeReachesJudge 는 Scope 가 판별기까지 **실제로 전달되는지** 본다.
// 여기서 끊기면 지시문이 도중에도 decision 형식을 보여 주게 되고, 위의 강등 가드는
// 매번 발동하는 사후 처리로 전락한다 — 가드가 정상 경로가 되면 안 된다.
func TestScopeReachesJudge(t *testing.T) {
	c := testutil.VaultConfig(t)
	f := &fake{v: judge.Verdict{Tier: judge.TierNone, Reason: "진행 보고다"}}
	One(context.Background(), f, store.NewLayout(c), c, endSeg())
	if f.got.Scope != judge.ScopeEnd {
		t.Errorf("판별기가 받은 Scope = %q, want %q", f.got.Scope, judge.ScopeEnd)
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
		if strings.Contains(e, "저장 엔진을 임베디드 DB") {
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
