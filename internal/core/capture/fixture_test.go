package capture

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
