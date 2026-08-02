package bitemporal

import (
	"testing"
	"time"
)

// PostgreSQL timestamptz holds microseconds. Go holds nanoseconds. An
// untruncated value written and read back is not the value written, and a
// comparison against a stored interval then fails for a reason invisible in
// the source.
func TestInstantTruncatesToPostgresResolution(t *testing.T) {
	t.Parallel()

	withNanos := time.Date(2026, 3, 14, 9, 26, 53, 589793238, time.UTC)

	got := At(withNanos)
	if got.Time().Nanosecond()%1000 != 0 {
		t.Errorf("At() kept sub-microsecond precision: %d ns",
			got.Time().Nanosecond())
	}

	// Two times differing only below a microsecond must compare equal, or a
	// round trip through the database changes the answer.
	a := At(time.Date(2026, 3, 14, 9, 26, 53, 589793238, time.UTC))
	b := At(time.Date(2026, 3, 14, 9, 26, 53, 589793999, time.UTC))

	if !a.Equal(b) {
		t.Error("instants differing below a microsecond are not equal; a " +
			"database round trip would change comparison results")
	}
}

func TestInstantIsAlwaysUTC(t *testing.T) {
	t.Parallel()

	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	local := time.Date(2026, 3, 14, 4, 26, 53, 0, chicago)

	got := At(local)
	if got.Time().Location() != time.UTC {
		t.Errorf("At() kept location %s", got.Time().Location())
	}

	if !got.Equal(At(local.UTC())) {
		t.Error("the same moment in two zones produced different instants")
	}
}

func TestForeverAndBeginning(t *testing.T) {
	t.Parallel()

	if !Forever().IsForever() {
		t.Error("Forever() is not forever")
	}

	if Now().IsForever() {
		t.Error("Now() reported itself as the open-end sentinel")
	}

	if !Beginning().Before(Now()) {
		t.Error("Beginning() should precede now")
	}

	if !Now().Before(Forever()) {
		t.Error("now should precede Forever()")
	}

	if got := Forever().String(); got != "forever" {
		t.Errorf("Forever().String() = %q, want %q", got, "forever")
	}
}

// The zero Instant means "unset", which is a different thing from an open
// bound and is almost always a bug. Interval.Validate depends on the two being
// distinguishable.
func TestZeroInstantIsDistinctFromBeginning(t *testing.T) {
	t.Parallel()

	var zero Instant

	if !zero.IsZero() {
		t.Error("the zero Instant does not report itself as zero")
	}

	if Beginning().IsZero() {
		t.Error("Beginning() reports itself as the zero Instant; an unset " +
			"bound and an open bound must stay distinguishable")
	}
}

// Half-open intervals tile: a fact valid until 09:00 and its successor valid
// from 09:00 meet exactly, and a query at 09:00 matches exactly one.
func TestHalfOpenIntervalsTileWithoutGapOrOverlap(t *testing.T) {
	t.Parallel()

	boundary := At(time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC))

	earlier, err := From(At(time.Date(2026, 3, 14, 8, 0, 0, 0, time.UTC)), boundary)
	if err != nil {
		t.Fatalf("From: %v", err)
	}

	later := StartingAt(boundary)

	if earlier.Contains(boundary) {
		t.Error("the earlier interval contains its own exclusive end")
	}

	if !later.Contains(boundary) {
		t.Error("the later interval does not contain its inclusive start")
	}

	if earlier.Overlaps(later) {
		t.Error("adjacent intervals reported as overlapping")
	}
}

func TestIntervalContains(t *testing.T) {
	t.Parallel()

	start := At(time.Date(2026, 3, 14, 8, 0, 0, 0, time.UTC))
	end := At(time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC))

	iv, err := From(start, end)
	if err != nil {
		t.Fatalf("From: %v", err)
	}

	tests := []struct {
		name string
		at   Instant
		want bool
	}{
		{"before the start", start.Add(-time.Second), false},
		{"exactly the start", start, true},
		{"inside", start.Add(time.Hour), true},
		{"one microsecond before the end", end.Add(-Resolution), true},
		{"exactly the end", end, false},
		{"after the end", end.Add(time.Second), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := iv.Contains(tt.at); got != tt.want {
				t.Errorf("Contains(%s) = %v, want %v", tt.at, got, tt.want)
			}
		})
	}
}

func TestIntervalOverlaps(t *testing.T) {
	t.Parallel()

	at := func(h int) Instant {
		return At(time.Date(2026, 3, 14, h, 0, 0, 0, time.UTC))
	}

	mk := func(from, to int) Interval {
		iv, err := From(at(from), at(to))
		if err != nil {
			t.Fatalf("From: %v", err)
		}

		return iv
	}

	tests := []struct {
		name string
		a, b Interval
		want bool
	}{
		{"disjoint", mk(8, 9), mk(10, 11), false},
		{"adjacent", mk(8, 9), mk(9, 10), false},
		{"partial", mk(8, 10), mk(9, 11), true},
		{"contained", mk(8, 12), mk(9, 10), true},
		{"identical", mk(8, 10), mk(8, 10), true},
		{"open end overlaps everything after", StartingAt(at(8)), mk(20, 21), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.a.Overlaps(tt.b); got != tt.want {
				t.Errorf("%s overlaps %s = %v, want %v", tt.a, tt.b, got, tt.want)
			}

			if got := tt.b.Overlaps(tt.a); got != tt.want {
				t.Errorf("overlap is not symmetric for %s and %s", tt.a, tt.b)
			}
		})
	}
}

// A zero-width version can never match any query, so producing one always
// means the interval arithmetic that made it was wrong. Storing it silently
// hides the bug.
func TestValidateRejectsEmptyAndInvertedIntervals(t *testing.T) {
	t.Parallel()

	now := Now()

	tests := []struct {
		name string
		iv   Interval
	}{
		{"empty", Interval{From: now, To: now}},
		{"inverted", Interval{From: now, To: now.Add(-time.Hour)}},
		{"unset start", Interval{To: now}},
		{"unset end", Interval{From: now}},
		{"both unset", Interval{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := tt.iv.Validate(); err == nil {
				t.Errorf("Validate accepted %s", tt.iv)
			}
		})
	}

	if err := Everything().Validate(); err != nil {
		t.Errorf("Everything() should be valid: %v", err)
	}

	if err := StartingAt(now).Validate(); err != nil {
		t.Errorf("StartingAt(now) should be valid: %v", err)
	}
}

func TestIsOpen(t *testing.T) {
	t.Parallel()

	if !StartingAt(Now()).IsOpen() {
		t.Error("an interval ending at Forever should be open")
	}

	closed, err := From(Now(), Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("From: %v", err)
	}

	if closed.IsOpen() {
		t.Error("a closed interval reported itself as open")
	}
}
