package health

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
)

// rollupConfig 는 픽스처에 rollup 네이밍 키를 얹는다. testutil 의 기본 설정에는
// 없다 — 그 자체가 아래 TestRollupSaysWhenSummaryFileIsUnconfigured 의 조건이다.
func rollupConfig(t *testing.T) (*config.Config, *store.Layout) {
	t.Helper()
	c := testutil.VaultConfig(t)
	c.Naming.Rollup = "98-{project}-작업-로그-요약.md"
	return c, store.NewLayout(c)
}

// writeWorklog 는 주어진 날짜들로 작업 로그를 만든다. 블록은 minBlockBytes(100)를
// 넘겨야 한다 — 안 넘기면 rollup 이 Short 로 건너뛰고 이 테스트가 무의미해진다.
func writeWorklog(t *testing.T, l *store.Layout, prefix string, dates ...string) {
	t.Helper()
	p, err := l.WorklogPath(prefix)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("# 작업 로그\n\n")
	for _, d := range dates {
		fmt.Fprintf(&b, "## %s — 그날 한 일\n\n", d)
		b.WriteString("무엇을 왜 그렇게 했는지 적어 둔 줄이다. 블록이 너무 짧으면 " +
			"요약 대상에서 빠지므로 실제 로그만큼 길게 둔다. 다시 한 번 같은 분량.\n\n")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRollupFile(t *testing.T, l *store.Layout, prefix string, weeks ...string) {
	t.Helper()
	p, err := l.RollupPath(prefix)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("# 작업 로그 요약\n\n")
	for _, w := range weeks {
		fmt.Fprintf(&b, "## %s\n\n그 주에 한 일 요약.\n\n", w)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rollupCheck(t *testing.T, l *store.Layout, now time.Time) Check {
	t.Helper()
	r := &Report{}
	checkRollup(r, l, now)
	if len(r.Checks) != 1 {
		t.Fatalf("검사가 %d개 — 1개여야 한다", len(r.Checks))
	}
	return r.Checks[0]
}

var sep2026 = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) // 2026-W36

// ★ 이 검사의 존재 이유다. 실볼트에서 13주가 밀려 있었는데 **아무도 그 사실을
// 말해 주지 않았다** — rollup 은 데몬에도 훅에도 doctor 에도 안 걸려 있는 순수
// 수동 명령이다. 목록을 보러 가는 일은 따로 시간을 내는 일이고, 따로 시간을
// 내는 일은 안 일어난다(2026-08-14 감독큐 결정문).
func TestRollupWarnsWhenWeeksArePiledUp(t *testing.T) {
	_, l := rollupConfig(t)
	// W32(08-03), W33(08-10), W34(08-17) — now 는 W36 이므로 4·3·2 주 밀렸다.
	writeWorklog(t, l, "alpha", "2026-08-03", "2026-08-10", "2026-08-17")

	ck := rollupCheck(t, l, sep2026)
	if ck.Level != Warn {
		t.Fatalf("등급이 %v — Warn 이어야 한다: %s", ck.Level, ck.Detail)
	}
	if !strings.Contains(ck.Detail, "3주") {
		t.Errorf("밀린 주 수가 안 보인다: %q", ck.Detail)
	}
	// **가장 오래된 것을 이름으로 말한다.** 숫자만 주면 어디부터 손대야 하는지
	// 모르고, 모르면 안 한다.
	if !strings.Contains(ck.Detail, "2026-W32") {
		t.Errorf("가장 오래된 주가 안 보인다: %q", ck.Detail)
	}
	if !strings.Contains(ck.Fix, "prior rollup") {
		t.Errorf("고칠 방법이 없다: %q", ck.Fix)
	}
}

// ★ 지난주 하나로는 말하지 않는다. 주가 끝나자마자 경고가 뜨면 **매주** 뜨고,
// 매주 뜨는 경고는 읽히지 않는다. 그러면 이 검사는 없는 것보다 나쁘다 —
// 다른 검사까지 같이 무시당한다.
func TestRollupSilentForLastWeekOnly(t *testing.T) {
	_, l := rollupConfig(t)
	writeWorklog(t, l, "alpha", "2026-08-24") // W35 — 1주 뒤

	if ck := rollupCheck(t, l, sep2026); ck.Level != OK {
		t.Fatalf("등급이 %v — 지난주 하나는 조용해야 한다: %s", ck.Level, ck.Detail)
	}
}

// ★ 이미 요약한 주는 세지 않는다. 세면 rollup 을 돌려도 경고가 안 사라지고,
// 안 사라지는 경고는 고칠 방법이 없는 것과 같다.
func TestRollupExcludesSummarizedWeeks(t *testing.T) {
	_, l := rollupConfig(t)
	writeWorklog(t, l, "alpha", "2026-08-03", "2026-08-10")
	writeRollupFile(t, l, "alpha", "2026-W32", "2026-W33")

	if ck := rollupCheck(t, l, sep2026); ck.Level != OK {
		t.Fatalf("등급이 %v — 다 요약했으면 조용해야 한다: %s", ck.Level, ck.Detail)
	}
}

// ★ 작업 로그가 아예 없는 볼트가 정상이다 (rollup.Scan 의 §). 새로 만든 볼트나
// 회사 볼트가 첫날부터 노란불이면 안 된다.
func TestRollupSilentWithoutWorklogs(t *testing.T) {
	_, l := rollupConfig(t)
	if ck := rollupCheck(t, l, sep2026); ck.Level != OK {
		t.Fatalf("등급이 %v — 로그가 없으면 조용해야 한다: %s", ck.Level, ck.Detail)
	}
}

// ★ 여러 도메인에 걸치면 도메인 수도 말한다. 실볼트가 8개 도메인에 13주였고,
// 그건 "한 프로젝트를 빼먹었다" 가 아니라 "의식 자체가 안 돈다" 는 뜻이다.
func TestRollupNamesDomains(t *testing.T) {
	_, l := rollupConfig(t)
	writeWorklog(t, l, "alpha", "2026-08-03")
	writeWorklog(t, l, "beta", "2026-08-10")

	ck := rollupCheck(t, l, sep2026)
	if ck.Level != Warn {
		t.Fatalf("등급이 %v — Warn 이어야 한다", ck.Level)
	}
	if !strings.Contains(ck.Detail, "alpha") || !strings.Contains(ck.Detail, "beta") {
		t.Errorf("도메인이 안 보인다: %q", ck.Detail)
	}
}

// ★ naming.rollup 이 없으면 작업 로그를 **영영 요약할 수 없다.** 조용히 넘기면
// 그 볼트는 초록불인 채로 로그만 무한히 자란다. 고칠 방법이 한 줄이므로 말한다.
func TestRollupSaysWhenSummaryFileIsUnconfigured(t *testing.T) {
	c := testutil.VaultConfig(t) // rollup 키가 없다
	l := store.NewLayout(c)
	writeWorklog(t, l, "alpha", "2026-08-03")

	ck := rollupCheck(t, l, sep2026)
	if ck.Level != Warn {
		t.Fatalf("등급이 %v — Warn 이어야 한다: %s", ck.Level, ck.Detail)
	}
	if !strings.Contains(ck.Detail, "rollup") {
		t.Errorf("무엇이 없는지 안 말한다: %q", ck.Detail)
	}
}

// ★ 설정에 rollup 키가 없고 작업 로그도 없으면 조용하다. 그건 rollup 을 안 쓰는
// 볼트이고, 안 쓰는 기능을 켜라고 조르는 것은 이 패키지의 일이 아니다.
func TestRollupSilentWhenUnusedEntirely(t *testing.T) {
	c := testutil.VaultConfig(t)
	l := store.NewLayout(c)
	if ck := rollupCheck(t, l, sep2026); ck.Level != OK {
		t.Fatalf("등급이 %v — 안 쓰는 볼트는 조용해야 한다: %s", ck.Level, ck.Detail)
	}
}
