package domain

import "time"

const (
	dateLayout  = "2006-01-02"
	stampLayout = "2006-01-02T15:04:05Z"
)

// Stamp returns the value to store for the moment now: UTC, in whole seconds.
// Stored dates never depend on the machine's zone.
func Stamp(now time.Time) time.Time { return now.UTC().Truncate(time.Second) }

// Day returns the UTC calendar date of t, at 00:00 UTC. It is for day
// arithmetic; a date shown to a person goes through DisplayDate.
func Day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// dateOnly reports whether t is a bare calendar date, such as a hand-written
// 2026-10-26: exactly 00:00:00 UTC.
func dateOnly(t time.Time) bool { return t.Equal(Day(t)) }

// StoredDate is t as files and JSON hold it: YYYY-MM-DD for a date-only value,
// else an RFC 3339 timestamp in UTC, such as 2026-11-04T23:52:10Z.
func StoredDate(t time.Time) string {
	if dateOnly(t) {
		return t.UTC().Format(dateLayout)
	}
	return t.UTC().Format(stampLayout)
}

// DisplayDate is the calendar date to show a person in loc. A date-only value
// is shown as written, with no zone conversion, so it can never shift a day.
func DisplayDate(t time.Time, loc *time.Location) string {
	if dateOnly(t) {
		return t.UTC().Format(dateLayout)
	}
	return t.In(loc).Format(dateLayout)
}
