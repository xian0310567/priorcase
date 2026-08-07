package search

import (
	"testing"

	"github.com/xian0310567/casebook/internal/core/config"
	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/testutil"
)

func fixtureLayoutConfig(t *testing.T) (*store.Layout, *config.Config) {
	t.Helper()
	c := testutil.VaultConfig(t)
	return store.NewLayout(c), c
}

// mustRecall 은 볼트가 읽히는 정상 경로에서만 쓴다. 에러는 테스트 실패다 —
// 채점·정렬을 보는 테스트가 I/O 실패를 "매칭 0건" 으로 오해하지 않게 한다.
func mustRecall(t *testing.T, l *store.Layout, c *config.Config, prompt string, o Options) []Hit {
	t.Helper()
	hits, err := Recall(l, c, prompt, o)
	if err != nil {
		t.Fatalf("Recall(%q) error = %v", prompt, err)
	}
	return hits
}
