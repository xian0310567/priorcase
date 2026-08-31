package transcript

import "strings"

// ★★★ **하네스가 만든 글은 대화가 아니다.**
//
// 이 파일은 `isMeta` 가드(claudecode/parse.go)와 같은 규칙의 연장이다. 그 주석이
// 이유를 이미 적어 뒀다 — 호스트가 주입한 글을 발화로 세면 자기 참조 고리가 생긴다.
// 여기서 막는 것은 그 고리의 나머지 세 갈래다.
//
// # 실측 (2026-08-31, 판정 원장 1,462건 · 트랜스크립트 964세션)
//
// 판정의 **12.6%(184건)** 가 발췌에 대화 아닌 글을 담고 있었다:
//
//	You have access to a memory folder   148건   ← Codex 의 developer 채널
//	<command-name>                        34건   ← 슬래시 명령 에코
//	safeguards flagged                     6건   ← API 차단 알림
//	API Error:                             5건
//	Request ID: req_                       5건
//
// 그 발췌 총 365,556자가 판별기(LLM CLI) 입력으로 나갔다. 판별기 한 번은 프로세스
// spawn + LLM 왕복이라 초 단위다.
//
// # 왜 **맨 앞**만 보는가 (부분 문자열이 아니라)
//
// **정상 대화가 이 글을 인용한다.** 실측으로 `API Error:` 는 발화 22건에 나오는데
// 그중 5건이 그 에러를 *분석하는* 대화였고(진단 결과를 요약한 에이전트 발화),
// `<command-name>` 은 46건 중 1건이 그랬다. 부분 문자열로 걸렀으면 그 6건을
// 통째로 지웠을 것이다 — 고치려는 것이 "잡음을 지운다" 인데 내용을 지우면 반대다.
//
// 하네스가 만든 글은 **언제나 그 표지로 시작한다.** 앵커를 두면 인용은 살고 원본만
// 빠진다. 실측에서 맨 앞 출현은 17·45·39·33건으로 전부 진짜 하네스 글이었다.
//
// # 무엇을 넣지 않았나
//
// `safeguards flagged` · `Request ID: req_` 는 따로 안 넣는다 — 그 글은 언제나
// `API Error:` 로 시작하는 같은 블록의 일부라 표지 하나면 충분하고, 낱개로 넣으면
// 위의 "인용은 살린다" 가 깨진다(이 문장 자체가 그 예다).
var harnessPrefixes = []string{
	// 호스트가 API 실패를 대화에 적어 넣은 것. **어시스턴트 발화로 저장되므로**
	// 종류로는 못 가른다 — 유일하게 텍스트로 가려야 하는 갈래다.
	"API Error:",

	// 슬래시 명령의 에코. 셋이 한 벌로 나온다(caveat → command-name → stdout).
	"<command-name>",
	"<local-command-stdout>",
	"<local-command-caveat>",
}

// IsHarnessText 는 이 글이 하네스가 만든 것인지 본다.
//
// 앞뒤 공백은 무시한다 — 호스트가 줄바꿈을 앞에 붙이는 경우가 있고, 그것 하나로
// 필터가 통째로 무력해지면 안 된다.
func IsHarnessText(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	for _, p := range harnessPrefixes {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}
