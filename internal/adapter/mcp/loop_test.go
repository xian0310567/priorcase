package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/daemon"
	"github.com/xian0310567/priorcase/internal/testutil"
)

// 스펙 §12 컷오버 게이트 4번:
//
//	"데몬이 놓친 결정을 pending 으로 잡고, 다음 세션 instructions 에 노출"
//
// 이 테스트가 그 한 줄이다. 데몬과 MCP 서버는 **다른 프로세스**이고 상태 파일 하나로만
// 만나므로, 각각을 따로 테스트해서는 고리가 닫히는지 알 수 없다. 여기서 한 바퀴를 돈다:
//
//	데몬이 표시 → 다음 세션 instructions 에 노출 → 에이전트가 기록 → 해소 → 사라짐
func TestSafetyNetFullLoop(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects", "proj-a")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	vaultCfg := testutil.VaultConfig(t)
	vaultCfg.Capture = config.Capture{Signals: []string{"결정"}, MinTurns: 6}

	// ── 1. 데몬을 띄운다 ──────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	var once bool
	go func() {
		_ = daemon.Run(ctx, daemon.Options{
			StateDir:       stateDir,
			TranscriptRoot: filepath.Dir(root),
			Config:         vaultCfg,
			Quiesce:        60 * time.Millisecond,
			OnEvent: func(e daemon.Event) {
				if e.Kind == "ready" && !once {
					once = true
					close(ready)
				}
			},
		})
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("데몬이 뜨지 않았다")
	}

	// ── 2. 에이전트가 결정을 내리고도 기록하지 않은 대화가 쌓인다 ──────
	tp := filepath.Join(root, "session.jsonl")
	var b strings.Builder
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&b, `{"type":"assistant","cwd":"/tmp/proj/alpha","sessionId":"S9","timestamp":"2026-08-07T01:00:%02dZ","message":{"role":"assistant","content":[{"type":"text","text":"캐시 계층을 두기로 결정했다"}]}}`+"\n", i)
	}
	if err := os.WriteFile(tp, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	// ── 3. 데몬이 표시할 때까지 기다린다 ──────────────────────────────
	var pend []daemon.Pending
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		if pend, err = daemon.ReadPending(stateDir); err == nil && len(pend) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(pend) != 1 {
		t.Fatalf("데몬이 표시한 구간 %d건, 1건이어야 한다 — 안전망이 놓쳤다", len(pend))
	}
	if pend[0].Domain != "alpha" {
		t.Errorf("Domain = %q, want alpha", pend[0].Domain)
	}

	// ── 4. 다음 세션이 열린다. 진입하자마자 보여야 한다 ────────────────
	cs := connectWithState(t, vaultCfg, stateDir)
	ins := cs.InitializeResult().Instructions
	if !strings.Contains(ins, "미확인 구간이 1건") {
		t.Fatalf("세션 진입에 미확인 구간이 없다 — 에이전트가 알 방법이 없다:\n%s", ins)
	}

	// ── 5. 에이전트가 목록을 보고 ────────────────────────────────────
	list := text(t, call(t, cs, "priorcase_pending", map[string]any{}))
	id := pend[0].ID()
	if !strings.Contains(list, id) {
		t.Fatalf("목록에 id 가 없다 — 지울 수단이 없다:\n%s", list)
	}

	// ── 6. 실제 결정이므로 기록한다 ──────────────────────────────────
	capOut := text(t, call(t, cs, "priorcase_capture", map[string]any{
		"domain": "alpha", "slug": "캐시계층", "date": "2026-08-07",
		"summary": "저장 엔진 앞에 캐시 계층을 둔다",
		"body":    "## 결정\n캐시를 둔다.\n",
	}))
	if !strings.Contains(capOut, "기록됨") {
		t.Fatalf("기록에 실패했다:\n%s", capOut)
	}
	notePath := filepath.Join(vaultCfg.Vault, "alpha", "decisions", "alpha-결정-캐시계층-2026-08-07.md")
	if _, err := os.Stat(notePath); err != nil {
		t.Fatalf("결정 노트가 디스크에 없다: %v", err)
	}

	// ── 7. 확인이 끝났으니 지운다 ────────────────────────────────────
	if r := call(t, cs, "priorcase_pending", map[string]any{"resolve": id}); r.IsError {
		t.Fatalf("해소 실패: %s", text(t, r))
	}
	left, err := daemon.ReadPending(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("해소 후 %d건 남았다 — 다음 세션에도 또 뜬다", len(left))
	}

	// ── 8. 새 세션에는 더 이상 안 뜬다 ───────────────────────────────
	cs2 := connectWithState(t, vaultCfg, stateDir)
	if strings.Contains(cs2.InitializeResult().Instructions, "미확인") {
		t.Error("해소했는데 다음 세션에 또 떴다")
	}
}
