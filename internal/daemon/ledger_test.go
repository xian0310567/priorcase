package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ★ **판정을 남기지 않으면 "봤는데 기록할 게 없었다" 와 "아예 안 돌았다" 가 같아진다.**
// 컷오버 1일차에 prior doctor 가 정확히 그 둘을 구분하지 못했다.
func TestLedgerKeepsRejectionsNotJustRecords(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

	for _, p := range []Promotion{
		{At: now, ID: "/t@0", Domain: "alpha", Recorded: true, Path: "alpha/decisions/x.md"},
		{At: now.Add(time.Minute), ID: "/t@1", Domain: "alpha", Reason: "기록할 결정이 아니다"},
		{At: now.Add(2 * time.Minute), ID: "/t@2", Domain: "alpha", Err: "Not logged in"},
	} {
		if err := AppendPromotion(dir, p); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ReadPromotions(dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%d건, want 3 — 기각·실패가 사라지면 판별기가 도는지 알 수 없다", len(got))
	}
	if got[0].Path == "" || got[1].Reason == "" || got[2].Err == "" {
		t.Errorf("세 갈래가 구분되지 않는다: %+v", got)
	}
	if got[0].Recorded == got[1].Recorded {
		t.Error("기록/미기록이 구분되지 않는다")
	}
}

// since 로 최근 것만 본다. 원장은 계속 자라므로 진단이 전체를 세면 안 된다.
func TestReadPromotionsFiltersBySince(t *testing.T) {
	dir := t.TempDir()
	old := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{old, recent} {
		if err := AppendPromotion(dir, Promotion{At: at, ID: "/t@0"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReadPromotions(dir, recent.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].At.Equal(recent) {
		t.Errorf("since 가 안 걸렸다: %+v", got)
	}
}

// 원장이 없으면 빈 목록이다 — 승격이 한 번도 없었다는 뜻이고 에러가 아니다.
func TestReadPromotionsOnMissingFile(t *testing.T) {
	got, err := ReadPromotions(t.TempDir(), time.Time{})
	if err != nil || len(got) != 0 {
		t.Errorf("빈 원장이 에러가 됐다: %v %v", got, err)
	}
}

// 덧붙이는 도중에 죽으면 마지막 줄이 잘린다. 그것 때문에 진단이 통째로 실패하면 안 된다.
func TestTruncatedLedgerLineIsSkipped(t *testing.T) {
	dir := t.TempDir()
	if err := AppendPromotion(dir, Promotion{At: time.Now().UTC(), ID: "/t@0", Recorded: true}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, ledgerFile), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"at":"2026-08-08T00:00:00Z","id":"/t@1"`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got, err := ReadPromotions(dir, time.Time{})
	if err != nil {
		t.Fatalf("잘린 줄 하나에 진단이 통째로 죽었다: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("%d건, want 1 (온전한 줄만)", len(got))
	}
}

// ★ 긴 줄 하나가 원장 전체를 못 읽게 만들면 안 된다.
//
// 판별기 실패 메시지에는 호스트 CLI 의 stderr 가 통째로 들어온다 (MCP 스택 트레이스,
// 대량 경고). ReadPromotions 의 스캐너 상한을 넘으면 **그 줄만 사라지는 것이 아니라
// 그 뒤가 통째로 안 읽힌다** — 그리고 하필 그때가 진단이 가장 필요한 순간이다.
func TestOversizedErrorDoesNotKillLedger(t *testing.T) {
	dir := t.TempDir()
	huge := strings.Repeat("스택트레이스 ", 400000) // 넉넉히 1MB 초과

	if err := AppendPromotion(dir, Promotion{
		At: time.Now().UTC(), ID: "/t@0", Domain: "alpha", Err: TrimLedgerText(huge)}); err != nil {
		t.Fatal(err)
	}
	if err := AppendPromotion(dir, Promotion{
		At: time.Now().UTC(), ID: "/t@1", Domain: "alpha", Recorded: true}); err != nil {
		t.Fatal(err)
	}

	got, err := ReadPromotions(dir, time.Time{})
	if err != nil {
		t.Fatalf("긴 줄 하나에 원장이 통째로 죽었다: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("%d건, want 2 — 긴 줄 뒤가 안 읽힌다", len(got))
	}
	if !strings.HasSuffix(got[0].Err, "(잘림)") {
		t.Error("잘렸다는 표시가 없다 — 사람이 원문이 다 있는 줄 안다")
	}
}

func TestTrimLedgerTextKeepsShortStrings(t *testing.T) {
	if got := TrimLedgerText("짧다"); got != "짧다" {
		t.Errorf("멀쩡한 문자열을 건드렸다: %q", got)
	}
}
