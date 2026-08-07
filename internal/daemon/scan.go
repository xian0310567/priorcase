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
	Advanced bool     // 체크포인트를 전진시켰나
	Excluded bool     // 제외된 경로라 표시를 건너뛰었나
	Recorded bool     // 이미 기록된 결정이 있어 표시를 건너뛰었나
}

// Scan 은 transcript 파일 하나의 새 구간을 처리한다. **파일에 쓰지 않는다.**
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
func Scan(s *Store, c *config.Config, l *store.Layout, path string) (ScanResult, error) {
	var r ScanResult

	info, err := os.Stat(path)
	if err != nil {
		return r, fmt.Errorf("transcript 를 볼 수 없다 (%s): %w", path, err)
	}
	size := info.Size()
	from := s.CheckpointFor(path, size)
	if from >= size {
		return r, nil // 새로 읽을 것이 없다
	}

	f, err := os.Open(path)
	if err != nil {
		return r, fmt.Errorf("transcript 를 열 수 없다 (%s): %w", path, err)
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return r, err
	}

	turns, meta, consumed, bad, err := claudecode.Parse(f)
	if err != nil {
		return r, fmt.Errorf("transcript 파싱 실패 (%s): %w", path, err)
	}
	r.Bad = bad

	for _, t := range turns {
		if t.Kind.Counts() {
			r.Turns++
		}
	}
	r.Signals = matchSignals(c.Capture.Signals, turns)

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
	if len(r.Signals) > 0 && !r.Excluded {
		domain := c.DomainForCwd(meta.Cwd)
		rec, ferr := alreadyRecorded(l, domain, segmentDays(turns))
		if ferr != nil {
			// 볼트를 못 읽었다고 표시를 건너뛰면 안전망이 조용히 꺼진다.
			// 모르면 표시하는 쪽으로 기운다 — 놓치는 것이 더 나쁘다.
			r.Signals = append(r.Signals, "(볼트 대조 실패: "+ferr.Error()+")")
		} else {
			r.Recorded = rec
		}
	}

	if len(r.Signals) > 0 && !r.Excluded && !r.Recorded {
		p := Pending{
			SessionID: meta.SessionID,
			Path:      path,
			Cwd:       meta.Cwd,
			Domain:    c.DomainForCwd(meta.Cwd),
			Turns:     r.Turns,
			Signals:   r.Signals,
			From:      from,
			To:        from + consumed,
			At:        time.Now().UTC(),
		}
		if err := s.AddPending(p); err != nil {
			return r, err
		}
		r.Flagged = true
	}

	if bad == 0 {
		if err := s.Advance(path, from+consumed, size); err != nil {
			return r, err
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

// alreadyRecorded 는 이 도메인·이 날짜에 결정 노트가 이미 있는지 본다.
//
// **날짜 단위인 이유**는 데몬과 기록 경로가 세션 식별자를 공유하지 않기 때문이다.
// MCP 의 세션 id 는 SDK 가 만든 것이고 Claude Code transcript 의 sessionId 와 다르다.
// 훅은 진짜 session_id 를 받으므로 Plan 4 에서 source_session 대조로 정밀해진다.
//
// 한계: 같은 날 같은 도메인에서 **두 번째** 결정은 표시되지 않는다. 안전망의 주된
// 값은 "아무것도 기록 안 된 세션" 을 잡는 데 있으므로 이 쪽으로 기울였다.
func alreadyRecorded(l *store.Layout, domain string, days []string) (bool, error) {
	if l == nil || domain == "" || len(days) == 0 {
		return false, nil
	}
	notes, _, err := l.List()
	if err != nil {
		return false, err
	}
	dayset := map[string]bool{}
	for _, d := range days {
		dayset[d] = true
	}
	for _, n := range notes {
		if !dayset[n.Meta.Date] {
			continue
		}
		for _, d := range n.Meta.Domain {
			if d == domain {
				return true, nil
			}
		}
	}
	return false, nil
}
