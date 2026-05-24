package events

import "testing"

// FuzzParseFromTimestamp exercises the SSE ?from_timestamp= query-param parser with
// arbitrary strings. The value is fully attacker-controlled, so the parser
// must never panic; and per its contract a non-nil error must come with the
// zero time, so callers can't act on a half-parsed instant.
func FuzzParseFromTimestamp(f *testing.F) {
	f.Add("2026-05-24T14:00:00Z")
	f.Add("2026-05-24T14:00:00 00:00") // Space-for-plus tolerance.
	f.Add("")
	f.Add("not-a-time")
	f.Add("0001-01-01T00:00:00Z")

	f.Fuzz(func(t *testing.T, ts string) {
		got, err := parseFromTimestamp(ts)
		if err != nil && !got.IsZero() {
			t.Fatalf("error returned but non-zero time for %q: %v", ts, got)
		}
	})
}
