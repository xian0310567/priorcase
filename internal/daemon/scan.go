package daemon

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/transcript"
	"github.com/xian0310567/casebook/internal/transcript/claudecode"
)

// ScanResult 는 파일 하나를 한 번 훑은 결과다.
//
// Advanced 와 Flagged 를 따로 두는 이유: 둘은 독립이다. 깨진 줄이 있으면
// **표시는 하되 전진은 못 한다**. 하나로 합치면 그 상태를 표현할 수 없다.
type ScanResult struct {
	Turns    int      // 이번 구간에서 임계에 세는 발화 수
	Bad      int      // 파싱 실패한 줄 수
	Signals  []string // 걸린 시그널
	Flagged  bool     // pending 을 남겼나
	NoFilter bool     // 시그널 필터를 건너뛰었나 (판별기가 있어서)
	Advanced bool     // 체크포인트를 전진시켰나
	Excluded bool     // 제외된 경로라 표시를 건너뛰었나
	// Recorded 는 **면제 크레딧을 소모해 표시를 건너뛰었나**다.
	//
	// "이미 기록됐다" 가 아니다. 볼트에 노트가 있어도 그 크레딧을 이미 썼으면
	// false 이고 표시된다 — 그것이 정상 동작이다. 이름은 옛 의미에서 왔다.
	Recorded bool
}

// Scan 은 transcript 파일 하나의 새 구간을 처리한다. **파일에 쓰지 않는다.**
//
// judgeAvailable 이 true 면 **키워드 시그널을 건너뛴다.**
//
// 시그널은 "이 구간에 결정이 있을까" 를 낱말로 어림하는 것인데, 이 프로젝트는 이미
// *"규칙으로는 결정을 판정할 수 없다"* 를 실측으로 확인했다(한 세션 후보 160개, 대부분
// 결정 아님). 그런데도 판별기 **앞에** 그 규칙을 세워 두면, 판별기가 보지도 못한 채
// 걸러진다.
//
// 실측이 그 필터가 거의 안 거른다는 것도 보여 준다 — 발화 6개를 넘는 세션 585개 중
// **578개(98.8%)** 가 시그널에 걸린다. 건너뛰어도 판별기 호출은 ~1.2% 늘 뿐이다.
//
// 그리고 **시그널은 설정에 적힌 낱말이라 언어에 묶인다.** 한국어 기본 시그널로
// 영어 대화를 훑으면 아무것도 안 걸리는데, 로그에는 "훑음 — 발화 8" 이라 정상으로
// 보인다. 실제로 그 상태를 재현해 확인했다. 판별기가 있으면 그 지뢰가 사라진다.
//
// 필터 체인 (스펙 §7.2):
//
//	체크포인트 이후 구간 → 턴 수 임계 → 키워드 시그널 → pending 기록
//
// 전진 규칙은 세 갈래다.
//
//  1. 깨진 줄이 있다 → **전진하지 않는다.** 스펙 §7.2 의 단일 규칙이고, 감사 결함
//     1·2 가 여기서 함께 닫힌다. 다음 스캔이 같은 구간을 다시 본다.
//  2. 턴 수가 임계 미만이다 → **전진하지 않는다.** 여기서 전진하면 임계가 영원히
//     안 찬다 — 4턴 보고 전진, 또 4턴 보고 전진, 매번 4 < 6 이라 대화가 아무리
//     길어져도 안전망이 한 번도 발동하지 않는다. 구간을 누적해야 임계가 의미를 갖는다.
//  3. 임계를 넘겼다 → 전진한다. 시그널이 있으면 표시하고, 없으면 다 보고 결정이
//     없다고 판단한 것이므로 그냥 전진한다.
func Scan(s *Store, c *config.Config, l *store.Layout, path string, judgeAvailable bool) (ScanResult, error) {
	var r ScanResult

	info, err := os.Stat(path)
	if err != nil {
		return r, fmt.Errorf("transcript 를 볼 수 없다 (%s): %w", path, err)
	}
	size := info.Size()

	// **성공한 스캔만 흔적을 남긴다.** 읽을 게 없어서 바로 나가는 경로도 성공이다 —
	// 그 경로가 가장 흔한데 흔적을 안 남기면 안전망이 도는지 알 방법이 없다.
	//
	// 다만 **실패한 스캔에는 남기지 않는다.** 깨진 줄로 매번 실패하는 파일이 doctor
	// 에게 "방금 훑음" 으로 보이면, 생존 증거로 세운 바로 그 값이 거짓말을 한다.
	var scanErr error
	defer func() {
		if scanErr == nil {
			_ = s.NoteScan(path, time.Now().UTC())
		}
	}()

	from := s.CheckpointFor(path, size)
	if from >= size {
		return r, nil // 새로 읽을 것이 없다
	}

	f, err := os.Open(path)
	if err != nil {
		scanErr = fmt.Errorf("transcript 를 열 수 없다 (%s): %w", path, err)
		return r, scanErr
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		scanErr = err
		return r, scanErr
	}

	turns, meta, consumed, bad, err := claudecode.Parse(f)
	if err != nil {
		scanErr = fmt.Errorf("transcript 파싱 실패 (%s): %w", path, err)
		return r, scanErr
	}
	r.Bad = bad

	for _, t := range turns {
		if t.Kind.Counts() {
			r.Turns++
		}
	}
	r.Signals = matchSignals(c.Capture.Signals, turns)
	r.NoFilter = judgeAvailable
	days := segmentDays(turns)

	minTurns := c.Capture.MinTurns
	if minTurns <= 0 {
		minTurns = 1
	}
	if r.Turns < minTurns {
		return r, nil // 갈래 2 — 누적한다
	}

	// 제외된 구역은 표시하지 않는다. 다만 전진은 시켜야 한다 — 안 그러면 그 프로젝트의
	// transcript 를 영원히 다시 읽는다.
	r.Excluded = meta.Cwd != "" && c.IsExcluded(meta.Cwd)

	// **이미 기록됐으면 표시하지 않는다.** 원본 명세 4-B 의 "INDEX 대조로 4-A 에서
	// 이미 기록된 결정과의 중복을 방지한다" 가 이것이다. 옛 구현에서는 판별 LLM 이
	// INDEX 를 보고 판단했는데, LLM 을 걷어내면서 이 검사까지 같이 사라졌었다.
	//
	// 이게 없으면 안전망이 소음이 된다 — 실측으로 이 머신의 transcript 1173개 중
	// 발화 6개를 넘는 585개의 **99%(578개)** 가 시그널에 걸린다. 기본 시그널이
	// "변경"·"선택"·"대신" 처럼 흔한 낱말이라, 사실상 모든 실질 세션이 표시된다.
	// 에이전트가 제 할 일을 다 한 세션까지 표시하면 무시하는 법을 배운다.
	// 판별기가 있으면 시그널이 안 걸려도 넘긴다 — 판정은 판별기가 한다.
	worthJudging := len(r.Signals) > 0 || judgeAvailable

	if worthJudging && !r.Excluded {
		domain := c.DomainForCwd(meta.Cwd)
		sessionN, perDay, ferr := coveringNotes(l, domain, meta.SessionID, days)
		if ferr != nil {
			// 볼트를 못 읽었다고 표시를 건너뛰면 안전망이 조용히 꺼진다.
			// 모르면 표시하는 쪽으로 기운다 — 놓치는 것이 더 나쁘다.
			r.Signals = append(r.Signals, "(볼트 대조 실패: "+ferr.Error()+")")
		} else {
			rec, cerr := s.Credit(path, sessionN, perDay)
			if cerr != nil {
				scanErr = cerr
				return r, scanErr
			}
			r.Recorded = rec
		}
	}

	if worthJudging && !r.Excluded && !r.Recorded {
		p := Pending{
			SessionID: meta.SessionID,
			Path:      path,
			Cwd:       meta.Cwd,
			Domain:    c.DomainForCwd(meta.Cwd),
			Turns:     r.Turns,
			Signals:   r.Signals,
			From:      from,
			To:        from + consumed,
			Days:      days,
			Excerpt:   excerpt(turns),
			At:        time.Now().UTC(),
		}
		if err := s.AddPending(p); err != nil {
			scanErr = err
			return r, scanErr
		}
		r.Flagged = true
	}

	if bad == 0 {
		if err := s.Advance(path, from+consumed, size); err != nil {
			scanErr = err
			return r, scanErr
		}
		r.Advanced = true
	}
	return r, nil
}

// matchSignals 는 걸린 시그널을 설정에 적힌 순서대로 준다.
//
// thinking 도 훑는다 — 다만 Claude Code 는 thinking 본문을 비워 두므로 실제로는
// 밖으로 나온 말만 보게 된다. 그게 이 안전망의 한계다 (transcript 패키지 주석 참조).
func matchSignals(signals []string, turns []transcript.Turn) []string {
	if len(signals) == 0 || len(turns) == 0 {
		return nil
	}
	var b strings.Builder
	for _, t := range turns {
		b.WriteString(t.Text)
		b.WriteByte('\n')
	}
	hay := b.String()

	var got []string
	for _, sig := range signals {
		if sig != "" && strings.Contains(hay, sig) {
			got = append(got, sig)
		}
	}
	return got
}

// segmentDays 는 구간이 걸친 날짜를 YYYY-MM-DD 로 준다.
func segmentDays(turns []transcript.Turn) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range turns {
		if t.Timestamp.IsZero() {
			continue
		}
		d := t.Timestamp.UTC().Format("2006-01-02")
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

// coveringNotes 는 이 구간을 가려 줄 수 있는 결정 노트를 **축별로** 센다.
//
// 있다/없다가 아니라 개수를 주는 것이 요점이다. 호출자(Store.Credit)가 지난번보다
// 늘었는지를 보고 면제를 판정하므로 면제가 소모성이 된다. 있다/없다로 답하면 노트
// 하나가 그 세션 전체를 영구히 면제한다 — 컷오버 1일차가 그 상태였다.
//
// **두 축을 합치지 않는다.** 세션 축은 날짜에 무관해 단조지만 날짜 축은 구간이
// 걸친 날짜로 걸러져 창이 움직인다. 하나로 합쳐 개수만 비교하면 넓은 창이 찍은
// 고점을 좁은 창이 넘지 못해 면제가 영영 안 걸린다(state.go 의 DayCredited 주석).
func coveringNotes(l *store.Layout, domain, sessionID string, days []string) (int, map[string]int, error) {
	perDay := map[string]int{}
	if l == nil {
		return 0, perDay, nil
	}
	notes, _, err := l.List()
	if err != nil {
		return 0, nil, err
	}
	dayset := map[string]bool{}
	for _, d := range days {
		dayset[d] = true
		perDay[d] = 0 // 노트가 없는 날도 0 으로 둔다 — 그래야 그날 첫 노트가 면제를 산다
	}

	sessionN := 0
	for _, note := range notes {
		// ① 세션 대조 — 정확하다. 이 대화에서 나온 결정이 기록돼 있다는 직접 증거다.
		//    도메인·날짜와 무관하게 성립하므로, 자정을 넘긴 세션이나 도메인이 바뀐 경우도 잡는다.
		if sessionID != "" && note.Meta.SourceSession == sessionID {
			sessionN++
			continue
		}
		// ② 날짜+도메인 폴백 — 거칠다. source_session 이 안 채워진 기록(에이전트가 세션 id 를
		//    안 넘겼거나 사람이 손으로 쓴 노트)을 위한 그물이다.
		//
		//    ②를 떼면 세션 id 를 안 넘기는 경로에서 면제가 통째로 사라져 실질 세션의
		//    99%가 표시된다(Plan 3 실측). 그래서 남겨 두되 날짜별로 따로 센다.
		if domain == "" || !dayset[note.Meta.Date] {
			continue
		}
		for _, d := range note.Meta.Domain {
			if d == domain {
				perDay[note.Meta.Date]++
				break
			}
		}
	}
	return sessionN, perDay, nil
}

// maxExcerpt 는 pending 에 담는 발췌의 상한이다.
//
// 상태 파일이 무한히 커지면 안 되고, 판별기에 넘길 때도 토큰이 든다. 결정은 보통
// 구간 끝쪽에서 내려지므로 **뒤에서부터** 담는다 — 앞을 자르는 편이 낫다.
const maxExcerpt = 6000

// excerpt 는 발화 원문을 뒤에서부터 maxExcerpt 만큼 모은다.
func excerpt(turns []transcript.Turn) string {
	var parts []string
	total := 0
	for i := len(turns) - 1; i >= 0; i-- {
		t := strings.TrimSpace(turns[i].Text)
		if t == "" {
			continue
		}
		who := "에이전트"
		if turns[i].Kind == transcript.KindUser {
			who = "사용자"
		}
		line := who + ": " + t
		if total+len(line) > maxExcerpt {
			break
		}
		parts = append(parts, line)
		total += len(line)
	}
	// 뒤에서부터 모았으므로 뒤집어 시간순으로 돌린다.
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "\n\n")
}
