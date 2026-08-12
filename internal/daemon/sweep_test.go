package daemon

import (
	"context"
	"fmt"
	"os"
	"reflect"
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

// ★★ **진행이 없는 항목만 지운다.**
//
// 사라진 파일을 가리키는 항목은 늘기만 한다 — 실측에서 175개 중 13개였고 하루
// 3개꼴로 는다. 그런데 **진행이 있는 항목을 지우면** 그 파일이 돌아왔을 때 0부터
// 다시 훑어 pending 이 쏟아진다. 외장 디스크·네트워크 마운트가 그 상황을 만든다.
func TestPruneMissingKeepsProgress(t *testing.T) {
	root := t.TempDir()
	sd := t.TempDir()
	st := NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	alive := writeJSONL(t, root, "alive.jsonl", 3)
	goneEmpty := filepath.Join(root, "gone-empty.jsonl")
	goneProgress := filepath.Join(root, "gone-progress.jsonl")
	goneCredit := filepath.Join(root, "gone-credit.jsonl")

	if err := st.Advance(alive, 10, 10); err != nil {
		t.Fatal(err)
	}
	// 훑은 흔적만 있고 진행은 없는 항목 (실측의 13개가 이 모양이다).
	if err := st.NoteScan(goneEmpty, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	// 진행이 있는 항목.
	if err := st.Advance(goneProgress, 500, 500); err != nil {
		t.Fatal(err)
	}
	// 바이트는 0 인데 크레딧을 소모한 항목 — 이것도 지우면 안 된다.
	if err := st.mutate(func(s *state) {
		cp := s.Checkpoints[goneCredit]
		cp.SessionCredited = 2
		s.Checkpoints[goneCredit] = cp
	}); err != nil {
		t.Fatal(err)
	}

	n, err := PruneMissing(st)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d개를 지웠다, 1이어야 한다", n)
	}
	cps := st.CheckpointSnapshot()
	if _, ok := cps[goneEmpty]; ok {
		t.Error("진행 없는 항목이 안 지워졌다")
	}
	if _, ok := cps[goneProgress]; !ok {
		t.Error("★ 진행이 있는 항목을 지웠다 — 파일이 돌아오면 0부터 훑어 pending 이 쏟아진다")
	}
	if _, ok := cps[goneCredit]; !ok {
		t.Error("★ 크레딧을 소모한 항목을 지웠다 — 면제가 무한해진다")
	}
	if _, ok := cps[alive]; !ok {
		t.Error("★ 살아 있는 파일의 항목을 지웠다")
	}
}

// ★ 정리는 상태 파일을 한 번만 써야 한다.
func TestPruneMissingWritesOnce(t *testing.T) {
	sd := t.TempDir()
	st := NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := st.NoteScan(fmt.Sprintf("/없는곳/s%02d.jsonl", i), time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	before := st.Writes()
	n, err := PruneMissing(st)
	if err != nil {
		t.Fatal(err)
	}
	if n != 20 {
		t.Fatalf("%d개를 지웠다", n)
	}
	if w := st.Writes() - before; w != 1 {
		t.Errorf("항목 20개를 지우는데 상태 파일을 %d번 썼다 — 한 번이어야 한다", w)
	}
}

// ★★ **판단할 수 없으면 건드리지 않는다.**
//
// 권한 오류는 "파일이 없다" 가 아니다. 부모 디렉토리의 권한이 잠깐 바뀌었을 때
// 그것을 삭제 신호로 읽으면, 권한을 되돌린 순간 그 파일들이 전부 0부터 훑힌다.
func TestPruneMissingIgnoresUnreadable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root 는 권한 검사를 우회한다")
	}
	root := t.TempDir()
	sub := filepath.Join(root, "locked")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(sub, "s.jsonl")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sd := t.TempDir()
	st := NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	if err := st.NoteScan(p, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(sub, 0o755) }()

	n, err := PruneMissing(st)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("읽을 수 없는 자리의 항목을 %d개 지웠다 — 권한이 돌아오면 0부터 훑는다", n)
	}
}

// ★★ **정리가 실제로 불리는지 본다.**
//
// 함수만 시험하면 호출부를 떼어내도 안 잡힌다. 이 세션에서만 그 부류의 결함을
// 두 번 냈다 (similarFor · sweepOthers) — 함수에는 통과하는 테스트가 있는데
// 아무도 부르지 않는 상태다.
func TestSweepPrunesMissingCheckpoints(t *testing.T) {
	root := t.TempDir()
	sd := t.TempDir()
	st := NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, root, "a.jsonl", 3)
	// 사라진 파일 · 진행 없음.
	ghost := filepath.Join(root, "ghost.jsonl")
	if err := st.NoteScan(ghost, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	r, _, err := sweepWithHosts(SweepOptions{
		StateDir: sd, Config: scanCfg(), Budget: time.Minute},
		[]hosts.Resolved{fakeHost(t, root, true)})
	if err != nil {
		t.Fatal(err)
	}
	if r.Pruned != 1 {
		t.Errorf("정리 %d건 — 훑기가 PruneMissing 을 안 부른다", r.Pruned)
	}
	if _, ok := st.CheckpointSnapshot()[ghost]; ok {
		t.Error("사라진 항목이 남았다")
	}
}

// ★★ **Empty() 는 필드 화이트리스트라 새 필드가 늘면 조용히 삭제 범위가 넓어진다.**
//
// 컴파일러도 기존 테스트도 안 잡는다. 폴백이 "지운다" 인 곳은 여기 하나뿐이라
// (Advance·SeedAll·Credit 은 읽고-덮기라 자동으로 안전하다) 여기만 지킨다.
func TestCheckpointEmptyCoversEveryField(t *testing.T) {
	const known = 6 // Offset · Size · At · SessionCredited · DayCredited · Suppressed
	if n := reflect.TypeOf(Checkpoint{}).NumField(); n != known {
		t.Fatalf("Checkpoint 필드가 %d개다 (알던 것 %d개) — Empty() 가 새 필드를 "+
			"안 보면 그 정보만 가진 항목이 조용히 지워진다. Empty() 를 고치고 이 수를 갱신하라", n, known)
	}
	// At 만 있는 것은 Empty 다 (실측의 13개가 이 모양이었다).
	if !(Checkpoint{At: time.Now()}).Empty() {
		t.Error("훑은 흔적만 있는 항목이 Empty 가 아니다 — 정리가 아무것도 못 지운다")
	}
	// 나머지 다섯은 하나라도 있으면 Empty 가 아니다.
	for name, cp := range map[string]Checkpoint{
		"Offset":          {Offset: 1},
		"Size":            {Size: 1},
		"SessionCredited": {SessionCredited: 1},
		"DayCredited":     {DayCredited: map[string]int{"2026-08-13": 1}},
		"Suppressed":      {Suppressed: 1},
	} {
		if cp.Empty() {
			t.Errorf("%s 가 있는데 Empty 다 — 그 정보가 지워진다", name)
		}
	}
}

// ★★ **잠금 밖에서 뽑은 삭제 목록을 잠금 안에서 다시 봐야 한다.**
//
// doomed 는 스냅샷으로 뽑고 그 뒤 os.Stat 을 수천 번 돈다. 그 창에서 남이 크레딧을
// 새길 수 있다 — 승격은 watch.lock 게이트 밖이고(D12), pending 은 발췌를 들고 있어
// 파일이 없어도 승격이 돈다. 하필 그 경로가 삭제 표적이다.
func TestPruneMissingRechecksInsideLock(t *testing.T) {
	sd := t.TempDir()
	st := NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	target := "/없는곳/s.jsonl"
	if err := st.NoteScan(target, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	// 스냅샷 시점에는 Empty 였다.
	if !st.CheckpointSnapshot()[target].Empty() {
		t.Fatal("전제가 깨졌다")
	}
	// 그 뒤(= stat 루프가 도는 동안) 남이 크레딧을 새긴다.
	if err := st.mutate(func(s *state) {
		cp := s.Checkpoints[target]
		cp.SessionCredited = 3
		s.Checkpoints[target] = cp
	}); err != nil {
		t.Fatal(err)
	}

	// **낡은 목록을 그대로 넘긴다.** 실제 경합에서 doomed 가 이 모양이다.
	n, err := pruneDoomed(st, []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d개를 지웠다 — 잠금 안에서 다시 안 봤다", n)
	}
	if cp := st.CheckpointSnapshot()[target]; cp.SessionCredited != 3 {
		t.Errorf("★ 그 사이 새긴 크레딧이 사라졌다: %+v", cp)
	}
}

// ★★★ **정리는 동작을 바꾸지 않는다. 그것이 이 기능의 안전 근거다.**
//
// PlanSweep 은 `cps[p].Offset != 0` 으로 "아는 파일" 을 판정한다. Empty 항목은
// Offset 이 0 이므로 **항목이 아예 없는 것과 똑같이** 분류된다 — 둘 다 시딩이다.
// 그래서 Empty 항목을 지우는 것은 관측 가능한 변화를 만들지 않는다.
//
// 이 논거를 한 번 잃은 적이 있다. 같은 작업에서 판정을 "항목의 존재" 로 바꿨더니
// 지우는 것이 곧 "끝으로 시딩" 이 되어 대화가 사라졌다 — 적대적 검증이 재현해서
// 잡았고 그 변경을 되돌렸다. **판정을 다시 건드리면 이 테스트가 먼저 깨진다.**
func TestPruneIsBehaviorNeutral(t *testing.T) {
	build := func(t *testing.T, prune bool) SweepPlan {
		t.Helper()
		root := t.TempDir()
		sd := t.TempDir()
		st := NewStore(sd)
		if err := st.Load(); err != nil {
			t.Fatal(err)
		}
		hs := []hosts.Resolved{fakeHost(t, root, true)}

		live := writeJSONL(t, root, "live.jsonl", 3)
		// 사라진 파일의 Empty 항목 — 정리 대상.
		if err := st.NoteScan(filepath.Join(root, "gone.jsonl"), time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		// 살아 있는 파일의 Empty 항목 — 정리 대상이 아니다.
		if err := st.NoteScan(live, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		if prune {
			if n, err := PruneMissing(st); err != nil || n != 1 {
				t.Fatalf("정리 %d건 err=%v", n, err)
			}
		}
		plan, err := PlanSweep(st, hs, false)
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	base := func(p SweepPlan) (int, int) { return len(p.Seed), len(p.Scan) }
	s1, c1 := base(build(t, false))
	s2, c2 := base(build(t, true))
	if s1 != s2 || c1 != c2 {
		t.Errorf("정리가 분류를 바꿨다: 정리없음 Seed=%d Scan=%d · 정리함 Seed=%d Scan=%d\n"+
			"  Empty 항목은 항목이 없는 것과 같게 분류돼야 한다 — 아니면 삭제가 곧 손실이다",
			s1, c1, s2, c2)
	}
}

// ★★ **Empty 항목과 항목 없음은 같게 분류돼야 한다.**
//
// 위 중립성의 근거를 직접 못 박는다. 판정을 "항목의 존재" 로 바꾸면 여기서 깨진다.
func TestEmptyEntryClassifiesLikeNoEntry(t *testing.T) {
	root := t.TempDir()
	hs := []hosts.Resolved{fakeHost(t, root, true)}
	p := writeJSONL(t, root, "a.jsonl", 3)

	withEmpty := func() SweepPlan {
		st := NewStore(t.TempDir())
		if err := st.Load(); err != nil {
			t.Fatal(err)
		}
		if err := st.NoteScan(p, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		plan, err := PlanSweep(st, hs, false)
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}()
	withNothing := func() SweepPlan {
		st := NewStore(t.TempDir())
		if err := st.Load(); err != nil {
			t.Fatal(err)
		}
		plan, err := PlanSweep(st, hs, false)
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}()
	if len(withEmpty.Seed) != len(withNothing.Seed) || len(withEmpty.Scan) != len(withNothing.Scan) {
		t.Errorf("Empty 항목(Seed=%d Scan=%d)과 항목 없음(Seed=%d Scan=%d)이 다르게 분류된다",
			len(withEmpty.Seed), len(withEmpty.Scan), len(withNothing.Seed), len(withNothing.Scan))
	}
}

// ★★ **데몬 경로도 정리를 부르는지 본다.**
//
// 정리를 훑기에만 넣으면 데몬을 켠 사용자는 한 번도 안 돈다 — 훑기는 락을 못 얻으면
// 통째로 건너뛰기 때문이다. 1차 적대적 검증이 정확히 그 구멍을 지적했다.
//
// 그리고 이건 이 세션에서 **세 번째**로 나온 같은 부류다 (similarFor · sweepOthers ·
// 여기). 함수에는 테스트가 있는데 호출부에는 없는 상태 — 변형 테스트만이 잡는다.
func TestDaemonStartupPassPrunes(t *testing.T) {
	root := t.TempDir()
	sd := t.TempDir()
	st := NewStore(sd)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, root, "a.jsonl", 3)
	ghost := filepath.Join(root, "ghost.jsonl")
	if err := st.NoteScan(ghost, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	d := &watcher{
		o:     Options{StateDir: sd, Config: scanCfg(), TranscriptRoot: root},
		st:    st,
		dirty: map[string]bool{},
		hosts: []hosts.Resolved{fakeHost(t, root, true)},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // drain 은 돌리지 않는다 — 정리가 불리는지만 본다
	d.startupPass(ctx)

	if _, ok := st.CheckpointSnapshot()[ghost]; ok {
		t.Error("데몬 기동 정리가 사라진 항목을 안 지웠다 — 데몬 사용자는 영영 안 준다")
	}
}
