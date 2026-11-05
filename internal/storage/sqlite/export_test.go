package sqlite

import "time"

// SetLockWaitLimit changes how long Lock waits when its caller sets no deadline
// and returns the old limit. For tests.
func SetLockWaitLimit(d time.Duration) time.Duration {
	old := lockWaitLimit
	lockWaitLimit = d
	return old
}
