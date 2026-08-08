package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/testutil"
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
	if _, err := s.Credit("/p", 3); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance("/p", 100, 100); err != nil {
		t.Fatal(err)
	}
	again, err := s.Credit("/p", 3)
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
