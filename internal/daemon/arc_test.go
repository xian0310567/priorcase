package daemon

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/judge"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
)

// arcFixture 는 아크 판정 한 판에 필요한 것을 만든다.
func arcFixture(t *testing.T, verdict string, turns int) (ArcOptions, *bytes.Buffer, string) {
	t.Helper()
	c := testutil.VaultConfig(t)
	c.Capture = config.Capture{MinTurns: 2, QuiesceSeconds: 1}
	if verdict != "" {
		c.Capture.JudgePath = stubJudge(t, verdict)
	}

	root := t.TempDir()
	tp := filepath.Join(root, "s.jsonl")
	var lines []string
	for i := 0; i < turns; i++ {
		lines = append(lines, fmt.Sprintf(
			`{"type":"assistant","cwd":"/tmp/proj/alpha","sessionId":"S1",`+
				`"timestamp":"2026-08-09T01:00:%02dZ","message":{"role":"assistant",`+
				`"content":[{"type":"text","text":"발화 %d — 대안을 검토했다"}]}}`, i, i))
	}
	if err := os.WriteFile(tp, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var errb bytes.Buffer
	sd := t.TempDir()
	return ArcOptions{
		StateDir: sd, Config: c, Layout: store.NewLayout(c), Path: tp,
		Hosts: []hosts.Resolved{fakeHost(t, root, true)},
		Err:   &errb, Label: "테스트",
	}, &errb, sd
}

func decidedOf(t *testing.T, sd, path string) int64 {
	t.Helper()
	s := NewStore(sd)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	return s.CheckpointSnapshot()[path].Decided
}

// ★★ **아크가 결정 노트를 만든다.** 이 경로가 없어서 결정 노트가 구조적으로 0건이었다.
//
// 실측: 최근 7일 자동 기록 63건이 전부 작업 로그이고 결정 노트 0건. 판별기는 136번
// 돌았는데 결정 등급이 한 번도 안 나왔다 — 대상이 pending 큐였고, 데몬이 대화 도중
// 그 큐를 비우므로 세션 끝에는 판정할 것이 남지 않았다.
func TestArcMakesDecisionNote(t *testing.T) {
	o, errb, sd := arcFixture(t,
		`{"tier":"decision","slug":"저장엔진","summary":"SQLite 로 간다","body":"## 결정\n\nSQLite.\n"}`, 6)

	r := PromoteArc(context.Background(), o)
	if r.Err != nil {
		t.Fatalf("판정 실패: %v", r.Err)
	}
	if r.Tier != judge.TierDecision {
		t.Fatalf("tier = %q, want decision (skipped=%q, err=%v)\n%s", r.Tier, r.Skipped, r.Err, errb)
	}
	if !strings.Contains(r.Path, "decisions/") {
		t.Errorf("결정 폴더에 안 만들어졌다: %q", r.Path)
	}
	if !strings.Contains(errb.String(), "아크 → 결정 노트") {
		t.Errorf("보고가 없다:\n%s", errb)
	}
	// 표식이 전진해야 한다 — 안 그러면 다음 세션 끝에 같은 아크를 또 판정한다.
	if decidedOf(t, sd, o.Path) == 0 {
		t.Error("Decided 가 전진하지 않았다")
	}
}

// ★ **판별기가 실패하면 표식을 전진시키지 않는다.** 다음 기회에 다시 봐야 한다.
func TestArcKeepsMarkOnJudgeFailure(t *testing.T) {
	o, _, sd := arcFixture(t, "이건 JSON 이 아니다", 6)

	r := PromoteArc(context.Background(), o)
	if r.Err == nil {
		t.Fatal("실패했어야 한다")
	}
	if got := decidedOf(t, sd, o.Path); got != 0 {
		t.Errorf("판별기가 실패했는데 표식이 전진했다 (%d) — 그 아크는 영영 안 본다", got)
	}
}

// ★ **"결정 아님" 은 전진시킨다.** 안 그러면 같은 아크를 매 세션 끝마다 다시 묻고
// 예산을 거기서 다 쓴다.
func TestArcAdvancesOnNone(t *testing.T) {
	o, errb, sd := arcFixture(t, `{"tier":"none","reason":"진행 보고다"}`, 6)

	r := PromoteArc(context.Background(), o)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.Tier != judge.TierNone {
		t.Errorf("tier = %q — 아무것도 안 남겨야 한다", r.Tier)
	}
	if decidedOf(t, sd, o.Path) == 0 {
		t.Error("'결정 아님' 인데 표식이 안 전진했다 — 매 세션 다시 묻는다")
	}
	if !strings.Contains(errb.String(), "진행 보고다") {
		t.Errorf("이유를 안 알린다:\n%s", errb)
	}
}

// ★ **임계 미달이면 전진시키지 않는다.** 여기서 밀면 아크가 영원히 임계를 못 채운다 —
// 1발화 보고 전진, 또 1발화 보고 전진, 매번 1 < 2 다 (Scan 의 갈래 2와 같은 함정).
func TestArcBelowThresholdDoesNotAdvance(t *testing.T) {
	o, _, sd := arcFixture(t, `{"tier":"decision","slug":"x","summary":"y"}`, 1)

	r := PromoteArc(context.Background(), o)
	if r.Skipped == "" {
		t.Fatalf("건너뛰어야 한다: tier=%q", r.Tier)
	}
	if got := decidedOf(t, sd, o.Path); got != 0 {
		t.Errorf("임계 미달인데 표식이 전진했다 (%d)", got)
	}
}

// ★★ **아크가 덮은 표시 구간은 다시 판정하지 않는다.**
//
// 안 거두면 세션 끝에 판별기가 두 번 돌고(원장 2건) 같은 주제로 결정 노트 하나와
// 작업 로그 하나가 나란히 생긴다. 훅 테스트가 실제로 그 상태를 잡았다.
func TestArcResolvesCoveredPending(t *testing.T) {
	o, errb, sd := arcFixture(t,
		`{"tier":"decision","slug":"저장엔진","summary":"SQLite 로 간다","body":"x"}`, 6)

	// 아크가 덮을 범위 안에 표시 구간을 하나 심는다.
	st := NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	// **From/To 는 실제 Scan 이 넣는 값과 같아야 한다.** 처음에 To 를 1MB 로 줬더니
	// 아크 범위 밖이라 안 거둬졌다 — 픽스처가 비현실적이면 통과해도 아무것도 안 지킨다.
	fi, err := os.Stat(o.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddPending(Pending{
		SessionID: "S1", Path: o.Path, Cwd: "/tmp/proj/alpha", Domain: "alpha",
		Turns: 6, From: 0, To: fi.Size(), Excerpt: "뭔가",
	}); err != nil {
		t.Fatal(err)
	}
	if items, _ := ReadPending(sd); len(items) != 1 {
		t.Fatalf("준비가 안 됐다: %d건", len(items))
	}

	if r := PromoteArc(context.Background(), o); r.Err != nil {
		t.Fatal(r.Err)
	}
	if items, _ := ReadPending(sd); len(items) != 0 {
		t.Errorf("덮인 구간이 %d건 남았다 — 같은 발화를 두 번 판정한다", len(items))
	}
	if !strings.Contains(errb.String(), "두 번 판정하지 않는다") {
		t.Errorf("거뒀다는 보고가 없다:\n%s", errb)
	}
}

// ★ 판별기 실패에는 덮인 구간을 **거두지 않는다** — 구간 드레인이 마지막 기회다.
func TestArcFailureKeepsCoveredPending(t *testing.T) {
	o, _, sd := arcFixture(t, "JSON 아님", 6)
	st := NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(o.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddPending(Pending{
		SessionID: "S1", Path: o.Path, Domain: "alpha", Turns: 6, From: 0, To: fi.Size(),
	}); err != nil {
		t.Fatal(err)
	}
	if r := PromoteArc(context.Background(), o); r.Err == nil {
		t.Fatal("실패했어야 한다")
	}
	if items, _ := ReadPending(sd); len(items) != 1 {
		t.Errorf("판별기가 실패했는데 구간을 거뒀다 — 그 대화의 마지막 기회가 사라진다")
	}
}

// ★ 판별기가 없으면 조용히 아무것도 안 한다. 고장이 아니라 설정이다.
func TestArcWithoutJudgeIsQuiet(t *testing.T) {
	o, errb, sd := arcFixture(t, "", 6)
	o.Config.Capture.JudgePath = filepath.Join(t.TempDir(), "없는판별기")

	r := PromoteArc(context.Background(), o)
	if r.Skipped == "" {
		t.Errorf("건너뛰어야 한다: %+v", r)
	}
	if decidedOf(t, sd, o.Path) != 0 {
		t.Error("판별기도 없이 표식이 전진했다")
	}
	if errb.Len() != 0 {
		t.Errorf("판별기 없는 설치에서 시끄럽다:\n%s", errb)
	}
}

// ★ **되짚기 상한.** 표식이 없으면 파일 끝에서 initialArcLookback 만 되짚는다.
//
// 0 부터 보면 쌓인 대화 전체(실측 35MB)를 한 번에 판정하는데, 발췌 상한이 24KB 라
// 그 판정은 뭉개지고 **그 한 번으로 표식이 끝까지 전진해 다시 볼 기회도 사라진다.**
func TestDecidedFromClampsLookback(t *testing.T) {
	s := newStore(t)
	small := int64(1000)
	if got := s.DecidedFrom("/p", small); got != 0 {
		t.Errorf("작은 파일은 처음부터 봐야 한다: %d", got)
	}
	big := int64(initialArcLookback) * 30
	if got := s.DecidedFrom("/p", big); got != big-initialArcLookback {
		t.Errorf("되짚기 = %d, want %d", got, big-initialArcLookback)
	}
	// 표식이 있으면 그것을 쓴다.
	if err := s.MarkDecided("/p", 4096); err != nil {
		t.Fatal(err)
	}
	if got := s.DecidedFrom("/p", big); got != 4096 {
		t.Errorf("표식을 안 쓴다: %d", got)
	}
	// **파일이 줄었으면 표식을 믿을 수 없다** — 잘렸거나 다른 파일로 바뀌었다.
	if got := s.DecidedFrom("/p", 100); got != 0 {
		t.Errorf("줄어든 파일에서 옛 표식을 썼다: %d", got)
	}
}

// ★ MarkDecided 는 뒤로 가지 않는다. 되돌아가면 같은 아크를 다시 판정한다.
func TestMarkDecidedNeverGoesBackward(t *testing.T) {
	s := newStore(t)
	if err := s.MarkDecided("/p", 900); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDecided("/p", 100); err != nil {
		t.Fatal(err)
	}
	if got := s.CheckpointSnapshot()["/p"].Decided; got != 900 {
		t.Errorf("Decided = %d, want 900", got)
	}
}

// ★★ **몇 번이고 실패하는 아크를 영원히 재시도하지 않는다.**
//
// 실측: 35MB transcript 의 아크가 75초 상한에 killed 되고, 그 한 번이 승격 예산 90초를
// 통째로 먹어 구간 드레인이 아무것도 못 했다. 표식이 전진하지 않으므로 다음 세션 끝에
// 같은 아크를 또 넣는다 — 매 세션을 그 한 건이 태운다.
// [[priorcase-결정-판별기-상한과-포기-카운터-2026-08-12]] 가 구간에 대해 고친 것과
// 같은 함정이고, 같은 답(포기 카운터)을 쓴다.
func TestArcGivesUpAfterRepeatedFailures(t *testing.T) {
	o, errb, sd := arcFixture(t, "JSON 아님", 6)

	for i := 1; i <= maxArcFails; i++ {
		errb.Reset()
		if r := PromoteArc(context.Background(), o); r.Err == nil {
			t.Fatalf("%d회차: 실패했어야 한다", i)
		}
		if i < maxArcFails {
			if decidedOf(t, sd, o.Path) != 0 {
				t.Fatalf("%d회차에 벌써 포기했다", i)
			}
			if !strings.Contains(errb.String(), fmt.Sprintf("(%d/%d)", i, maxArcFails)) {
				t.Errorf("%d회차 횟수를 안 알린다:\n%s", i, errb)
			}
		}
	}
	// 마지막 판에서 포기하고 전진해야 한다.
	if decidedOf(t, sd, o.Path) == 0 {
		t.Error("연속 실패했는데 포기하지 않았다 — 매 세션이 이 한 건에 탄다")
	}
	if !strings.Contains(errb.String(), "포기한다") {
		t.Errorf("포기를 조용히 했다 — 잃은 것을 사람이 알아야 한다:\n%s", errb)
	}
}

// ★ 실패하면 다음 판은 **더 작은 아크**를 본다. 같은 크기로 다시 넣으면 같은 이유로 죽는다.
func TestArcLookbackShrinksOnFailure(t *testing.T) {
	s := newStore(t)
	size := int64(initialArcLookback) * 8

	first := s.DecidedFrom("/p", size)
	if _, err := s.ArcFailed("/p"); err != nil {
		t.Fatal(err)
	}
	second := s.DecidedFrom("/p", size)
	if second <= first {
		t.Errorf("실패 뒤에도 되짚기가 안 줄었다: %d → %d", size-first, size-second)
	}
	if got, want := size-second, int64(initialArcLookback)/2; got != want {
		t.Errorf("되짚기 = %d, want %d (반으로 줄어야 한다)", got, want)
	}

	// **바닥이 있다.** 이보다 작으면 아크가 아니라 또 하나의 구간이 된다.
	for i := 0; i < 20; i++ {
		if _, err := s.ArcFailed("/p"); err != nil {
			t.Fatal(err)
		}
	}
	if got := size - s.DecidedFrom("/p", size); got != minArcLookback {
		t.Errorf("바닥 = %d, want %d", got, minArcLookback)
	}

	// 성공하면 온전한 크기로 돌아온다.
	if err := s.ArcSucceeded("/p"); err != nil {
		t.Fatal(err)
	}
	if got := size - s.DecidedFrom("/p", size); got != initialArcLookback {
		t.Errorf("성공 뒤 되짚기 = %d, want %d", got, initialArcLookback)
	}
}
