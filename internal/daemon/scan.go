package daemon

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/xian0310567/casebook/internal/core/config"
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
func Scan(s *Store, c *config.Config, path string) (ScanResult, error) {
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

	if len(r.Signals) > 0 && !r.Excluded {
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
