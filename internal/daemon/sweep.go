package daemon

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
)

// SweepPlan 은 호스트들의 기록 파일을 훑기 전에 나눈 결과다.
type SweepPlan struct {
	// Seed 는 **처음 보는 파일**이다. 끝으로 시딩만 하고 훑지 않는다.
	Seed []string
	// Scan 은 **이미 아는 파일**이다. 지난번 지점부터 자란 만큼 훑는다.
	Scan []string
	// Unreadable 은 못 읽은 디렉토리 수다. 조용히 줄어든 목록은
	// "결정이 없었다" 와 구별되지 않는다.
	Unreadable int
}

// PlanSweep 은 호스트들의 파일을 시딩 대상과 훑기 대상으로 나눈다.
//
// **규칙이 하나여야 한다.** 데몬의 기동 정리와 훅의 훑기가 각자 이 판단을 구현하면
// 반드시 어긋난다 — 한쪽만 시딩을 빠뜨리면 그 경로에서 pending 이 쏟아지고, 안전망이
// 소음이 되면 에이전트가 그것을 무시하는 법을 배운다. 실측으로 기존 transcript 가
// 1,173개였고 Codex 를 더하면 3,350개다.
//
//   - 처음 보는 파일 → **끝으로 시딩한다.** 도구를 깔기 전의 대화는 안전망 대상이
//     아니다. backfill 이면 시딩하지 않고 훑는다.
//   - 이미 아는 파일 → **훑는다.** 감시가 꺼져 있는 동안 자란 구간이 있을 수 있고,
//     그 파일은 다시 안 바뀌므로 여기서 안 잡으면 영원히 안 잡힌다.
//
// 빈 파일은 어느 쪽도 아니다 — 시딩할 지점이 없고 읽을 것도 없다.
func PlanSweep(st *Store, rs []hosts.Resolved, backfill bool) (SweepPlan, error) {
	var plan SweepPlan
	// **스냅샷을 한 번만 뜬다.** 파일마다 st.Checkpoint() 를 부르면 그때마다 상태
	// 파일을 다시 파싱한다 — 실측에서 그것 때문에 10초에 119개밖에 못 돌았다.
	cps := st.CheckpointSnapshot()
	for _, r := range rs {
		got, unreadable, err := r.Host.List(r.Root)
		if err != nil {
			// **필수 호스트의 실패만 에러다.** Codex 를 안 쓰는 사람에게 그 자리가
			// 없다고 매번 알리면 진짜 문제를 가린다.
			if r.Host.Required {
				return plan, err
			}
			continue
		}
		plan.Unreadable += unreadable
		for _, p := range got {
			if cps[p].Offset != 0 {
				plan.Scan = append(plan.Scan, p)
				continue
			}
			if backfill {
				plan.Scan = append(plan.Scan, p)
				continue
			}
			plan.Seed = append(plan.Seed, p)
		}
	}
	return plan, nil
}

// SeedToEnd 는 처음 보는 파일의 체크포인트를 파일 끝에 찍는다. 찍은 수를 준다.
//
// 실패는 건너뛴다 — 파일 하나를 못 봐서 나머지 시딩이 멎으면, 그 나머지가 전부
// 처음부터 훑히면서 pending 이 쏟아진다.
func SeedToEnd(st *Store, paths []string) int {
	// **쓰기 한 번으로 끝낸다.** 파일마다 Advance 를 부르면 그때마다 상태 파일을
	// 통째로 다시 쓴다 — 처음 설치할 때 파일이 수천 개면 O(n²) 다. 실측에서
	// 3,360개에 11.2초가 걸렸고, 그건 사용자의 첫 세션 종료가 그만큼 멎는 것이다.
	sizes := make(map[string]int64, len(paths))
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil || fi.Size() == 0 {
			continue
		}
		sizes[p] = fi.Size()
	}
	if err := st.SeedAll(sizes); err != nil {
		return 0
	}
	return len(sizes)
}

// SweepOptions 는 훅이 도는 훑기의 설정이다.
type SweepOptions struct {
	StateDir string
	Config   *config.Config
	Layout   *store.Layout
	// Skip 은 이미 훑은 파일이다 (훅이 자기 transcript 를 먼저 처리한다).
	Skip           string
	JudgeAvailable bool
	// Budget 은 이 훑기에 쓸 수 있는 총 시간이다.
	Budget time.Duration
	Err    io.Writer
}

// SweepResult 는 훑기 한 판의 결과다.
type SweepResult struct {
	Seeded  int
	Scanned int
	Flagged int
	// Skipped 는 예산이 모자라 못 본 파일 수다. **0 이 아니면 훑기가 불완전하다** —
	// 조용히 넘기면 "결정이 없었다" 로 보인다.
	Skipped int
	Errs    int
}

// SweepOnce 는 데몬이 없을 때 **다른 호스트의 기록까지** 훑는다.
//
// # 왜 필요한가
//
// 훅은 자기를 부른 호스트의 transcript 하나만 받는다. 그래서 Codex CLI 파서를
// 붙여도 **데몬을 켜지 않으면 영영 안 읽힌다** — 실측에서 상태 파일의 체크포인트
// 162개가 전부 Claude Code 였고 Codex 는 0개였다. 파서는 있는데 연료가 안 들어가는
// 상태고, 그건 조용히 실패한다.
//
// # 데몬이 돌면 아무것도 안 한다
//
// 같은 락을 쓴다. 얻으면 데몬이 없는 것이므로 우리가 훑고, 못 얻으면 데몬이
// 그 일을 하고 있다. 소유자가 언제나 하나뿐이라 중복 처리가 구조적으로 불가능하다.
//
// # 예산을 넘기면 멈춘다
//
// 이건 세션이 끝날 때 도는 코드라 **사람이 그 시간을 그대로 겪는다.** 파일이
// 3,350개이므로 상한이 없으면 세션 종료가 눈에 띄게 늦어진다. 자라지 않은 파일은
// stat 한 번으로 끝나지만(Scan 이 일찍 나간다) 그것도 수천 번이면 쌓인다.
func SweepOnce(o SweepOptions) (SweepResult, bool, error) {
	var r SweepResult
	if o.StateDir == "" || o.Config == nil {
		return r, false, nil
	}
	lk := flock.New(filepath.Join(o.StateDir, lockFile))
	got, err := lk.TryLock()
	if err != nil {
		return r, false, fmt.Errorf("락을 잡을 수 없다: %w", err)
	}
	if !got {
		return r, false, nil // prior watch 가 훑기의 주인이다
	}
	defer func() { _ = lk.Unlock() }()

	st := NewStore(o.StateDir)
	if err := st.Load(); err != nil {
		return r, true, err
	}
	rs, err := hosts.Resolve("")
	if err != nil {
		return r, true, err
	}
	return sweepPlanned(o, st, rs)
}

// sweepWithHosts 는 호스트 목록을 지정해 훑는다. 테스트가 진짜 홈 디렉토리를
// 건드리지 않게 하는 문이다 — 그게 없으면 예산 검증을 사용자의 3,350개 파일로
// 하게 된다.
func sweepWithHosts(o SweepOptions, rs []hosts.Resolved) (SweepResult, bool, error) {
	var r SweepResult
	lk := flock.New(filepath.Join(o.StateDir, lockFile))
	got, err := lk.TryLock()
	if err != nil {
		return r, false, fmt.Errorf("락을 잡을 수 없다: %w", err)
	}
	if !got {
		return r, false, nil
	}
	defer func() { _ = lk.Unlock() }()
	st := NewStore(o.StateDir)
	if err := st.Load(); err != nil {
		return r, true, err
	}
	return sweepPlanned(o, st, rs)
}

func sweepPlanned(o SweepOptions, st *Store, rs []hosts.Resolved) (SweepResult, bool, error) {
	var r SweepResult
	budget := o.Budget
	if budget == 0 {
		budget = DefaultSweepBudget
	}
	deadline := time.Now().Add(budget)
	plan, err := PlanSweep(st, rs, false)
	if err != nil {
		return r, true, err
	}
	r.Seeded = SeedToEnd(st, plan.Seed)
	// 시딩이 상태를 바꿨으므로 다시 뜬다. 그 뒤로는 루프 안에서 다시 읽지 않는다.
	cps := st.CheckpointSnapshot()

	for i, p := range plan.Scan {
		if p == o.Skip {
			continue // 훅이 방금 훑었다
		}
		if time.Now().After(deadline) {
			r.Skipped = len(plan.Scan) - i
			break
		}
		// **자라지 않은 파일은 Scan 을 부르지도 않는다.**
		//
		// Scan 은 읽을 것이 없으면 일찍 나가지만, 나가면서 NoteScan 으로 "방금 훑음"
		// 흔적을 남긴다 — 그건 **상태 파일 전체를 다시 쓰는 일**이다. 파일 하나에
		// 한 번씩이면 3,000개를 도는 동안 상태 파일을 3,000번 쓴다.
		//
		// 실측으로 잡혔다. 시딩이 끝난 뒤에도 10초 예산에 4개밖에 못 돌았고, 다음
		// 판에도 123개였다 — 그 속도로는 3,300개를 도는 데 세션 수십 번이 걸린다.
		// stat 으로 걸러내니 그 비용이 사라진다.
		//
		// 흔적을 잃지 않는다: 훅은 자기 transcript 를 Scan 으로 처리하므로 "안전망이
		// 돌았다" 는 증거는 그쪽에서 남는다.
		if fi, err := os.Stat(p); err == nil && !cps[p].Behind(fi.Size()) {
			continue
		}
		res, serr := Scan(st, o.Config, o.Layout, p, o.JudgeAvailable, rs)
		if serr != nil {
			r.Errs++
			continue
		}
		r.Scanned++
		if res.Flagged {
			r.Flagged++
		}
	}
	return r, true, nil
}

// DefaultSweepBudget 은 훅이 다른 호스트를 훑는 데 쓸 시간이다.
//
// 짧다. 이건 세션이 끝날 때 도는 코드이고 그 앞에 이미 승격(예산 90초)이 있다 —
// 훅 상한(120초) 안에 둘 다 들어가야 한다. 자라지 않은 파일은 stat 한 번이라
// 대부분의 판은 훨씬 빨리 끝난다.
//
// 예산이 모자라 못 본 파일은 다음 세션에서 다시 후보가 된다. 파일이 사라지지 않는
// 한 기회는 계속 온다 — 한 판에 다 하려다 세션 종료를 늦추는 것이 더 나쁘다.
const DefaultSweepBudget = 10 * time.Second
