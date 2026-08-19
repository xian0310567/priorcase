package daemon

// 실제 트랜스크립트 + 실제 판별기로 두 등급이 나오는지 본다.
//
// **일부러 기본 테스트에서 뺀다.** 실제 claude CLI 를 부르므로 느리고(실측 중앙 15초)
// 비결정적이며 로그인 상태에 기댄다. PRIORCASE_LIVE_JUDGE 를 줄 때만 돈다.
//
//	PRIORCASE_LIVE_JUDGE=<transcript.jsonl> go test ./internal/daemon/ -run TestLiveJudge -v

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/judge"
	"github.com/xian0310567/priorcase/internal/transcript/claudecode"
)

func TestLiveJudgeProducesTiers(t *testing.T) {
	path := os.Getenv("PRIORCASE_LIVE_JUDGE")
	if path == "" {
		t.Skip("PRIORCASE_LIVE_JUDGE 가 없다")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	turns, _, _, bad, err := claudecode.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("발화 %d개를 읽었다 (깨진 줄 %d)", len(turns), bad)

	ex, st := buildExcerpt(turns)
	t.Logf("발췌 %dB · 발화 %d (생략 %d · 잘림 %d)", st.Bytes, st.Turns, st.Omitted, st.Clipped)
	if len(ex) == 0 {
		t.Fatal("발췌가 비었다")
	}

	j := judge.Find("", "")
	if j == nil {
		t.Skip("판별기를 찾을 수 없다")
	}

	for _, scope := range []judge.Scope{judge.ScopeMid, judge.ScopeEnd} {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		v, err := j.Decide(ctx, judge.Request{
			Excerpt: ex, Domain: "priorcase",
			Date:  time.Now().Format("2006-01-02"),
			Scope: scope,
		})
		cancel()
		if err != nil {
			t.Fatalf("scope=%s 판별기 실패: %v", scope, err)
		}
		t.Logf("── scope=%s ──\n등급: %s\n이유: %s\nslug: %s\n요약: %s\n본문:\n%s",
			scope, v.Tier, v.Reason, v.Slug, v.Summary, v.Body)

		if v.Tier == judge.TierNone {
			t.Errorf("scope=%s 에서 아무것도 안 남긴다 — 옛 실패가 그대로다: %s", scope, v.Reason)
		}
		if scope == judge.ScopeMid && v.Tier == judge.TierDecision {
			t.Errorf("도중 판정인데 결정 등급이 나왔다 — promote 가 강등해야 하지만 지시문이 새고 있다")
		}
		// 사용자 요구의 핵심: 결론만이 아니라 근거·대안이 본문에 있어야 한다.
		if v.Tier != judge.TierNone && strings.TrimSpace(v.Body) == "" {
			t.Errorf("scope=%s 본문이 비었다 — 근거가 안 남는다", scope)
		}
	}
}
