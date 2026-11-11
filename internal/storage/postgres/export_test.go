package postgres

import "errors"

// FailCreateAfterFirstTable makes Create fail right after its first table, so
// a test can see what a failed Create leaves behind. It returns a restore func.
func FailCreateAfterFirstTable() func() {
	old := afterFirstTable
	afterFirstTable = func() error { return errors.New("injected failure") }
	return func() { afterFirstTable = old }
}
