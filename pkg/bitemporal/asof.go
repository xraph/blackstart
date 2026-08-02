package bitemporal

import (
	"context"
	"fmt"
)

// AsOf fixes both time axes for a read.
//
// ValidAt selects the state of the world; KnownAt selects which version of
// that state the system had recorded. The pair answers "what did we believe,
// at time K, about how things were at time V" — which is the only question a
// backtest may honestly ask.
type AsOf struct {
	// ValidAt is the moment in the physical world being asked about.
	ValidAt Instant

	// KnownAt is the moment of knowledge. Facts recorded after this are
	// invisible, including corrections to facts recorded before it.
	KnownAt Instant
}

// Current reads the present state according to everything known now. This is
// the default for ordinary operation.
func Current() AsOf {
	now := Now()

	return AsOf{ValidAt: now, KnownAt: now}
}

// Historical reads the state of the world at validAt, according to everything
// known now — including corrections that arrived later.
//
// This is the right choice for a report ("what actually happened during the
// February storm") and the wrong choice for evaluating a model, because the
// corrections were not available when the model ran.
func Historical(validAt Instant) AsOf {
	return AsOf{ValidAt: validAt, KnownAt: Now()}
}

// PointInTime reads the world at validAt using only what was known at knownAt.
//
// This is the constructor a backtest uses, with knownAt set to the moment the
// prediction would have been made. It is the whole reason this package exists:
// a model scored with PointInTime cannot see the amended outage report or the
// revised forecast, because those rows are excluded by the predicate rather
// than filtered out by discipline.
func PointInTime(validAt, knownAt Instant) AsOf {
	return AsOf{ValidAt: validAt, KnownAt: knownAt}
}

// Snapshot reads a single moment on both axes: the world as it was at t,
// according to what was known at t.
func Snapshot(t Instant) AsOf {
	return AsOf{ValidAt: t, KnownAt: t}
}

// Validate reports whether both bounds are set.
func (a AsOf) Validate() error {
	if a.ValidAt.IsZero() || a.KnownAt.IsZero() {
		return fmt.Errorf(
			"bitemporal: both axes must be set, got valid=%s known=%s",
			a.ValidAt, a.KnownAt)
	}

	return nil
}

// IsLeakFree reports whether this read can see facts recorded after the world
// state it asks about.
//
// A read with KnownAt at or before ValidAt cannot: nothing recorded after the
// moment of interest is visible. A read with KnownAt after ValidAt can, and is
// perfectly legitimate for a report — it just must not be used to score a
// model. The backtest harness asserts this.
func (a AsOf) IsLeakFree() bool {
	return !a.KnownAt.After(a.ValidAt)
}

// String renders the pair for logs and error messages.
func (a AsOf) String() string {
	return "valid=" + a.ValidAt.String() + " known=" + a.KnownAt.String()
}

type contextKey struct{}

// WithAsOf attaches a temporal context.
//
// The temporal context travels in the context rather than in every store
// method signature. The alternative — a List and a ListAsOf on every store —
// looks more explicit and is worse in practice: the plain variant becomes the
// path of least resistance, and a single forgotten call site silently reads
// the present in the middle of a backtest. Carrying it in the context means
// every read below a boundary inherits it, and the boundaries that set it are
// few and reviewable: HTTP handlers from query parameters, bsctl from flags,
// and the backtest harness per training example.
func WithAsOf(ctx context.Context, a AsOf) (context.Context, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}

	return context.WithValue(ctx, contextKey{}, a), nil
}

// MustWithAsOf is WithAsOf for constructed values that cannot be invalid, such
// as those from Current or Snapshot.
func MustWithAsOf(ctx context.Context, a AsOf) context.Context {
	out, err := WithAsOf(ctx, a)
	if err != nil {
		panic(err)
	}

	return out
}

// FromContext returns the attached temporal context, if any.
func FromContext(ctx context.Context) (AsOf, bool) {
	a, ok := ctx.Value(contextKey{}).(AsOf)

	return a, ok
}

// Require returns the attached temporal context, defaulting to Current.
//
// Defaulting rather than failing is deliberate. An unattached context means
// ordinary operation — a health check, a startup task, an admin query — and
// making those fail would push callers into attaching a temporal context they
// have no opinion about, which is worse than the default. The paths where
// getting this wrong matters are backtests, and those set it explicitly and
// assert it with IsLeakFree.
func Require(ctx context.Context) AsOf {
	if a, ok := FromContext(ctx); ok {
		return a
	}

	return Current()
}
