package daemon

import (
	"fmt"
	"github.com/xian0310567/priorcase/internal/transcript"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// note 는 픽스처 볼트에 결정 노트 하나를 심는다. session 이 비면 세션 대조에 안 걸린다.
func note(t *testing.T, vc *config.Config, slug, date, session string) {
	t.Helper()
	p := filepath.Join(vc.Vault, "alpha", "decisions",
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

// ★★ 결정 노트 하나는 **구간 하나**를 면제한다. 세션 전체가 아니다.
//
// 옛 구현은 그 세션 id 를 단 노트가 하나라도 있으면 true 를 돌려줬다. 그래서 세션
// 첫머리에서 한 번 기록하면 그 뒤로 무엇을 놓쳐도 pending 이 안 생기고, pending 이
// 없으면 ②넛지와 ③판별기 승격이 **둘 다** 첫 줄에서 반환한다. 컷오버 1일차에
// 이 세션의 노트 11건이 정확히 그 상태를 만들어 판별기가 한 번도 못 돌았다.
func TestSuppressionIsConsumedNotPermanent(t *testing.T) {
	vc := creditCfg(t)
	l := store.NewLayout(vc)
	note(t, vc, "세션기록", "2026-01-01", "S1") // 날짜는 대화(08-07)와 다르다 — 세션 축만 본다

	tp := filepath.Join(t.TempDir(), "s.jsonl")
	s := newStore(t)

	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)
	first, err := Scan(s, vc, l, tp, false)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Recorded || first.Flagged {
		t.Fatalf("첫 구간은 그 노트가 가려 줘야 한다: recorded=%v flagged=%v", first.Recorded, first.Flagged)
	}

	// 노트는 그대로. 대화만 8발화 더 이어졌다 — 이 구간은 아무도 안 봤다.
	writeLines(t, tp, turns(t, 8, "또 다른 결정을 했다", "/tmp/proj/alpha")...)
	second, err := Scan(s, vc, l, tp, false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Recorded {
		t.Error("면제가 소모되지 않았다 — 노트 하나가 세션 전체를 영구히 가리고 있다")
	}
	if !second.Flagged {
		t.Error("새 구간을 표시하지 않았다 — 판별기가 볼 것이 없어진다")
	}
}

// 새 노트가 생기면 면제를 새로 산다. 부지런한 에이전트는 계속 조용하다.
func TestNewNoteBuysNewSuppression(t *testing.T) {
	vc := creditCfg(t)
	l := store.NewLayout(vc)
	note(t, vc, "첫기록", "2026-01-01", "S1")

	tp := filepath.Join(t.TempDir(), "s.jsonl")
	s := newStore(t)

	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)
	if r, err := Scan(s, vc, l, tp, false); err != nil || !r.Recorded {
		t.Fatalf("첫 구간 면제 실패: %+v %v", r, err)
	}

	note(t, vc, "둘째기록", "2026-01-02", "S1") // 에이전트가 또 기록했다
	writeLines(t, tp, turns(t, 8, "또 다른 결정을 했다", "/tmp/proj/alpha")...)
	r, err := Scan(s, vc, l, tp, false)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Recorded {
		t.Error("노트가 새로 생겼는데 면제하지 않았다 — 제 할 일을 한 세션까지 표시하면 무시를 학습시킨다")
	}
	if r.Flagged {
		t.Error("면제했는데 표시까지 했다")
	}
}

// 날짜+도메인 축도 소모성이어야 한다. 세션 id 를 안 넘기는 경로가 여기로 온다.
func TestDayDomainSuppressionIsAlsoConsumed(t *testing.T) {
	vc := creditCfg(t)
	l := store.NewLayout(vc)

	tp := filepath.Join(t.TempDir(), "s.jsonl")
	s := newStore(t)
	day := func(n int, text string) []string {
		var out []string
		for i := 0; i < n; i++ {
			out = append(out, fmt.Sprintf(
				`{"type":"assistant","cwd":"/tmp/proj/alpha","sessionId":"","timestamp":"2026-08-01T01:00:%02dZ","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`+"\n",
				i, text))
		}
		return out
	}
	// 픽스처 볼트에는 alpha 의 2026-08-01 결정이 이미 있다.
	writeLines(t, tp, day(8, "여기서 결정했다")...)
	if r, err := Scan(s, vc, l, tp, false); err != nil || !r.Recorded {
		t.Fatalf("같은 날 같은 도메인 노트가 가려 줘야 한다: %+v %v", r, err)
	}

	writeLines(t, tp, day(8, "또 다른 결정을 했다")...)
	r, err := Scan(s, vc, l, tp, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Recorded {
		t.Error("날짜 축 면제가 소모되지 않았다 — 그날 노트 하나가 그날 전체를 가린다")
	}
}

// Advance 가 체크포인트를 통째로 갈아치우면 Credited 가 0 으로 돌아가 면제가
// 다시 무한해진다. 조용히 되돌아가는 종류의 회귀라 따로 못 박는다.
func TestAdvancePreservesCredit(t *testing.T) {
	s := newStore(t)
	if _, err := s.Credit("/p", 3, map[string]int{"2026-08-08": 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance("/p", 100, 100); err != nil {
		t.Fatal(err)
	}
	again, err := s.Credit("/p", 3, map[string]int{"2026-08-08": 2})
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

// 읽을 것이 없어도 훑은 흔적은 남아야 한다. 이게 없으면 "돌고 있는데 할 일이 없다" 와
// "한 번도 안 돌았다" 가 doctor 에게 똑같이 보인다 — 컷오버 1일차의 오진이 그것이다.
func TestScanLeavesTraceEvenWhenNothingToRead(t *testing.T) {
	vc := creditCfg(t)
	l := store.NewLayout(vc)
	tp := filepath.Join(t.TempDir(), "s.jsonl")
	s := newStore(t)
	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)

	if _, err := Scan(s, vc, l, tp, false); err != nil {
		t.Fatal(err)
	}
	firstTrace := s.LastScan()
	if firstTrace.IsZero() {
		t.Fatal("첫 스캔의 흔적이 없다")
	}

	time.Sleep(2 * time.Millisecond)
	// 새 내용이 없다 — from >= size 로 즉시 반환하는 가장 흔한 경로다.
	r, err := Scan(s, vc, l, tp, false)
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

// ★ 나중에 면제가 다시 걸려도 **이미 표시된 구간을 지우지는 않는다.**
//
// 날짜 축은 구간이 걸친 날짜로 세므로, 세션이 길어져 날짜 범위가 넓어지면 그동안
// 안 세던 노트를 집어와 크레딧을 또 산다. 실 트랜스크립트로 재보니 실제로 그랬다
// (1구간 억제 → 2구간 표시 → 3구간 다시 억제). 그 자체는 옳다 — 그 날짜에 노트가
// 있다는 것은 그날 기록이 있었다는 증거다. 다만 **앞서 표시한 것이 그것 때문에
// 사라지면** 안전망이 뒤늦게 자기 일을 취소하는 꼴이 된다.
func TestLaterSuppressionDoesNotEraseEarlierFlag(t *testing.T) {
	vc := creditCfg(t)
	l := store.NewLayout(vc)
	note(t, vc, "첫기록", "2026-01-01", "S1")

	tp := filepath.Join(t.TempDir(), "s.jsonl")
	s := newStore(t)

	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)
	if r, _ := Scan(s, vc, l, tp, false); !r.Recorded {
		t.Fatal("1구간은 면제돼야 한다")
	}

	writeLines(t, tp, turns(t, 8, "또 다른 결정을 했다", "/tmp/proj/alpha")...)
	if r, _ := Scan(s, vc, l, tp, false); !r.Flagged {
		t.Fatal("2구간은 표시돼야 한다")
	}

	// 새 노트가 생겨 3구간이 다시 면제된다.
	note(t, vc, "둘째기록", "2026-01-02", "S1")
	writeLines(t, tp, turns(t, 8, "세 번째 결정을 했다", "/tmp/proj/alpha")...)
	if r, _ := Scan(s, vc, l, tp, false); !r.Recorded {
		t.Fatal("3구간은 새 노트로 면제돼야 한다")
	}

	items, err := ReadPending(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Errorf("표시가 %d건 — 2구간의 표시가 3구간의 면제에 지워졌다", len(items))
	}
}

// ★★ 리뷰가 잡은 정반대 고장: 바쁜 날을 지나면 **영영 면제되지 않는다.**
//
// 처음 구현은 크레딧을 개수 하나로 두고 `Credited = count` 로 최댓값 래칫을 걸었다.
// 그런데 count 는 *그 구간이 걸친 날짜*로 걸러진다 — 고점을 넓은 창으로 찍고 비교를
// 좁은 창으로 하는 구조라, 8-01 에 노트 6건이 있는 날을 한 번 지나면 그 뒤로는 매일
// 성실히 기록해도 count 가 6 을 못 넘어 면제가 안 걸린다. 자정 넘김 · claude --continue ·
// 데몬 정지 뒤 backfill · 볼트 아카이브가 전부 이 상태를 만든다.
//
// 안전망이 소음이 되면 에이전트가 무시하는 법을 배운다 — 이 프로젝트가 죄목으로 드는 상태다.
func TestBusyDayDoesNotPoisonLaterDays(t *testing.T) {
	vc := creditCfg(t)
	l := store.NewLayout(vc)
	for i := 0; i < 6; i++ {
		note(t, vc, fmt.Sprintf("남이쓴%d", i), "2026-08-01", "")
	}

	tp := filepath.Join(t.TempDir(), "s.jsonl")
	s := newStore(t)
	seg := func(day string) []string {
		var out []string
		for i := 0; i < 8; i++ {
			out = append(out, fmt.Sprintf(
				`{"type":"assistant","cwd":"/tmp/proj/alpha","sessionId":"S1","timestamp":"%sT01:00:%02dZ","message":{"role":"assistant","content":[{"type":"text","text":"여기서 결정했다"}]}}`+"\n",
				day, i))
		}
		return out
	}

	writeLines(t, tp, seg("2026-08-01")...)
	if r, _ := Scan(s, vc, l, tp, false); !r.Recorded {
		t.Fatal("바쁜 날은 면제돼야 한다")
	}

	// 이튿날. 에이전트가 이 세션으로 결정을 제대로 남겼다.
	note(t, vc, "제대로기록", "2026-08-02", "S1")
	writeLines(t, tp, seg("2026-08-02")...)
	r2, err := Scan(s, vc, l, tp, false)
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Recorded {
		t.Error("성실히 기록했는데 면제가 안 걸렸다 — 넓은 창의 고점이 좁은 창을 막고 있다")
	}

	// 사흘째. 또 기록했다.
	note(t, vc, "또기록", "2026-08-03", "S1")
	writeLines(t, tp, seg("2026-08-03")...)
	if r, _ := Scan(s, vc, l, tp, false); !r.Recorded {
		t.Error("사흘째도 면제가 안 걸렸다 — 래칫이 살아 있다")
	}
}

// 세션 축과 날짜 축이 서로를 오염시키면 안 된다. 같은 날 **다른 세션**이 남긴 노트가
// 이쪽 세션의 고점을 밀어 올려 이쪽을 영구히 막던 경로다.
func TestOtherSessionsNotesDoNotPoisonThisSession(t *testing.T) {
	vc := creditCfg(t)
	l := store.NewLayout(vc)
	for i := 0; i < 5; i++ {
		note(t, vc, fmt.Sprintf("다른세션%d", i), "2026-08-07", "다른세션ID")
	}

	tp := filepath.Join(t.TempDir(), "s.jsonl")
	s := newStore(t)
	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...) // 2026-08-07, S1
	if r, _ := Scan(s, vc, l, tp, false); !r.Recorded {
		t.Fatal("같은 날 노트가 있으니 첫 구간은 면제된다")
	}

	// 이 세션이 자기 결정을 기록한다. 날짜 축의 고점(5)에 막히면 안 된다.
	note(t, vc, "내가기록", "2026-08-07", "S1")
	writeLines(t, tp, turns(t, 8, "또 여기서 결정했다", "/tmp/proj/alpha")...)
	r, err := Scan(s, vc, l, tp, false)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Recorded {
		t.Error("내 세션 노트가 다른 세션의 고점에 막혔다 — 축이 섞였다")
	}
}

// ★ 안전망은 **자기 출력으로 자기를 억제하면 안 된다.**
//
// 승격이 만든 노트는 그 구간의 스캔 뒤에 생긴다. 다음 스캔이 그것을 "새로 생겼다" 로
// 세면, 아직 아무도 안 본 다음 구간이 면제된다 — 자동 경로에서만 깨지는 off-by-one 이다.
// 노트에 출처 필드가 없어 사후 구분이 불가능하므로 만든 자리에서 소모시켜야 한다.
func TestAutoPromotedNoteDoesNotBuyFutureExemption(t *testing.T) {
	vc := creditCfg(t)
	l := store.NewLayout(vc)
	tp := filepath.Join(t.TempDir(), "s.jsonl")
	s := newStore(t)

	writeLines(t, tp, turns(t, 8, "여기서 결정했다", "/tmp/proj/alpha")...)
	if r, _ := Scan(s, vc, l, tp, false); !r.Flagged {
		t.Fatal("1구간은 표시돼야 한다 (볼트에 가려 줄 노트가 없다)")
	}

	// 승격이 노트를 만들고, 그 자리에서 크레딧을 소모시킨다.
	note(t, vc, "판별기가만듦", "2026-08-07", "S1")
	if err := s.CreditNote(tp, "2026-08-07", "S1"); err != nil {
		t.Fatal(err)
	}

	writeLines(t, tp, turns(t, 8, "또 다른 결정을 했다", "/tmp/proj/alpha")...)
	r, err := Scan(s, vc, l, tp, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Recorded {
		t.Error("승격이 만든 노트가 다음 구간을 면제했다 — 안전망이 자기 출력으로 자기를 껐다")
	}
	if !r.Flagged {
		t.Error("2구간을 표시하지 않았다")
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

	if _, err := Scan(s, vc, l, tp, false); err == nil {
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
