package index

import (
	"testing"

	"github.com/xian0310567/casebook/internal/core/store"
	"github.com/xian0310567/casebook/internal/testutil"
)

func fixtureLayout(t *testing.T) *store.Layout {
	t.Helper()
	return store.NewLayout(testutil.VaultConfig(t))
}
