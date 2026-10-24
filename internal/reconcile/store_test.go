package reconcile_test

import (
	"testing"

	"github.com/sperano/sillage/internal/reconcile"
	"github.com/sperano/sillage/internal/reconcile/storetest"
)

// TestFakeStoreContract checks that the store the loop's tests run on
// keeps the contract the PostgreSQL store's adapter is checked against.
func TestFakeStoreContract(t *testing.T) {
	storetest.Run(t, func(*testing.T) reconcile.Store { return reconcile.NewFakeStore() })
}
