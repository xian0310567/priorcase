package hook

import (
	"strings"

	"github.com/xian0310567/priorcase/internal/daemon"
	"github.com/xian0310567/priorcase/internal/transcript"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
)

// ★★★ **회수 질의에 직전 대화를 붙인다.**
//
// # 고치려던 고장
//
// 회수는 지금까지 사람이 방금 친 한 줄만 검색어로 썼다. 그런데 **주제는 프롬프트가
// 아니라 세션에 있다** — 대화체는 대명사·생략·직전 화제 참조로 굴러가서 그 한 줄에
// 내용어가 거의 없다. 실측한 프롬프트가 이런 것들이다:
//
//	"좋아. 일단 priorcase에 관련 내용을 저장해놔줘."
//	"GA 업데이트 관련 내용을 어디까지 진행했었지?"
//	"그럼 이제 뭘 하는게 좋을까? 혹시 어제 완료하지 않은 작업이 있어?"
//
// 셋 다 그 자리에서 필요했던 결정이 분명히 있었는데(볼트가 그것을 `related` 로
// 남겼다) 회수는 엉뚱한 것을 꺼냈다.
//
// # 실측 (2026-09-04, 실볼트 결정 679건 · 실물 라벨 165건 · 서로 다른 질의 60개)
//
// 라벨은 `prior eval --make-labels` 가 볼트에서 만든다 — 노트의 `source_session`
// 으로 그 대화를 찾고, `related` 를 "그 자리에서 실제로 필요했던 노트" 로 삼는다.
//
//	맥락 글자수   상위3에 정답이 든 질의   좋아짐/나빠짐(질의 단위)
//	0 (현행)         15 / 60  (25%)            —
//	500              22 / 60  (37%)         11 / 4
//	1000             30 / 60  (50%)         18 / 2
//	**1500**         33 / 60  (55%)         22 / 3
//	2000             30 / 60  (50%)         21 / 6
//	4000             28 / 60  (47%)         20 / 5
//
// 1500 을 넘으면 **들어오는 것은 안 늘고 밀려나는 것만 는다.** 맥락이 길어질수록
// 우연히 스치는 낱말이 함께 늘어서, 어느 지점부터는 그 잡음이 정답을 밀어낸다.
//
// # 왜 사람이 친 말만 담지 않는가 (재 보고 기각)
//
// 어시스턴트 출력과 도구 활동을 빼는 것이 깨끗해 보였는데 **더 나빴다**
// (상위3 진입 21 대 28). 기제가 분명하다 — 그쪽에 `word`·`extension`·파일 경로 같은
// **라틴 원어**가 들어 있고, 그것이 한국어 음차(`워드`·`익스텐션`)와 볼트 어휘를
// 잇는 다리가 된다. 표기 불일치는 2026-09-02 실측에서 어휘 미스의 주요 갈래였다.
//
// # 비용
//
// 회수 한 번이 86ms → 108ms 가 된다(실볼트, 워밍 후 10회 평균). 기록 파일은
// 끝부분만 읽는다 (transcript.Tail 의 §).

// contextRunes 는 질의에 붙이는 직전 대화의 길이다. 근거는 위 표다.
const contextRunes = 1500

// tailBytes 는 기록 파일에서 읽어 볼 끝부분의 크기다.
//
// 맥락 1,500자를 채우려면 발화 몇 개면 되는데, JSONL 한 줄에는 도구 입력·결과가
// 같이 실려 있어 발화 한 개당 바이트가 훨씬 크다. 256KB 는 이 기계 기록의
// p90(128KB)의 두 배라 대부분의 파일을 통째로 덮고, 51MB 짜리에서도 직전 대화는
// 넉넉히 들어온다.
const tailBytes = 256 << 10

// recentContext 는 이번 프롬프트 **앞의** 대화를 준다.
//
// **무슨 일이 있어도 빈 문자열로 끝난다.** 맥락은 회수를 좋게 하려는 것이지
// 회수의 전제가 아니다 — 기록을 못 읽었다고 회수까지 죽으면 지금보다 나빠진다.
func (o Options) recentContext() string {
	path := o.Input.TranscriptPath
	if path == "" {
		return ""
	}
	// ★ **파서는 경로가 아니라 `--host` 로 고른다.**
	//
	// 처음에는 `hosts.For(경로, …)` 로 골랐다. 그러면 기록이 그 호스트의 **기본
	// 자리 아래에 있을 때만** 맞는데, 훅은 자기 호스트를 이미 정확히 알고 있다 —
	// `prior init` 이 설정 파일에 써 넣은 값이다(host.go 의 "왜 자동 감지가
	// 아닌가"). 경로로 추론하면 기록 자리를 옮긴 사람과 시험 픽스처에서 조용히
	// 빈 맥락이 되고, 그 침묵은 "대화가 없다" 와 구별되지 않는다.
	//
	// 설정에서 끈 호스트는 여기서도 안 읽는다 — 그 스위치는 "이 도구의 대화를
	// 읽지 마라" 는 뜻이고, 회수 맥락도 그 대화를 읽는 일이다.
	h := o.parserFor(o.Host)
	if h == nil {
		return ""
	}
	r, err := transcript.Tail(path, tailBytes)
	if err != nil {
		return ""
	}
	// consumed·bad 는 버린다 — 잘라 낸 조각의 값이라 체크포인트에 쓰면 안 된다
	// (transcript.Tail 의 §).
	turns, _, _, _, err := h.Parse(r)
	if err != nil {
		return ""
	}
	return joinTail(turns, contextRunes)
}

// parserFor 는 그 호스트의 파서를 준다. 설정에서 꺼져 있으면 nil 이다.
func (o Options) parserFor(want Host) *hosts.Host {
	rs, err := daemon.ResolveHosts(o.Config, "")
	if err != nil {
		return nil
	}
	id := string(want)
	if id == "" {
		id = string(HostClaudeCode) // 빈 값은 Claude Code 다 (ParseHost 의 §)
	}
	for i := range rs {
		if rs[i].Host.ID == id {
			return &rs[i].Host
		}
	}
	return nil
}

// joinTail 은 발화를 **뒤에서부터** 담아 limit 글자를 채우고 원래 순서로 되돌린다.
//
// 뒤에서부터인 이유는 직전 화제가 주제이기 때문이다. 순서를 되돌리는 것은
// 점수 때문이 아니라(회수는 낱말 집합만 본다) 진단 때문이다 — `prior recall
// --context` 로 같은 맥락을 손에 놓고 볼 때 대화가 거꾸로면 사람이 못 읽는다.
func joinTail(turns []transcript.Turn, limit int) string {
	var parts []string
	total := 0
	for i := len(turns) - 1; i >= 0 && total < limit; i-- {
		t := strings.TrimSpace(turns[i].Text)
		if t == "" {
			continue
		}
		r := []rune(t)
		if total+len(r) > limit {
			r = r[len(r)-(limit-total):]
		}
		parts = append(parts, string(r))
		total += len(r)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "\n")
}
