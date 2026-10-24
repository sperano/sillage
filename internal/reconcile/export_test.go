package reconcile

// NewFakeStore returns the loop tests' in-memory store, for the contract
// test in package reconcile_test.
func NewFakeStore() Store { return newFakeStore() }
