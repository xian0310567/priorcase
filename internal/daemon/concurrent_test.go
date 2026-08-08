package daemon

import (
	"sync"
	"testing"
	"time"
)

// ★★ 메모리를 정본으로 쓰면 한쪽의 해소가 다른 쪽의 저장에 되살아난다.
//
// 데몬은 Store 를 기동 때 한 번 Load 하고 살아 있는 내내 그 메모리를 쓴다. 그 사이
// 훅이 pending 을 해소하면(승격을 끝냈으므로) 데몬의 다음 저장이 옛 목록을 통째로
// 다시 써서 해소가 사라진다. 그러면 같은 구간이 다음 세션에 또 판별기로 간다 —
// 승격을 락 밖으로 빼는 순간 이 경합이 실제로 생긴다.
func TestOtherProcessResolveIsNotClobbered(t *testing.T) {
	dir := t.TempDir()

	daemonSide := NewStore(dir)
	if err := daemonSide.Load(); err != nil {
		t.Fatal(err)
	}
	p := Pending{Path: "/t.jsonl", From: 0, Domain: "alpha", At: time.Now().UTC()}
	if err := daemonSide.AddPending(p); err != nil {
		t.Fatal(err)
	}

	// 다른 프로세스(훅)가 승격을 마치고 해소한다.
	if err := ResolvePending(dir, p.ID()); err != nil {
		t.Fatal(err)
	}

	// 데몬은 그것을 모른 채 제 일을 계속한다.
	if err := daemonSide.Advance("/t.jsonl", 10, 10); err != nil {
		t.Fatal(err)
	}

	left, err := ReadPending(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("해소한 구간이 되살아났다 (%d건) — 같은 구간이 판별기로 다시 간다", len(left))
	}
}

// 잠금이 실제로 직렬화하는지 본다. 잃어버린 갱신이 있으면 건수가 모자란다.
func TestConcurrentAddPendingDoesNotLoseUpdates(t *testing.T) {
	dir := t.TempDir()
	const n = 12

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := NewStore(dir) // 프로세스가 여럿인 상황을 흉내낸다
			if err := s.AddPending(Pending{
				Path: "/t.jsonl", From: int64(i * 100), Domain: "alpha", At: time.Now().UTC(),
			}); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	got, err := ReadPending(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Errorf("pending %d건, want %d — 잃어버린 갱신이 있다", len(got), n)
	}
}
