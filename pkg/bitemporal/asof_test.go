package bitemporal

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestConstructors(t *testing.T) {
	t.Parallel()

	storm := At(time.Date(2026, 2, 14, 3, 0, 0, 0, time.UTC))
	predictedAt := At(time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC))

	if got := Current(); !got.ValidAt.Equal(got.KnownAt) {
		t.Error("Current() should fix both axes at the same moment")
	}

	if got := Snapshot(storm); !got.ValidAt.Equal(storm) || !got.KnownAt.Equal(storm) {
		t.Errorf("Snapshot(%s) = %s", storm, got)
	}

	historical := Historical(storm)
	if !historical.ValidAt.Equal(storm) {
		t.Errorf("Historical kept the wrong valid time: %s", historical)
	}

	if !historical.KnownAt.After(storm) {
		t.Error("Historical should read with present knowledge")
	}

	pit := PointInTime(storm, predictedAt)
	if !pit.ValidAt.Equal(storm) || !pit.KnownAt.Equal(predictedAt) {
		t.Errorf("PointInTime = %s", pit)
	}
}

// The distinction the package exists for. A report may look back with present
// knowledge; a model score may not.
func TestIsLeakFree(t *testing.T) {
	t.Parallel()

	storm := At(time.Date(2026, 2, 14, 3, 0, 0, 0, time.UTC))
	before := storm.Add(-24 * time.Hour)
	after := storm.Add(7 * 24 * time.Hour)

	tests := []struct {
		name string
		asOf AsOf
		want bool
	}{
		{
			name: "prediction made before the event cannot see it",
			asOf: PointInTime(storm, before),
			want: true,
		},
		{
			name: "knowledge fixed at the event itself",
			asOf: Snapshot(storm),
			want: true,
		},
		{
			name: "reading a past event with present knowledge leaks",
			asOf: PointInTime(storm, after),
			want: false,
		},
		{
			name: "Historical always leaks, by construction",
			asOf: Historical(storm),
			want: false,
		},
		{
			name: "Current does not leak, since both axes are now",
			asOf: Current(),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.asOf.IsLeakFree(); got != tt.want {
				t.Errorf("IsLeakFree() = %v, want %v for %s", got, tt.want, tt.asOf)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	if err := Current().Validate(); err != nil {
		t.Errorf("Current() should be valid: %v", err)
	}

	for _, bad := range []AsOf{
		{},
		{ValidAt: Now()},
		{KnownAt: Now()},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("Validate accepted %s", bad)
		}
	}
}

func TestContextRoundTrip(t *testing.T) {
	t.Parallel()

	want := PointInTime(
		At(time.Date(2026, 2, 14, 3, 0, 0, 0, time.UTC)),
		At(time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)),
	)

	ctx, err := WithAsOf(t.Context(), want)
	if err != nil {
		t.Fatalf("WithAsOf: %v", err)
	}

	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("the temporal context did not survive the round trip")
	}

	if !got.ValidAt.Equal(want.ValidAt) || !got.KnownAt.Equal(want.KnownAt) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestWithAsOfRejectsAnIncompleteValue(t *testing.T) {
	t.Parallel()

	if _, err := WithAsOf(t.Context(), AsOf{ValidAt: Now()}); err == nil {
		t.Error("attached a temporal context with no knowledge time")
	}
}

// An unattached context means ordinary operation — a health check, a startup
// task — and defaulting is better than forcing those callers to invent a
// temporal context they have no opinion about.
func TestRequireDefaultsToCurrent(t *testing.T) {
	t.Parallel()

	got := Require(t.Context())

	if got.ValidAt.IsZero() || got.KnownAt.IsZero() {
		t.Fatalf("Require returned an incomplete default: %s", got)
	}

	if !got.IsLeakFree() {
		t.Error("the default temporal context should not leak")
	}
}

func TestRequireReturnsTheAttachedValue(t *testing.T) {
	t.Parallel()

	want := PointInTime(
		At(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)),
		At(time.Date(2019, 12, 31, 0, 0, 0, 0, time.UTC)),
	)

	ctx := MustWithAsOf(t.Context(), want)

	got := Require(ctx)
	if !got.ValidAt.Equal(want.ValidAt) || !got.KnownAt.Equal(want.KnownAt) {
		t.Errorf("Require = %s, want %s", got, want)
	}
}

// A nested boundary may narrow the temporal context — a backtest harness
// setting it per training example inside a request that set its own.
func TestInnerContextOverridesOuter(t *testing.T) {
	t.Parallel()

	outer := MustWithAsOf(t.Context(), Current())

	inner := PointInTime(
		At(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)),
		At(time.Date(2019, 12, 31, 0, 0, 0, 0, time.UTC)),
	)

	got := Require(MustWithAsOf(outer, inner))
	if !got.KnownAt.Equal(inner.KnownAt) {
		t.Errorf("inner temporal context did not take effect: %s", got)
	}
}

func TestFromContextReportsAbsence(t *testing.T) {
	t.Parallel()

	if _, ok := FromContext(context.Background()); ok {
		t.Error("found a temporal context in a bare background context")
	}
}

// The CLI and the HTTP API accept the same pair of strings and must interpret
// them identically. Two parsers would be two chances to disagree about what an
// empty knownAt means, and the disagreement would stay invisible until someone
// compared a report against a command.
func TestParseAsOfReadsBothAxes(t *testing.T) {
	// Neither given is the present, and the present is leak-free by
	// construction: there is no knowledge to be missing.
	present, err := ParseAsOf("", "")
	if err != nil {
		t.Fatalf("ParseAsOf: %v", err)
	}

	if !present.IsLeakFree() {
		t.Error("a read of the present was reported as leaking knowledge")
	}

	// Both given fixes both axes. Whether it leaks depends on their order, and
	// that ordering is the whole point of having two: knowledge at or before
	// the world state cannot see later corrections, which is what a backtest
	// requires.
	backtest, err := ParseAsOf("2024-02-01T00:00:00Z", "2024-01-15T09:30:00Z")
	if err != nil {
		t.Fatalf("ParseAsOf: %v", err)
	}

	if !backtest.IsLeakFree() {
		t.Error("knowledge earlier than the world state was reported as leaking")
	}

	// The reverse is legitimate for a report — "what do we now know about last
	// January" — and must be reported as leaking, so it is never used to score
	// a model.
	report, err := ParseAsOf("2024-01-15T09:30:00Z", "2024-02-01T00:00:00Z")
	if err != nil {
		t.Fatalf("ParseAsOf: %v", err)
	}

	if report.IsLeakFree() {
		t.Error("knowledge later than the world state was reported as leak-free")
	}
}

// Only validAt is a historical read with *present* knowledge — it includes
// corrections filed since, so it is not reproducible. That has to be
// detectable, or a caller will quote it as though it were.
func TestAValidAtWithoutAKnownAtLeaksPresentKnowledge(t *testing.T) {
	asOf, err := ParseAsOf("2024-01-15T09:30:00Z", "")
	if err != nil {
		t.Fatalf("ParseAsOf: %v", err)
	}

	if asOf.IsLeakFree() {
		t.Error("reading the past with present knowledge was reported as leak-free")
	}
}

// The message has to say what a valid time looks like. "parsing error" sends
// the reader to the source; an example tells them what to type.
func TestAMalformedTimeSaysWhatIsAccepted(t *testing.T) {
	for _, tc := range []struct{ validAt, knownAt string }{
		{"yesterday", ""},
		{"", "yesterday"},
		{"2024-01-15", ""},
	} {
		_, err := ParseAsOf(tc.validAt, tc.knownAt)
		if err == nil {
			t.Fatalf("ParseAsOf accepted %q / %q", tc.validAt, tc.knownAt)
		}

		if !strings.Contains(err.Error(), "RFC 3339") {
			t.Errorf("message %q does not say what format is accepted", err)
		}

		if !strings.Contains(err.Error(), "2024-01-15T09:30:00Z") {
			t.Errorf("message %q gives no example", err)
		}
	}
}

// The failing field has to be named. With two time parameters, "parsing error"
// leaves the caller guessing which one they got wrong.
func TestTheRejectionNamesTheFieldThatFailed(t *testing.T) {
	_, err := ParseAsOf("nonsense", "")
	if err == nil || !strings.Contains(err.Error(), "validAt") {
		t.Errorf("got %v, want it to name validAt", err)
	}

	_, err = ParseAsOf("", "nonsense")
	if err == nil || !strings.Contains(err.Error(), "knownAt") {
		t.Errorf("got %v, want it to name knownAt", err)
	}
}
