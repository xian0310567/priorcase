package daemon

import (
	"fmt"
	"os"
	"time"

	"github.com/gofrs/flock"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xian0310567/priorcase/internal/transcript/claudecode"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
)

func fakeHost(t *testing.T, root string, required bool) hosts.Resolved {
	t.Helper()
	return hosts.Resolved{
		Host: hosts.Host{
			Name: "test", Required: required,
			DefaultRoot: func() (string, error) { return root, nil },
			List:        claudecode.List, Parse: claudecode.Parse,
		},
		Root: root,
	}
}

func writeJSONL(t *testing.T, dir, name string, lines int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	var b strings.Builder
	for i := 0; i < lines; i++ {
		b.WriteString(`{"type":"user","sessionId":"S","cwd":"/x","timestamp":"2026-08-09T00:00:00Z","message":{"content":"결정했다"}}` + "\n")
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ★★ **처음 보는 파일은 훑지 않고 끝으로 시딩한다.**
//
// 도구를 깔기 전의 대화는 안전망 대상이 아니다. 이걸 어기면 첫 훑기에 pending 이
// 쏟아지고, 안전망이 소음이 되면 에이전트가 무시하는 법을 배운다. 실측으로 파일이
// 3,350개다.
func TestPlanSweepSeedsUnknownFiles(t *testing.T) {
	root := t.TempDir()
	a := writeJSONL(t, root, "a.jsonl", 3)
	b := writeJSONL(t, root, "b.jsonl", 3)

	sd := t.TempDir()
	st := NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	// a 만 아는 파일로 만든다.
	if err := st.Advance(a, 10, 10); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanSweep(st, []hosts.Resolved{fakeHost(t, root, true)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Scan) != 1 || plan.Scan[0] != a {
		t.Errorf("아는 파일을 훑기 대상으로 안 골랐다: %v", plan.Scan)
	}
	if len(plan.Seed) != 1 || plan.Seed[0] != b {
		t.Errorf("처음 보는 파일을 시딩 대상으로 안 골랐다: %v", plan.Seed)
	}
}

// ★ backfill 이면 처음 보는 파일도 훑는다.
func TestPlanSweepBackfillScansEverything(t *testing.T) {
	root := t.TempDir()
	writeJSONL(t, root, "a.jsonl", 3)
	writeJSONL(t, root, "b.jsonl", 3)

	st := NewStore(t.TempDir())
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSweep(st, []hosts.Resolved{fakeHost(t, root, true)}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Seed) != 0 {
		t.Errorf("backfill 인데 시딩했다: %v", plan.Seed)
	}
	if len(plan.Scan) != 2 {
		t.Errorf("훑기 대상이 %d개다", len(plan.Scan))
	}
}

// ★★ **필수가 아닌 호스트의 부재는 에러가 아니다.**
//
// Codex 를 안 쓰는 사람에게 그 자리가 없다고 매번 알리면 진짜 문제를 가린다.
// 반대로 필수 호스트가 없으면 배선이 틀린 것이라 알려야 한다.
func TestPlanSweepToleratesMissingOptionalHost(t *testing.T) {
	root := t.TempDir()
	writeJSONL(t, root, "a.jsonl", 3)
	st := NewStore(t.TempDir())
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}

	gone := filepath.Join(t.TempDir(), "없는곳")
	optional := fakeHost(t, gone, false)
	// claudecode.List 는 루트가 없으면 에러다 — 선택 호스트의 그 에러는 삼켜야 한다.
	plan, err := PlanSweep(st,
		[]hosts.Resolved{fakeHost(t, root, true), optional}, false)
	if err != nil {
		t.Fatalf("선택 호스트가 없다고 전체가 실패했다: %v", err)
	}
	if len(plan.Seed) != 1 {
		t.Errorf("필수 호스트의 파일을 못 봤다: %v", plan.Seed)
	}

	// 필수 호스트가 없으면 알려야 한다.
	if _, err := PlanSweep(st, []hosts.Resolved{fakeHost(t, gone, true)}, false); err == nil {
		t.Error("필수 호스트가 없는데 조용히 통과했다")
	}
}

// ★★ **데몬이 돌면 훑지 않는다.**
//
// 같은 락을 쓴다. 못 얻으면 데몬이 그 일을 하고 있으므로 둘이 겹치지 않는다.
func TestSweepOnceYieldsToDaemon(t *testing.T) {
	sd := t.TempDir()
	st := NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	// 데몬을 흉내내 락을 잡는다.
	lk := flock.New(filepath.Join(sd, lockFile))
	got, err := lk.TryLock()
	if err != nil || !got {
		t.Fatalf("락을 못 잡았다: %v", err)
	}
	defer func() { _ = lk.Unlock() }()

	_, owned, err := SweepOnce(SweepOptions{StateDir: sd, Config: scanCfg()})
	if err != nil {
		t.Fatal(err)
	}
	if owned {
		t.Error("데몬이 락을 쥐고 있는데 훑었다 — 같은 파일을 둘이 처리한다")
	}
}

// ★★ **예산을 넘으면 멈추고 몇 개를 못 봤는지 알린다.**
//
// 세션이 끝날 때 도는 코드라 사람이 그 시간을 그대로 겪는다. 그리고 조용히 짧게
// 훑고 끝나면 "결정이 없었다" 와 구별되지 않는다 — 이 도구가 가장 피해야 하는
// 실패 모양이다.
func TestSweepOnceStopsAtBudgetAndReportsSkipped(t *testing.T) {
	root := t.TempDir()
	sd := t.TempDir()
	st := NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		p := writeJSONL(t, root, string(rune('a'+i))+".jsonl", 3)
		if err := st.Advance(p, 1, 1); err != nil { // 아는 파일로 만든다
			t.Fatal(err)
		}
	}

	// **예산을 음수로 준다** — 첫 파일 앞에서 이미 마감이라 하나도 못 훑는다.
	// 시딩은 마감과 무관하다(파일 목록을 만드는 값싼 일이라 예산 앞에 둔다).
	r, owned, err := sweepWithHosts(SweepOptions{
		StateDir: sd, Config: scanCfg(), Budget: -time.Second,
	}, []hosts.Resolved{fakeHost(t, root, true)})
	if err != nil {
		t.Fatal(err)
	}
	if !owned {
		t.Fatal("락을 못 얻었다")
	}
	if r.Scanned != 0 {
		t.Errorf("예산이 지났는데 %d개를 훑었다", r.Scanned)
	}
	if r.Skipped != 5 {
		t.Errorf("못 본 파일을 %d개로 셌다, 5여야 한다 — 조용히 넘기면 '결정 없음' 으로 보인다",
			r.Skipped)
	}
}

// ★★ **파일마다 상태 파일을 다시 쓰거나 다시 읽으면 O(n²) 가 된다.**
//
// 실측이 잡았다. 훅 훑기가 파일 3,360개를 도는데 10초 예산에 **119개**밖에 못
// 돌았다. 원인이 둘이었다.
//
//   - PlanSweep 이 파일마다 st.Checkpoint() 를 불렀다 → 그때마다 623KB JSON 재파싱
//   - SeedToEnd 가 파일마다 Advance() 를 불렀다 → 그때마다 상태 파일 전체 재작성
//
// 고친 뒤 시딩 11.2초 → 0.77초, 평상시 10.06초 → 0.07초다.
//
// 이건 조용히 되돌아온다 — 기능은 멀쩡하고 결과도 맞고, 느려질 뿐이다. 그런데 느린
// 훑기는 예산에 잘려서 **못 본 파일을 남기고**, 그건 "결정이 없었다" 와 구별되지 않는다.
func TestSweepScalesWithManyFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	root := t.TempDir()
	sd := t.TempDir()
	st := NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	const n = 1200
	for i := 0; i < n; i++ {
		writeJSONL(t, root, fmt.Sprintf("s%04d.jsonl", i), 2)
	}

	// ── 1판: 전부 처음 보는 파일이므로 시딩만 한다.
	start := time.Now()
	r, owned, err := sweepWithHosts(SweepOptions{
		StateDir: sd, Config: scanCfg(), Budget: time.Minute,
	}, []hosts.Resolved{fakeHost(t, root, true)})
	seedTook := time.Since(start)
	if err != nil || !owned {
		t.Fatalf("err=%v owned=%v", err, owned)
	}
	if r.Seeded != n {
		t.Fatalf("시딩 %d/%d", r.Seeded, n)
	}
	// **시간이 아니라 쓰기 횟수로 잰다.** 시간은 기계와 경로 길이에 따라 흔들려서,
	// 임시 디렉토리(짧은 경로 = 작은 상태 파일)에서는 O(n²) 도 몇 초 안에 끝난다 —
	// 실제로 시간 문턱 5초짜리 시험이 이 변형을 놓쳤다. 횟수는 결정적이다.
	//
	// 시딩은 **쓰기 한 번**이어야 한다. 계획 세우기와 스냅샷은 읽기라 안 센다.
	_ = seedTook

	// ── 2판: 자란 파일이 없으므로 아무것도 안 읽어야 한다.
	start = time.Now()
	r2, _, err := sweepWithHosts(SweepOptions{
		StateDir: sd, Config: scanCfg(), Budget: time.Minute,
	}, []hosts.Resolved{fakeHost(t, root, true)})
	steadyTook := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Scanned != 0 {
		t.Errorf("자란 파일이 없는데 %d개를 훑었다", r2.Scanned)
	}
	if r2.Skipped != 0 {
		t.Errorf("예산이 넉넉한데 %d개를 못 봤다", r2.Skipped)
	}
	t.Logf("파일 %d개 · 시딩 %v · 평상시 %v", n, seedTook.Round(time.Millisecond),
		steadyTook.Round(time.Millisecond))
}

// ★ 자란 파일은 실제로 훑어야 한다. 위 최적화가 "아무것도 안 하기" 로 퇴화하면 안 된다.
func TestSweepStillScansGrownFiles(t *testing.T) {
	root := t.TempDir()
	sd := t.TempDir()
	st := NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	p := writeJSONL(t, root, "a.jsonl", 2)
	hs := []hosts.Resolved{fakeHost(t, root, true)}

	// 1판: 시딩.
	if _, _, err := sweepWithHosts(SweepOptions{
		StateDir: sd, Config: scanCfg(), Budget: time.Minute}, hs); err != nil {
		t.Fatal(err)
	}
	// 파일을 키운다.
	writeJSONL(t, root, "a.jsonl", 12)

	r, _, err := sweepWithHosts(SweepOptions{
		StateDir: sd, Config: scanCfg(), Budget: time.Minute}, hs)
	if err != nil {
		t.Fatal(err)
	}
	if r.Scanned != 1 {
		t.Errorf("자란 파일을 %d개 훑었다, 1이어야 한다 — 최적화가 훑기를 통째로 껐다", r.Scanned)
	}
	_ = p
}

// ★★ **시딩은 상태 파일을 한 번만 써야 한다.**
//
// 파일마다 Advance 를 부르면 그때마다 상태 파일을 통째로 다시 쓴다 — 처음 설치할 때
// 파일이 수천 개면 O(n²) 다. 실측에서 3,360개에 11.2초가 걸렸고, 그건 사용자의 첫
// 세션 종료가 그만큼 멎는 것이다. 고친 뒤 0.77초다.
//
// **시간이 아니라 횟수로 잰다.** 시간은 기계와 경로 길이에 따라 흔들린다 — 임시
// 디렉토리는 경로가 짧아 상태 파일이 작고, 거기서는 O(n²) 도 1.6초에 끝나 시간
// 문턱 5초를 통과했다. 실제로 그 시험이 이 회귀를 놓쳤다.
func TestSeedToEndWritesStateOnce(t *testing.T) {
	root := t.TempDir()
	st := NewStore(t.TempDir())
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for i := 0; i < 50; i++ {
		paths = append(paths, writeJSONL(t, root, fmt.Sprintf("s%02d.jsonl", i), 2))
	}

	before := st.Writes()
	if n := SeedToEnd(st, paths); n != len(paths) {
		t.Fatalf("시딩 %d/%d", n, len(paths))
	}
	if w := st.Writes() - before; w != 1 {
		t.Errorf("파일 %d개를 시딩하는데 상태 파일을 %d번 썼다 — 한 번이어야 한다",
			len(paths), w)
	}
	// 결과가 맞아야 한다. 안 쓰고 빨라지는 것은 고친 것이 아니다.
	cps := st.CheckpointSnapshot()
	for _, p := range paths {
		fi, _ := os.Stat(p)
		if cps[p].Offset != fi.Size() {
			t.Fatalf("%s: 체크포인트가 끝에 안 찍혔다 (%d != %d)", p, cps[p].Offset, fi.Size())
		}
	}
}

// ★★ **평상시 훑기는 상태 파일을 아예 쓰지 않아야 한다.**
//
// 자란 파일이 없으면 할 일이 없다. 그런데 Scan 은 읽을 것이 없어도 NoteScan 으로
// "방금 훑음" 을 남기고, 그건 상태 파일 전체를 다시 쓰는 일이다 — 파일마다 한 번씩이면
// 3,000번이다. 실측에서 10초 예산에 119개밖에 못 돌았다.
func TestSweepWritesNothingWhenNothingGrew(t *testing.T) {
	root := t.TempDir()
	sd := t.TempDir()
	st := NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		writeJSONL(t, root, fmt.Sprintf("s%02d.jsonl", i), 2)
	}
	hs := []hosts.Resolved{fakeHost(t, root, true)}
	if _, _, err := sweepWithHosts(SweepOptions{
		StateDir: sd, Config: scanCfg(), Budget: time.Minute}, hs); err != nil {
		t.Fatal(err)
	}

	// 두 번째 판은 자란 파일이 없다.
	st2 := NewStore(sd)
	if err := st2.Load(); err != nil {
		t.Fatal(err)
	}
	before := st2.Writes()
	if _, _, err := sweepPlanned(SweepOptions{
		StateDir: sd, Config: scanCfg(), Budget: time.Minute}, st2, hs); err != nil {
		t.Fatal(err)
	}
	if w := st2.Writes() - before; w != 0 {
		t.Errorf("자란 파일이 없는데 상태 파일을 %d번 썼다 — 파일마다 Scan 을 부르고 있다", w)
	}
}
