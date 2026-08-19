package daemon

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/transcript"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
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
	// Quiet 는 **면제 크레딧을 소모해 그 표시를 조용히 했나**다.
	//
	// 이름이 Recorded 였고 뜻은 "이미 기록됐으니 pending 을 만들지 않았다" 였다.
	// 그 뜻이 통째로 뒤집혔다(state.go 의 Credit 주석). 면제된 구간에 pending 이
	// 안 생기면 판별기가 그 구간을 볼 방법이 없고(Promote 는 ReadPending 으로만
	// 대상을 찾는다), 깨진 줄이 없으면 Scan 이 그대로 Advance 를 부르므로 그 대화는
	// **다시 오지 않는다.** 실측: 최근 7일 판정 23건 / 자동 기록 0건 / 같은 기간 면제
	// 6회 — 볼트에 노트가 하나 생길 때마다 다음 구간 하나가 판별기에 닿지도 못하고
	// 소멸했다.
	//
	// 지금 이 값이 하는 일은 pending 에 Quiet 표를 다는 것뿐이다. 기록은 그대로 가고,
	// 묻지 않았는데 들이미는 곳만 그 표를 본다(ForNudge).
	Quiet bool

	// Excerpt* 는 **판별기가 실제로 무엇을 봤나**다.
	//
	// 왜 반환값으로 빼는가: 발췌가 잘렸는지를 사후에 대조할 방법이 없었다. 이 머신의
	// 원장 32줄 중 excerpt 키가 있는 줄이 **0건**이다 (원장은 승격 성공 때만 발췌를
	// 싣는데, 최근 7일 판정 23건에 자동 기록이 0건이라 그 경로를 한 번도 안 탔다).
	// 그래서 "판별기가 결정을 못 알아봤다" 와 "판별기에게 근거를 안 보여 줬다" 가
	// 밖에서 구별되지 않았다 — 이 프로젝트가 죄목으로 드는 침묵 실패 그 자체다.
	//
	// 표시하지 않은 구간에서는 셋 다 0이다 (발췌를 만들지 않는다).
	ExcerptBytes   int // 만들어진 발췌 크기 (바이트)
	ExcerptTurns   int // 발췌에 실린 발화 수
	ExcerptOmitted int // 상한에 걸려 가운데에서 버린 발화 수
}

// ExcerptNote 는 발췌 통계를 로그 한 조각으로 만든다. 낼 것이 없으면 빈 문자열이다.
//
// **헬퍼로 둔 이유는 문구가 하나여야 하기 때문이다.** 이걸 읽는 곳이 둘이다 —
// 훅의 "훑음" 줄(adapter/hook)과 데몬 watch 의 "scan" 이벤트(daemon/command.go).
// 각자 포맷하면 같은 사실이 두 문장으로 갈리고, 그러면 두 경로를 나란히 놓고
// 대조하는 일이 안 된다. 이 값을 뺀 목적이 바로 그 대조다.
//
// **"생략 0" 과 "발췌 없음" 을 같은 0 으로 보여 주면 안 된다.** 표시하지 않은 구간은
// 발췌를 아예 만들지 않아 셋 다 0인데, 거기에 "생략 0" 이라 적으면 "다 담았다" 로
// 읽힌다 — 그건 이 필드들을 만든 이유(잘림을 사후에 대조한다)를 정확히 무너뜨린다.
// 그래서 발췌가 없으면 아무 말도 안 하고, 다 담았을 때만 **"전부"** 라고 못 박는다.
func (r ScanResult) ExcerptNote() string {
	if !r.Flagged {
		return "" // 발췌를 만들지 않았다. 0을 보여 주면 "다 담았다" 로 읽힌다
	}
	if r.ExcerptBytes == 0 {
		// 표시는 했는데 판별기에게 보여 줄 것이 없다. 이건 정상이 아니라 고장이고,
		// 조용하면 "판별기가 결정을 못 알아봤다" 로 오진된다.
		return "⚠️ 발췌가 비었다 (판별기가 볼 것이 없다)"
	}
	if r.ExcerptOmitted > 0 {
		return fmt.Sprintf("발췌 %dB · 발화 %d (%d 생략)",
			r.ExcerptBytes, r.ExcerptTurns, r.ExcerptOmitted)
	}
	return fmt.Sprintf("발췌 %dB · 발화 %d (전부)", r.ExcerptBytes, r.ExcerptTurns)
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
//
// Scan 은 파일 하나를 훑는다.
//
// hs 는 이 파일이 어느 호스트의 것인지 고르는 데 쓴다. 비어 있으면 기본 목록을
// 쓴다 — 호출부 대부분이 그렇고, 테스트만 좁혀 준다.
func Scan(s *Store, c *config.Config, l *store.Layout, path string, judgeAvailable bool, hs []hosts.Resolved) (ScanResult, error) {
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

	// **어느 호스트의 파일인지 못 고르면 읽지 않는다.**
	//
	// 아무 파서나 대면 발화가 0개로 나오는데, 그건 "그 세션에 결정이 없었다" 와
	// 구별되지 않는다 — 안전망이 조용히 아무것도 안 하는 상태가 된다.
	if len(hs) == 0 {
		// 호출부가 안 넘겼으면 기본 목록으로 푼다. 실패해도 계속 간다 — 그러면
		// 아래에서 "어느 호스트인지 모르겠다" 로 걸려 조용히 넘어가지 않는다.
		hs, _ = hosts.Resolve("")
	}
	h := hosts.For(path, hs)
	if h == nil {
		scanErr = fmt.Errorf("어느 호스트의 기록인지 모르겠다 (%s) — 지원하는 호스트의 루트 밖이다", path)
		return r, scanErr
	}
	turns, meta, consumed, bad, err := h.Host.Parse(f)
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

	// **이미 기록됐으면 표시를 조용히 한다. 표시를 없애지는 않는다.**
	//
	// 원본 명세 4-B 의 "INDEX 대조로 4-A 에서 이미 기록된 결정과의 중복을 방지한다"
	// 가 이 자리인데, 그 대조가 **pending 자체를 없애고** 있었다. pending 은 판별기의
	// 유일한 입력이고(Promote 는 ReadPending 으로만 대상을 찾는다), 깨진 줄이 없으면
	// 아래에서 그대로 Advance 를 부른다 — 그래서 면제된 구간은 판정에 닿지도 못한 채
	// **영구 소멸**했다. 실측: 최근 7일 판정 23건 / 자동 기록 0건 / 같은 기간 면제 6회.
	// 볼트에 노트가 하나 생길 때마다 다음 구간 하나가 사라졌다는 뜻이고, 사람이 손으로
	// 기록할수록 자동 경로가 더 눈감는 되먹임이다 — 정확히 거꾸로다.
	//
	// **그래도 어림 자체는 남긴다.** 판별기가 있으면 시그널을 건너뛰므로 임계를 넘는
	// 모든 구간이 표시가 되는데, 실측으로 이 머신의 transcript 1173개 중 발화 6개를
	// 넘는 585개의 **99%(578개)** 가 어차피 시그널에도 걸린다 — 기본 시그널이
	// "변경"·"선택"·"대신" 처럼 흔한 낱말이라 사실상 모든 실질 세션이 표시된다.
	// 거기서 면제를 지우면 안전망이 소음이 되고, 에이전트는 무시하는 법을 배운다.
	//
	// 그래서 답을 버리지 않고 **pending 에 Quiet 로 싣는다.** 틀렸을 때의 비용이 다른
	// 두 경로를 나눈 것이다(state.go 의 Credit 주석): 알림을 잘못 아끼면 구간이 큐에
	// 남아 회복되지만, 기록을 잘못 아끼면 발췌째 사라져 회복되지 않는다.
	worthJudging := len(r.Signals) > 0 || judgeAvailable

	if worthJudging && !r.Excluded {
		domain := c.DomainForCwd(meta.Cwd)
		sessionN, perDay, ferr := coveringNotes(l, domain, meta.SessionID, days)
		if ferr != nil {
			// 볼트를 못 읽었다고 표시를 건너뛰면 안전망이 조용히 꺼진다.
			// 모르면 표시하는 쪽으로 기운다 — 놓치는 것이 더 나쁘다.
			r.Signals = append(r.Signals, "(볼트 대조 실패: "+ferr.Error()+")")
		} else {
			quiet, cerr := s.CreditQuiet(path, sessionN, perDay)
			if cerr != nil {
				scanErr = cerr
				return r, scanErr
			}
			r.Quiet = quiet
		}
	}

	if worthJudging && !r.Excluded {
		ex, st := buildExcerpt(turns)
		r.ExcerptBytes, r.ExcerptTurns, r.ExcerptOmitted = st.Bytes, st.Turns, st.Omitted
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
			Excerpt:   ex,
			Quiet:     r.Quiet,
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
// 있다/없다가 아니라 개수를 주는 것이 요점이다. 호출자(Store.CreditQuiet)가 지난번보다
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
// **6000 에서 24000 으로 올렸다 (2026-08-18).** 6000 은 판별기에게 결론만 보여 주고
// 근거는 안 보여 주는 크기였다. 실측(이 머신의 트랜스크립트 27개, 발화 1527개):
//
//   - 6000B 를 뒤에서만 채우면 근거·대안이 든 발화의 **29.4%**, 결론이 든 발화의
//     **35.5%** 만 실린다. 메인 세션만 보면 더 나쁘다 — 원문 0.24MB 중 5.4%,
//     발화 646개 중 91개(14.1%)뿐이다.
//   - 24000 으로 올리고 줄 상한을 함께 두면 근거 회수 **65.9%**, 결론 회수 **80.6%**.
//
// **대가는 생각보다 작다.** 상한을 4배로 올려도 평균 발췌는 4.9KB → 6.8KB 밖에 안 는다
// (대부분의 구간이 상한에 닿지 않는다). 판별기 지연도 직접 쟀다 — 같은 발췌를 크기만
// 바꿔 3회씩, claude-haiku-4-5:
//
//	 6000B → 13.9 · 17.6 · 15.3초  (중앙 15.3)
//	24000B → 18.3 · 22.7 · 17.4초  (중앙 18.3)
//
// 중앙값 +3.0초, 최댓값 22.7초로 judge.DefaultTimeout(75초)에 여유가 남는다.
// 판별기 지연의 지배항은 입력 크기가 아니라 호출 자체의 편차다 (judge.go 의
// DefaultTimeout 주석: 같은 2.4KB 로 10.2~28.5초).
//
// **48000 으로 더 올리지 않은 이유.** 회수가 65.9% → 70.6% 로 완만해지는데,
// pending 에는 만료가 없어서(state.go 에 TTL 이 없다) `--backfill` 로 수백 건이 쌓이면
// state.json 이 그만큼 커지고 그 파일은 **매 스캔마다 통째로 다시 쓰인다.**
// 얻는 5%p 보다 그 비용이 크다고 봤다.
const maxExcerpt = 24000

// maxExcerptLine 은 발화 **한 줄**의 상한이다.
//
// 이게 없어서 발췌가 통째로 죽고 있었다. 실측: 발화 줄 길이의 중앙값은 111B,
// p90 은 168B 인데 **상위 1%가 전체 바이트의 25.4%**를 먹는다. 6000B 를 혼자 넘는
// 줄도 0.46% 있다 (붙여넣은 로그, 긴 지시문).
//
// 옛 구현은 예산을 넘는 줄을 만나면 `break` 했다. 그래서 구간 끝에 거대한 발화가
// 하나 있으면 **거기서 발췌가 끝났다** — 2.68MB·40발화짜리 구간의 발췌가 단 1줄이었던
// 것이 이 경로다. 줄 하나가 예산을 독식하지 못하게 막는 것이 앞뒤 배분보다 효과가 크다:
// 6000B 예산에서 줄 상한 1500B 만 도입해도 근거 회수 29.4% → 43.5%, 결론 회수
// 35.5% → 71.0% 로 뛴다.
//
// 1500 인 이유: p90(168B)의 9배라 정상 발화는 하나도 안 건드리면서, 상한(24000)의
// 1/16 이라 한 줄이 예산의 6% 넘게 못 먹는다.
const maxExcerptLine = 1500

// excerptHeadShare 는 상한 중 **앞**에 배정하는 몫(%)이다.
//
// **뒤가 다수여야 하는 이유**와 **앞이 0이면 안 되는 이유**를 둘 다 실측으로 정했다.
// 메인 세션 3개를 십분위로 갈라 "근거·대안이 든 발화" 와 "결론이 든 발화" 를 세니:
//
//	십분위    0    1    2    3    4    5    6    7    8    9
//	근거%    4.7  9.3 11.6 16.3 16.3  7.0 16.3  9.3  4.7  4.7
//	결론%    0.0  0.0  0.0 25.0 25.0  0.0  8.3 16.7  0.0 25.0
//
// **결론은 앞 30%에 한 건도 없다.** 그래서 뒤를 깎으면 판별기가 무엇으로 정했는지를
// 통째로 잃는다 — 뒤가 다수여야 한다. 반대로 근거는 앞 절반에 58.1%가 있다.
// 뒤에서만 담던 옛 구현은 그 절반 이상을 매번 버렸고, 사용자가 가장 원하는 것이
// 바로 그 "왜" 다.
//
// 40 을 고른 것은 이 분포 때문이지 회수 시뮬레이션 때문이 아니다 — 35 와 40 의 차이는
// 표본(메인 세션 3개, 근거 발화 43개) 안에서 구별되지 않았다. 분포가 말하는 것은
// "뒤가 다수, 앞이 0은 아님" 까지이고, 그 구간에서 반올림한 값이다.
const excerptHeadShare = 40

// excerptLine 은 발췌에 실을 줄 하나와 그것이 대표하는 발화 수다.
//
// turns 를 같이 드는 이유: 반복을 접으면 줄 하나가 발화 여럿이 된다. 생략 표시가
// "몇 발화를 건너뛰었나" 를 말하려면 접힌 수까지 세야 맞다.
type excerptLine struct {
	text  string
	turns int
}

// excerptStats 는 발췌를 만들면서 **잃은 것**이다. 앞의 셋은 ScanResult 로 나간다.
type excerptStats struct {
	Bytes   int // 만들어진 발췌 크기
	Turns   int // 발췌에 실린 발화 수 (접힌 반복을 낱개로 센다)
	Omitted int // 가운데에서 버린 발화 수
	// Clipped 는 **구간 전체**에서 줄 상한에 걸려 잘린 줄 수다. 발췌에 실린 것만
	// 세지 않는다 — 버린 가운데의 잘림까지 세야 "이 구간에 거대한 발화가 있었다"
	// 를 알 수 있고, 그게 옛 구현이 죽던 자리다.
	Clipped int
}

// excerpt 는 구간의 발췌를 준다. 통계가 필요 없는 호출부용 얇은 겉면이다.
func excerpt(turns []transcript.Turn) string {
	s, _ := buildExcerpt(turns)
	return s
}

// buildExcerpt 는 구간의 원문을 **앞뒤 양쪽에서** 모아 maxExcerpt 안에 담는다.
//
// 옛 구현은 뒤에서만 담았다. 근거가 되는 관찰은 "결정은 구간 끝쪽에서 내려진다" 로
// 맞는 말이었는데, 거기서 **결론과 근거를 같은 것으로 다뤘다.** 실제로는 갈린다 —
// 결론은 뒤에, 근거와 기각된 대안은 앞에 있다(excerptHeadShare 주석의 십분위 표).
// 그래서 뒤만 담으면 판별기는 "결론 같은 것" 만 보고 왜 그렇게 정했는지는 못 본다.
// 그 상태로 만들어진 노트의 근거 절은 비거나(프롬프트가 시키는 대로) 지어내진다.
//
// **가운데를 줄이고 생략 표시를 남긴다.** 표시가 없으면 판별기는 앞줄과 뒷줄이
// 잇달아 나온 것으로 읽는다 — 없는 인과를 만들어 낸다. "여기 뭔가 더 있었다" 를
// 알려 주면 최소한 근거를 지어내지 않을 근거가 생긴다.
//
// 생략 표시는 한국어로 고정한다. 발췌의 다른 표지("사용자: "·"에이전트: "·"· "·
// "(×3)")가 전부 한국어 고정이고 judge.go 의 프롬프트가 그것들을 한국어로 설명하기
// 때문이다. 여기만 이중화하면 프롬프트와 어긋난다 — i18n 은 그 표지들을 통째로
// 손볼 때 같이 갈 자리다.
//
// **도구 활동도 싣는다.** 되돌리기 어려운 선택은 산문이 아니라 편집과 명령으로 남는
// 경우가 많다 — "저장 엔진을 바꾼다" 는 문장이 아니라 파일 편집이다. 실측으로
// 트랜스크립트 바이트의 67.6%가 도구 활동인데 전부 버리고 있었고, 판별기는 산문만
// 보고 "기록할 결정인가" 를 판정하고 있었다.
//
// 결과 본문은 담지 않는다(파서가 tool_result 를 아예 안 읽는다) — 이 세션만 840KB 라
// 발췌가 터진다. 무엇을 했는지는 도구 이름과 대상만으로 전해진다.
//
// **실 트랜스크립트 3개로 옛 것과 새 것을 나란히 돌린 결과:**
//
//	발화 119 → 옛 65줄(6098B)  · 새 119발화 전부(11636B, 생략 0)
//	발화 350 → 옛 44줄(6046B)  · 새 157발화(24001B, 193발화 생략 표시)
//	발화 218 → 옛 41줄(6009B)  · 새 122발화(22795B, 96발화 생략 표시)
//
// 첫 줄이 이 변경을 시킨 그 세션이다. 사용자가 **"내가 ai랑 얘기하는 모든것들을
// 전부 다 기록했으면 좋겠거든 … 왜 그런 선택을 했었는지, 근거가 뭔지, 번복했었다면
// 이유가 뭔지"** 라고 쓴 문장이 세션 앞머리에 있는데, 옛 발췌에는 **없고** 새 발췌에는
// 있다. 요구 자체가 발췌 밖에 있었다.
func buildExcerpt(turns []transcript.Turn) (string, excerptStats) {
	var st excerptStats
	lines := renderExcerpt(turns, &st)
	if len(lines) == 0 {
		return "", st
	}

	// ① 뒤부터 제 몫(100-excerptHeadShare)까지 채운다 — 결론이 거기 있다.
	//
	// 뒤를 **먼저** 채우는 것이 중요하다. 앞을 먼저 채우면 짧은 구간에서 앞이 예산을
	// 다 먹고 결론이 밀려난다. 결론 없는 발췌는 기록할 것이 없는 발췌다.
	total := 0
	tailBudget := maxExcerpt * (100 - excerptHeadShare) / 100
	hi := len(lines) - 1
	for hi >= 0 && total+len(lines[hi].text) <= tailBudget {
		total += len(lines[hi].text)
		hi--
	}

	// ② 남은 예산으로 앞을 채운다 — 근거와 기각된 대안이 거기 있다.
	lo := 0
	for lo <= hi && total+len(lines[lo].text) <= maxExcerpt {
		total += len(lines[lo].text)
		lo++
	}

	// ③ 그래도 예산이 남으면 뒤가 더 먹는다. 앞이 거대한 줄에 막혀 멈춘 경우인데,
	//    예산을 남긴 채 버리는 것보다 뒤를 더 보여 주는 쪽이 낫다.
	for hi >= lo && total+len(lines[hi].text) <= maxExcerpt {
		total += len(lines[hi].text)
		hi--
	}

	// lines[lo:hi+1] 이 버린 가운데다. 위 세 고리가 lo <= hi+1 을 지킨다.
	omitted := 0
	for _, l := range lines[lo : hi+1] {
		omitted += l.turns
	}

	parts := make([]string, 0, lo+(len(lines)-hi-1)+1)
	for _, l := range lines[:lo] {
		parts = append(parts, l.text)
		st.Turns += l.turns
	}
	if omitted > 0 {
		parts = append(parts, fmt.Sprintf("… (%d 발화 생략) …", omitted))
	}
	for _, l := range lines[hi+1:] {
		parts = append(parts, l.text)
		st.Turns += l.turns
	}

	out := strings.Join(parts, "\n\n")
	st.Bytes, st.Omitted = len(out), omitted
	return out, st
}

// renderExcerpt 는 발화를 **시간순으로** 줄로 옮긴다. 상한은 여기서 보지 않는다.
//
// 예산 배분보다 **먼저** 접는 것이 요점이다. 옛 구현은 예산을 세면서 접었는데,
// 그러면 예산 밖으로 밀려난 반복은 접히지도 세어지지도 않는다 — 같은 명령을 20번
// 돌린 구간이 예산 안에서는 (×3) 으로 보인다.
func renderExcerpt(turns []transcript.Turn, st *excerptStats) []excerptLine {
	var lines []excerptLine
	for _, t := range turns {
		txt := strings.TrimSpace(t.Text)
		if txt == "" {
			continue
		}
		var line string
		switch t.Kind {
		case transcript.KindUser:
			line = "사용자: " + txt
		case transcript.KindTool:
			// **한 일**이다. 발화와 다른 표지를 붙여 판별기가 구분하게 한다 —
			// "Edit foo.go" 를 에이전트가 한 말로 읽으면 안 된다.
			line = "· " + txt
		default:
			line = "에이전트: " + txt
		}
		if clipped, cut := clipLine(line); cut {
			line = clipped
			st.Clipped++
		}
		// **같은 줄이 잇달아 오면 접는다.** 도구 활동은 반복이 흔하다 —
		// 같은 테스트를 세 번 돌리는 것이 발췌 세 줄을 먹으면 안 된다.
		if n := len(lines); n > 0 && sameActivity(lines[n-1].text, line) {
			lines[n-1].text = bumpRepeat(lines[n-1].text)
			lines[n-1].turns++
			continue
		}
		lines = append(lines, excerptLine{text: line, turns: 1})
	}
	return lines
}

// clipLine 은 줄 하나를 maxExcerptLine 까지 줄인다. **가운데를 뺀다.**
//
// 앞만 남기지 않는 이유는 구간을 앞뒤로 나눠 담는 이유와 같다 — 긴 발화 하나
// 안에서도 문제 서술이 앞에, 결론이 끝에 오는 일이 흔하다. 붙여넣은 로그라면
// 끝이 결과다.
func clipLine(s string) (string, bool) {
	if len(s) <= maxExcerptLine {
		return s, false
	}
	// 2:1 로 앞을 더 남긴다. 앞에는 누가 무엇을 말하는지가 있고, 뒤는 결말만 있으면
	// 된다. 표지("사용자: ")가 잘리면 그 줄이 누구 것인지 사라진다.
	head := cutHead(s, maxExcerptLine*2/3)
	tail := cutTail(s, maxExcerptLine/3)
	return head + "…(중략)…" + tail, true
}

// cutHead·cutTail 은 바이트 상한까지 자르되 **룬 경계를 지킨다.** 한글 한 글자가
// 3바이트라 그냥 자르면 깨진 바이트가 남고, 그 발췌는 JSON 으로도 프롬프트로도
// 지저분해진다.
func cutHead(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func cutTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}

// repeatSuffix 는 접은 반복 횟수를 적는 꼬리다.
var repeatSuffix = regexp.MustCompile(` \(×(\d+)\)$`)

// sameActivity 는 두 줄이 같은 활동인지 본다 (반복 표시는 무시).
func sameActivity(prev, line string) bool {
	return repeatSuffix.ReplaceAllString(prev, "") == line
}

// bumpRepeat 는 반복 횟수를 하나 올린다.
func bumpRepeat(s string) string {
	if m := repeatSuffix.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return repeatSuffix.ReplaceAllString(s, fmt.Sprintf(" (×%d)", n+1))
	}
	return s + " (×2)"
}
