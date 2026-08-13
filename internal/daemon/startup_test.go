package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ★★★ **기동 패스가 따라잡은 파일까지 전부 훑는다.**
//
// `sweepPlanned` 에는 stat 필터가 있다 — 자라지 않은 파일은 Scan 을 부르지도
// 않는다. `Scan` 은 읽을 것이 없어도 나가면서 흔적을 남기는데 그건 **상태 파일
// 전체를 다시 쓰는 일**이라, 3,000개를 도는 동안 3,000번 쓴다.
//
// 그런데 `startupPass` 는 그 필터 없이 plan.Scan 을 통째로 큐에 넣는다. 실측으로
// 이 기계의 체크포인트 3,605건 중 **3,563건이 이미 따라잡은 상태**다 — 매 기동에
// 그만큼을 헛돈다 (실측 29초 · 상태 쓰기 3,417회).
//
// 같은 필터를 여기에도 둔다. 판단이 두 곳에 있으면 한쪽만 고쳐진 채로 남는다.
func TestStartupPassSkipsCaughtUpFiles(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	c, l := accCfg(t)
	root := t.TempDir()
	rs := accHosts(t, root)

	const N = 30
	sizes := map[string]int64{}
	for i := 0; i < N; i++ {
		p := filepath.Join(root, "proj", fmt.Sprintf("done%02d.jsonl", i))
		writeTurns(t, p, 2, "가")
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		sizes[p] = fi.Size()
	}
	// 자란 파일 하나 — 이건 반드시 훑어야 한다.
	grown := filepath.Join(root, "proj", "grown.jsonl")
	writeTurns(t, grown, 2, "나")
	fi, err := os.Stat(grown)
	if err != nil {
		t.Fatal(err)
	}
	sizes[grown] = fi.Size()
	if err := st.SeedAll(sizes); err != nil {
		t.Fatal(err)
	}
	appendTurns(t, grown, 2, "결정") // 체크포인트보다 커졌다

	d := &watcher{
		o:     Options{Config: c, StateDir: dir},
		st:    st,
		l:     l,
		hosts: rs,
		dirty: map[string]bool{},
	}
	w0 := st.Writes()
	d.startupPass(context.Background())
	writes := st.Writes() - w0

	// **따라잡은 파일 수에 비례해 쓰면 안 된다.** 한 자릿수여야 한다
	// (정리·시딩·자란 파일 하나).
	if writes > 6 {
		t.Errorf("상태 쓰기 %d회 — 따라잡은 %d개까지 훑고 있다", writes, N)
	}
}

// ★★ **밀린 것이 많아도 기동이 멎으면 안 된다.**
//
// drain 에 예산이 없다. 데몬이 오래 꺼져 있었거나 --backfill 이면 밀린 파일이
// 수백 개일 수 있고, 그걸 다 훑는 동안 `prior watch` 가 뜨지 않는다. 남은 것은
// 정상 감시 루프의 drain 이 곧 가져간다.
func TestStartupDrainHasBudget(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	c, l := accCfg(t)
	root := t.TempDir()
	rs := accHosts(t, root)

	const N = 40
	sizes := map[string]int64{}
	paths := make([]string, 0, N)
	for i := 0; i < N; i++ {
		p := filepath.Join(root, "proj", fmt.Sprintf("b%02d.jsonl", i))
		writeTurns(t, p, 2, "가")
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		sizes[p] = fi.Size()
		paths = append(paths, p)
	}
	if err := st.SeedAll(sizes); err != nil {
		t.Fatal(err)
	}
	for _, p := range paths { // 전부 자라게 한다 → 전부 밀렸다
		appendTurns(t, p, 2, "결정")
	}

	d := &watcher{
		o:     Options{Config: c, StateDir: dir, StartupBudget: time.Nanosecond},
		st:    st,
		l:     l,
		hosts: rs,
		dirty: map[string]bool{},
	}
	d.startupPass(context.Background())

	// 예산이 1ns 이므로 거의 아무것도 못 훑어야 한다. 남은 것은 dirty 에 남아
	// 다음 drain 이 가져간다 — **버리면 안 된다.**
	d.mu.Lock()
	left := len(d.dirty)
	d.mu.Unlock()
	if left == 0 {
		t.Error("예산이 1ns 인데 전부 훑었다 — 예산이 안 걸린다")
	}
}
