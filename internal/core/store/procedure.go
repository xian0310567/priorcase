package store

import (
	"regexp"
	"sort"
	"strings"
)

// ── 본문의 실행 절차를 드러낸다 ────────────────────────────────────────
//
// **2026-08-31 재현 고장의 나머지 절반이 여기다.**
//
// 회수가 orca 결정을 제대로 주입했는데도 에이전트는 "지라·슬랙·구글챗에 붙을
// 수단이 이 환경에 없습니다" 라고 결론냈다. 받은 것이 이 한 줄이었기 때문이다:
//
//	- 2026-08-24 슬랙은 Orca 브라우저에 이미 살아 있는 세션으로 URL 이동 +
//	  DOM 읽기로 읽는다 … (active/good) → editup/decisions/….md
//
// 이건 **과거에 무엇을 정했는가**(사건)지 **지금 무엇을 부를 수 있는가**(능력)가
// 아니다. 실제 호출법은 본문의 `## 절차` 안에 있다:
//
//	orca tab create --url 'https://app.slack.com/client/T017MTC9004'
//	orca eval --expression '…'
//
// 그리고 `orca` 는 MCP 도구가 아니라 `/Applications/Orca.app/…/bin/orca` CLI 라,
// 그 프로젝트의 CLAUDE.md 에도 .mcp.json 에도 없다. **에이전트가 그 도구의 존재를
// 알 수 있는 통로가 이 노트의 본문뿐인데 주입되는 것은 요약뿐이다.** 사용자가
// "priorcase 에 기록이 있으니 찾아봐" 라고 말해야 그제서야 파일을 열었다.
//
// 요약을 길게 쓰라는 것은 답이 아니다 — 길이 정규화(search)가 긴 요약을 감점하고,
// 그 감점의 근거가 실측된 47배 편향이다. 대신 **절차가 있다는 사실과 명령 이름만**
// 주입한다. 그 한 낱말이 "이 도구는 존재하고 부를 수 있다" 를 만든다.
//
// # 비용
//
// 실볼트 543건 중 셸 블록을 가진 것은 **5건(0.9%)** 이고 그중 orca 가 21회다.
// 이 줄은 거의 안 붙고, 붙는 자리가 정확히 필요한 자리다.

// shellFence 는 셸 코드 블록을 잡는다. 언어 표시가 있는 것만 본다 — 언어 없는 블록은
// 로그·출력·JSON 이 훨씬 많아서 첫 낱말을 명령으로 읽으면 잡음이 된다.
var shellFence = regexp.MustCompile("(?s)```(?:bash|sh|shell|zsh|console)\\r?\\n(.*?)```")

// cmdToken 은 명령 이름으로 받을 모양이다. 경로·변수·플래그는 뺀다.
var cmdToken = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]*$`)

// 셸 문법이라 명령 이름이 아닌 것들. 이것만 뽑히면 절차가 아니다.
var shellNoise = map[string]bool{
	"cd": true, "echo": true, "for": true, "do": true, "done": true, "if": true,
	"then": true, "fi": true, "while": true, "export": true, "set": true,
	"var": true, "let": true, "sleep": true, "true": true, "false": true,
	"sudo": true, "env": true, "exit": true, "return": true, "source": true,
}

// maxProcedureCmds 는 주입 줄에 실을 명령 수다. 이 줄의 목적은 **그 도구가
// 있다는 사실**이라 이름 몇 개면 족하다 — 절차 전체는 노트를 열어야 한다.
const maxProcedureCmds = 3

// ProcedureCommands 는 본문 셸 블록에서 실행하는 명령 이름을 준다.
//
// 등장 횟수가 많은 것부터 준다 — 절차의 주인공이 가장 자주 나온다
// (실볼트: orca 21회).
func ProcedureCommands(body []byte) []string {
	count := map[string]int{}
	for _, m := range shellFence.FindAllSubmatch(body, -1) {
		for _, raw := range strings.Split(string(m[1]), "\n") {
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			// 프롬프트 표시를 벗긴다: `$ orca …`
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "$"), ">"))
			f := strings.Fields(line)
			if len(f) == 0 {
				continue
			}
			tok := f[0]
			if !cmdToken.MatchString(tok) || shellNoise[strings.ToLower(tok)] {
				continue
			}
			count[tok]++
		}
	}
	if len(count) == 0 {
		return nil
	}
	out := make([]string, 0, len(count))
	for k := range count {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if count[out[i]] != count[out[j]] {
			return count[out[i]] > count[out[j]]
		}
		return out[i] < out[j]
	})
	if len(out) > maxProcedureCmds {
		out = out[:maxProcedureCmds]
	}
	return out
}
