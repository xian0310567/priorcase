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
	"sort"
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
		// **반쯤 진행된 rebase 를 남기지 않는다.**
		//
		// 2026-09-01 실측: 공유 볼트에서 두 사람이 같은 작업 로그에 적으면
		// 충돌하는데, 그때 볼트가 rebase 중단 상태로 남았다. 그 상태는
		// **detached HEAD** 라 그 뒤로 pull 도 push 도 안 되고, 훅은 무슨 일이
		// 있어도 exit 0 이라 사람은 계속 잘 되는 줄 안다 — 기록이 로컬에만 쌓인다.
		//
		// abort 는 autostash 도 함께 되돌려 준다. 그래서 내가 쓰던 것을 안 잃는다.
		if aerr := abortRebase(vault, budget); aerr != nil {
			return Result{Err: fmt.Errorf("pull 실패, 되돌리기도 실패했다 — 볼트를 손으로 봐야 한다: %w (되돌리기: %v)", err, aerr)}
		}
		return Result{Err: fmt.Errorf("pull 실패: %w", err)}
	}
	return Result{Pulled: true}
}

// abortRebase 는 진행 중인 rebase 를 되돌린다. 진행 중이 아니면 아무것도 안 한다
// (네트워크 실패처럼 rebase 에 들어가지도 못한 경우가 흔하다).
func abortRebase(vault string, budget time.Duration) error {
	if !rebaseInProgress(vault, budget) {
		return nil
	}
	_, err := run(vault, budget, "rebase", "--abort")
	return err
}

// rebaseInProgress 는 git 에게 직접 묻는다. `.git` 은 디렉토리가 아니라 파일일
// 수 있어서(worktree·submodule) 경로를 손으로 조립하면 틀린다.
func rebaseInProgress(vault string, budget time.Duration) bool {
	dir, err := run(vault, budget, "rev-parse", "--git-path", "rebase-merge")
	if err == nil && exists(vault, strings.TrimSpace(dir)) {
		return true
	}
	dir, err = run(vault, budget, "rev-parse", "--git-path", "rebase-apply")
	return err == nil && exists(vault, strings.TrimSpace(dir))
}

func exists(vault, p string) bool {
	if p == "" {
		return false
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(vault, p)
	}
	_, err := os.Stat(p)
	return err == nil
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
	// **충돌 마커가 든 파일은 절대 안 담는다.**
	//
	// `add -A` 는 의도된 결정이지만(위 §) 그 대가로 무엇이든 담긴다. 충돌을
	// 손으로 풀다 만 파일이 그대로 커밋되면 한 사람의 충돌이 **팀 전원의 볼트로
	// 퍼진다.** 게다가 그 파일은 회수 대상이라, 이후 회수가 `<<<<<<<` 가 든
	// 텍스트를 과거 결정이라며 내놓는다.
	//
	// add 보다 **먼저** 본다. 담은 뒤에 막으면 커밋만 안 될 뿐 다음 세션이 그대로
	// 밀어내서, 한 번 늦출 뿐 결과가 같다.
	if bad, err := conflicted(vault, budget); err != nil {
		return Result{Err: err}
	} else if len(bad) > 0 {
		return Result{Err: fmt.Errorf("충돌이 안 풀린 파일이 있어 밀지 않는다 — 손으로 고쳐라: %s",
			strings.Join(bad, ", "))}
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
			// **공유 볼트는 작업 로그를 빼고 민다.** 밀기 직전이어야 한다 —
			// 개인 볼트로 쓰다 공유로 돌리는 것이 흔한 경로라, 이미 추적 중인
			// 파일을 떼어 내는 일까지 여기서 한다 (excludeWorklogs 의 §).
			//
			// 실패해도 동기화를 막지 않는다. 막으면 결정까지 못 가는데, 그건
			// 작업 로그가 한 번 더 섞이는 것보다 나쁘다. 대신 결과에 남긴다.
			if v.Shared {
				if err := excludeWorklogs(v.Path, c, o.Timeout); err != nil {
					vr.Results = append(vr.Results, Result{Err: err})
				}
			}
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

// ── 리모트 설정 ────────────────────────────────────────────────────────
//
// **이 패키지는 지금까지 리모트를 읽기만 했다** (precheck 의 `git remote`).
// 그래서 볼트를 새로 만들면 사람이 터미널에서 `git init` 과 `git remote add` 를
// 직접 쳐야 했고, 그건 앱만 받은 사람에게는 막힌 길이다.
//
// 볼트를 개인·회사로 가르기로 하면서(코드주권 결정 2026-08-31) 이게 진짜 문제가
// 됐다 — 회사 볼트는 **만들자마자 회사 리모트에 붙어야** 그 결정이 개인 머신에만
// 남지 않는다. 사업주 요구도 같다: "리모트는 앱에서 설정할 수 있어야 해."

// Remote 는 볼트의 origin URL 이다. 없으면 빈 문자열.
func Remote(vault string) (string, error) {
	out, err := run(vault, 0, "remote", "get-url", "origin")
	if err != nil {
		// 리모트가 없는 것은 고장이 아니다 — 아직 안 붙인 볼트다.
		if strings.Contains(out, "No such remote") || strings.Contains(out, "no such remote") {
			return "", nil
		}
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// SetRemote 는 볼트의 origin 을 정한다. 없으면 만들고 있으면 바꾼다.
//
// **git 저장소가 아니면 만들어 준다.** 앱에서 볼트를 추가한 사람에게 "먼저
// git init 을 치세요" 라고 말하는 것은 그 사람이 할 수 없는 일을 시키는 것이다.
//
// URL 은 검증하지 않는다 — CodeCommit·GitHub·사내 GitLab 이 전부 모양이 다르고,
// 우리가 아는 모양만 받으면 멀쩡한 주소를 거절한다. 틀린 주소는 첫 push 에서
// 드러나고 그때는 `prior doctor` 의 동기화 검사가 말한다.
func SetRemote(vault, url string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return fmt.Errorf("리모트 URL 이 비었다")
	}
	if _, err := run(vault, 0, "rev-parse", "--git-dir"); err != nil {
		if out, ierr := run(vault, 0, "init"); ierr != nil {
			return fmt.Errorf("git 저장소를 만들 수 없다: %w (%s)", ierr, strings.TrimSpace(out))
		}
	}
	// set-url 이 먼저다 — 이미 있는데 add 하면 "remote origin already exists" 로 죽는다.
	if _, err := run(vault, 0, "remote", "set-url", "origin", url); err == nil {
		return nil
	}
	if out, err := run(vault, 0, "remote", "add", "origin", url); err != nil {
		return fmt.Errorf("리모트를 붙일 수 없다: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// conflicted 는 작업 트리에서 **충돌이 안 풀린 파일**을 찾는다.
//
// # 무엇을 마커로 보는가
//
// 줄 **첫머리**의 `<<<<<<< ` 와 `>>>>>>> ` 둘 다 있을 때만 충돌로 본다.
//
// `=======` 는 안 본다 — 마크다운에서 그것은 제목 밑줄(setext)이라 볼트에
// 정당하게 존재한다. 첫머리로 한정하는 이유도 같다: 이 저장소의 결정문이 실제로
// git 충돌을 설명하며 그 문자열을 인용하고, 들여쓴 코드 예시로도 적는다.
// 인용을 고장이라 부르면 그 경고는 곧 무시당한다.
//
// # 왜 git 에게 안 묻는가
//
// `git diff --check` 는 마커를 잡지만 **staged 여부에 따라 안 보는 구간이 생긴다.**
// 여기는 add 보다 먼저 도는 자리라 그 구간이 정확히 우리가 봐야 할 곳이다.
// `ls-files -u` 는 rebase 를 abort 한 뒤 사람이 손으로 남긴 마커를 못 잡는다 —
// 그때 인덱스는 이미 깨끗하기 때문이다.
func conflicted(vault string, budget time.Duration) ([]string, error) {
	out, err := run(vault, budget, "status", "--porcelain", "-z")
	if err != nil {
		return nil, fmt.Errorf("변경 확인 실패: %w", err)
	}
	var bad []string
	for _, rel := range porcelainPaths(out) {
		b, rerr := os.ReadFile(filepath.Join(vault, rel))
		if rerr != nil {
			continue // 지워진 것이거나 못 읽는 것 — 마커가 있을 수 없다
		}
		if hasConflictMarkers(b) {
			bad = append(bad, rel)
		}
	}
	sort.Strings(bad)
	return bad, nil
}

// porcelainPaths 는 `status --porcelain -z` 에서 경로만 뽑는다.
//
// **-z 를 쓰는 이유**: 기본 출력은 한글·공백이 든 파일명을 따옴표로 감싸고
// 이스케이프한다. 이 볼트의 파일명은 거의 전부 한글이라 그 형식을 되돌리다
// 틀리면 검사가 조용히 아무것도 안 보게 된다.
//
// 이름이 바뀐 항목(`R`)은 레코드를 둘 낸다 — 새 이름은 상태 두 글자를 달고 오고
// **옛 이름은 경로만** 온다. 그래서 옛 이름 쪽은 앞 세 글자가 잘려 엉뚱한 경로가
// 되는데, 읽기가 실패해 조용히 넘어간다. 검사가 봐야 하는 것은 새 이름 쪽이고
// 그쪽은 온전하다.
func porcelainPaths(out string) []string {
	var paths []string
	for _, rec := range strings.Split(out, "\x00") {
		// 형식: XY + 공백 + 경로. 상태 두 글자와 구분 공백을 뗀다.
		if len(rec) < 4 {
			continue
		}
		paths = append(paths, rec[3:])
	}
	return paths
}

func hasConflictMarkers(b []byte) bool {
	var open, close bool
	for _, line := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(line, "<<<<<<< "):
			open = true
		case strings.HasPrefix(line, ">>>>>>> "):
			close = true
		}
		if open && close {
			return true
		}
	}
	return false
}

// ── 공유 볼트: 작업 로그를 동기화에서 뺀다 ──────────────────────────

// ignoreHeader 는 우리가 넣은 구획의 표식이다. 두 번 넣지 않으려고 이것을 찾는다.
const ignoreHeader = "# priorcase: 공유 볼트라 작업 로그는 동기화하지 않는다"

// excludeWorklogs 는 공유 볼트에서 작업 로그를 git 밖으로 내보낸다.
//
// # 왜 파일을 안 지우나
//
// **git 에서만 뺀다.** 파일은 제자리에 그대로 남아 회수·rollup·doctor 가 전부
// 하던 대로 돈다 — 작업 로그는 여전히 내 기록이고, 안 보이면 그건 손실이다.
// 볼트에 둔 것을 지우지 않는다는 규칙은 여기도 같다.
//
// # 왜 .gitignore 에 적나
//
// pathspec 으로 add 에서만 빼면 **왜 그 파일이 안 올라가는지 git 도구로는 알 수
// 없다.** 나중에 누가 `git status` 를 보고 "왜 안 뜨지" 로 한참 헤맨다.
// 파일에 이유를 적어 두면 그 물음에 파일이 직접 답한다.
//
// # 왜 이미 추적 중인 것도 떼나
//
// .gitignore 는 **추적 중인 파일에는 안 걸린다.** 개인 볼트로 쓰다 공유로 돌리는
// 것이 흔한 경로인데(사업주가 지금 그 길이다), 새로 만든 것만 막으면 옛 파일은
// 계속 동기화되어 충돌이 그대로 남는다.
func excludeWorklogs(vault string, c *config.Config, budget time.Duration) error {
	pat := worklogGlob(c)
	if pat == "" {
		return nil // 작업 로그 규약이 없다 — 뺄 것도 없다
	}
	if err := ensureIgnored(vault, pat); err != nil {
		return err
	}
	return untrack(vault, pat, budget)
}

// worklogGlob 은 `99-{project}-작업-로그.md` 를 `99-*-작업-로그.md` 로 바꾼다.
//
// 프로젝트 이름을 하나씩 나열하지 않는 이유: 도메인은 늘어나는데 .gitignore 는
// 그때 안 고쳐진다. 늘어난 프로젝트의 로그만 조용히 새어 나가는 것이 정확히
// 이 프로젝트가 경계하는 실패다.
func worklogGlob(c *config.Config) string {
	t := strings.TrimSpace(c.Naming.Worklog)
	if t == "" || !strings.Contains(t, "{project}") {
		return ""
	}
	return strings.ReplaceAll(t, "{project}", "*")
}

// ensureIgnored 는 .gitignore 에 패턴을 **한 번만** 넣는다.
//
// 동기화는 세션마다 도므로 멱등이 아니면 그 파일이 곧 같은 줄 수백 개가 된다.
func ensureIgnored(vault, pat string) error {
	p := filepath.Join(vault, ".gitignore")
	raw, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(".gitignore 를 읽을 수 없다: %w", err)
	}
	cur := string(raw)
	if strings.Contains(cur, ignoreHeader) {
		return nil
	}
	var b strings.Builder
	b.WriteString(cur)
	if cur != "" && !strings.HasSuffix(cur, "\n") {
		b.WriteString("\n")
	}
	if cur != "" {
		b.WriteString("\n")
	}
	b.WriteString(ignoreHeader + "\n")
	b.WriteString("# 프로젝트당 파일 하나에 여럿이 같은 자리에 붙여 충돌이 일상이 된다.\n")
	b.WriteString("# 파일은 그대로 있고 회수도 계속 읽는다 — 리모트로만 안 간다.\n")
	b.WriteString(pat + "\n")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf(".gitignore 를 쓸 수 없다: %w", err)
	}
	return nil
}

// untrack 은 이미 추적 중인 작업 로그를 인덱스에서만 뺀다 (`--cached`).
//
// 맞는 파일이 없으면 git 이 실패하는데 그건 고장이 아니라 **정상**이다 —
// 처음부터 공유였던 볼트가 그렇다.
func untrack(vault, pat string, budget time.Duration) error {
	// **앞에 `*` 를 붙인다.** 작업 로그는 프로젝트 폴더 안에 있는데
	// (`priorcase/99-priorcase-작업-로그.md`) git pathspec 의 glob 은 그냥 두면
	// 최상위만 본다. `.gitignore` 는 슬래시 없는 패턴을 모든 깊이에서 맞추므로
	// 그쪽과 뜻이 갈리는데, 그 차이가 조용해서 "무시는 되는데 추적은 계속되는"
	// 상태를 만든다. pathspec 의 `*` 는 `/` 를 넘으므로 이걸로 뜻이 맞는다.
	out, err := run(vault, budget, "ls-files", "-z", "--", "*"+pat)
	if err != nil {
		return fmt.Errorf("추적 목록을 읽을 수 없다: %w", err)
	}
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if strings.TrimSpace(p) != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"rm", "--cached", "-q", "--"}, paths...)
	if _, err := run(vault, budget, args...); err != nil {
		return fmt.Errorf("작업 로그를 git 에서 떼지 못했다: %w", err)
	}
	return nil
}
