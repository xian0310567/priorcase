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

// Tier 는 이 발췌를 **어느 등급으로** 남길지다.
//
// 등급이 하나뿐이던 시절의 대가가 실측으로 남아 있다. 판정은 record 불리언
// 하나였고, "확정되지 않은 것" 은 담을 그릇이 없어 **버리는 것으로** 처리됐다 —
// 원장 23건 중 11건이 "아직 최종 결정이 내려지지 않았다" 로 기각됐다.
//
// 그런데 같은 세션에서 사람이 손으로 결정 노트 8건을 썼다. 판별기가 버린 바로
// 그 내용을. 판정이 틀린 게 아니라 **선택지가 둘밖에 없었던 것**이 틀렸다.
type Tier string

const (
	// TierNone 은 남기지 않는다.
	TierNone Tier = "none"
	// TierWorklog 는 작업 로그에 남긴다 — 검토중·대안·기각 이유·측정·미결.
	// 회수에 자동 주입되지 않으므로 문턱이 낮아도 볼트가 오염되지 않는다.
	TierWorklog Tier = "worklog"
	// TierDecision 은 결정 노트로 남긴다 — 확정된 것. 회수가 자동 주입한다.
	TierDecision Tier = "decision"
)

// Verdict 는 판정 결과다.
type Verdict struct {
	// Tier 가 판정의 정본이다.
	Tier Tier `json:"tier"`
	// Record 는 옛 형식이다. 판별기가 tier 를 안 주고 record 만 줄 때 대비해 남긴다 —
	// 모델 출력은 우리가 통제하지 못하고, 옛 원장도 이 키로 쓰여 있다.
	// parse 가 이걸 Tier 로 접는다.
	Record  bool     `json:"record"`
	Domain  string   `json:"domain"`
	Slug    string   `json:"slug"`
	Summary string   `json:"summary"`
	Body    string   `json:"body"`
	Tags    []string `json:"tags"`
	// Reason 은 tier=none 일 때 왜 아닌지다. 사람이 판별기를 못 믿을 때 볼 것.
	Reason string `json:"reason"`
}

// Recorded 는 어딘가에 남기는 판정인지다.
func (v Verdict) Recorded() bool { return v.Tier == TierWorklog || v.Tier == TierDecision }

// Normalized 는 Tier 가 비었을 때 옛 Record 불리언에서 등급을 유도한다.
//
// **parse 밖에서도 이게 필요하다.** judge.Judge 는 인터페이스라 Verdict 가 parse 를
// 거치지 않고 올 수 있다 — 테스트의 가짜 판별기가 그렇고, 나중에 붙을 다른 구현도
// 그렇다. 소비자(core/promote)가 Tier 만 보고 분기하는데 그 값이 비어 있으면
// "알 수 없는 등급" 으로 떨어져 **판정이 통째로 버려진다.** 실제로 그렇게 깨졌다.
//
// 값 수신자라 원본을 안 건드린다 — 호출부가 반환값을 써야 한다.
func (v Verdict) Normalized() Verdict {
	if v.Tier == "" {
		if v.Record {
			v.Tier = TierDecision
		} else {
			v.Tier = TierNone
		}
	}
	v.Record = v.Recorded()
	return v
}

// Scope 는 판별기가 **대화의 어느 만큼을** 보고 있는지다.
//
// 이걸 판별기에게 알려 주지 않던 것이 "미결정" 기각 11건의 진짜 원인이었다.
// 실측: min_turns=6 에 닿는 순간 체크포인트가 전진해서, watch.log 전진 26건 중
// 25건이 정확히 "발화 6" 이다. 구간 @3467548 은 마지막 발화 01:18:49 → 판정
// 01:19:06 → **대화가 7초 뒤 01:19:13 에 이어졌다.**
//
// 즉 판별기는 언제나 진행 중인 대화의 한 토막을 보면서 "이게 최종 결정인가" 를
// 물어보고 있었다. 답이 "아니다" 인 것이 당연하다. 물음이 틀렸다.
type Scope string

const (
	// ScopeMid 는 대화 도중의 창이다. 결정 노트를 쓰지 않는다 — 아크가 안 끝났다.
	ScopeMid Scope = "mid"
	// ScopeEnd 는 세션이 끝나 아크 전체를 보는 것이다. 여기서만 결정 노트가 나온다.
	ScopeEnd Scope = "end"
)

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
	// Scope 는 발췌가 대화의 한 토막인지 아크 전체인지다. 비면 ScopeMid 로 본다 —
	// 옛 호출부는 전부 도중 판정이었다.
	Scope Scope
	// Existing 은 그 도메인에 이미 있는 결정 요약이다. 중복 판정에 쓴다.
	Existing []string
	// Worklog 는 이 세션에서 **이미 작업 로그에 쌓인** 항목들이다.
	//
	// ScopeEnd 판정에서 이게 결정적이다. 도중 판정이 대안·기각 이유·측정을 미리
	// 쌓아 두므로, 세션 끝 판별기는 발췌만이 아니라 그 축적분을 함께 보고
	// 결정 노트의 근거 절을 채울 수 있다 — 발췌 상한에 잘려 나간 앞부분이
	// 여기에 남아 있다.
	Worklog []string
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
		//
		// 등급이 둘로 늘어도 이 비대칭은 그대로다: **남기는 판정만 받는다.**
		// worklog 든 decision 든 파일이 생기므로 사람이 검토 큐에서 볼 수 있고,
		// tier=none 은 잘린 출력과 구별되지 않는다.
		if v, perr := parse(o.String()); perr == nil && v.Recorded() {
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

	// **옛 형식을 접는다.** 모델이 tier 대신 record 를 줄 수 있고(프롬프트를 바꿔도
	// 출력은 우리가 통제하지 못한다), 옛 원장도 record 로 쓰여 있다. tier 가
	// 비었을 때만 본다 — 둘 다 있으면 tier 가 정본이다.
	v = v.Normalized()
	switch v.Tier {
	case TierNone, TierWorklog, TierDecision:
	default:
		// 모르는 등급은 안 남기는 쪽으로 접는다. 다만 **무엇이 왔는지 남긴다** —
		// 조용히 버리면 프롬프트가 망가진 것을 영영 모른다.
		return Verdict{Tier: TierNone,
			Reason: fmt.Sprintf("판별기가 모르는 등급을 줬다: %q", v.Tier)}, nil
	}
	// 남기라면서 무엇을 남길지 안 주면 쓸 수 없다. 조용히 빈 노트를 만드는 것보다
	// 안 만드는 것이 낫다.
	//
	// **등급마다 요구가 다르다.** 결정 노트는 파일명이 되므로 slug 가 필수지만,
	// 작업 로그는 한 파일에 덧붙이는 것이라 slug 가 필요 없다 — 거기에 slug 를
	// 요구하면 문턱을 낮춘 의미가 없어진다.
	switch v.Tier {
	case TierDecision:
		if v.Summary == "" {
			return Verdict{Tier: TierNone,
				Reason: "판별기가 결정 등급을 줬는데 summary 가 없다"}, nil
		}
		if v.Slug == "" {
			// **버리지 않고 등급을 내린다.**
			//
			// 옛 판은 여기서 none 을 줬고, 실측으로 그게 물렸다: 이 저장소의 실제
			// 세션(발화 359개)을 도중 판정에 넣으니 판별기가 slug 없는 decision 을
			// 줬고, 본문에 진단·근거가 다 들어 있는데도 통째로 사라졌다.
			//
			// slug 는 **파일명을 짓기 위한 것**이지 내용의 값어치와 무관하다.
			// 작업 로그는 파일명이 필요 없으므로 그대로 담을 수 있다. 남길 값어치가
			// 있다고 판정한 것을 형식 하나 때문에 버리는 것이 이 프로젝트가 방금
			// 고친 그 고장이다.
			v.Tier = TierWorklog
			v.Reason = "결정 등급인데 slug 가 없어 작업 로그로 내렸다"
			return v, nil
		}
	case TierWorklog:
		if v.Summary == "" {
			return Verdict{Tier: TierNone,
				Reason: "판별기가 작업 로그 등급을 줬는데 summary 가 없다"}, nil
		}
	}
	return v, nil
}

// prompt 는 판정 지시문이다.
//
// # 이 문자열이 자동 기록 0건의 원인이었다
//
// 옛 판이 스스로 밝힌 목적은 이랬다: *"보수적으로 판정하게 만드는 것이 이 문자열의
// 전부다."* 그리고 그 목적을 정확히 달성했다 — 실측으로 최근 7일 **판정 23건에
// 자동 기록 0건.** 원장의 기각 사유는 이 문자열의 문장을 거의 직역해서 되돌려준다.
//
// 무엇이 뒤집었나. 판별기가 11건을 "아직 최종 결정이 내려지지 않았다" 로 버린
// 세션에서, **볼트에는 같은 source_session 의 결정 노트 8건이 사람 손으로 기록돼
// 있었다.** 판별기가 버린 것을 사람이 다시 썼다. 오답률이 추정이 아니라 실측이다.
//
// 옛 판이 만든 기각 세 군집과 그 출처 문장:
//
//   - 미결정 11건 ← "대안을 검토하고 하나를 골랐다"(골라야 통과),
//     "결정을 '하겠다' 고 말만 하고 무엇으로 정했는지 없는 것"
//   - 중복 6건 ← "이미 있는 결정의 반복"
//   - 일상적 작업 5건 ← 기준 열거가 "아키텍처·스키마·외부 서비스·가격" 뿐이라
//     사실상 화이트리스트로 작동했다. 조직·프로세스 제약이 빠져 있었다.
//
// # 무엇을 바꿨나
//
// **등급을 둘로 나눴다.** 옛 판의 보수성은 틀린 판단이 아니라 **선택지 부족**이었다 —
// record=true 면 회수에 자동 주입되는 결정 노트가 되므로, 애매한 것을 올리면 볼트가
// 오염된다는 걱정은 옳았다. 그래서 오염되지 않는 자리를 만들었다(작업 로그).
// 이제 "애매하면 버린다" 가 아니라 **"애매하면 작업 로그"** 다.
//
// 정본은 사용자가 볼트에 남긴 정책이다
// ([[common-decision-결정기록-결론뿐아니라-대안기각이유-번복까지-2026-08-19]]).
// 그 문서가 요구하는 다섯 가지 — 대안별 기각 이유, 재현 가능한 측정, 번복과 그 계기,
// 코드로는 알 수 없는 조직·프로세스 제약, 미결 사항과 확정 조건 — 을 기준과 본문 절에
// 그대로 옮겼다. 그 문서는 이 실패 양상을 **이미 사건으로 기록해 뒀는데**(훅이 6턴
// 연속 알렸으나 에이전트가 "확정 전" 이라며 미룸), 판별기가 자동으로 같은 오류를
// 11번 반복했다.
//
// **지어내기 금지 조항은 그대로 뒀다.** 문턱을 낮추는 것과 없는 근거를 채우는 것은
// 다른 문제다. 오히려 문턱이 낮아지면 그 위험이 커지므로 더 필요하다.
func prompt(req Request) string {
	var b strings.Builder
	b.WriteString(`너는 개발 대화를 읽고 **무엇을, 어느 등급으로 남길지** 정하는 판별기다.
JSON 하나만 출력하라. 설명·인사·코드펜스를 붙이지 마라.

# 등급이 둘이다

- "decision" — **확정된 결정.** 결정 노트가 되고, 앞으로 모든 대화 시작에
  자동으로 주입된다. 그래서 문턱이 높다.
- "worklog" — **확정 전의 것 전부.** 작업 로그에 쌓인다. 자동 주입되지 않고
  물어볼 때만 검색되므로, **문턱이 낮아도 아무것도 오염되지 않는다.**
- "none" — 남기지 않는다.

**애매하면 "worklog" 다.** 이것이 가장 중요한 규칙이다.
예전에는 애매한 것을 버렸고, 그 결과 7일간 23번 판정해서 0건을 남겼다.
버려진 것 중에는 대안 4개를 비교한 구간, 어떤 안을 왜 기각했는지, 측정 결과가
있었다 — 사람이 나중에 손으로 다시 써야 했던 것들이다. **버리지 마라.**

"none" 은 정말로 남길 것이 없을 때만이다:
- 순수한 조회·확인 (파일 읽기, 빌드 통과 확인, 상태 출력)
- 인사·잡담
- 바로 앞 항목과 **글자 그대로** 같은 내용의 반복

**"아직 안 정했다" 는 none 의 이유가 아니다.** 그건 worklog 의 전형이다.
무엇을 검토 중인지, 어떤 선택지가 있는지, 무엇이 걸리는지가 바로 남길 값어치다.

# 발췌 읽는 법

- "사용자:" "에이전트:" 로 시작하는 줄은 **말**이다.
- 가운뎃점으로 시작하는 줄은 **실제로 한 일**이다 (파일 편집·명령 실행).
  뒤에 붙는 (x3) 은 같은 일을 몇 번 했는지다.
- 가운뎃점 줄에 화살표가 있으면 (도구이름 → 결과) 화살표 뒤는 **도구가 돌려준 것**이다.
  에이전트가 한 말이 아니다. 사실로 취급하라.
  - **"AskUserQuestion → …" 는 사용자가 실제로 고른 답이다.** 발췌에서 가장 강한 근거다 —
    "무엇으로 정했는지" 를 찾을 때 이 줄을 먼저 봐라.
  - "도구이름 실패 → …" 는 시도가 실패한 것이다. **무엇이 선택을 뒤집었는지**가 흔히 여기 있다.

**두 가지 생략 표지가 있다. 대화 내용이 아니다.**

- "… (N 발화 생략) …" — 발췌 상한 때문에 **우리가 버린 자리**다. 그 사이에 발화가 N개 있었다.
  앞줄과 뒷줄을 잇달아 일어난 일로 읽지 마라. 없는 인과를 만들게 된다.
- "…(중략)…" — 발화 **하나**가 길어서 가운데를 뺀 자리다. 그 발화의 앞과 끝만 보고 있다.

되돌리기 어려운 선택은 말이 아니라 **한 일**로 남는 경우가 많다 — "저장 엔진을
바꾼다" 가 문장이 아니라 파일 편집인 식이다. 둘을 같이 보고 판정하라.
다만 **한 일만 있고 그것이 일상적 작업이면 결정이 아니다** (빌드·테스트·조회).
그때도 그 안에 측정값이나 걸린 제약이 있으면 worklog 다 — none 은 정말 아무것도
없을 때다.

# decision 의 기준

아래 중 하나라도 해당하면 decision 이다.

- 되돌리기 어렵다 (아키텍처·스키마·외부 서비스·가격·배포 경로)
- 대안을 검토하고 하나를 골랐다
- **어떤 안을 기각했고 그 이유가 코드에 안 남는다**
- **조직·프로세스·계약상의 제약이다** — 코드를 아무리 읽어도 알 수 없는 것.
  (예: "스토어 심사 경로가 우리 통제 밖이라 클라이언트 경고를 못 넣는다")
  이런 것이 재사용 가치가 가장 높다. 안 남기면 같은 제안이 무한 반복된다.
- 실측·실험으로 통념이 깨졌다
- **"하지 않기로" 또는 "보류하기로" 정했다.** 보류도 결정이다 —
  무엇을 기다리는지, 무엇이 확정되면 풀리는지가 함께 있으면 decision 이다.
- 나중에 "왜 이렇게 했지" 를 물을 것 같다

# 중복 판정 — 좁게 하라

아래 "이미 기록된 결정" 목록은 **요약 한 줄씩**이다. 본문을 못 보고 있다는 것을
잊지 마라. 겹쳐 보인다고 쉽게 중복으로 접지 마라.

**중복이 아닌 것:**
- 같은 주제인데 **새 근거·측정·반례**가 나왔다
- 기존 결정을 **뒤집거나 좁히거나 넓혔다** (이건 decision 이고, 무엇을 뒤집는지 적어라)
- 기존 결정을 적용하다 **새로 드러난 제약**이 있다

**중복인 것:** 이미 적힌 것을 말만 바꿔 되풀이하고 새 정보가 없다.
그때도 none 이 아니라, 새 정보가 조금이라도 있으면 worklog 다.

# 지어내지 마라

**본문은 발췌에 있는 것만으로 쓴다.**

너는 요약하는 것이지 설명하는 것이 아니다. 발췌에 없는 어원·정의·수치·인용·
비교·배경지식을 보태지 마라. 네가 그 주제를 안다고 해서 쓰면 안 된다 —
쓰는 순간 그것은 "사람이 그렇게 판단했다" 는 기록이 되어 버린다.

특히 **근거 절이 위험하다.** 발췌에 결론만 있고 이유가 없는 경우가 흔한데,
그때 절을 비워 두는 대신 그럴듯한 이유를 채우게 된다. 채우지 마라.
근거가 대화에 없으면 그렇게 적어라 — "근거가 대화에 남지 않았다".
빈 근거는 나중에 사람이 채울 수 있지만, 틀린 근거는 아무도 의심하지 않는다.

다만 **발췌에 생략 표지가 있으면 근거가 발췌 밖에 있었을 수 있다.** 그때는
"근거가 대화에 남지 않았다" 가 아니라 **"근거가 이 발췌 범위 밖이다"** 라고 적어라.
둘은 다른 사실이고, 나중에 사람이 원문을 다시 볼지 말지가 그 한 줄에 갈린다.

같은 이유로 대안도 발췌에 실제로 등장한 것만 적는다. 하나뿐이면 하나만
적고, 없으면 절을 비운다. "완결된 문서처럼 보이게" 만들지 마라.
문턱이 낮아졌다고 해서 없는 내용을 채워 넣으라는 뜻이 **아니다.**

# 회수 구조를 알고 써라

나중에 결정을 찾을 때 검색되는 것은 **파일명·summary·tags 뿐이다.**
본문은 그 셋 중 하나가 먼저 걸린 뒤에만 점수를 더한다 — 본문에만 있는 낱말로는
**영원히 찾을 수 없다.**

그러니 summary 와 tags 를 "설명" 이 아니라 **"검색어"** 로 써라.

- summary: 한 줄. 무엇을 왜 정했는지. 이것만 주입되므로 그 자체로 읽혀야 한다.
  나중에 **물어볼 때 쓸 낱말**을 넣어라. "시장 전략" 보다 "타겟 국가" 가 낫다면 그쪽이다.
- tags: **회수 키워드 6~10개.** 주제 분류가 아니다.
  이 결정을 다시 찾을 상황을 서너 개 상상하고, 그때 쓸 낱말을 넣어라.
  동의어와 상위어를 같이 넣어라 — "미국" 만 있으면 "타겟 국가" 로는 안 걸린다.
  본문에서 중요한 낱말이 summary 에 없으면 tags 로 끌어올려라.

# 본문 절

발췌에 **있는 절만** 쓴다. 없는 절은 통째로 빼라 — 빈 제목만 남기지 마라.

- 결정 / 보류 — 무엇으로 정했나 (또는 무엇을 기다리기로 했나)
- 근거 — 왜. 없으면 "근거가 대화에 남지 않았다"
- 고려한 대안 — 각각에 **왜 기각했는지**를 붙여라. 이게 가장 값어치 있다.
- 측정 — 무엇을 어떻게 쟀나. **재현 가능하게** (파일:줄, 커밋, 명령어, 수치)
- 번복 — 중간에 틀렸다가 정정한 판단과 그 계기
- 제약 — 코드로는 알 수 없는 조직·프로세스·계약상의 사유
- 미결 — 남은 것과 그것이 확정되는 조건

**출력은 발췌와 같은 언어로 써라.** slug·summary·body·tags 전부다.
영어 대화면 영어로, 일본어면 일본어로 쓴다. 이 지시문이 한국어인 것과 무관하다.
섞어 쓰지 마라 — 파일명이 두 언어로 갈리면 나중에 찾을 수 없다.

절 제목도 그 언어로 쓴다. 한국어면
"## 결정 / ## 근거 / ## 고려한 대안 / ## 측정 / ## 번복 / ## 제약 / ## 미결",
영어면
"## Decision / ## Rationale / ## Alternatives considered / ## Measurements /
 ## Reversals / ## Constraints / ## Open questions".

`)

	// **Scope 가 출력 형식을 가른다.** 도중 판정에 decision 을 허용하면 옛 실패로
	// 되돌아간다 — 아크가 안 끝난 창을 보고 "최종인가" 를 묻게 되고, 답이 "아니다"
	// 인 것이 당연해진다. 실측으로 그 물음이 11번 "아니다" 를 받았다.
	//
	// 도중에는 등급을 아예 안 보여 준다. 있는 줄 알면 모델은 쓴다.
	switch req.Scope {
	case ScopeEnd:
		b.WriteString(`# 지금은 세션이 끝났다 — 아크 전체를 보고 있다

발췌는 대화의 한 토막이 아니라 **한 흐름 전체**다. 그래서 여기서만 decision 이 나온다.
도중에 쌓인 작업 로그가 아래 함께 주어지면, 그것까지 근거로 삼아 본문을 채워라 —
발췌 상한에 잘려 나간 앞부분이 거기 남아 있다.

한 흐름에서 결정이 여럿 나왔으면 **가장 되돌리기 어려운 것 하나**를 고르고,
나머지는 본문에 적어라. 출력은 JSON 하나다.

출력 형식:
{"tier": "decision",
 "slug": "짧은-주제어-하이픈으로",
 "summary": "한 줄. 무엇을 왜 정했는지 + 나중에 물어볼 낱말",
 "body": "발췌의 언어로. 위 절 중 해당하는 것만. 발췌에 있는 것만",
 "tags": ["회수", "키워드", "동의어", "상위어", "..."]}

또는
{"tier": "worklog",
 "summary": "한 줄 제목. 무엇을 검토·측정·보류했는지",
 "body": "발췌의 언어로. 위 절 중 해당하는 것만. 발췌에 있는 것만",
 "tags": ["키워드", "..."]}

또는
{"tier": "none", "reason": "왜 아닌지 한 줄"}
`)
	default:
		b.WriteString(`# 지금은 대화가 진행 중이다 — 한 토막만 보고 있다

발췌는 대화의 **한 창**이고, 이 대화는 아마 아직 안 끝났다.
그러니 "이게 최종 결정인가" 를 묻지 마라. 그 물음의 답은 거의 언제나 "아니다" 이고,
그렇게 물었기 때문에 예전에 7일간 0건이 남았다.

대신 물어라: **"나중에 이 대화를 되짚을 사람이 알아야 할 것이 여기 있나?"**
검토한 선택지, 기각한 것과 그 이유, 측정한 값, 걸린 제약, 아직 못 정한 것 —
있으면 남겨라. 확정 여부는 상관없다.

**이 판정에서 유효한 tier 는 "worklog" 와 "none" 둘뿐이다.**
"decision" 은 쓰지 마라 — 아크가 끝나야 나올 수 있는 등급이고, 세션이 끝날 때
같은 대화를 아크 전체로 다시 본다. 여기서 남긴 것은 그때 근거로 함께 넘어간다.
확정된 결정처럼 보여도 지금은 "worklog" 로 적어라. 잃는 것은 없다.

출력 형식:
{"tier": "worklog",
 "summary": "한 줄 제목. 무엇을 검토·측정·보류했는지",
 "body": "발췌의 언어로. 위 절 중 해당하는 것만. 발췌에 있는 것만",
 "tags": ["키워드", "..."]}

또는
{"tier": "none", "reason": "왜 아닌지 한 줄"}
`)
	}

	b.WriteString("\n")
	fmt.Fprintf(&b, "프로젝트 도메인: %s\n", req.Domain)
	if len(req.Existing) > 0 {
		// **판정 지시를 헤더에서 뺐다.** 옛 판은 "(중복이면 record=false)" 를
		// 데이터 제목에 박아 뒀는데, 그러면 목록을 훑는 동안 계속 그 지시가 붙어
		// 다닌다 — 중복 기각 6건이 거기서 나왔다. 판정 규칙은 위 "중복 판정" 절에
		// 한 번만 쓰고, 여기는 자료만 준다.
		b.WriteString("\n이미 기록된 결정 (요약 한 줄씩 — 본문은 안 보인다):\n")
		for _, e := range req.Existing {
			fmt.Fprintf(&b, "- %s\n", e)
		}
	}
	if len(req.Worklog) > 0 {
		b.WriteString("\n이번 대화에서 이미 작업 로그에 쌓인 것:\n")
		for _, w := range req.Worklog {
			fmt.Fprintf(&b, "- %s\n", w)
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
