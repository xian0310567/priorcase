package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/xian0310567/priorcase/internal/daemon"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// ★★★ **검토 표시가 검토 큐를 줄여야 한다.**
//
// 검토 큐는 승격 원장의 recorded=true 를 전부 보여 준다. "검토했다" 상태가
// 없으면 큐가 영영 안 줄어들고, 사람은 같은 줄을 매번 다시 보다가 **화면 전체를
// 무시하는 법을 배운다** — 그러면 판별기 검증이라는 이 화면의 존재 이유가 죽는다.
func TestReviewedShrinksReviewQueue(t *testing.T) {
	cfgPath, sd := reviewedFixture(t)

	before := reviewQueue(t, cfgPath)
	if len(before) != 2 {
		t.Fatalf("준비가 잘못됐다: 검토 큐 %d건 (2건이어야 한다)", len(before))
	}

	if out, err := runReviewed(t, "/t.jsonl@1"); err != nil {
		t.Fatalf("표시 실패: %v (%s)", err, out)
	}

	after := reviewQueue(t, cfgPath)
	if len(after) != 1 {
		t.Fatalf("검토 큐가 %d건 — 1건이어야 한다", len(after))
	}
	if after[0].ID != "/t.jsonl@2" {
		t.Errorf("엉뚱한 것이 남았다: %s", after[0].ID)
	}
	_ = sd
}

// ★★ **outcome 을 건드리면 안 된다.**
//
// outcome 은 "그 결정이 결과적으로 좋았나" 이고 회고 큐가 outcome != pending 인
// 노트를 영영 제외한다. 검토는 "판별기가 사실대로 썼나" 라는 다른 질문이다 —
// 둘을 한 값에 실으면 노트를 검증했을 뿐인데 **나중에 결과를 묻는 자리가 조용히
// 사라진다.**
func TestReviewedDoesNotTouchOutcome(t *testing.T) {
	cfgPath, _ := reviewedFixture(t)

	before := retroQueue(t, cfgPath)
	// **회고 큐가 비면 이 시험은 아무것도 검사하지 않는다.** 0 → 0 은 언제나
	// 통과한다. 픽스처가 바뀌어 조용히 공허해지는 것을 여기서 막는다.
	if len(before) == 0 {
		t.Fatal("회고 큐가 비었다 — 이 시험이 공허하다")
	}
	// 검토할 노트 자신이 회고 큐에 있어야 대조가 의미 있다.
	const stem = "alpha-결정-저장엔진-2026-08-01"
	if !hasStem(before, stem) {
		t.Fatalf("검토 대상 노트(%s)가 회고 큐에 없다 — 회고를 망가뜨렸는지 볼 수 없다", stem)
	}

	if _, err := runReviewed(t, "/t.jsonl@1"); err != nil {
		t.Fatalf("표시 실패: %v", err)
	}

	after := retroQueue(t, cfgPath)
	if len(after) != len(before) {
		t.Errorf("회고 큐가 %d → %d 로 바뀌었다 — 검토가 outcome 을 건드렸다",
			len(before), len(after))
	}
	if !hasStem(after, stem) {
		t.Errorf("%s 가 회고 큐에서 사라졌다 — 검토가 outcome 을 good 으로 만들었다", stem)
	}
}

func hasStem(items []retroItemForTest, stem string) bool {
	for _, it := range items {
		if it.Stem == stem {
			return true
		}
	}
	return false
}

// ★★ **위 시험이 공허해지지 않게 지키는 시험이다.**
//
// TestReviewedDoesNotTouchOutcome 은 "검토 전후로 회고 큐가 같다" 를 본다.
// 그런데 회고 큐가 애초에 outcome 에 무감각하다면 그 비교는 언제나 통과한다 —
// 검토가 outcome 을 good 으로 만들어도 못 잡는다.
//
// 그래서 민감도 자체를 여기서 못박는다. 둘이 짝이어야 논증이 성립한다:
// (가) 회고 큐는 outcome 이 판명되면 그 노트를 뺀다  ← 이 시험
// (나) 검토 전후로 회고 큐가 같다                    ← 위 시험
func TestRetroQueueDropsSettledOutcome(t *testing.T) {
	cfgPath, _ := reviewedFixture(t)
	before := retroQueue(t, cfgPath)
	const stem = "alpha-결정-저장엔진-2026-08-01"
	if !hasStem(before, stem) {
		t.Fatalf("%s 가 회고 큐에 없다 — 픽스처가 바뀌었다", stem)
	}

	p := filepath.Join(vaultPathOf(t, cfgPath), "alpha", "decisions", stem+".md")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("노트를 못 읽는다: %v", err)
	}
	nb := strings.Replace(string(b), "outcome: pending", "outcome: good", 1)
	if nb == string(b) {
		t.Fatal("픽스처 노트에 outcome: pending 이 없다")
	}
	if err := os.WriteFile(p, []byte(nb), 0o644); err != nil {
		t.Fatal(err)
	}

	if after := retroQueue(t, cfgPath); hasStem(after, stem) {
		t.Errorf("outcome 을 good 으로 바꿨는데 %s 가 회고 큐에 남았다", stem)
	}
}

// vaultPathOf 는 설정 파일에서 볼트 경로를 읽는다.
func vaultPathOf(t *testing.T, cfgPath string) string {
	t.Helper()
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "path = ") {
			return strings.Trim(strings.TrimPrefix(ln, "path = "), `"`)
		}
	}
	t.Fatalf("설정에서 볼트 경로를 못 찾았다:\n%s", b)
	return ""
}

// ★ 두 번 눌러도 원장이 부풀지 않는다. 앱의 큐가 잠깐 낡으면 실제로 두 번 온다.
func TestReviewedIsIdempotent(t *testing.T) {
	_, sd := reviewedFixture(t)
	for i := 0; i < 3; i++ {
		if out, err := runReviewed(t, "/t.jsonl@1"); err != nil {
			t.Fatalf("%d회차 실패: %v (%s)", i+1, err, out)
		}
	}
	recs, err := daemon.ReadPromotions(sd, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range recs {
		if r.Reviewed {
			n++
		}
	}
	if n != 1 {
		t.Errorf("검토 표시가 %d줄 — 1줄이어야 한다", n)
	}
}

// ★★ **없는 ID 는 거부한다.** 오타를 조용히 받으면 원장에 아무도 안 보는 줄이
// 쌓이고, 사람은 눌렀는데 큐가 안 줄어드는 것만 본다.
func TestReviewedRejectsUnknownID(t *testing.T) {
	reviewedFixture(t)
	if out, err := runReviewed(t, "/없는.jsonl@99"); err == nil {
		t.Fatalf("없는 ID 인데 통과했다: %s", out)
	}
}

// ★ 기록 안 된 승격(판별기가 "결정 아님" 이라고 한 것)은 검토 대상이 아니다.
func TestReviewedRejectsUnrecorded(t *testing.T) {
	reviewedFixture(t)
	if out, err := runReviewed(t, "/t.jsonl@3"); err == nil {
		t.Fatalf("기록 안 된 승격인데 통과했다: %s", out)
	}
}

// reviewedFixture 는 승격 원장에 세 줄을 심는다:
// @1 · @2 는 기록됨(검토 대상), @3 은 "결정 아님"(검토 대상 아님).
func reviewedFixture(t *testing.T) (cfgPath, stateDir string) {
	t.Helper()
	cfgPath, _ = testutil.VaultConfigFile(t)
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	stateDir = filepath.Join(stateHome, "priorcase")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []daemon.Promotion{
		{At: time.Now().UTC(), ID: "/t.jsonl@1", Domain: "alpha", Recorded: true,
			Path: "alpha/decisions/alpha-결정-저장엔진-2026-08-01.md", Excerpt: "가"},
		{At: time.Now().UTC(), ID: "/t.jsonl@2", Domain: "alpha", Recorded: true,
			Path: "alpha/decisions/alpha-결정-스키마-2026-08-02.md", Excerpt: "나"},
		{At: time.Now().UTC(), ID: "/t.jsonl@3", Domain: "alpha", Recorded: false,
			Reason: "결정이 아니다", Excerpt: "다"},
	} {
		if err := daemon.AppendPromotion(stateDir, p); err != nil {
			t.Fatal(err)
		}
	}
	return cfgPath, stateDir
}

func runReviewed(t *testing.T, id string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "prior"}
	root.PersistentFlags().String("config", "", "")
	root.AddCommand(newReviewedCmd())
	var out, errb strings.Builder
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs([]string{"reviewed", id})
	err := root.Execute()
	return out.String() + errb.String(), err
}

func reviewQueue(t *testing.T, cfgPath string) []QueueReview {
	t.Helper()
	return runQueueForTest(t, cfgPath).Review
}

func retroQueue(t *testing.T, cfgPath string) []retroItemForTest {
	t.Helper()
	q := runQueueForTest(t, cfgPath)
	out := make([]retroItemForTest, 0, len(q.Retro))
	for _, r := range q.Retro {
		out = append(out, retroItemForTest{Stem: r.Stem})
	}
	return out
}

type retroItemForTest struct{ Stem string }

func runQueueForTest(t *testing.T, cfgPath string) Queue {
	t.Helper()
	root := &cobra.Command{Use: "prior"}
	root.PersistentFlags().String("config", "", "")
	root.AddCommand(newQueueCmd())
	var out, errb strings.Builder
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs([]string{"queue", "--json", "--config", cfgPath})
	if err := root.Execute(); err != nil {
		t.Fatalf("queue 실행: %v (stderr=%s)", err, errb.String())
	}
	var q Queue
	if err := json.Unmarshal([]byte(out.String()), &q); err != nil {
		t.Fatalf("출력이 JSON 이 아니다: %v", err)
	}
	return q
}
