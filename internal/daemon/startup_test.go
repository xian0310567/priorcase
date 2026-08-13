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

// ★★★ **호스트가 더는 다루지 않는 기록의 체크포인트를 지운다.**
//
// 서브에이전트 기록을 목록에서 빼기 시작하면 그 파일들의 체크포인트가 죽은 채
// 남는다 — 실측으로 3,648항목 중 1,417개가 이 모양이었다. 상태 파일은 mutate
// 마다 통째로 다시 쓰므로 죽은 항목의 무게가 **모든 쓰기에** 실린다.
func TestStartupPrunesUnlistedCheckpoints(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	c, l := accCfg(t)
	root := t.TempDir()
	rs := accHosts(t, root)

	live := filepath.Join(root, "proj", "c851bbeb-e0a9-49bb-aeef-79bccdab0b67.jsonl")
	dead := filepath.Join(root, "proj", "agent-a5801c63bd316cd1b.jsonl")
	writeTurns(t, live, 2, "가")
	writeTurns(t, dead, 2, "나")
	sizes := map[string]int64{}
	for _, p := range []string{live, dead} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		sizes[p] = fi.Size()
	}
	if err := st.SeedAll(sizes); err != nil {
		t.Fatal(err)
	}

	d := &watcher{o: Options{Config: c, StateDir: dir}, st: st, l: l, hosts: rs, dirty: map[string]bool{}}
	d.startupPass(context.Background())

	cps := st.CheckpointSnapshot()
	if _, ok := cps[dead]; ok {
		t.Error("서브에이전트 기록의 체크포인트가 남았다 — 상태 파일이 안 줄어든다")
	}
	if _, ok := cps[live]; !ok {
		t.Error("본 대화의 체크포인트가 지워졌다 — 다시 시딩되면 그 사이 대화를 잃는다")
	}
}

// ★★★ **목록이 실패한 루트의 것은 지우면 안 된다.**
//
// 호스트가 잠깐 안 보이면(외장 디스크·권한) 목록이 빈다. 그때 "목록에 없으니
// 죽은 것" 으로 지우면 멀쩡한 체크포인트를 잃고, 그 파일들이 다시 시딩돼
// **그 사이의 대화가 사라진다.**
func TestPruneUnlistedIgnoresOtherRoots(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir() // 다른 호스트의 자리 — 이번 목록에 안 들어간다
	p := filepath.Join(other, "s.jsonl")
	if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.SeedAll(map[string]int64{p: 3}); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	n, err := PruneUnlisted(st, []string{root}, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d개를 지웠다 — 목록을 만들지 않은 자리는 건드리면 안 된다", n)
	}
	if _, ok := st.CheckpointSnapshot()[p]; !ok {
		t.Error("다른 루트의 체크포인트가 사라졌다")
	}
}
