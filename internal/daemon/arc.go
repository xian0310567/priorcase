package daemon

// 아크 판정 — 세션 하나의 **흐름 전체**를 보고 결정 노트를 만든다.
//
// # 왜 pending 큐로는 안 되나
//
// [[priorcase-결정-기록계층-2단-worklog신설-판정시점-이원화-2026-08-19]] 이 "세션 끝에
// 대화 아크 전체를 다시 보고 결정 노트를 쓴다" 로 정했는데, 처음 구현은 judge.Scope
// 배선만 하고 **대상을 그대로 pending 큐로 뒀다.** 그래서 판정할 것이 없었다.
//
// 인과가 코드로 닫힌다.
//
//  1. 데몬이 대화 도중 계속 Promote(ScopeMid) 를 부른다 (daemon.go 의 drain).
//  2. 도중 승격은 작업 로그를 쓰고 그 구간을 **해소한다** (promote.go 의 ResolvePending).
//  3. 세션 끝 훅의 Promote(ScopeEnd) 는 ReadPending 으로만 대상을 찾는다 — 그때 큐는 비었다.
//
// 실측: 최근 7일 자동 기록 63건이 **전부 작업 로그**이고 결정 노트 0건. 판별기는
// 136번 돌았는데 결정 등급이 한 번도 안 나왔다 — 나올 자리가 없었다.
//
// # 그래서 축을 따로 든다
//
// Checkpoint.Decided 는 "아크를 어디까지 결정 판정했나" 다. Offset(훑기)과 따로 가고
// 세션이 끝날 때·압축될 때·오래 잠잠할 때만 전진한다. 그 세 자리가 이 파일을 부른다.

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/judge"
	"github.com/xian0310567/priorcase/internal/core/promote"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/core/worklog"
	"github.com/xian0310567/priorcase/internal/transcript"
	"github.com/xian0310567/priorcase/internal/transcript/hosts"
)

// parseFrom 은 transcript 를 from 바이트부터 읽어 발화로 만든다.
//
// Scan 이 같은 일을 인라인으로 하는데, 거기서 빼내지 않고 따로 둔다 — Scan 은 읽은
// 뒤에 체크포인트·크레딧·pending 까지 한 흐름으로 처리하고, 여기는 읽기만 필요하다.
// 공통화하면 그 흐름 전체를 인자로 갈라야 해서 양쪽이 다 읽기 어려워진다.
//
// **어느 호스트의 파일인지 못 고르면 읽지 않는다.** 아무 파서나 대면 발화가 0개로
// 나오는데, 그건 "그 세션에 결정이 없었다" 와 구별되지 않는다.
func parseFrom(path string, from int64, hs []hosts.Resolved) (
	turns []transcript.Turn, meta transcript.Meta, consumed int64, bad int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, meta, 0, 0, fmt.Errorf("transcript 를 열 수 없다 (%s): %w", path, err)
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil, meta, 0, 0, err
	}
	if len(hs) == 0 {
		// **ScanOnce 와 같은 목록을 쓴다.** 거기는 `hosts.ClaudeCode()` 를 넘긴다 —
		// 루트가 없는 호스트 정의라 경로가 호스트 기본 자리 밖이어도 고를 수 있다.
		// 처음에 `hosts.Resolve("")` 로 폴백했더니 기본 루트 밖의 transcript 를
		// 통째로 못 골라서 아크가 **조용히** 건너뛰었다 (훅 테스트 2건이 잡았다).
		hs = hosts.ClaudeCode()
	}
	h := hosts.For(path, hs)
	if h == nil {
		return nil, meta, 0, 0, fmt.Errorf(
			"어느 호스트의 기록인지 모르겠다 (%s) — 지원하는 호스트의 루트 밖이다", path)
	}
	turns, meta, consumed, bad, err = h.Host.Parse(f)
	if err != nil {
		return nil, meta, 0, 0, fmt.Errorf("transcript 파싱 실패 (%s): %w", path, err)
	}
	return turns, meta, consumed, bad, nil
}

// ArcOptions 는 아크 판정 한 번에 필요한 것이다.
type ArcOptions struct {
	StateDir string
	Config   *config.Config
	Layout   *store.Layout
	// Path 는 판정할 transcript 다.
	Path string
	// Author 는 만들어질 결정 노트에 박을 사람이다. 비면 설정·git 신원에서 정한다.
	Author string
	// Hosts 는 파서 선택용이다. 비면 기본 목록을 쓴다 (테스트만 좁혀 준다).
	Hosts []hosts.Resolved
	// Err 는 사람에게 보이는 보고다. nil 이면 버린다.
	Err io.Writer
	// Label 은 보고 줄 앞에 붙는 이름이다.
	Label string
	// OnResult 는 원장에 남기는 것과 같은 기록을 호출자에게도 준다. nil 이면 안 부른다.
	OnResult func(Promotion)
}

// ArcResult 는 아크 판정 하나의 결과다.
type ArcResult struct {
	// Skipped 는 판정을 **안 한** 이유다. 비어 있으면 판정했다.
	Skipped string
	Tier    judge.Tier
	Path    string // 만들어진 노트 또는 덧붙인 작업 로그
	Reason  string
	Turns   int
	Bytes   int
	Omitted int
	Err     error
}

// arcMinTurns 는 아크 판정에 필요한 최소 발화 수다.
//
// 설정의 min_turns 를 그대로 쓴다. 별도 상수를 두면 "왜 저기서만 다르지" 가 되고,
// 세션 끝은 어차피 마지막 기회라 문턱을 더 낮출 이유가 없다 — 발화가 그보다 적으면
// 도중 판정이 이미 작업 로그로 담았다.
func arcMinTurns(c *config.Config) int {
	if n := c.Capture.MinTurns; n > 0 {
		return n
	}
	return 1
}

// PromoteArc 는 transcript 하나의 아직 판정하지 않은 아크를 판정한다.
//
// 판별기가 없으면 아무것도 하지 않는다 — 그때는 표시만 남고 에이전트가 판단한다.
//
// 재귀 차단: 판별기가 띄운 세션에도 훅이 붙는다. PRIORCASE_JUDGE 가 있으면 그
// 세션이므로 즉시 끝낸다.
func PromoteArc(ctx context.Context, o ArcOptions) ArcResult {
	if os.Getenv("PRIORCASE_JUDGE") == "1" {
		return ArcResult{Skipped: "판별기가 띄운 세션이다"}
	}
	if o.Path == "" || o.StateDir == "" {
		return ArcResult{Skipped: "transcript 나 상태 디렉토리를 모른다"}
	}
	// **이 대화를 만든 호스트의 CLI 로 판정한다** (judgepick.go).
	// o.Hosts 를 그대로 넘긴다 — 파서를 고르는 것과 같은 근거로 골라야 둘이
	// 어긋나지 않는다("codex 파서로 읽고 claude 로 판정한다" 가 없어진다).
	j := judgeFor(o.Config, o.Path, o.Hosts)
	if j == nil {
		return ArcResult{Skipped: "판별기가 없다"}
	}

	info, err := os.Stat(o.Path)
	if err != nil {
		return ArcResult{Err: fmt.Errorf("transcript 를 볼 수 없다 (%s): %w", o.Path, err)}
	}
	size := info.Size()

	st := NewStore(o.StateDir)
	if err := st.Load(); err != nil {
		return ArcResult{Err: err}
	}
	from := st.DecidedFrom(o.Path, size)
	if from >= size {
		return ArcResult{Skipped: "새로 결정 판정할 것이 없다"}
	}

	turns, meta, consumed, _, err := parseFrom(o.Path, from, o.Hosts)
	if err != nil {
		// **읽기 실패는 반드시 알린다.** 이게 조용하면 "아크에 결정이 없었다" 와
		// "아크를 아예 못 읽었다" 가 밖에서 같아 보인다 — 실제로 파서 선택을 잘못
		// 폴백해서 그 상태를 만들었고, 아무 줄도 안 나와서 원인을 못 찾았다.
		o.report(fmt.Sprintf("아크를 읽지 못했다: %v", err))
		return ArcResult{Err: err}
	}

	n := 0
	for _, t := range turns {
		if t.Kind.Counts() {
			n++
		}
	}
	// **전진하지 않는다.** 여기서 표식을 밀면 아크가 영원히 임계를 못 채운다 —
	// 3발화 보고 전진, 또 3발화 보고 전진, 매번 3 < 6 이다 (Scan 의 갈래 2와 같은 함정).
	if n < arcMinTurns(o.Config) {
		return ArcResult{Skipped: fmt.Sprintf("발화 %d — 임계 미달", n), Turns: n}
	}

	// 제외 구역은 판정하지 않되 **표식은 전진시킨다.** 안 그러면 그 프로젝트의
	// transcript 를 세션마다 다시 읽는다.
	if meta.Cwd != "" && o.Config.IsExcluded(meta.Cwd) {
		_ = st.MarkDecided(o.Path, from+consumed)
		return ArcResult{Skipped: "제외 구역이다", Turns: n}
	}

	domain := o.Config.DomainForCwd(meta.Cwd)
	if domain == "" {
		// 조용히 넘기면 "안전망이 도는데 아무것도 안 남는" 상태가 된다 — 설정에
		// default_domain 이 없는 새 사용자가 정확히 여기 빠진다.
		o.report("도메인을 알 수 없어 아크를 판정하지 못했다 — 설정에 [[domain]] 이나 default_domain 을 적어라")
		return ArcResult{Skipped: "도메인을 알 수 없다", Turns: n}
	}
	if _, ok := o.Config.FolderFor(domain); !ok {
		o.report(fmt.Sprintf("설정에 없는 도메인이라 아크를 건너뛴다: %q", domain))
		// 설정에 없는 도메인은 판별기를 부르기 전에 걸러낸다 — 부르고 나서 capture 가
		// 거부하면 호출 한 번(실측 10~45초)이 통째로 버려진다.
		return ArcResult{Skipped: fmt.Sprintf("설정에 없는 도메인이다: %q", domain), Turns: n}
	}

	ex, stats := buildExcerpt(turns)
	if ex == "" {
		// 발화는 임계를 넘겼는데 발췌가 비었다 = 고장이다. 정상 경로가 아니다.
		o.report(fmt.Sprintf("⚠️ 발화 %d 인데 발췌가 비었다 — 판별기가 볼 것이 없다", n))
		return ArcResult{Skipped: "발췌가 비었다", Turns: n}
	}

	author := o.Author
	if author == "" {
		author = o.Config.AuthorFor(meta.Cwd)
	}

	// **id 를 pending 과 다른 이름공간에 둔다.** 같은 오프셋의 구간 승격과 원장에서
	// 구별되어야 하고, 작업 로그 중복 검사(worklog.Entry.Source)도 이 값을 쓴다.
	id := fmt.Sprintf("%s@arc:%d", o.Path, from)

	r := promote.One(ctx, j, o.Layout, o.Config, promote.Segment{
		ID:      id,
		Domain:  domain,
		Date:    arcDate(turns),
		Excerpt: ex,
		Session: meta.SessionID,
		Author:  author,
		Scope:   judge.ScopeEnd,
		// 도중 판정이 쌓아 둔 것을 근거로 넘긴다. 발췌 상한에 잘려 나간 앞부분이
		// 여기 남아 있다.
		Worklog: worklog.SessionTitles(o.Layout, domain, meta.SessionID, 20),
	})

	out := ArcResult{
		Tier: r.Tier, Reason: r.Reason, Err: r.Err,
		Turns: n, Bytes: stats.Bytes, Omitted: stats.Omitted,
	}
	if r.Path != "" {
		out.Path = o.Layout.RelPath(r.Path)
	}

	// **중단은 실패가 아니다.** 취소된 컨텍스트에서 판별기는 에러를 내는데, 그걸
	// "판별기 실패" 로 남기면 원장이 거짓말을 한다 — 판별기는 멀쩡했고 우리가 시간을
	// 못 준 것뿐이다. 표식도 전진시키지 않는다.
	if r.Err != nil && ctx.Err() != nil {
		out.Err = nil
		out.Skipped = "중단됐다 — 다음 기회에 다시 본다"
		o.report(out.Skipped)
		return out
	}

	rec := Promotion{
		At: time.Now().UTC(), ID: id, Domain: domain,
		Recorded: r.Recorded, Tier: string(r.Tier), Reason: TrimLedgerText(r.Reason),
	}
	if out.Path != "" {
		rec.Path = out.Path
	}
	if r.Err != nil {
		rec.Err = TrimLedgerText(r.Err.Error())
	} else {
		// 아크는 pending 이 아니라 표식으로 관리되므로 실패가 아니면 이 발췌를 다시
		// 볼 일이 없다. 원장에 남기지 않으면 판별기가 무엇을 보고 썼는지 대조할
		// 방법이 사라진다 — Promotion.Excerpt 를 둔 이유와 같다.
		rec.Excerpt = TrimLedgerText(ex)
	}
	if lerr := AppendPromotion(o.StateDir, rec); lerr != nil {
		o.report(fmt.Sprintf("승격 원장을 쓰지 못했다: %v", lerr))
	}
	if o.OnResult != nil {
		o.OnResult(rec)
	}

	if r.Err != nil {
		// **몇 번이고 실패하는 아크를 영원히 재시도하지 않는다.**
		//
		// 실측: 35MB transcript 의 아크(발췌 24KB·발화 126)가 75초 상한에 killed 되고,
		// 그 한 번이 승격 예산 90초를 통째로 먹어 구간 드레인이 아무것도 못 했다.
		// 표식이 전진하지 않으므로 다음 세션 끝에 같은 아크를 또 넣는다 — 매 세션을
		// 그 한 건이 태운다. [[priorcase-결정-판별기-상한과-포기-카운터-2026-08-12]] 가
		// 구간에 대해 고친 것과 같은 함정이다.
		//
		// 실패마다 되짚기가 반으로 줄어드므로(Store.DecidedFrom) 다음 판은 더 작은
		// 아크를 본다. 큰 아크가 안 되면 작은 아크로라도 남기는 편이 낫다.
		n, ferr := st.ArcFailed(o.Path)
		if ferr != nil {
			o.report(fmt.Sprintf("아크 실패 횟수를 새기지 못했다: %v", ferr))
		}
		switch {
		case n >= maxArcFails:
			// **포기하고 전진시킨다.** 그 아크 하나를 잃는 대신 매 세션이 여기서
			// 타지 않는다. 조용히 넘기지 않는다 — 잃은 것을 사람이 알아야 한다.
			o.report(fmt.Sprintf("아크 판정을 %d번 연속 실패해 포기한다 (%s): %v — "+
				"그 대화의 결정은 자동으로 남지 않는다", n, o.Path, r.Err))
			_ = st.MarkDecided(o.Path, from+consumed)
		default:
			o.report(fmt.Sprintf("아크 판정 실패 (%d/%d): %v — 다음엔 더 작은 아크로 다시 본다",
				n, maxArcFails, r.Err))
		}
		// 덮은 구간은 그대로 둔다 — 구간 드레인이 아직 기회를 가져야 한다.
		return out
	}

	// 성공했으니 실패 횟수를 지운다. 안 지우면 한 번 실패한 파일이 그 뒤로 계속
	// 작은 아크만 보게 된다.
	if cerr := st.ArcSucceeded(o.Path); cerr != nil {
		o.report(fmt.Sprintf("아크 실패 횟수를 지우지 못했다: %v", cerr))
	}

	switch {
	case r.Tier == judge.TierDecision:
		o.report("아크 → 결정 노트 " + out.Path)
	case r.Recorded:
		o.report("아크 → 작업 로그 " + out.Path)
	default:
		// "결정 아님" 도 전진시킨다. 같은 아크를 매 세션 끝마다 다시 물으면 예산을
		// 거기서 다 쓴다.
		o.report(fmt.Sprintf("아크에 남길 것이 없다: %s", r.Reason))
	}
	_ = st.MarkDecided(o.Path, from+consumed)
	resolveCovered(o, st, from, from+consumed)
	return out
}

// resolveCovered 는 아크가 방금 판정한 범위에 든 표시 구간을 해소한다.
//
// **같은 발화를 두 번 판정하지 않기 위해서다.** 아크는 [from, to) 의 발화를 통째로
// 봤고, 그 안의 pending 은 같은 대화의 6발화 창들이다. 그대로 두면 세션 끝에
// 판별기가 두 번 돌고(원장 2건) 같은 주제로 결정 노트 하나와 작업 로그 하나가
// 나란히 생긴다 — 훅 테스트가 실제로 그 상태를 잡았다.
//
// **아크가 실패했을 때는 부르지 않는다** (호출부 참고). 그때는 구간 드레인이
// 그 대화의 마지막 기회다.
//
// 데몬이 대화 도중에 만든 작업 로그 항목은 그대로 남는다 — 이미 쓰인 것을 지우는
// 것이 아니고, 아직 판정 안 된 표시만 거둔다.
func resolveCovered(o ArcOptions, st *Store, from, to int64) {
	n := 0
	for _, p := range st.Pending() {
		if p.Path != o.Path || p.From < from || p.To > to {
			continue
		}
		if err := ResolvePending(o.StateDir, p.ID()); err == nil {
			n++
		}
	}
	if n > 0 {
		o.report(fmt.Sprintf("아크가 덮은 표시 구간 %d건을 거뒀다 — 같은 발화를 두 번 판정하지 않는다", n))
	}
}

// arcDate 는 아크의 날짜다. **마지막 발화의 날짜**를 쓴다.
//
// 아크는 여러 날에 걸칠 수 있다(자정을 넘긴 세션, `claude --continue`). 그때 결정이
// 선 날은 첫 발화가 아니라 마지막에 가깝다 — 앞은 조사이고 뒤가 결론이다.
// 날짜를 못 알면 빈 문자열을 주고, capture 가 오늘로 채운다.
func arcDate(turns []transcript.Turn) string {
	if days := segmentDays(turns); len(days) > 0 {
		return days[len(days)-1]
	}
	return ""
}

func (o ArcOptions) report(msg string) {
	if o.Err == nil {
		return
	}
	if o.Label != "" {
		fmt.Fprintf(o.Err, "%s: %s\n", o.Label, msg)
		return
	}
	fmt.Fprintln(o.Err, msg)
}
