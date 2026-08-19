package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
	"github.com/xian0310567/priorcase/internal/transcript"
)

// note 는 픽스처 볼트에 결정 노트 하나를 심는다. session 이 비면 세션 대조에 안 걸린다.
func note(t *testing.T, vc *config.Config, slug, date, session string) {
	t.Helper()
	p := filepath.Join(vc.DefaultVaultPath(), "alpha", "decisions",
		fmt.Sprintf("alpha-결정-%s-%s.md", slug, date))
	body := fmt.Sprintf("---\ntype: decision\ndate: %s\ndomain: [alpha]\nsummary: \"x\"\n"+
		"status: active\noutcome: pending\nsupersedes: \"\"\nrelated: []\ntags: []\n"+
		"source_session: %q\n---\n\n## 결정\n\nx\n", date, session)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func creditCfg(t *testing.T) *config.Config {
	vc := testutil.VaultConfig(t)
	vc.Capture = config.Capture{Signals: []string{"결정"}, MinTurns: 6}
	return vc
}

// ★★★ **면제는 기록 경로를 건드리지 않는다.** 이 파일에서 가장 중요한 테스트다.
//
// # 이 테스트가 왜 생겼나
//
// 면제는 "이미 기록한 세션을 두 번 조르지 않는다" 로 태어났다. 그때 pending 은
// 에이전트에게 들이미는 알림일 뿐이었으므로 pending 을 안 만드는 것이 곧 안 조르는
// 것이었다. 그 뒤 판별기가 붙었고(D8/D12), pending 은 **판별기의 유일한 입력**이 됐다 —
// Promote 는 ReadPending 으로만 대상을 찾는다. 로직은 그대로인데 억제 대상이 알림에서
// 기록 자체로 바뀌었다.
//
// 실측: 최근 7일 판정 23건 / 자동 기록 0건, 같은 기간 doctor 의 면제 6회. 면제된
// 구간은 pending 없이 Advance 로 지나가므로 **그 대화는 다시 오지 않는다.** 사람이
// 손으로 기록할수록 자동 경로가 더 눈감았다.
//
// 그래서 이제 면제는 구간을 지우지 않는다. 조용하게(Pending.Quiet) 만들 뿐이다.
func TestCreditNeverSkipsTheRecordPath(t *testing.T) {
	vc := creditCfg(t)
	l := store.NewLayout(vc)
	// 이 세션(S1)으로 기록된 노트가 이미 있다 — 옛 동작이라면 이 구간이 통째로 사라진다.
	note(t, vc, "세션기록", "2026-01-01", "S1")

	tp := filepath.Join(t.TempDir(), "s.jsonl")
	s := newStore(t)

	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)
	r, err := Scan(s, vc, l, tp, true, anyHost(tp))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Advanced {
		t.Fatal("전제가 깨졌다 — 다 본 구간은 전진한다")
	}

	items, err := ReadPending(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("표시가 %d건 — 면제가 기록 경로를 죽였다. "+
			"체크포인트는 이미 전진했으므로 이 대화는 다시 오지 않는다", len(items))
	}
	// 승격이 이걸 그대로 집어 갈 수 있어야 한다 — 발췌가 없으면 판별기에 넘길 것이 없다.
	if items[0].Excerpt == "" {
		t.Error("발췌가 비었다 — 판별기가 볼 것이 없다")
	}
}

// 면제가 여러 번 걸려도 앞서 표시한 구간이 지워지면 안 된다.
//
// 옛 테스트(TestLaterSuppressionDoesNotEraseEarlierFlag)는 "1구간 면제 → 2구간 표시 →
// 3구간 면제" 를 만들어 놓고 표시가 1건 남는지를 봤다. 그 시나리오의 전제(면제된 구간은
// 표시가 없다)가 사라졌으므로 **셋 다 남는지**를 본다. 안전망은 뒤늦게 자기 일을
// 취소하지 않는다.
func TestSuppressionNeverErasesFlaggedSegments(t *testing.T) {
	vc := creditCfg(t)
	l := store.NewLayout(vc)
	note(t, vc, "첫기록", "2026-01-01", "S1")

	tp := filepath.Join(t.TempDir(), "s.jsonl")
	s := newStore(t)

	for i, text := range []string{"여기서 결정했다", "또 다른 결정을 했다", "세 번째 결정을 했다"} {
		if i == 2 {
			note(t, vc, "둘째기록", "2026-01-02", "S1") // 새 노트가 면제를 다시 산다
		}
		writeLines(t, tp, turns(t, 8, text, "/tmp/proj/alpha")...)
		if _, err := Scan(s, vc, l, tp, true, anyHost(tp)); err != nil {
			t.Fatal(err)
		}
	}

	items, err := ReadPending(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Errorf("표시가 %d건, 3건이어야 한다 — 나중 면제가 앞서 표시한 구간을 지웠다", len(items))
	}
}

// ★★ 결정 노트 하나는 **구간 하나**를 조용히 한다. 세션 전체가 아니다.
//
// 옛 구현은 그 세션 id 를 단 노트가 하나라도 있으면 true 를 돌려줬다. 그래서 세션
// 첫머리에서 한 번 기록하면 그 뒤로 무엇을 놓쳐도 영원히 안전망 밖이었다. 컷오버
// 1일차에 이 세션의 노트 11건이 정확히 그 상태를 만들었다.
//
// (옛 판은 Scan 을 거쳐 ScanResult.Recorded 를 봤다. 그 필드는 이제 "표시를 조용히
// 했나" 이고 볼트 대조는 scan.go 가 한다 — 여기서는 크레딧 장부 자체만 못 박는다.)
func TestSuppressionIsConsumedNotPermanent(t *testing.T) {
	s := newStore(t)
	quiet, err := s.CreditQuiet("/p", 1, nil) // 이 세션으로 기록된 노트 1건
	if err != nil {
		t.Fatal(err)
	}
	if !quiet {
		t.Fatal("새 노트가 생겼는데 면제가 안 걸렸다")
	}

	// 노트는 그대로. 대화만 더 이어졌다 — 이 구간은 아직 아무도 안 봤다.
	again, err := s.CreditQuiet("/p", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Error("면제가 소모되지 않았다 — 노트 하나가 세션 전체를 영구히 가리고 있다")
	}
}

// 새 노트가 생기면 면제를 새로 산다. 부지런한 에이전트는 계속 조용하다.
func TestNewNoteBuysNewSuppression(t *testing.T) {
	s := newStore(t)
	if quiet, err := s.CreditQuiet("/p", 1, nil); err != nil || !quiet {
		t.Fatalf("첫 면제 실패: %v %v", quiet, err)
	}
	if quiet, err := s.CreditQuiet("/p", 2, nil); err != nil || !quiet {
		t.Errorf("노트가 새로 생겼는데 면제하지 않았다 (%v %v) — "+
			"제 할 일을 한 세션까지 들이밀면 무시를 학습시킨다", quiet, err)
	}
}

// 날짜+도메인 축도 소모성이어야 한다. 세션 id 를 안 넘기는 경로가 여기로 온다.
func TestDayDomainSuppressionIsAlsoConsumed(t *testing.T) {
	s := newStore(t)
	day := map[string]int{"2026-08-01": 1}
	if quiet, err := s.CreditQuiet("/p", 0, day); err != nil || !quiet {
		t.Fatalf("같은 날 같은 도메인 노트가 가려 줘야 한다: %v %v", quiet, err)
	}
	again, err := s.CreditQuiet("/p", 0, map[string]int{"2026-08-01": 1})
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Error("날짜 축 면제가 소모되지 않았다 — 그날 노트 하나가 그날 전체를 가린다")
	}
}

// Advance 가 체크포인트를 통째로 갈아치우면 Credited 가 0 으로 돌아가 면제가
// 다시 무한해진다. 조용히 되돌아가는 종류의 회귀라 따로 못 박는다.
func TestAdvancePreservesCredit(t *testing.T) {
	s := newStore(t)
	if _, err := s.CreditQuiet("/p", 3, map[string]int{"2026-08-08": 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance("/p", 100, 100); err != nil {
		t.Fatal(err)
	}
	again, err := s.CreditQuiet("/p", 3, map[string]int{"2026-08-08": 2})
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Error("Advance 가 Credited 를 지웠다 — 같은 노트로 또 면제됐다")
	}
	if got := s.Suppressed(); got != 1 {
		t.Errorf("억제 횟수 %d, want 1 — 진단이 억제를 못 센다", got)
	}
}

// ★★ **면제는 표시만 건너뛰게 하고, 기록은 언제나 간다.**
//
// 과도기에는 옛 이름 `Store.Credit` 이 "언제나 false" 껍데기로 남아 있었다 —
// 호출부(scan.go)를 같은 작업에서 못 고쳐서, 기록이 새지 않는 쪽으로 기울여 둔
// 임시 상태였다. 호출부가 CreditQuiet 로 옮긴 뒤 그 껍데기는 지웠다.
//
// 이 테스트는 그 이행이 **되돌아가지 않는지**를 본다: 면제가 걸려도 그 답은
// 표시용일 뿐이고, 장부와 진단 계수는 그대로 굴러야 한다(doctor 가 "조용히 넘김
// N회" 를 이 계수로 낸다).
func TestQuietingConsumesLedgerAndCounts(t *testing.T) {
	s := newStore(t)
	// 그날 노트가 하나 있고 아직 아무것도 소모하지 않았다 — 면제가 걸린다.
	quiet, err := s.CreditQuiet("/p", 1, map[string]int{"2026-08-08": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !quiet {
		t.Fatal("노트가 새로 생겼는데 면제가 안 걸렸다")
	}
	// **소모성이다.** 같은 노트로 두 번 면제되면 크레딧이 무한해지고, 그러면
	// 한 번 기록한 세션이 영원히 조용해진다 — 컷오버 1일차가 그 상태였다.
	if quiet, err := s.CreditQuiet("/p", 1, map[string]int{"2026-08-08": 1}); err != nil || quiet {
		t.Errorf("같은 노트로 또 면제됐다 (%v %v)", quiet, err)
	}
	// 진단이 세어야 한다. doctor 가 이 계수로 "조용히 넘김 N회" 를 낸다 —
	// 안 세면 안전망이 왜 조용한지 밖에서 알 수 없다.
	if got := s.Suppressed(); got != 1 {
		t.Errorf("억제 횟수 %d, want 1 — doctor 가 조용한 이유를 못 말한다", got)
	}
}

// ★★ 리뷰가 잡은 정반대 고장: 바쁜 날을 지나면 **영영 면제되지 않는다.**
//
// 처음 구현은 크레딧을 개수 하나로 두고 `Credited = count` 로 최댓값 래칫을 걸었다.
// 그런데 count 는 *그 구간이 걸친 날짜*로 걸러진다 — 고점을 넓은 창으로 찍고 비교를
// 좁은 창으로 하는 구조라, 노트 6건이 있는 날을 한 번 지나면 그 뒤로는 매일 성실히
// 기록해도 count 가 6 을 못 넘어 면제가 안 걸린다. 자정 넘김 · claude --continue ·
// 데몬 정지 뒤 backfill · 볼트 아카이브가 전부 이 상태를 만든다.
func TestBusyDayDoesNotPoisonLaterDays(t *testing.T) {
	s := newStore(t)
	if quiet, err := s.CreditQuiet("/p", 0, map[string]int{"2026-08-01": 6}); err != nil || !quiet {
		t.Fatalf("바쁜 날은 면제돼야 한다: %v %v", quiet, err)
	}
	// 이튿날. 그날 노트는 1건뿐이다 — 통짜 최댓값 래칫이면 6 을 못 넘어 막힌다.
	if quiet, err := s.CreditQuiet("/p", 0, map[string]int{"2026-08-02": 1}); err != nil || !quiet {
		t.Errorf("성실히 기록했는데 면제가 안 걸렸다 (%v %v) — 넓은 창의 고점이 좁은 창을 막고 있다", quiet, err)
	}
	if quiet, err := s.CreditQuiet("/p", 0, map[string]int{"2026-08-03": 1}); err != nil || !quiet {
		t.Errorf("사흘째도 면제가 안 걸렸다 (%v %v) — 래칫이 살아 있다", quiet, err)
	}
}

// 세션 축과 날짜 축이 서로를 오염시키면 안 된다. 같은 날 **다른 세션**이 남긴 노트가
// 이쪽 세션의 고점을 밀어 올려 이쪽을 영구히 막던 경로다.
func TestOtherSessionsNotesDoNotPoisonThisSession(t *testing.T) {
	s := newStore(t)
	// 같은 날 다른 세션의 노트 5건이 날짜 축을 채운다.
	if quiet, err := s.CreditQuiet("/p", 0, map[string]int{"2026-08-07": 5}); err != nil || !quiet {
		t.Fatalf("같은 날 노트가 있으니 첫 구간은 면제된다: %v %v", quiet, err)
	}
	// 이 세션이 자기 결정을 기록한다. 날짜 축의 고점(5)에 막히면 안 된다.
	quiet, err := s.CreditQuiet("/p", 1, map[string]int{"2026-08-07": 5})
	if err != nil {
		t.Fatal(err)
	}
	if !quiet {
		t.Error("내 세션 노트가 다른 세션의 고점에 막혔다 — 축이 섞였다")
	}
}

// ★ 안전망은 **자기 출력으로 자기를 억제하면 안 된다.**
//
// 승격이 만든 노트는 그 구간의 스캔 뒤에 생긴다. 다음 스캔이 그것을 "새로 생겼다" 로
// 세면, 아직 아무도 안 본 다음 구간이 면제된다 — 자동 경로에서만 깨지는 off-by-one 이다.
// 노트에 출처 필드가 없어 사후 구분이 불가능하므로 만든 자리에서 소모시켜야 한다.
//
// 면제가 표시 전용이 된 뒤에도 유효하다. 막는 것이 '기록 누락' 에서 '알림 누락' 으로
// 가벼워졌을 뿐, 판별기가 "기록 안 함" 으로 본 구간까지 사람 눈에서 사라지는 것은 같다.
func TestAutoPromotedNoteDoesNotBuyFutureExemption(t *testing.T) {
	s := newStore(t)
	// 승격이 노트를 만들고, 그 자리에서 크레딧을 소모시킨다.
	if err := s.CreditNote("/p", "2026-08-07", "S1"); err != nil {
		t.Fatal(err)
	}
	// 다음 스캔이 그 노트를 세어 온다.
	quiet, err := s.CreditQuiet("/p", 1, map[string]int{"2026-08-07": 1})
	if err != nil {
		t.Fatal(err)
	}
	if quiet {
		t.Error("승격이 만든 노트가 다음 구간을 면제했다 — 안전망이 자기 출력으로 자기를 껐다")
	}
}

// 읽을 것이 없어도 훑은 흔적은 남아야 한다. 이게 없으면 "돌고 있는데 할 일이 없다" 와
// "한 번도 안 돌았다" 가 doctor 에게 똑같이 보인다 — 컷오버 1일차의 오진이 그것이다.
func TestScanLeavesTraceEvenWhenNothingToRead(t *testing.T) {
	vc := creditCfg(t)
	l := store.NewLayout(vc)
	tp := filepath.Join(t.TempDir(), "s.jsonl")
	s := newStore(t)
	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)

	if _, err := Scan(s, vc, l, tp, false, anyHost(tp)); err != nil {
		t.Fatal(err)
	}
	firstTrace := s.LastScan()
	if firstTrace.IsZero() {
		t.Fatal("첫 스캔의 흔적이 없다")
	}

	time.Sleep(2 * time.Millisecond)
	// 새 내용이 없다 — from >= size 로 즉시 반환하는 가장 흔한 경로다.
	r, err := Scan(s, vc, l, tp, false, anyHost(tp))
	if err != nil {
		t.Fatal(err)
	}
	if r.Turns != 0 {
		t.Fatalf("읽을 것이 없어야 한다: %+v", r)
	}
	if !s.LastScan().After(firstTrace) {
		t.Error("아무 일도 안 한 스캔이 흔적을 안 남겼다 — 안전망이 도는 증거가 사라진다")
	}
}

// 실패한 스캔은 훑은 흔적을 남기면 안 된다. 매번 실패하는 파일이 doctor 에게
// "방금 훑음" 으로 보이면, 생존 증거로 세운 값이 거짓말을 한다.
//
// (깨진 *줄*은 실패가 아니다 — 파서가 세어서 돌려주고 전진만 막는다. 여기서 말하는
// 실패는 파일을 못 읽는 것 같은 진짜 실패다.)
func TestFailedScanLeavesNoTrace(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 는 권한을 무시한다")
	}
	vc := creditCfg(t)
	l := store.NewLayout(vc)
	tp := filepath.Join(t.TempDir(), "s.jsonl")
	s := newStore(t)
	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)
	if err := os.Chmod(tp, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tp, 0o644) })

	if _, err := Scan(s, vc, l, tp, false, anyHost(tp)); err == nil {
		t.Fatal("읽을 수 없는 파일인데 성공했다")
	}
	if got := s.LastScan(); !got.IsZero() {
		t.Errorf("실패한 스캔이 흔적을 남겼다 (%v) — 생존 증거가 거짓말을 한다", got)
	}
}

// 도구 활동은 반복이 흔하다. 같은 테스트를 세 번 돌린 것이 발췌 세 줄을 먹으면
// 정작 필요한 발화가 예산 밖으로 밀린다.
func TestExcerptFoldsRepeatedActivity(t *testing.T) {
	turns := []transcript.Turn{
		{Kind: transcript.KindUser, Text: "고쳐 줘"},
		{Kind: transcript.KindTool, Text: "Bash go test ./..."},
		{Kind: transcript.KindTool, Text: "Bash go test ./..."},
		{Kind: transcript.KindTool, Text: "Bash go test ./..."},
		{Kind: transcript.KindTool, Text: "Edit main.go"},
		{Kind: transcript.KindAssistant, Text: "고쳤다"},
	}
	got := excerpt(turns)
	if strings.Count(got, "Bash go test") != 1 {
		t.Errorf("반복이 안 접혔다:\n%s", got)
	}
	if !strings.Contains(got, "(×3)") {
		t.Errorf("반복 횟수가 안 보인다 — 세 번 돌린 것과 한 번이 같아 보인다:\n%s", got)
	}
	if !strings.Contains(got, "Edit main.go") || !strings.Contains(got, "사용자: 고쳐 줘") {
		t.Errorf("접다가 다른 줄을 잃었다:\n%s", got)
	}
}

// 도구 활동에는 발화와 다른 표지가 붙어야 한다 — 판별기가 "Edit foo.go" 를
// 에이전트가 한 말로 읽으면 안 된다.
func TestExcerptMarksActivityDistinctly(t *testing.T) {
	got := excerpt([]transcript.Turn{
		{Kind: transcript.KindAssistant, Text: "저장 엔진을 바꾼다"},
		{Kind: transcript.KindTool, Text: "Edit store.go"},
	})
	if !strings.Contains(got, "· Edit store.go") {
		t.Errorf("도구 활동 표지가 없다:\n%s", got)
	}
	if strings.Contains(got, "에이전트: Edit store.go") {
		t.Errorf("도구 활동을 발화로 실었다:\n%s", got)
	}
}
