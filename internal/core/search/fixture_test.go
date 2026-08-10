package search

import (
	"testing"

	"github.com/xian0310567/priorcase/internal/core/config"
	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

func fixtureLayoutConfig(t *testing.T) (*store.Layout, *config.Config) {
	t.Helper()
	c := testutil.VaultConfig(t)
	return store.NewLayout(c), c
}

// mustRecall 은 볼트가 읽히는 정상 경로에서만 쓴다. 에러는 테스트 실패다 —
// 채점·정렬을 보는 테스트가 I/O 실패를 "매칭 0건" 으로 오해하지 않게 한다.
// 건너뛴 노트도 마찬가지다: 정상 픽스처에서 노트가 조용히 빠지면 채점 테스트가
// "그 노트는 원래 점수가 낮았다" 로 오해한다.
func mustRecall(t *testing.T, l *store.Layout, c *config.Config, prompt string, o Options) []Hit {
	t.Helper()
	hits, skipped, err := Recall(l, c, prompt, o)
	if err != nil {
		t.Fatalf("Recall(%q) error = %v", prompt, err)
	}
	if len(skipped) != 0 {
		t.Fatalf("Recall(%q): 정상 픽스처인데 건너뛴 노트가 있다: %+v", prompt, skipped)
	}
	return hits
}
