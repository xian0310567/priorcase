package health

import (
	"fmt"
	"strings"
	"time"

	"github.com/xian0310567/priorcase/internal/core/rollup"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// 주간 요약이 밀린 것을 말한다.
//
// # 고치려는 고장
//
// `prior rollup` 은 **아무도 부르지 않는다.** 데몬도 훅도 doctor 도 한 번도
// 언급하지 않는 순수 수동 명령이라, 안 도는 것이 도는 것과 구별되지 않는다.
// 이 패키지의 존재 이유가 정확히 그 상태다(패키지 §).
//
// 2026-09-01 실볼트: 8개 도메인에 **13주**가 밀려 있었다 (priorcase W32~W35,
// common·editup W34~W35, bard W33~W34, tutela·create·draft.ai W33~W34). 그동안
// 볼트에 쌓인 요약 파일은 2개뿐이고, 작업 로그는 2.8MB 로 **결정 노트 전체
// (2.1MB)보다 커졌다.**
//
// 이건 이 프로젝트가 이미 한 번 겪은 병이다. 감독 큐를 걷어낼 때 그 결정문이
// 이렇게 적었다 — "목록을 보러 가는 일은 따로 시간을 내는 일이고, 따로 시간을
// 내는 일은 안 일어난다." 그때의 해법도 지금과 같다: **이미 보고 있는 자리에
// 띄운다.**
//
// # 왜 지난주 하나로는 말하지 않는가
//
// 주가 끝나자마자 경고를 띄우면 **매주** 뜬다. 매주 뜨는 경고는 읽히지 않고,
// 읽히지 않는 경고는 옆의 검사까지 같이 무시당하게 만든다(Check.Fix 주석).
// 그래서 한 주의 유예를 준다 — 지난주는 아직 "할 일"이지 "밀린 것"이 아니다.
//
// **개수가 아니라 나이로 잰다.** 개수로 재면 도메인이 여덟이라는 이유만으로
// 매주 문턱을 넘는데, 그건 사용자의 잘못이 아니라 프로젝트가 많다는 뜻이다.
// 나이로 재면 "지난주를 아직 안 했다"와 "한 달째 안 돌고 있다"가 갈린다.
const staleRollupWeeks = 2

// StaleRollups 는 문턱을 넘겨 밀린 주를 오래된 순으로 준다.
func StaleRollups(l *store.Layout, now time.Time) ([]rollup.Week, error) {
	weeks, err := rollup.Scan(l, now)
	if err != nil {
		return nil, err
	}
	var out []rollup.Week
	for _, w := range weeks {
		if w.Todo() && w.Behind(now) >= staleRollupWeeks {
			out = append(out, w)
		}
	}
	// Scan 은 도메인 순서로 주므로 여기서 나이순으로 다시 세운다. 사람이 손댈
	// 자리는 "어느 도메인" 이 아니라 "가장 오래 방치된 주" 다.
	sortByAge(out, now)
	return out, nil
}

func sortByAge(ws []rollup.Week, now time.Time) {
	for i := 1; i < len(ws); i++ {
		for j := i; j > 0 && ws[j].Behind(now) > ws[j-1].Behind(now); j-- {
			ws[j], ws[j-1] = ws[j-1], ws[j]
		}
	}
}

func checkRollup(r *Report, l *store.Layout, now time.Time) {
	const name = "주간 요약"

	stale, err := StaleRollups(l, now)
	if err != nil {
		// **여기서 조용하면 그 볼트는 초록불인 채로 로그만 무한히 자란다.**
		// naming.rollup 이 없거나 로그를 못 읽는 경우인데, 둘 다 요약을 영영
		// 못 하게 만든다. 기록 자체는 멀쩡하므로 Warn 이다.
		r.add(name, Warn, err.Error(),
			`[naming] 에 rollup = "98-{project}-작업-로그-요약.md" 를 넣어라`)
		return
	}
	if len(stale) == 0 {
		r.add(name, OK, "밀린 주 없다", "")
		return
	}

	seen := map[string]bool{}
	var domains []string
	for _, w := range stale {
		if !seen[w.Prefix] {
			seen[w.Prefix] = true
			domains = append(domains, w.Prefix)
		}
	}
	oldest := stale[0]

	detail := fmt.Sprintf("요약 안 한 주 %d주 · 도메인 %d개 (%s) — 가장 오래된 것은 %s %s (%d주 전)",
		len(stale), len(domains), strings.Join(clip(domains), ", "),
		oldest.Prefix, oldest.Week, oldest.Behind(now))

	r.add(name, Warn, detail,
		fmt.Sprintf("prior rollup 이 남은 주를 보여 준다 (그 주를 읽는 것은 "+
			"prior rollup %s %s)", oldest.Prefix, oldest.Week))
}
