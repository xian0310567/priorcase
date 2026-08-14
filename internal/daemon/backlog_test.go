package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
)

// ★★★ **밀린 구간은 새 대화가 없어도 갚혀야 한다.**
//
// 승격을 부르는 자리가 둘인데 둘 다 "방금 생긴 것" 만 봤다 — 훅은 세션이 끝날
// 때, 데몬의 drain 은 그 판에 새 표시가 생겼을 때(flagged). 그래서 이미 쌓인
// 구간은 **아무도 안 건드렸고**, 실측으로 30건이 그대로 남아 있었다.
//
// 그 상태가 앱에서는 "확인 큐 30건" 이라는 사람이 눌러야 하는 화면이 됐고,
// 그건 자동 기록이라는 전제를 사람에게 떠넘기는 일이었다.
func TestBacklogIsChewedWithoutNewActivity(t *testing.T) {
	d, dir := backlogFixture(t, 4, "")

	// 새 파일도, 새 표시도 없다. 밀린 것만 있다.
	var got []Promotion
	var mu sync.Mutex
	d.o.OnEvent = func(Event) {}
	d.chewBacklog(context.Background())
	waitIdle(t, d)

	items, err := ReadPending(dir)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	_ = got
	mu.Unlock()
	if len(items) != 0 {
		t.Errorf("밀린 구간 %d건이 남았다 — 소화가 안 됐다", len(items))
	}
}

// ★★★ **소화가 감시 루프를 막으면 안 된다.**
//
// 판별기 호출은 초 단위다. select 안에서 돌리면 그동안 fsnotify 이벤트가 쌓이고,
// 대화가 한창인 파일의 쓰기 알림을 놓친다 — 그 대화는 통째로 안 읽힌다.
func TestChewBacklogDoesNotBlock(t *testing.T) {
	d, _ := backlogFixture(t, 2, "3") // 판별기가 3초 걸린다

	start := time.Now()
	d.chewBacklog(context.Background())
	if el := time.Since(start); el > time.Second {
		t.Errorf("%v 동안 붙잡혔다 — 고루틴으로 돌지 않는다", el)
	}
}

// ★★★ **두 판이 겹치면 안 된다.**
//
// 같은 구간을 두 번 판별기에 물리면 노트가 둘 생길 수 있다. 주기가 한 판보다
// 짧아도 겹치지 않아야 한다.
func TestChewBacklogRunsOneAtATime(t *testing.T) {
	d, _ := backlogFixture(t, 2, "1")

	d.chewBacklog(context.Background())
	if !d.backlog.running.Load() {
		t.Fatal("첫 판이 안 돌고 있다")
	}
	// 도는 중에 또 부른다 — 아무 일도 안 일어나야 한다.
	d.chewBacklog(context.Background())
	waitIdle(t, d)
}

// ★★★ **성과가 없으면 물러선다.**
//
// 판별기가 못 도는 상태(로그인 풀림)에서 5분마다 부르면 호스트 CLI 만 두들긴다.
// 기각은 성과다 — 그 구간은 해소된다. 에러만 성과가 아니다.
func TestBacklogBacksOffWhenNothingGetsJudged(t *testing.T) {
	d, _ := backlogFixture(t, 2, "")
	base := 5 * time.Minute

	if got := d.backlog.wait(base); got != base {
		t.Errorf("처음 주기 %v — %v 여야 한다", got, base)
	}
	d.backlog.idle.Store(3)
	if got := d.backlog.wait(base); got != 40*time.Minute {
		t.Errorf("3판 헛돈 뒤 %v — 40분이어야 한다", got)
	}
	// **상한이 있어야 한다.** 안 그러면 하루 뒤에 도는 루프가 된다.
	d.backlog.idle.Store(30)
	if got := d.backlog.wait(base); got != maxBacklogInterval {
		t.Errorf("오래 헛돈 뒤 %v — 상한 %v 여야 한다", got, maxBacklogInterval)
	}
}

// ★★★ **밀린 것이 없으면 물러섬을 되돌린다.**
//
// 안 그러면 한동안 조용했다가 갑자기 쌓였을 때 한 시간을 기다린다.
func TestEmptyBacklogResetsBackoff(t *testing.T) {
	d, _ := backlogFixture(t, 0, "")
	d.backlog.idle.Store(5)
	d.chewBacklog(context.Background())
	waitIdle(t, d)
	if n := d.backlog.idle.Load(); n != 0 {
		t.Errorf("물러섬이 %d 로 남았다 — 0 이어야 한다", n)
	}
}

// ★★ 판별기가 답하면 물러섬이 풀린다.
func TestSuccessResetsBackoff(t *testing.T) {
	d, _ := backlogFixture(t, 2, "")
	d.backlog.idle.Store(4)
	d.chewBacklog(context.Background())
	waitIdle(t, d)
	if n := d.backlog.idle.Load(); n != 0 {
		t.Errorf("물러섬이 %d 로 남았다 — 판정이 있었으면 0 이어야 한다", n)
	}
}

// ── 픽스처 ────────────────────────────────────────────────────────────────

func waitIdle(t *testing.T, d *watcher) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for d.backlog.running.Load() {
		if time.Now().After(deadline) {
			t.Fatal("소화가 안 끝난다")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// backlogFixture 는 밀린 구간 n개와 sleep 초짜리 가짜 판별기를 가진 데몬을 만든다.
func backlogFixture(t *testing.T, n int, sleep string) (*watcher, string) {
	t.Helper()
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "proj", "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}

	// **판정은 언제나 "기록 안 함" 이다.** 이 시험이 보는 것은 밀린 것이 줄어드는가
	// 이지 노트를 잘 쓰는가가 아니다.
	jp := filepath.Join(t.TempDir(), "judge.sh")
	slow := ""
	if sleep != "" {
		slow = "sleep " + sleep + "\n"
	}
	body := "#!/bin/sh\ncat >/dev/null\n" + slow + `echo '{"record":false,"reason":"시험용"}'` + "\n"
	if err := os.WriteFile(jp, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	c := &config.Config{
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
	o := Options{StateDir: dir, Config: c, OnEvent: func(Event) {}}
	return &watcher{o: o, st: st, l: store.NewLayout(c), dirty: map[string]bool{}}, dir
}

// ★★★ **진짜 데몬 루프가 밀린 것을 갚는가.**
//
// 위의 시험들은 chewBacklog 를 직접 부른다 — 그건 "함수가 옳은가" 만 본다.
// 이 프로젝트에서 다섯 번 난 사고가 전부 **함수는 옳은데 조립부가 안 부른다**
// 였다. 여기서는 Run 을 띄우고, 새 대화를 한 줄도 만들지 않고, 밀린 구간이
// 저절로 줄어드는지 본다.
//
// 되돌려 확인했다: 루프의 chew 타이머를 빼면 이 시험만 실패한다.
func TestDaemonLoopChewsBacklogOnItsOwn(t *testing.T) {
	o := baseOpts(t)
	o.BacklogInterval = 50 * time.Millisecond

	// 판별기를 심고 밀린 구간을 만든다. 대화 파일은 건드리지 않는다.
	jp := filepath.Join(t.TempDir(), "judge.sh")
	if err := os.WriteFile(jp,
		[]byte("#!/bin/sh\ncat >/dev/null\necho '{\"record\":false,\"reason\":\"시험용\"}'\n"),
		0o755); err != nil {
		t.Fatal(err)
	}
	o.Config.Capture.JudgePath = jp

	st := NewStore(o.StateDir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := st.AddPending(Pending{
			// **설정에 있는 도메인이어야 한다.** 없는 이름을 주면 승격이 정당하게
			// 건너뛰고, 그러면 이 시험은 배선이 아니라 픽스처를 시험하게 된다.
			Path: fmt.Sprintf("/backlog%02d.jsonl", i), From: 0, Domain: "alpha",
			SessionID: fmt.Sprintf("B%02d", i), Days: []string{"2026-08-14"},
			At: time.Now().UTC(), Excerpt: strings.Repeat("결정을 내렸다. ", 20),
		}); err != nil {
			t.Fatal(err)
		}
	}

	start(t, o)

	waitFor(t, 20*time.Second, "밀린 구간이 저절로 줄어든다", func() bool {
		items, err := ReadPending(o.StateDir)
		return err == nil && len(items) == 0
	})
}
