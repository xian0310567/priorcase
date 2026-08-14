// Package judge 는 대화 발췌를 보고 "기록할 결정인가" 를 판정한다.
//
// **이 패키지는 이 프로젝트의 기록된 결정 하나를 뒤집는다.**
// [[priorcase-결정-기록회수모델-에이전트주도-2026-08-07]] 은 *"데몬은 LLM 을 부르지
// 않는다"* 로 정했고, 이유는 키 등록이 오픈소스 진입 장벽이라는 것이었다.
//
// 뒤집는 근거는 둘이다.
//
//  1. **규칙만으로는 안 된다.** 실측: 한 세션에서 시그널 후보 문장이 160개 나오는데
//     대부분 결정이 아니다 — 표 조각, 일반 서술까지 걸린다. "이게 기록할 결정인가" 는
//     이해가 필요한 판단이라 패턴 매칭으로 판정할 수 없다.
//  2. **호스트 CLI 는 이미 인증돼 있다.** Claude Code 사용자에게 `claude` 는 PATH 에
//     있고 추가 키가 필요 없다. "키 등록 장벽" 이라는 전제가 그 경우에는 성립하지 않는다.
//
// 그래서 판별기는 **호스트 CLI 만** 쓴다. API 키를 직접 읽지 않는다 — 그건 진짜로
// 장벽이고, 사용자가 모르는 사이에 과금되는 경로를 만들지 않기 위해서다.
//
// 판별기가 없으면 이 패키지는 아무것도 하지 않는다. 그때는 표시만 남고, 그것이
// 지금까지의 동작이다.
package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultModel 은 판별에 쓰는 모델이다. 판정 하나에 문장 몇 개면 되므로 작은 것을 쓴다.
const DefaultModel = "claude-haiku-4-5"

// DefaultTimeout — 판별이 이보다 오래 걸리면 포기한다.
//
// 옛 셸 구현의 감사 결함 5가 *"`claude -p` 자체에 타임아웃이 없다"* 였다.
// 백그라운드로 돌려도 무한 대기는 프로세스를 쌓는다.
// DefaultTimeout 은 판별기 한 건의 상한이다.
//
// **승격 예산(hook.promoteBudget)보다 작아야 한다.** 크면 한 건이 예산을 통째로
// 먹고도 남아서, 예산을 다 쓴 뒤에야 죽는다 — 예산 75초에 상한 90초로 두고 있었다.
//
// 45초에서 75초로 올렸다 (2026-08-12). **실측 때문이다.**
//
// 같은 크기(2.4KB)의 발췌로 5번 재니 10.2 · 10.4 · 13.9 · 15.2 · 28.5초가 나왔다.
// 편차가 3배 가까이 난다. 중앙값만 보면 45초가 5배로 넉넉해 보이지만, 꼬리가 그
// 상한을 넘긴다 — 원장에 상한 초과 19건이 남았고 **뭉치지 않고 하나씩** 흩어져
// 있었다(뭉쳐 있었다면 취소 인공물이다). 그중 한 구간은 6번 연속으로 넘겼다.
//
// 입력 크기 탓이 아니다. 걸린 발췌는 1.9~3.1KB 로, 10초에 통과한 것과 같은 크기대다.
// 판별기 호출 자체의 편차다. 그래서 중앙값이 아니라 **꼬리에 맞춘다.**
//
// 예산(90초)은 올리지 않았다. 그래서 이제 한 판에 판별기가 **한 번만** 확실히
// 들어간다 — 세션 끝에서 사람을 붙잡는 시간을 늘리지 않기로 한 선택의 대가다.
// 그 대신 실패가 무한히 반복되지 않게 daemon.MaxJudgeFails 로 끊는다.
const DefaultTimeout = 75 * time.Second

// Verdict 는 판정 결과다.
type Verdict struct {
	// Record 가 false 면 나머지는 비어 있다.
	Record  bool     `json:"record"`
	Domain  string   `json:"domain"`
	Slug    string   `json:"slug"`
	Summary string   `json:"summary"`
	Body    string   `json:"body"`
	Tags    []string `json:"tags"`
	// Reason 은 record=false 일 때 왜 아닌지다. 사람이 판별기를 못 믿을 때 볼 것.
	Reason string `json:"reason"`
}

// Judge 는 판정기다. 테스트가 갈아 끼울 수 있게 인터페이스로 둔다 —
// 실제 LLM 을 부르는 테스트는 느리고 결정적이지 않다.
type Judge interface {
	Decide(ctx context.Context, req Request) (Verdict, error)
}

// Request 는 판정에 넘기는 것이다.
type Request struct {
	Excerpt string
	Domain  string
	Date    string
	// Existing 은 그 도메인에 이미 있는 결정 요약이다. 중복 판정에 쓴다.
	Existing []string
}

// CLI 는 호스트의 `claude` 명령으로 판정한다.
type CLI struct {
	Path    string // claude 실행 파일. 비면 PATH 에서 찾는다
	Model   string
	Timeout time.Duration
}

// Find 는 쓸 수 있는 판별기를 찾는다. 없으면 nil 을 준다 — 에러가 아니다.
//
// 판별기가 없는 것은 고장이 아니라 **설정이다.** 그때는 표시만 남고 에이전트가
// 판단한다. 에러로 만들면 판별기 없는 사용자에게 매번 경고가 뜬다.
func Find(explicitPath, model string) *CLI {
	path := ""
	if explicitPath != "" {
		// **명시 경로도 검증한다.** 안 그러면 오타 하나로 매 세션 실행 실패가 뜬다.
		// 여기서 조용히 nil 을 주고, "설정했는데 안 된다" 는 prior doctor 가 알린다 —
		// 훅은 대화 흐름에 있어서 반복 경고를 낼 자리가 아니다.
		if usable(explicitPath) {
			return newCLI(explicitPath, model)
		}
		return nil
	}
	if path == "" {
		// 옛 셸 구현이 쓰던 자리를 먼저 본다. PATH 에 없는 설치가 흔하다 —
		// prior 자신이 오늘 그 문제로 걸렸다.
		if home, err := os.UserHomeDir(); err == nil {
			cand := filepath.Join(home, ".local", "bin", "claude")
			if usable(cand) {
				path = cand
			}
		}
	}
	if path == "" {
		p, err := exec.LookPath("claude")
		if err != nil {
			return nil
		}
		path = p
	}
	return newCLI(path, model)
}

func newCLI(path, model string) *CLI {
	if model == "" {
		model = DefaultModel
	}
	return &CLI{Path: path, Model: model, Timeout: DefaultTimeout}
}

// usable 은 실행 가능한 파일인지 본다.
func usable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0o111 != 0
}

// Configured 는 설정에 판별기를 적었는데 쓸 수 없는 상태인지 알려 준다.
// prior doctor 가 이걸로 "설정했는데 안 된다" 를 구별한다.
func Configured(explicitPath string) (set bool, ok bool) {
	if explicitPath == "" {
		return false, false
	}
	return true, usable(explicitPath)
}

// Decide 는 발췌를 판정한다.
func (c *CLI) Decide(ctx context.Context, req Request) (Verdict, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.Path,
		"--print", "--model", c.Model, "--strict-mcp-config", "--max-turns", "1")
	cmd.Stdin = strings.NewReader(prompt(req))
	// **재귀 차단.** 판별기가 띄우는 세션에도 훅이 붙으면 그 세션이 또 판별기를
	// 부른다. 옛 셸 구현이 SECOND_BRAIN_SCRIBE 로 막던 것과 같은 자리다.
	cmd.Env = append(os.Environ(), "PRIORCASE_JUDGE=1")

	var o, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &errb
	// **상한이 걸려도 Wait 가 안 끝날 수 있다.**
	//
	// Stdout 이 파일이 아니면 exec 가 파이프를 만들고 복사 고루틴을 띄운다.
	// Wait 는 그 고루틴을 기다리고, 고루틴은 파이프의 쓰기 끝이 닫히기를 기다린다 —
	// 그건 그 파이프를 물려받은 **모든** 프로세스가 끝나야 닫힌다. claude 는
	// 정리(텔레메트리·MCP 종료)에 자식을 쓰므로, 부모를 죽여도 Wait 가 계속 막힌다.
	//
	// 실측: 상한 2초를 준 호출이 30초를 기다렸다. 그러면 훅이 호스트 상한(120초)에
	// 죽고, 그때는 원장도 못 쓰고 선점 도장만 남아 그 구간이 5분간 건너뛰어진다.
	//
	// WaitDelay 는 컨텍스트가 끝난 뒤 이만큼만 더 기다리고 파이프를 닫는다.
	// 2초를 주는 이유: 이미 찍힌 답을 복사 고루틴이 옮길 시간은 줘야 한다
	// (아래 주석대로 killed 인데 stdout 에 완전한 JSON 이 있는 판이 실제로 있었다).
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		// ★ **답이 나왔으면 쓴다. 프로세스가 어떻게 끝났든 상관없다.**
		//
		// claude CLI 는 답을 찍은 뒤에도 정리(텔레메트리·MCP 종료)에 시간을 쓴다.
		// 그 사이에 상한이 걸리면 프로세스는 killed 인데 **stdout 에는 완전한 JSON 이
		// 이미 들어 있다.** 그걸 실패로 버리면 판별기를 부른 값을 통째로 날린다 —
		// 그리고 그 구간은 다음에도 같은 일을 반복한다.
		//
		// 실측으로 확인했다: 원장에 남은 killed 기록 중 하나가 완전한 verdict 를
		// 담고 있었다 (record·slug·summary·body 전부). 그 발췌는 그 뒤로도 계속
		// 큐에 남아 있었다.
		//
		// **`record=true` 만 받는다.** 이 비대칭이 이 가드의 핵심이다.
		//
		// parse 는 불완전한 답(`{"record":true}` 처럼 slug 가 없는 것)을 에러로
		// 만들지 않고 **record=false 로 바꿔서** 준다. 그건 정상 경로에서는 옳다 —
		// 조용히 빈 노트를 만드는 것보다 안 만드는 것이 낫기 때문이다. 그런데
		// 프로세스가 실패한 뒤에는 그 변환이 위험해진다: 출력이 중간에 잘린 것과
		// 판별기가 "결정이 아니다" 라고 판정한 것이 **같은 모양이 된다.**
		//
		// 그래서 증거를 남기는 쪽만 받는다. record=true 는 노트가 만들어지므로
		// 사람이 검토 큐에서 볼 수 있다. record=false 를 받으면 구간이 조용히
		// 해소되고 — 그게 잘린 출력 때문이었다면 그 결정은 영영 사라진다.
		//
		// 못 받은 record=false 는 손해가 작다. 구간이 남아 다음에 다시 시도된다.
		if v, perr := parse(o.String()); perr == nil && v.Record {
			return v, nil
		}
		// **stdout 도 같이 보여 준다.** claude CLI 는 "Not logged in · Please run /login"
		// 같은 실패를 stdout 에 쓴다 — stderr 만 보면 `exit status 1` 뒤가 비어서
		// 사용자가 무엇을 해야 할지 알 수 없다. 실제로 새 사용자 시뮬레이션에서
		// 그 상태를 만났다.
		msg := strings.TrimSpace(errb.String())
		if out := strings.TrimSpace(head(o.String(), 300)); out != "" {
			if msg != "" {
				msg += " / "
			}
			msg += out
		}
		if msg == "" {
			msg = "(출력 없음)"
		}
		return Verdict{}, fmt.Errorf("판별기 실행 실패 (%s): %w — %s", c.Path, err, msg)
	}
	return parse(o.String())
}

// parse 는 응답에서 JSON 을 꺼낸다. 모델이 앞뒤에 말을 붙여도 견딘다.
func parse(s string) (Verdict, error) {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i < 0 || j <= i {
		return Verdict{}, fmt.Errorf("판별기 응답에 JSON 이 없다: %.200s", s)
	}
	var v Verdict
	if err := json.Unmarshal([]byte(s[i:j+1]), &v); err != nil {
		return Verdict{}, fmt.Errorf("판별기 응답을 읽을 수 없다: %w — %.200s", err, s)
	}
	if v.Record && (v.Slug == "" || v.Summary == "") {
		// 기록하라면서 무엇을 기록할지 안 주면 쓸 수 없다. 조용히 빈 노트를
		// 만드는 것보다 안 만드는 것이 낫다.
		return Verdict{Record: false, Reason: "판별기가 slug 나 summary 를 주지 않았다"}, nil
	}
	return v, nil
}

// prompt 는 판정 지시문이다.
//
// **보수적으로 판정하게 만드는 것이 이 문자열의 전부다.** 자동 생성된 노트는 손으로
// 쓴 것과 구분되지 않으므로(설계 결정), 애매한 것을 기록하면 볼트가 조용히 오염된다.
// 회수는 결정 노트를 신뢰하는데 그 신뢰가 깨지면 시스템 전체가 무의미해진다.
func prompt(req Request) string {
	var b strings.Builder
	b.WriteString(`너는 개발 대화에서 "볼트에 남길 결정" 만 골라내는 판별기다.
JSON 하나만 출력하라. 설명·인사·코드펜스를 붙이지 마라.

**출력은 발췌와 같은 언어로 써라.** slug·summary·body·tags 전부다.
영어 대화면 영어로, 일본어면 일본어로 쓴다. 이 지시문이 한국어인 것과 무관하다.
섞어 쓰지 마라 — 파일명이 두 언어로 갈리면 나중에 찾을 수 없다.
(본문 절 제목도 그 언어로: 한국어면 "## 결정 / ## 근거 / ## 고려한 대안",
 영어면 "## Decision / ## Rationale / ## Alternatives considered".)

**발췌에는 두 종류가 섞여 있다.**
- "사용자:" "에이전트:" 로 시작하는 줄은 **말**이다.
- 가운뎃점으로 시작하는 줄은 **실제로 한 일**이다 (파일 편집·명령 실행).
  뒤에 붙는 (x3) 은 같은 일을 몇 번 했는지다.

되돌리기 어려운 선택은 말이 아니라 **한 일**로 남는 경우가 많다 — "저장 엔진을
바꾼다" 가 문장이 아니라 파일 편집인 식이다. 둘을 같이 보고 판정하라.
다만 **한 일만 있고 그것이 일상적 작업이면 결정이 아니다** (빌드·테스트·조회).

기록할 결정의 기준 — 아래 중 하나라도 확실히 해당할 때만이다.
- 되돌리기 어렵다 (아키텍처·스키마·외부 서비스·가격 선택)
- 대안을 검토하고 하나를 골랐다
- 나중에 "왜 이렇게 했지" 를 물을 것 같다
- 실측·실험으로 통념이 깨졌다

**애매하면 record=false 다.** 이 결과는 사람이 쓴 결정 노트와 섞이므로,
잘못 기록하면 볼트를 오염시킨다. 안 기록하는 쪽이 언제나 안전하다.

아래는 record=false 다.
- 진행 상황 보고, 할 일 나열, 코드 설명
- 결정을 "하겠다" 고 말만 하고 무엇으로 정했는지 없는 것
- 이미 있는 결정의 반복 (아래 기존 목록 참고)
- 사소한 구현 디테일, 시행착오

**본문은 발췌에 있는 것만으로 쓴다. 지어내지 마라.**

너는 요약하는 것이지 설명하는 것이 아니다. 발췌에 없는 어원·정의·수치·인용·
비교·배경지식을 보태지 마라. 네가 그 주제를 안다고 해서 쓰면 안 된다 —
쓰는 순간 그것은 "사람이 그렇게 판단했다" 는 기록이 되어 버린다.

특히 **근거 절이 위험하다.** 발췌에 결론만 있고 이유가 없는 경우가 흔한데,
그때 절을 비워 두는 대신 그럴듯한 이유를 채우게 된다. 채우지 마라.
근거가 대화에 없으면 그렇게 적어라 — "근거가 대화에 남지 않았다".
빈 근거는 나중에 사람이 채울 수 있지만, 틀린 근거는 아무도 의심하지 않는다.

같은 이유로 고려한 대안도 발췌에 실제로 등장한 것만 적는다. 하나뿐이면 하나만
적고, 없으면 절을 비운다. "완결된 문서처럼 보이게" 만들지 마라.

**회수 구조를 알고 써라. 이게 이 작업의 절반이다.**

나중에 이 결정을 찾을 때 검색되는 것은 **파일명·summary·tags 뿐이다.**
본문은 그 셋 중 하나가 먼저 걸린 뒤에만 점수를 더한다 — 본문에만 있는 낱말로는
**영원히 찾을 수 없다.**

그러니 summary 와 tags 를 "설명" 이 아니라 **"검색어"** 로 써라.

- summary: 한 줄. 무엇을 왜 정했는지. 이것만 주입되므로 그 자체로 읽혀야 한다.
  나중에 **물어볼 때 쓸 낱말**을 넣어라. "시장 전략" 보다 "타겟 국가" 가 낫다면 그쪽이다.
- tags: **회수 키워드 6~10개.** 주제 분류가 아니다.
  이 결정을 다시 찾을 상황을 서너 개 상상하고, 그때 쓸 낱말을 넣어라.
  동의어와 상위어를 같이 넣어라 — "미국" 만 있으면 "타겟 국가" 로는 안 걸린다.
  본문에서 중요한 낱말이 summary 에 없으면 tags 로 끌어올려라.

출력 형식:
{"record": true,
 "slug": "짧은-주제어-하이픈으로",
 "summary": "한 줄. 무엇을 왜 정했는지 + 나중에 물어볼 낱말",
 "body": "발췌의 언어로. 절 셋: 결정 / 근거 / 고려한 대안. 발췌에 있는 것만",
 "tags": ["회수", "키워드", "동의어", "상위어", "..."]}

또는
{"record": false, "reason": "왜 아닌지 한 줄"}

`)
	fmt.Fprintf(&b, "프로젝트 도메인: %s\n", req.Domain)
	if len(req.Existing) > 0 {
		b.WriteString("\n이미 기록된 결정 (중복이면 record=false):\n")
		for _, e := range req.Existing {
			fmt.Fprintf(&b, "- %s\n", e)
		}
	}
	b.WriteString("\n--- 대화 발췌 ---\n")
	b.WriteString(req.Excerpt)
	b.WriteString("\n--- 끝 ---\n")
	return b.String()
}

// head 는 앞부분만 자른다. 룬 경계를 지켜 한글이 깨지지 않게 한다.
func head(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Check 는 판별기가 **실제로 답하는지** 본다. 있기만 한 것과 쓸 수 있는 것은 다르다 —
// claude CLI 는 설치돼 있어도 로그인이 안 됐을 수 있고, 그러면 세션이 끝나는
// 순간에야 실패를 알게 된다. prior doctor 가 미리 물어본다.
func (c *CLI) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Path, "--print", "--model", c.Model, "--max-turns", "1")
	cmd.Stdin = strings.NewReader(`{"ok":true} 를 그대로 출력하라. 다른 말은 하지 마라.`)
	cmd.Env = append(os.Environ(), "PRIORCASE_JUDGE=1")
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(e.String())
		if s := strings.TrimSpace(head(o.String(), 200)); s != "" {
			if msg != "" {
				msg += " / "
			}
			msg += s
		}
		return fmt.Errorf("%w — %s", err, msg)
	}
	return nil
}
