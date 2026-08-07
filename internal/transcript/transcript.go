// Package transcript 는 호스트의 대화 기록을 casebook 이 다룰 수 있는 형태로 옮긴다.
//
// **읽기 전용이다.** 이 패키지도, 이걸 쓰는 데몬도 transcript 파일에 절대 쓰지 않는다.
// 호스트가 소유한 파일이고, 우리가 건드리면 그 호스트의 대화가 깨진다.
//
// 호스트별 파서는 하위 패키지(claudecode 등)에 둔다. 인터페이스가 `(파일) → []Turn`
// 하나로 좁아서, 하나가 깨져도 다른 호스트에 번지지 않는다.
package transcript

import "time"

// Kind 는 발화의 종류다.
//
// **셋을 나눠 두는 이유가 감사 결함 6 이다.** 옛 셸 구현은 JSONL 레코드를 그냥 세서
// 턴 수 임계를 판정했는데, 어시스턴트가 툴을 세 번 부르면 tool_use 3 + tool_result 3 으로
// 여섯 턴이 차 버렸다. 실측(개발 중 실 transcript 8607줄)에서 레코드 5920개 중
// tool_use 1981 · tool_result 1981 · thinking 932 이고 실제 발화는 991개였다 — 6배다.
//
// 규칙은 **턴 수는 User·Assistant 만 세고, 시그널 검색은 셋 다 본다** 이다.
//
// ⚠️ **다만 Claude Code 에서 KindThinking 은 사실상 나오지 않는다.** transcript 의
// thinking 블록은 암호화된 `signature` 만 담고 `thinking` 본문은 비어 있다 — 실측으로
// 파일 1173개의 블록 13451개가 **전부** 그랬다. 그래서 파서는 이 종류를 다룰 줄 알지만
// 실제로 만들어 내지 못한다.
//
// 이게 안전망의 진짜 한계다: **사고 안에서만 내려지고 밖으로 한 줄도 안 나온 결정은
// 데몬이 볼 수 없다.** 종류를 남겨 두는 것은 호스트가 나중에 본문을 실어 주면 그때
// 자동으로 잡히게 하기 위해서고, 지금 그렇지 않다는 사실은 문서에 적는다.
type Kind string

const (
	KindUser      Kind = "user"      // 사람의 발화
	KindAssistant Kind = "assistant" // 에이전트가 밖으로 낸 말
	KindThinking  Kind = "thinking"  // 에이전트의 내부 사고
)

// Counts 는 턴 수 임계에 세는 발화인지 알려준다.
func (k Kind) Counts() bool { return k == KindUser || k == KindAssistant }

// Turn 은 대화 한 조각이다.
type Turn struct {
	Kind      Kind
	Text      string
	Timestamp time.Time
	// Sidechain 은 서브에이전트 대화를 뜻한다. 서브에이전트도 결정을 내리므로
	// 시그널 검색에서 빼지 않는다 — 안전망은 놓치는 쪽이 더 나쁘다.
	Sidechain bool
}

// Meta 는 파일 하나에서 뽑아낸 세션 정보다.
type Meta struct {
	SessionID string
	Cwd       string
}
