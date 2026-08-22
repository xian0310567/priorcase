// Package sync 는 볼트를 git 리모트와 맞춘다.
//
// **이 패키지의 실패는 대화를 막지 않는다.** 훅이 세션 경계에서 이걸 부르는데,
// 회사망에서 네트워크가 막혔거나 인증이 만료됐다고 대화가 멎으면 안 된다.
// 그래서 모든 실패는 Result 에 실려 나가고, 부르는 쪽이 조용히 넘어갈 수 있다.
//
// **대신 조용한 실패를 doctor 가 잡는다.** 일주일간 push 가 죽어 있어도 모르면
// 그게 이 프로젝트가 계속 경계해 온 "조용한 무동작" 이다 — 마지막 동기화 시각을
// 상태 디렉토리에 남기고(Stamp), doctor 가 그것을 읽는다.
//
// git 을 직접 부른다. go-git 같은 라이브러리를 안 쓰는 이유는 인증 때문이다 —
// 사용자는 이미 자기 머신에서 git push 가 되게 해 뒀고(credential helper·SSH
// 에이전트·gh), 라이브러리를 쓰면 그 배선을 우리가 다시 만들어야 한다.
package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
)

// timeout 은 git 한 번의 상한이다.
//
// 훅이 세션 경계에서 부르므로 이건 **사람을 붙잡는 시간**이다. 네트워크가 죽으면
// git 은 그냥 매달리고, 그러면 세션 종료가 같이 멎는다 — judge 가 상한을 두는
// 것과 같은 이유다.
//
// 변수인 것은 테스트가 줄이기 위해서다. 밖으로 내지 않는다.
var timeout = 20 * time.Second

// Result 는 동기화 시도 하나의 결과다.
type Result struct {
	Pulled  bool
	Pushed  bool
	Files   int    // 이번에 커밋한 파일 수
	Skipped string // 아무것도 안 한 이유 (리모트 없음 등). 고장이 아니다.
	Err     error
}

// OK 는 실패도 건너뜀도 아닌지 본다.
func (r Result) OK() bool { return r.Err == nil && r.Skipped == "" }

// Pull 은 리모트의 변경을 가져온다.
//
// `--rebase --autostash` 를 쓴다. 볼트는 대부분 새 파일이 붙는 곳이라 rebase 가
// 깨끗하고, autostash 는 **작업 중인 로컬 변경을 잃지 않으려는 것**이다 —
// 세션 시작 시점에 어제 쓰다 만 노트가 커밋 안 된 채 있을 수 있다.
func Pull(vault string) Result { return pull(vault, timeout) }

func pull(vault string, budget time.Duration) Result {
	if s := precheck(vault, budget); s != "" {
		return Result{Skipped: s}
	}
	if _, err := run(vault, budget, "pull", "--rebase", "--autostash"); err != nil {
		return Result{Err: fmt.Errorf("pull 실패: %w", err)}
	}
	return Result{Pulled: true}
}

// Push 는 로컬 변경을 커밋해 리모트로 보낸다.
//
// # 왜 `add -A` 인가
//
// [[common-결정-볼트에서-git-add-A-를-쓰지않는다-2026-08-14]] 는 `add -A` 를
// 금지한다. 그 근거는 **여러 에이전트가 같은 볼트를 쓰므로 남이 만든 것을 확인
// 없이 커밋한다** 였다. 동기화는 그 전제가 뒤집히는 자리다 — 목적 자체가 "이
// 머신에 있는 것을 저 머신에서도 보이게" 이고, 빠뜨린 파일은 커밋 안 된 채
// 남는 것이 아니라 **다른 머신에서 영영 안 보이는 것**이 된다.
//
// 대신 무엇이 들어갔는지 Result.Files 로 돌려주어 부르는 쪽이 보여 줄 수 있게 한다.
// 조용히 담지 않는다.
func Push(vault, message string) Result { return push(vault, message, timeout) }

func push(vault, message string, budget time.Duration) Result {
	if s := precheck(vault, budget); s != "" {
		return Result{Skipped: s}
	}
	if _, err := run(vault, budget, "add", "-A"); err != nil {
		return Result{Err: fmt.Errorf("add 실패: %w", err)}
	}
	staged, err := run(vault, budget, "diff", "--cached", "--name-only")
	if err != nil {
		return Result{Err: fmt.Errorf("변경 확인 실패: %w", err)}
	}
	n := len(nonEmptyLines(staged))
	if n > 0 {
		if _, err := run(vault, budget, "commit", "-m", message); err != nil {
			return Result{Err: fmt.Errorf("commit 실패: %w", err)}
		}
	}
	// **커밋할 것이 없어도 push 는 한다.** 지난번에 커밋은 됐는데 push 가 실패해
	// 밀리지 않은 커밋이 남아 있을 수 있다 — 그게 정확히 이 도구가 막아야 할 상태다.
	if _, err := run(vault, budget, "push"); err != nil {
		return Result{Files: n, Err: fmt.Errorf("push 실패: %w", err)}
	}
	return Result{Pushed: true, Files: n}
}

// precheck 는 동기화를 할 수 없는 정상적인 이유를 준다. 없으면 빈 문자열.
func precheck(vault string, budget time.Duration) string {
	if _, err := run(vault, budget, "rev-parse", "--git-dir"); err != nil {
		return "볼트가 git 저장소가 아니다"
	}
	out, err := run(vault, budget, "remote")
	if err != nil || strings.TrimSpace(out) == "" {
		return "리모트가 없다"
	}
	return ""
}

func run(dir string, budget time.Duration, args ...string) (string, error) {
	if budget <= 0 {
		budget = timeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	c := exec.CommandContext(ctx, "git", args...)
	c.Dir = dir
	// 상한이 걸린 뒤에도 파이프가 안 닫혀 Wait 가 매달릴 수 있다 — judge 가 겪은
	// 것과 같은 자리다. 이미 찍힌 출력을 옮길 시간만 주고 끊는다.
	//
	// **예산에 비례시킨다.** 고정 2초로 두면 짧은 예산에서 그것이 예산을 지배한다 —
	// 200ms 예산에 2초 유예면 명령 하나가 2.2초다. 예산을 짧게 준 의미가 없어진다.
	c.WaitDelay = min(budget/4, 2*time.Second)
	// **대화형으로 빠지지 않게 한다.** 인증이 만료되면 git 이 자격증명을 물으려
	// 멈추는데, 훅에는 사람이 답할 터미널이 없어 상한까지 그냥 매달린다.
	c.Env = append(c.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	out, err := c.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%w — %s", err, strings.TrimSpace(head(string(out), 300)))
	}
	return string(out), nil
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// State 는 지금 볼트가 리모트와 얼마나 어긋나 있는지다.
//
// **doctor 가 볼 것은 시각이 아니라 이것이다.** "마지막 동기화가 N일 전" 은
// 며칠 안 썼을 때 거짓 경고가 되지만, **쓴 것이 안 밀린 것**은 언제 그랬든
// 손해다 — 다른 머신에서 그 결정이 안 보인다.
type State struct {
	HasRemote bool
	Ahead     int // 밀리지 않은 커밋
	Dirty     int // 커밋되지 않은 파일
}

// Status 는 지금 상태를 본다. 네트워크를 안 탄다 — 로컬이 아는 것만 읽는다.
func Status(vault string) State {
	if precheck(vault, timeout) != "" {
		return State{}
	}
	s := State{HasRemote: true}
	if out, err := run(vault, timeout, "status", "--porcelain"); err == nil {
		s.Dirty = len(nonEmptyLines(out))
	}
	// @{upstream} 이 없으면(브랜치가 추적을 안 하면) 셀 것이 없다. 에러가 아니다.
	if out, err := run(vault, timeout, "rev-list", "--count", "@{upstream}..HEAD"); err == nil {
		fmt.Sscanf(strings.TrimSpace(out), "%d", &s.Ahead)
	}
	return s
}

// Stamp 는 마지막 동기화 시도의 결과다.
//
// **조용한 실패를 잡을 유일한 근거다.** 훅은 실패해도 대화를 막지 않으므로,
// 여기 남기지 않으면 일주일간 push 가 죽어 있어도 아무도 모른다.
type Stamp struct {
	At     time.Time `json:"at"`
	OK     bool      `json:"ok"`
	Detail string    `json:"detail"`
}

// stampName 은 상태 디렉토리 안의 파일 이름이다.
//
// **상태 디렉토리는 동기화 대상이 아니다.** 여기 있는 것은 머신마다 다르다 —
// pending 이 그 머신의 transcript 절대 경로를 키로 쓰기 때문이다. 이 도장도
// 마찬가지로 "이 머신이 마지막으로 언제 밀었나" 라 머신 것이다.
const stampName = "sync.json"

func WriteStamp(stateDir string, s Stamp) error {
	if stateDir == "" {
		return fmt.Errorf("상태 디렉토리가 없다")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, stampName), b, 0o600)
}

// ReadStamp 는 마지막 도장을 읽는다. 없으면 ok=false — 고장이 아니라 "아직 안 했다" 다.
func ReadStamp(stateDir string) (Stamp, bool) {
	if stateDir == "" {
		return Stamp{}, false
	}
	b, err := os.ReadFile(filepath.Join(stateDir, stampName))
	if err != nil {
		return Stamp{}, false
	}
	var s Stamp
	if err := json.Unmarshal(b, &s); err != nil {
		return Stamp{}, false
	}
	return s, true
}

// VaultResult 는 볼트 하나의 동기화 결과다 (pull·push 를 합쳐서).
type VaultResult struct {
	Name    string
	Path    string
	Results []Result
}

// Failed 는 **진짜 실패**인지 본다. 건너뜀은 실패가 아니다 —
// 리모트가 없는 볼트는 고장이 아니라 "동기화를 안 쓰는 볼트" 다.
func (v VaultResult) Failed() bool {
	for _, r := range v.Results {
		if r.Err != nil {
			return true
		}
	}
	return false
}

// OK 는 실제로 동기화가 일어났는지 본다.
func (v VaultResult) OK() bool {
	if v.Failed() || len(v.Results) == 0 {
		return false
	}
	for _, r := range v.Results {
		if r.Skipped != "" {
			return false
		}
	}
	return true
}

// Files 는 이번에 커밋한 파일 수다.
func (v VaultResult) Files() int {
	n := 0
	for _, r := range v.Results {
		n += r.Files
	}
	return n
}

// All 은 선언된 볼트를 전부 돈다.
//
// **하나가 실패해도 나머지는 계속한다.** 한 리모트의 인증이 만료됐다고 다른
// 볼트까지 안 밀 이유가 없다.
//
// 순회가 core 에 있는 이유: CLI 와 훅이 둘 다 이걸 부르는데 어댑터끼리는 서로를
// import 할 수 없다(§4.1). 공유할 것이 생기면 core 로 내린다 — 어댑터는 렌더링만 한다.
// Options 는 동기화 한 번의 조건이다.
type Options struct {
	// Timeout 은 git 한 번의 상한이다.
	//
	// **훅이 부를 때는 짧아야 한다.** SessionStart 의 pull 은 에이전트가 컨텍스트를
	// 받기 전에 사람이 기다리는 시간이다 — 회사 VPN 이나 캡티브 포털에서 git 이
	// 매달리면 매 세션이 그만큼 느려진다. 못 가져오면 그 세션은 어제 것으로 돌지만,
	// 그 손해가 매 세션 20초보다 작다.
	Timeout time.Duration

	// Stamp 는 볼트에 남길 이 머신의 판이다. 비면 안 남긴다.
	//
	// **밀 때만 남긴다** — pull 만 하는 자리에서 남기면 커밋 안 된 파일이 생겨
	// doctor 가 "안 밀렸다" 를 띄우고, 사람은 아무것도 안 했는데 경고를 본다.
	Stamp Build
}

func All(c *config.Config, o Options, doPull, doPush bool, message string) []VaultResult {
	if c == nil {
		return nil
	}
	out := make([]VaultResult, 0, len(c.Vaults))
	for _, v := range c.Vaults {
		vr := VaultResult{Name: v.Name, Path: v.Path}
		if doPull {
			vr.Results = append(vr.Results, pull(v.Path, o.Timeout))
		}
		if doPush {
			// **밀기 직전에 도장을 남긴다.** 그래야 같은 커밋에 실려 저쪽이 본다.
			// 실패해도 동기화를 막지 않는다 — 도장은 곁다리다.
			if o.Stamp.Host != "" {
				_ = RecordBuild(v.Path, o.Stamp)
			}
			vr.Results = append(vr.Results, push(v.Path, message, o.Timeout))
		}
		out = append(out, vr)
	}
	return out
}

// CommitMessage 는 동기화 커밋의 문구다.
//
// **머신 이름이 들어가야 한다.** 두 머신을 오가는 것이 이 기능의 목적인데,
// 원장에 "sync" 만 스무 줄 있으면 어디서 뭐가 왔는지 못 가린다.
//
// core 가 갖는 이유: CLI 와 훅이 둘 다 커밋을 만들고, 어댑터끼리는 서로를
// import 할 수 없다(§4.1). 형식이 두 벌이 되면 원장이 갈라진다.
func CommitMessage(now time.Time) string {
	h := hostname()
	if h == "" {
		h = "unknown"
	}
	return fmt.Sprintf("sync(%s): %s", h, now.Format("2006-01-02 15:04"))
}
