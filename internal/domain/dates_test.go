package domain

import (
	"testing"
	"time"
)

func TestStampIsUTCInWholeSeconds(t *testing.T) {
	in := time.Date(2026, 11, 4, 8, 52, 10, 999_000_000, time.FixedZone("UTC+9", 9*3600))
	got := Stamp(in)
	assertEqual(t, got, time.Date(2026, 11, 3, 23, 52, 10, 0, time.UTC))
	assertEqual(t, got.Location(), time.UTC)
}

func TestStampDoesNotDependOnTheZone(t *testing.T) {
	moment := time.Date(2026, 11, 4, 2, 30, 10, 0, time.UTC)
	west := moment.In(time.FixedZone("UTC-5", -5*3600))
	east := moment.In(time.FixedZone("UTC+9", 9*3600))
	assertTrue(t, Stamp(west).Equal(Stamp(east)))
	assertEqual(t, StoredDate(Stamp(west)), StoredDate(Stamp(east)))
}

func TestStoredDate(t *testing.T) {
	assertEqual(t, StoredDate(time.Date(2026, 11, 4, 23, 52, 10, 0, time.UTC)), "2026-11-04T23:52:10Z")
	assertEqual(t, StoredDate(time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)), "2026-10-26")
	assertEqual(t, StoredDate(time.Date(2026, 11, 4, 10, 0, 0, 0, time.FixedZone("", 3600))), "2026-11-04T09:00:00Z")
}

func TestDisplayDate(t *testing.T) {
	west := time.FixedZone("UTC-5", -5*3600)
	east := time.FixedZone("UTC+9", 9*3600)
	stamp := time.Date(2026, 11, 4, 2, 30, 0, 0, time.UTC)
	assertEqual(t, DisplayDate(stamp, west), "2026-11-03")
	assertEqual(t, DisplayDate(stamp, east), "2026-11-04")
	assertEqual(t, DisplayDate(stamp, time.UTC), "2026-11-04")

	bare := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	assertEqual(t, DisplayDate(bare, west), "2026-10-26")
	assertEqual(t, DisplayDate(bare, east), "2026-10-26")
}

func TestParseStoredRoundTripsStoredDate(t *testing.T) {
	for _, in := range []string{"2026-10-26", "2026-11-04T23:52:10Z"} {
		got, err := ParseStored(in)
		if err != nil {
			t.Fatalf("ParseStored(%q): %v", in, err)
		}
		if StoredDate(got) != in {
			t.Fatalf("StoredDate(ParseStored(%q)) = %q", in, StoredDate(got))
		}
	}
	if got, err := ParseStored(""); err != nil || !got.IsZero() {
		t.Fatalf("ParseStored(\"\") = %v, %v; want the zero time", got, err)
	}
	if _, err := ParseStored("yesterday"); err == nil {
		t.Fatal("ParseStored(\"yesterday\") succeeded")
	}
}
