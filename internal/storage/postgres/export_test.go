package postgres

import (
	"errors"
	"time"
)

// FailCreateAfterFirstTable makes Create fail right after its first table, so
// a test can see what a failed Create leaves behind. It returns a restore func.
func FailCreateAfterFirstTable() func() {
	old := afterFirstTable
	afterFirstTable = func() error { return errors.New("injected failure") }
	return func() { afterFirstTable = old }
}

// SetLockWaitLimit changes how long Lock waits when its caller sets no deadline
// and returns the old limit. For tests.
func SetLockWaitLimit(d time.Duration) time.Duration {
	old := lockWaitLimit
	lockWaitLimit = d
	return old
}

// FailAfterSave makes CreateFrom fail after the cards are written but before the
// transaction commits. It returns a restore func.
func FailAfterSave() func() {
	old := afterSave
	afterSave = func() error { return errors.New("injected failure") }
	return func() { afterSave = old }
}
