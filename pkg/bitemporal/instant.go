// Package bitemporal gives every fact two time axes.
//
//   - Valid time is when the fact was true of the physical world. A breaker
//     opened at 03:14; a conductor rating applied for the summer season.
//   - Knowledge time is when this system learned it. The outage report arrived
//     at 09:00; the corrected report arrived a week later.
//
// Keeping both is what makes a backtest mean anything. Outage records are
// amended after the fact and weather forecasts are revised, so a model
// evaluated against "the data for last March" is usually evaluated against a
// tidied-up version nobody had at the time. The published skill is then
// inflated, and nothing in the result shows it.
//
// A query fixes both axes. AsOf(validAt, knownAt) reads the world as it was at
// validAt, according to what was known at knownAt. Setting knownAt to the
// moment a prediction was made makes leakage structurally impossible rather
// than merely discouraged.
//
// Neither Grove nor Fabriq offers anything here — no AsOf, no valid-time or
// knowledge-time support of any kind. This package and the history tables it
// describes are the whole mechanism.
package bitemporal

import (
	"fmt"
	"time"
)

// Instant is a point on either time axis.
//
// It is always UTC and always truncated to microseconds, because PostgreSQL's
// timestamptz has microsecond resolution. Go's time.Time carries nanoseconds,
// so an untruncated value written and read back is not the value that was
// written — and comparisons against a stored interval would then fail for
// reasons invisible in the source.
type Instant struct {
	t time.Time
}

// Resolution is the granularity of an Instant, matching PostgreSQL timestamptz.
const Resolution = time.Microsecond

// forever is the end sentinel: the largest timestamp PostgreSQL round-trips
// cleanly, minus nothing. Chosen over NULL deliberately — see Forever.
var foreverTime = time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)

// beginningTime is the start sentinel, for facts whose origin predates any
// record the system holds.
//
// It is 1800 rather than Go's zero time, and that is load-bearing: the zero
// time is what an *unset* bound looks like, and Validate rejects those. If
// Beginning were also the zero time, a struct field somebody forgot to
// populate would be indistinguishable from a deliberately open start, and the
// check that exists to catch the first would silently accept it.
//
// 1800 is chosen over an arbitrary offset from the zero value because it reads
// as a decision rather than as a trick when someone finds it in a database
// dump. Nothing in this domain predates it — the first electrical grids are
// 1880s, and the oldest datasets here begin in the twentieth century.
var beginningTime = time.Date(1800, 1, 1, 0, 0, 0, 0, time.UTC)

// At builds an Instant from a time, converting to UTC and truncating to
// microseconds.
func At(t time.Time) Instant {
	return Instant{t: t.UTC().Truncate(Resolution)}
}

// Now returns the current instant.
func Now() Instant { return At(time.Now()) }

// Forever is the open end of an interval that has not closed.
//
// Nullable end times are the classic bitemporal footgun: every predicate
// acquires an "OR end IS NULL" branch, indexes stop being usable for range
// scans, and the exclusion constraint that prevents overlapping versions
// cannot be expressed at all. A sentinel costs one magic value and removes all
// three problems.
func Forever() Instant { return Instant{t: foreverTime} }

// Beginning is the open start of an interval, for facts that were already true
// before the system knew anything.
func Beginning() Instant { return Instant{t: beginningTime} }

// Time returns the underlying time.
func (i Instant) Time() time.Time { return i.t }

// IsZero reports whether this is the zero Instant, which is distinct from
// Beginning and is almost always a mistake — an unset field rather than a
// deliberate open start.
func (i Instant) IsZero() bool { return i.t.IsZero() }

// IsForever reports whether this is the open end sentinel.
func (i Instant) IsForever() bool { return i.t.Equal(foreverTime) }

// Before, After and Equal compare two instants.
func (i Instant) Before(other Instant) bool { return i.t.Before(other.t) }
func (i Instant) After(other Instant) bool  { return i.t.After(other.t) }
func (i Instant) Equal(other Instant) bool  { return i.t.Equal(other.t) }

// Compare returns -1, 0 or 1 as i is before, equal to, or after other.
func (i Instant) Compare(other Instant) int { return i.t.Compare(other.t) }

// Add returns the instant offset by d, truncated to the stored resolution.
func (i Instant) Add(d time.Duration) Instant { return At(i.t.Add(d)) }

// String renders the instant in RFC 3339 with microseconds, or "forever".
func (i Instant) String() string {
	if i.IsForever() {
		return "forever"
	}

	return i.t.Format("2006-01-02T15:04:05.000000Z07:00")
}

// Interval is a half-open range on one time axis: [From, To).
//
// Half-open is what makes adjacent versions tile without overlapping or
// leaving a gap: a fact valid until 09:00 and its successor valid from 09:00
// meet exactly, and a query at 09:00 matches exactly one of them.
type Interval struct {
	From Instant
	To   Instant
}

// Everything is the interval covering all time.
func Everything() Interval {
	return Interval{From: Beginning(), To: Forever()}
}

// From builds a half-open interval.
func From(from, to Instant) (Interval, error) {
	iv := Interval{From: from, To: to}
	if err := iv.Validate(); err != nil {
		return Interval{}, err
	}

	return iv, nil
}

// StartingAt builds an interval that is open at the end.
func StartingAt(from Instant) Interval {
	return Interval{From: from, To: Forever()}
}

// Validate reports whether the interval is well-formed.
//
// An empty interval — From equal to To — is rejected rather than tolerated. A
// zero-width version can never match any query, so its existence always means
// the interval arithmetic that produced it was wrong, and silently storing one
// hides that.
func (iv Interval) Validate() error {
	if iv.From.IsZero() || iv.To.IsZero() {
		return fmt.Errorf(
			"bitemporal: interval bounds must be set; got [%s, %s). "+
				"An unset bound is not an open bound — use Beginning() or Forever()",
			iv.From, iv.To)
	}

	if !iv.From.Before(iv.To) {
		return fmt.Errorf(
			"bitemporal: interval [%s, %s) is empty or inverted; a zero-width "+
				"version can never match a query, so producing one means the "+
				"interval arithmetic was wrong", iv.From, iv.To)
	}

	return nil
}

// Contains reports whether the instant falls inside the half-open interval.
func (iv Interval) Contains(at Instant) bool {
	return !at.Before(iv.From) && at.Before(iv.To)
}

// Overlaps reports whether two intervals share any instant.
func (iv Interval) Overlaps(other Interval) bool {
	return iv.From.Before(other.To) && other.From.Before(iv.To)
}

// IsOpen reports whether the interval has not been closed.
func (iv Interval) IsOpen() bool { return iv.To.IsForever() }

// String renders the interval in mathematical half-open notation.
func (iv Interval) String() string {
	return "[" + iv.From.String() + ", " + iv.To.String() + ")"
}
