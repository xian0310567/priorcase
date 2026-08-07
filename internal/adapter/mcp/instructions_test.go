package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/daemon"
	"github.com/xian0310567/casebook/internal/testutil"
)

// instructions 는 최근 결정 덤프가 아니라 행동 계약이다 (스펙 §8). 그래서 검사하는
// 것은 "요약이 실려 있는가" 가 아니라 "언제 무엇을 불러야 하는지가 적혀 있는가" 다.
func TestBuildInstructionsCarriesContract(t *testing.T) {
	c := testutil.VaultConfig(t)
	got, skipped := buildInstructions(store.NewLayout(c), pendingView{})

	if len(skipped) != 0 {
		t.Fatalf("픽스처 볼트에서 건너뛴 노트가 나왔다: %v", skipped)
	}
	for _, want := range []string{"casebook_recall", "casebook_capture", "4건"} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions 에 %q 가 없다:\n%s", want, got)
		}
	}
}

// 결정이 하나도 없어도 계약은 남아야 한다. 첫 결정을 기록하게 만드는 것이
// 빈 볼트에서 이 서버가 할 수 있는 유일한 일이기 때문이다.
func TestBuildInstructionsOnEmptyVault(t *testing.T) {
	c := &config.Config{
		Vault:  t.TempDir(),
		Naming: testutil.VaultConfig(t).Naming,
		Domain: []config.Domain{{Prefix: "alpha", Folder: "alpha"}},
	}
	got, _ := buildInstructions(store.NewLayout(c), pendingView{})

	if !strings.Contains(got, "casebook_capture") {
		t.Errorf("빈 볼트에서 기록 계약이 빠졌다:\n%s", got)
	}
	if strings.Contains(got, "casebook_recall") {
		t.Errorf("결정이 0건인데 회수를 요구한다 — 부를 것이 없다:\n%s", got)
	}
}

// 읽지 못한 노트는 instructions 에서도 알려야 한다. stderr 로 보내면 호스트
// 로그로 가고 에이전트는 영영 모른다 — 세션 진입은 그 사실을 알릴 첫 기회다.
func TestBuildInstructionsReportsSkipped(t *testing.T) {
	c := testutil.VaultConfig(t)
	broken := filepath.Join(c.Vault, "alpha", "decisions", "alpha-결정-깨짐-2026-01-01.md")
	if err := os.WriteFile(broken, []byte("---\ntitle: 구 스키마\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, skipped := buildInstructions(store.NewLayout(c), pendingView{})

	if len(skipped) != 1 {
		t.Fatalf("건너뛴 노트 %d건, 1건이어야 한다", len(skipped))
	}
	if !strings.Contains(got, "1건") {
		t.Errorf("instructions 가 건너뛴 노트를 알리지 않는다:\n%s", got)
	}
}

// pending 은 세는 것만으로는 소용이 없다 — 무엇을 확인해야 하는지, 어떻게 지우는지가
// 같이 있어야 에이전트가 행동할 수 있다.
func TestInstructionsListPending(t *testing.T) {
	c := testutil.VaultConfig(t)
	pv := pendingView{Enabled: true, Items: []daemon.Pending{
		{Domain: "alpha", Turns: 12, Signals: []string{"결정"}, Path: "/t/a.jsonl", From: 0,
			At: time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)},
		{Domain: "beta", Turns: 7, Signals: []string{"채택"}, Path: "/t/b.jsonl", From: 0,
			At: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)},
	}}
	got, _ := buildInstructions(store.NewLayout(c), pv)

	for _, want := range []string{"2건", "alpha", "beta", "casebook_capture", "casebook_pending"} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions 에 %q 가 없다:\n%s", want, got)
		}
	}
}

// **"미확인 0건" 과 "확인할 수 없다" 는 다른 사실이다.** 상태 파일이 깨졌는데 조용히
// 0건으로 보여 주면, 안전망이 죽은 것을 할 일이 없는 것으로 읽게 된다.
func TestInstructionsDistinguishUnreadablePendingFromZero(t *testing.T) {
	c := testutil.VaultConfig(t)

	zero, _ := buildInstructions(store.NewLayout(c), pendingView{Enabled: true})
	if strings.Contains(zero, "미확인") {
		t.Errorf("0건인데 미확인 문단이 나왔다:\n%s", zero)
	}

	broken, _ := buildInstructions(store.NewLayout(c), pendingView{Enabled: true, Err: errors.New("상태 파일이 깨졌다")})
	if !strings.Contains(broken, "확인할 수 없다") {
		t.Errorf("읽기 실패를 알리지 않는다:\n%s", broken)
	}
	if !strings.Contains(broken, "꺼져 있다") {
		t.Errorf("안전망이 꺼졌다는 사실을 말하지 않는다:\n%s", broken)
	}
}

// 데몬을 안 쓰는 설치에서는 pending 문단이 아예 없어야 한다.
func TestInstructionsOmitPendingWhenDisabled(t *testing.T) {
	c := testutil.VaultConfig(t)
	got, _ := buildInstructions(store.NewLayout(c), pendingView{})
	if strings.Contains(got, "미확인") {
		t.Errorf("데몬 연동이 꺼졌는데 미확인 문단이 나왔다:\n%s", got)
	}
}

// 길어지면 instructions 자체가 소음이 된다 — 세션당 한 번 실리고 갱신되지 않는다.
func TestInstructionsCapPendingList(t *testing.T) {
	c := testutil.VaultConfig(t)
	var items []daemon.Pending
	for i := 0; i < 12; i++ {
		items = append(items, daemon.Pending{Domain: "alpha", Turns: 7, Path: "/t/a.jsonl",
			From: int64(i), At: time.Date(2026, 8, 7, 0, i, 0, 0, time.UTC)})
	}
	got, _ := buildInstructions(store.NewLayout(c), pendingView{Enabled: true, Items: items})
	if !strings.Contains(got, "12건") {
		t.Errorf("전체 건수를 안 알린다:\n%s", got)
	}
	if !strings.Contains(got, "그 밖 7건") {
		t.Errorf("생략한 건수를 안 알린다 — 조용히 잘라 내면 다 본 줄 안다:\n%s", got)
	}
}
