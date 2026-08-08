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

// ★ 동시에 끝나는 두 세션이 같은 구간을 각자 판별기에 넘기면, 판별기가 비결정적이라
// 같은 대화에 slug 가 다른 결정 노트가 둘 생긴다. 선점으로 막는다.
func TestClaimIsExclusive(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	p := Pending{Path: "/t.jsonl", From: 0, Domain: "alpha", At: time.Now().UTC()}
	if err := s.AddPending(p); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()

	first, err := ClaimPending(dir, p.ID(), now)
	if err != nil || !first {
		t.Fatalf("첫 선점이 실패했다: %v %v", first, err)
	}
	second, err := ClaimPending(dir, p.ID(), now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second {
		t.Error("둘이 같은 구간을 집었다 — 결정 노트가 둘 생긴다")
	}

	// 프로세스가 죽어도 사람이 치울 것이 없어야 한다.
	later, err := ClaimPending(dir, p.ID(), now.Add(claimTTL+time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !later {
		t.Error("선점이 영구히 남았다 — 죽은 프로세스가 구간을 인질로 잡는다")
	}
}

// 없는 구간을 집으려 하면 false 다 (이미 해소됐다).
func TestClaimMissingPendingIsFalse(t *testing.T) {
	got, err := ClaimPending(t.TempDir(), "/t.jsonl@0", time.Now())
	if err != nil || got {
		t.Errorf("없는 구간을 집었다: %v %v", got, err)
	}
}
