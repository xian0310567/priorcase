package judge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// 판별기 종류 — **어느 호스트의 CLI 로 판정하는가.**
//
// # 왜 갈라야 하는가
//
// 판별기는 "API 키를 읽지 않는다" 는 규칙 때문에 **이미 인증된 호스트 CLI** 에
// 셸아웃한다 (패키지 주석). 그런데 그 규칙을 쓸 때 호스트는 하나였고, 탐지 코드가
// `claude` 만 찾았다.
//
// 2026-08-23 에 Codex 를 1급 훅 호스트로 받으면서 훅·주입·데몬은 갈랐지만 **판별기
// 쪽은 안 건드렸다.** 그래서 Codex 로 일하는 동안에도 자동 기록이 `claude` 를 부른다.
// 사용자가 그것을 보고 물었고, 그것이 설계가 아니라 빠진 구멍임이 확인됐다:
//
//   - 벤더 경계가 어긋난다 — Codex 에서 일하는데 Claude 쿼터가 나간다
//   - `claude` 가 없거나 로그아웃이면 자동 기록이 조용히 "표시만" 으로 떨어진다
//
// # 실측 (2026-08-24)
//
// **발췌 상한(daemon.maxExcerpt = 24000B)에서 쟀다.** scan.go 의 maxExcerpt 주석이
// claude 로 같은 방법을 쓴 것과 비교 가능하게 맞췄다 — 작은 프롬프트로 재면
// 상한을 잘못 잡는다(처음에 그렇게 했다: 소형 프롬프트 7.7초를 근거로 30초를 뒀는데
// 실제 크기에서 24.9초가 나왔다).
//
//	24000B  codex(gpt-5.4-mini)   16.4 · 18.6 · 24.9 초   실패 0/3
//	24000B  claude(haiku-4-5)     21.8 ·  9.9 · 10.7 초   실패 1/3
//
// 소형 프롬프트(참고): codex 4.0~7.7초, 6000B 에서 16~22.5초.
//
// **매달림이 없었다.** 앞서 `codex exec` 가 두 번 5분+ 매달린 것은 그때 심어 둔
// 잘못된 탐침 훅(셸 없이 `>>` 를 쓴 것) 때문일 가능성이 크다 — Codex 는 훅에
// 기본 타임아웃이 없다. 프롬프트를 인자가 아니라 **stdin** 으로 주면 안정적이다.
//
// # codex 는 잘못된 UTF-8 을 거절한다 (claude 는 받는다)
//
// 측정 중에 `Failed to read prompt from stdin: input is not valid UTF-8 (invalid byte
// at offset …)` 로 3/3 실패했다. 원인은 코드가 아니라 **측정 픽스처**였다 —
// 발췌를 `s[:n]` 으로 바이트 단위로 잘라 한글이 반 토막 났다. claude 는 그걸 그냥
// 받아서 넘어갔고 codex 만 걸렀다.
//
// 실제 경로는 안전하다: `buildExcerpt` 는 줄 단위로 담고 `clipLine` 은
// `utf8.RuneStart` 로 자른다. 그래도 적어 두는 이유는 **codex 가 이 조건에서 더
// 엄격한 검사기**라는 것이다 — 앞으로 바이트 단위로 자르는 코드가 들어오면
// claude 에서는 조용히 지나가고 codex 에서만 터진다.
//
// # 플래그 선택의 근거
//
// `--ignore-user-config`: 사용자의 플러그인·MCP 를 안 읽는다. 판정은 발췌 한 덩이를
// 보고 JSON 하나를 내는 일이라 그 설정이 전부 순수 오버헤드다.
//
// `--ephemeral`: 판정 실행이 `~/.codex/sessions` 에 쌓이지 않게 한다. 쌓이면
// **데몬이 판별기의 판별을 다시 판별한다** — 순환이다. (`PRIORCASE_JUDGE=1` 이
// 승격을 막지만, 파일이 안 생기는 쪽이 한 겹 더 확실하다.)
//
// `-` : 프롬프트를 stdin 으로 받는다. 위 실측의 안정성이 여기서 왔다.
type Flavor string

const (
	// FlavorClaude 는 `claude --print` 다. 이 프로젝트의 원래 판별기다.
	FlavorClaude Flavor = "claude"
	// FlavorCodex 는 `codex exec` 다.
	FlavorCodex Flavor = "codex"
)

// DefaultCodexModel 은 Codex 판별기의 기본 모델이다.
//
// `gpt-5.3-codex-spark` 가 가장 빨랐지만(평균 5.0초) 코드에 맞춘 모델이다.
// 판별기는 한국어 요약·본문을 쓰므로 범용 소형 모델을 쓴다 — 0.7초 차이보다
// 산문 품질이 중요하다. 둘 다 유효한 판정을 냈다.
const DefaultCodexModel = "gpt-5.4-mini"

// CodexTimeout 은 Codex 판별기 한 건의 상한이다.
//
// **DefaultTimeout(75초)보다 짧다.** 이유는 이것이 **사슬의 앞자리**라는 것이다.
// 앞자리가 예산을 다 먹으면 폴백이 이름만 남고 실제로는 안 뛴다 — 그러면 "모델은
// 썼는데 기록은 없다" 가 흔해진다. 사용자가 폴백을 붙일 때 정확히 그것을 물었다.
//
// # 40초인 근거 — 두 제약이 맞물린 구간의 값이다
//
// 아래 둘을 동시에 만족해야 하고, 승격 예산이 90초라 남는 구간이 좁다:
//
//	앞자리 여유:   codex 실측 최대 24.9초 × 1.5 = 37.5초 이상이어야 한다
//	              (자주 넘기면 codex 시간과 claude 시간을 **둘 다** 쓴다)
//	폴백 여유:     90 − codex ≥ claude 실측 최대 22.7초 × 2 = 46초
//	              → codex ≤ 44초
//
// 즉 37.5 ~ 44초가 가능한 구간이고 40초를 골랐다. 양쪽에 각각 1.6배·2.2배가 남는다.
//
// **처음에 30초로 뒀던 것은 근거가 틀렸다.** 소형 프롬프트 실측(최대 7.7초)의 4배로
// 잡았는데, 판별기가 실제로 받는 것은 24000B 짜리 발췌다. 그 크기로 재니 최대가
// 24.9초여서 여유가 1.2배밖에 안 됐다 — 앞자리가 자주 시간을 넘기면 codex 시간과
// claude 시간을 **둘 다** 쓰게 되고, 그것이 이 설계에서 가장 나쁜 결과다.
//
// TestCodexTimeoutLeavesRoomForFallback 이 이 관계를 못 박는다.
const CodexTimeout = 40 * time.Second

// args 는 이 종류의 CLI 를 부를 인자다. 프롬프트는 항상 stdin 으로 간다.
func (f Flavor) args(model string) []string {
	if f == FlavorCodex {
		return []string{
			"exec",
			"--skip-git-repo-check", // 판별기는 아무 cwd 에서나 돈다 (데몬은 홈)
			"--ignore-user-config",  // 플러그인·MCP 오버헤드를 뺀다
			"--ephemeral",           // 판정 실행이 세션 목록에 쌓이지 않게
			"-m", model,
			"-", // 프롬프트는 stdin
		}
	}
	return []string{"--print", "--model", model, "--strict-mcp-config", "--max-turns", "1"}
}

// defaultModel 은 종류별 기본 모델이다.
//
// **설정의 `judge_model` 은 claude 쪽 값이다.** 지금 실볼트 설정이
// `claude-haiku-4-5` 이고, 그것을 codex 에 넘기면 없는 모델이라 실패한다.
// 그래서 codex 는 자기 기본값을 쓴다 — 종류별 키를 새로 만들지 않는 이유는
// 설정 키가 머신 사이를 건너오지 않기 때문이다(오늘 실측: 도메인 미선언으로
// 결정 23건이 안 보였다). 키를 늘릴수록 그 구멍이 커진다.
func (f Flavor) defaultModel() string {
	if f == FlavorCodex {
		return DefaultCodexModel
	}
	return DefaultModel
}

// defaultTimeout 은 종류별 상한이다.
func (f Flavor) defaultTimeout() time.Duration {
	if f == FlavorCodex {
		return CodexTimeout
	}
	return DefaultTimeout
}

// exeNames 는 PATH 에서 찾을 실행 파일 이름이다.
func (f Flavor) exeNames() []string {
	if f == FlavorCodex {
		return []string{"codex"}
	}
	return []string{"claude"}
}

// FlavorOf 는 실행 파일 경로에서 종류를 짐작한다.
//
// 설정의 `judge_path` 를 사람이 직접 적었을 때 쓴다. 파일 이름으로만 본다 —
// 경로에 "codex" 가 들어간 디렉토리에 claude 를 둔 경우(예: `~/codex-tools/claude`)
// 를 잘못 읽지 않으려면 이름만 봐야 한다.
func FlavorOf(path string) Flavor {
	base := strings.ToLower(filepath.Base(path))
	if strings.Contains(base, "codex") {
		return FlavorCodex
	}
	return FlavorClaude
}

// FindFlavor 는 그 종류의 판별기를 찾는다. 없으면 nil 이다 — 에러가 아니다.
func FindFlavor(f Flavor, model string) *CLI {
	for _, name := range f.exeNames() {
		// 옛 셸 구현이 쓰던 자리를 먼저 본다. PATH 에 없는 설치가 흔하다.
		if home, err := os.UserHomeDir(); err == nil {
			if cand := filepath.Join(home, ".local", "bin", name); usable(cand) {
				return newFlavorCLI(f, cand, model)
			}
		}
		if p, err := exec.LookPath(name); err == nil {
			return newFlavorCLI(f, p, model)
		}
	}
	return nil
}

func newFlavorCLI(f Flavor, path, model string) *CLI {
	if model == "" {
		model = f.defaultModel()
	}
	return &CLI{Path: path, Model: model, Timeout: f.defaultTimeout(), Flavor: f}
}

// FindFor 는 **선호하는 종류를 앞에 두고** 쓸 수 있는 판별기를 사슬로 준다.
//
// 아무것도 못 찾으면 nil 이다 (판별기 없는 것은 고장이 아니라 설정이다).
//
// `explicitPath` 가 있으면 그것만 쓴다 — 사람이 직접 적은 것을 우리가 사슬로
// 늘리면 "왜 다른 것이 도는가" 를 설명할 수 없다.
//
// 선호 종류를 왜 호출부가 정하는가: 판정 대상 발췌가 **어느 호스트의 대화인지**는
// 어댑터만 안다 (§4.1 — core 는 cwd 도 호스트도 모른다). Codex 대화는 codex 로,
// Claude Code 대화는 claude 로 판정하는 것이 벤더 경계와 맞는다.
func FindFor(prefer Flavor, explicitPath, model string) Judge {
	if explicitPath != "" {
		if c := FindFlavorAt(explicitPath, model); c != nil {
			return c
		}
		return nil
	}

	order := []Flavor{prefer, FlavorClaude}
	if prefer == FlavorClaude {
		order = []Flavor{FlavorClaude, FlavorCodex}
	}

	var chain []Judge
	seen := map[Flavor]bool{}
	for _, f := range order {
		if seen[f] {
			continue
		}
		seen[f] = true
		// 설정 모델은 claude 에만 준다 (defaultModel 주석).
		m := ""
		if f == FlavorClaude {
			m = model
		}
		if c := FindFlavor(f, m); c != nil {
			chain = append(chain, c)
		}
	}
	switch len(chain) {
	case 0:
		return nil
	case 1:
		return chain[0]
	}
	return Chain(chain)
}

// Chain 은 판별기를 **순서대로** 시도한다. 앞이 실패하면 다음으로 넘어간다.
//
// # 왜 사슬인가
//
// 호스트 CLI 는 우리 것이 아니라서 우리가 못 고치는 이유로 실패한다 — 로그아웃,
// 쿼터 소진, 그 벤더의 장애, 새 판의 회귀. 하나에 걸면 자동 기록이 그때 통째로
// 멈추고, 멈춘 것은 **조용하다**(구간이 큐에 남을 뿐이다).
//
// # 왜 앞자리 상한이 짧아야 하는가
//
// 사슬은 예산을 나눠 쓴다. 앞자리가 상한을 크게 잡으면 그것이 예산을 다 먹고
// 폴백이 못 뛴다 — "모델은 썼는데 기록은 없다". CodexTimeout 주석에 그 산수가 있다.
//
// # 마지막 실패만 보고한다
//
// 앞자리 실패는 폴백이 성공하면 사용자에게 알릴 것이 아니다. 다 실패했을 때만
// 전부를 묶어 낸다 — 그때는 무엇이 왜 안 됐는지가 전부 필요하다.
type Chain []Judge

func (ch Chain) Decide(ctx context.Context, req Request) (Verdict, error) {
	var errs []error
	for _, j := range ch {
		// **예산이 없으면 시도하지 않는다.** 남은 시간이 0 인데 부르면 즉시
		// 실패하고, 그 실패가 로그를 채워 진짜 원인을 가린다.
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		v, err := j.Decide(ctx, req)
		if err == nil {
			return v, nil
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return Verdict{}, errors.New("판별기가 없다")
	}
	return Verdict{}, errors.Join(errs...)
}

// FindFlavorAt 은 **경로가 정해진** 판별기를 만든다. 종류는 파일 이름에서 짐작한다.
//
// `judge_path` 를 사람이 직접 적었을 때 쓴다. 실행할 수 없으면 nil 이다.
// FindFor 와 같은 판단을 doctor 도 해야 하므로 여기 한 자리에 둔다 — 두 곳에서
// 따로 짐작하면 doctor 가 "이걸 쓴다" 고 말한 것과 실제로 도는 것이 갈린다.
func FindFlavorAt(path, model string) *CLI {
	if !usable(path) {
		return nil
	}
	f := FlavorOf(path)
	if f != FlavorClaude {
		model = "" // 설정의 judge_model 은 claude 값이다 (defaultModel 주석)
	}
	return newFlavorCLI(f, path, model)
}
