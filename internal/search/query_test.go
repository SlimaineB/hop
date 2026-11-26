package search

import (
	"testing"
	"time"
)

func fixedNow() time.Time {
	return time.Date(2026, 11, 25, 16, 0, 0, 0, time.UTC)
}

func assertRange(t *testing.T, q Query, from, to time.Time) {
	t.Helper()
	if q.TimeRange == nil {
		t.Fatalf("expected time range, got nil (terms=%v)", q.Terms)
	}
	if !q.TimeRange.From.Equal(from) {
		t.Fatalf("from = %s, want %s", q.TimeRange.From, from)
	}
	if !q.TimeRange.To.Equal(to) {
		t.Fatalf("to = %s, want %s", q.TimeRange.To, to)
	}
}

func TestTimeRangeAnyOrder(t *testing.T) {
	now := fixedNow()
	q := ParseAt("pod1 ns1 env1 12h 14h", now)
	if len(q.Terms) != 3 {
		t.Fatalf("terms = %v, want 3 terms", q.Terms)
	}
	assertRange(t, q,
		time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC),
	)
	if len(q.TimeRange.Raw) != 2 {
		t.Fatalf("raw = %v", q.TimeRange.Raw)
	}
}

func TestTimeRangeTokensInterleaved(t *testing.T) {
	now := fixedNow()
	q := ParseAt("12h pod1 ns1 14h", now)
	if len(q.Terms) != 2 || q.Terms[0] != "pod1" || q.Terms[1] != "ns1" {
		t.Fatalf("terms = %v", q.Terms)
	}
	assertRange(t, q,
		time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC),
	)
}

func TestTimeRangeReversedClockUsesShortestWindow(t *testing.T) {
	now := fixedNow()
	q := ParseAt("api 14h 12h", now)
	assertRange(t, q,
		time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC),
	)
}

func TestTimeRangeCrossesMidnight(t *testing.T) {
	now := fixedNow()
	q := ParseAt("api 22h 2h", now)
	assertRange(t, q,
		time.Date(2026, 11, 25, 22, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 26, 2, 0, 0, 0, time.UTC),
	)
}

func TestTimeRangeRelative(t *testing.T) {
	now := fixedNow()
	q := ParseAt("api -2h", now)
	if len(q.Terms) != 1 || q.Terms[0] != "api" {
		t.Fatalf("terms = %v", q.Terms)
	}
	assertRange(t, q, now.Add(-2*time.Hour), now)
}

func TestTimeRangeExplicitBoundaries(t *testing.T) {
	now := fixedNow()
	q := ParseAt("api from:2026-11-25T12:00 to:2026-11-25T14:00", now)
	assertRange(t, q,
		time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC),
	)

	q = ParseAt("api to:14h", now)
	assertRange(t, q,
		time.Date(2026, 11, 25, 13, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC),
	)
}

func TestTimeRangeSingleClock(t *testing.T) {
	now := fixedNow()
	q := ParseAt("api 12h30", now)
	assertRange(t, q,
		time.Date(2026, 11, 25, 12, 30, 0, 0, time.UTC),
		now,
	)
}

func TestTimeRangeDates(t *testing.T) {
	now := fixedNow()
	q := ParseAt("api 2026-11-20 2026-11-22", now)
	assertRange(t, q,
		time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC),
	)
}

func TestFiltersAndTimeRange(t *testing.T) {
	now := fixedNow()
	q := ParseAt("ns:dev label:app=api env:prod 12h 14h", now)
	if got := q.Filters[FieldNamespace]; len(got) != 1 || got[0] != "dev" {
		t.Fatalf("namespace filters = %v", got)
	}
	if got := q.Filters[FieldLabel]; len(got) != 1 || got[0] != "app=api" {
		t.Fatalf("label filters = %v", got)
	}
	if got := q.Filters[FieldEnv]; len(got) != 1 || got[0] != "prod" {
		t.Fatalf("env filters = %v", got)
	}
	if len(q.Terms) != 0 {
		t.Fatalf("terms = %v", q.Terms)
	}
}

func TestInvalidTimeTokenStaysTerm(t *testing.T) {
	now := fixedNow()
	q := ParseAt("12h123", now)
	if q.TimeRange != nil {
		t.Fatalf("unexpected range: %+v", q.TimeRange)
	}
	if len(q.Terms) != 1 || q.Terms[0] != "12h123" {
		t.Fatalf("terms = %v", q.Terms)
	}
}
