package index

import (
	"testing"

	"github.com/xian0310567/priorcase/internal/core/store"
	"github.com/xian0310567/priorcase/internal/testutil"
)

func fixtureLayout(t *testing.T) *store.Layout {
	t.Helper()
	l, _ := fixtureLayoutVault(t)
	return l
}

// fixtureLayoutVault 는 Layout 과 그 볼트 경로를 함께 준다. Layout 은 설정을
// 비공개로 감추므로, 픽스처에 파일을 심어야 하는 테스트는 경로를 따로 받아야 한다.
func fixtureLayoutVault(t *testing.T) (*store.Layout, string) {
	t.Helper()
	c := testutil.VaultConfig(t)
	return store.NewLayout(c), c.Vault
}
