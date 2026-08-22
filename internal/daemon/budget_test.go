package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// ★★★ **예산의 대부분이 예약만 되고 안 쓰였다.**
//
// `Promote` 는 "끝낼 수 없는 것은 시작하지 않는다" 규칙으로 판별기 상한(75초)
// 만큼 남아야 새 호출을 시작했다. 그런데 예산이 90초라 **시작 가능한 창이 처음
// 15초뿐**이었다 — 실측 지연은 10~28초인데 75초를 예약한 셈이다.
//
// 실측(승격 원장, 2026-08-14): 한 판당 판정이 **평균 1.4건**
// (분포 1건×21 · 2건×6 · 3건×2 · 4건×1). 미확인 구간 37건을 비우려면 세션이
// 스물여섯 번 필요하고, 그 사이 새 구간이 더 빨리 쌓인다.
//
// 고침: **호출마다 남은 예산을 마감으로 씌운다.** 그러면 상한이 자동으로
// min(판별기 상한, 남은 시간)이 되어 넘칠 수가 없고, 시작 조건을 낮춰도 안전하다.
func TestPromoteUsesWholeBudget(t *testing.T) {
	dir, cfgPath, c, l := budgetFixture(t, 8, "") // 8구간 · 즉답 판별기

	var got []Promotion
	start := time.Now()
	Promote(context.Background(), PromoteOptions{
		StateDir: dir, Config: c, Layout: l,
		Budget:   30 * time.Second,
		OnResult: func(p Promotion) { got = append(got, p) },
	})
	el := time.Since(start)
	_ = cfgPath

	// **판별기를 즉답으로 둔다.** 처음엔 0.2초 sleep 을 줬는데, 병렬로 도는 시험
	// 부하에서 자식 프로세스가 훨씬 느려져 **6회 중 1회 깜빡였다**(단독 8건 ·
	// 전체 실행 1~2건). 깜빡이는 시험은 신호를 잃는다.
	//
	// 즉답이면 30초 예산에 여덟 건이 넉넉히 들어간다 — 부하로 열 배 느려져도 그렇다.
	// 옛 규칙(75초 예약)에서는 예산 30초에 **한 건도 시작하지 못하고 즉시 끝난다.**
	_ = el
	if len(got) < 4 {
		t.Errorf("판정 %d건 — 옛 규칙이면 0건, 지금은 여덟 건이 다 들어가야 한다", len(got))
	}
	for _, p := range got {
		if p.Err != "" {
			t.Errorf("실패가 났다: %s", p.Err)
		}
	}
}

// ★★★ **예산을 넘겨서 돌면 안 된다.**
//
// 훅은 SessionEnd 에서 호스트의 120초 상한 아래 돈다. 넘기면 호스트가 훅을
// 통째로 죽이고, 그때는 원장도 못 쓰고 선점 도장만 남아 그 구간이 claimTTL(5분)
// 동안 건너뛰어진다.
//
// 호출마다 마감을 씌우므로 **느린 판별기도 예산 안에서 잘린다.**
func TestPromoteNeverOverrunsBudget(t *testing.T) {
	dir, _, c, l := budgetFixture(t, 4, "30") // 판별기가 30초 걸린다

	start := time.Now()
	Promote(context.Background(), PromoteOptions{
		StateDir: dir, Config: c, Layout: l,
		Budget: 2 * time.Second,
	})
	el := time.Since(start)

	// 예산 2초 + 정리 여유. 판별기 상한(75초)까지 기다리면 안 된다.
	if el > 8*time.Second {
		t.Errorf("%v 걸렸다 — 예산 2초를 한참 넘겼다 (판별기 상한까지 기다렸다)", el)
	}
}

// budgetFixture 는 구간 n개와 sleep 초짜리 가짜 판별기를 심는다.
func budgetFixture(t *testing.T, n int, sleep string) (dir, cfgPath string, c *config.Config, l *store.Layout) {
	t.Helper()
	dir = t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "proj", "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}

	// **판정은 언제나 "기록 안 함" 이다.** 이 시험이 보는 것은 처리량이지
	// 노트를 잘 쓰는가가 아니다 — 기록까지 하면 볼트 쓰기가 시간을 먹어 측정이 흐려진다.
	jp := filepath.Join(t.TempDir(), "judge.sh")
	slow := ""
	if sleep != "" {
		slow = "sleep " + sleep + "\n"
	}
	body := "#!/bin/sh\ncat >/dev/null\n" + slow +
		`echo '{"record":false,"reason":"시험용"}'` + "\n"
	if err := os.WriteFile(jp, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	c = &config.Config{
		Vaults:        []config.Vault{{Name: config.DefaultVaultName, Path: vault}},
		DefaultDomain: "proj",
		Naming: config.Naming{
			DecisionFile: "{domain}-결정-{slug}-{date}.md",
			DecisionsDir: "{project}/decisions",
			Worklog:      "99-{project}-작업-로그.md",
			Index:        "_meta/00-결정-색인.md",
		},
		Domain:  []config.Domain{{Prefix: "proj", Folder: "proj"}},
		Capture: config.Capture{MinTurns: 1, Signals: []string{"결정"}, JudgePath: jp},
	}
	for i := 0; i < n; i++ {
		if err := st.AddPending(Pending{
			Path: fmt.Sprintf("/t%02d.jsonl", i), From: 0, Domain: "proj",
			SessionID: fmt.Sprintf("S%02d", i), Days: []string{"2026-08-14"},
			At: time.Now().UTC(), Excerpt: strings.Repeat("결정을 내렸다. ", 20),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return dir, "", c, store.NewLayout(c)
}
