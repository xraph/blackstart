// Package store composes the hazard extension's per-domain stores.
//
// Three, split along the same line the weather extension's are: two relational
// stores for the catalog — conductor assignments and computation passes — and
// one series store for the numbers, which lives in a Timescale hypertable that
// fabriq's Timeseries port writes.
//
// The series records live here rather than in a domain package, unlike
// weather's forecast.Sample. A hazard value has no life outside its series:
// nothing computes with a stored SpanValue, nothing validates one against
// physics, and the only thing that reads one is an API turning it back into
// JSON. Giving it a domain package would be a package with one struct in it.
//
// Assignments are the exception and do live in a domain package, because
// conductor.Assignment is consumed by the compute engine as a fact rather than
// as a row.
package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/hazard/conductor"
	hid "github.com/xraph/blackstart/extensions/hazard/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store"
)

// Store is everything the hazard extension persists.
type Store interface {
	store.Lifecycle

	// Assignments reads and writes which conductor each branch is.
	Assignments() AssignmentStore

	// Computations reads and writes passes of the models over a weather cycle.
	Computations() ComputationStore

	// Values reads and writes the numbers.
	Values() ValueStore
}

// Quantity names one derived series.
//
// A closed vocabulary, because these strings are half of every series key and a
// typo would write a series nothing ever reads back.
type Quantity string

// The quantities.
const (
	// QuantityWindLoad is transverse drag, in newtons per meter.
	QuantityWindLoad Quantity = "wind_load"

	// QuantityIceThickness is equivalent radial ice, in meters.
	QuantityIceThickness Quantity = "ice_thickness"

	// QuantityIceLoad is accreted ice, in kilograms per meter.
	QuantityIceLoad Quantity = "ice_load"

	// QuantityCombinedLoad is the resultant of drag and ice weight, in newtons
	// per meter.
	QuantityCombinedLoad Quantity = "combined_load"

	// QuantityAmpacity is the thermal rating, in amperes.
	QuantityAmpacity Quantity = "ampacity"
)

// Quantities returns every quantity, in a stable order.
func Quantities() []Quantity {
	return []Quantity{
		QuantityWindLoad,
		QuantityIceThickness,
		QuantityIceLoad,
		QuantityCombinedLoad,
		QuantityAmpacity,
	}
}

// Valid reports whether this is a quantity the extension computes.
func (q Quantity) Valid() bool { return slices.Contains(Quantities(), q) }

// String returns the quantity as a plain string.
func (q Quantity) String() string { return string(q) }

// SpanValue is one derived number, for one span, at one moment, from one
// forecast issuance.
//
// Both time axes, for the reason forecast.Sample carries both: a rating derived
// from a forecast inherits the forecast's knowledge time, and a hazard series
// scored against values revised after the fact has seen the future.
type SpanValue struct {
	// SpanID is the span it describes.
	SpanID id.ID `json:"span_id"`

	// Quantity is which series.
	Quantity Quantity `json:"quantity"`

	// IssuedAt is when the weather this was computed from was issued: the
	// knowledge-time axis.
	IssuedAt time.Time `json:"issued_at"`

	// ValidAt is the moment the value describes.
	ValidAt time.Time `json:"valid_at"`

	// Value is the number, in the quantity's own unit.
	Value float64 `json:"value"`
}

// Validate reports whether a value is storable.
func (v SpanValue) Validate() error {
	if _, err := gid.ParseSpan(v.SpanID.String()); err != nil {
		return fmt.Errorf("%w: %w", bserr.ErrInvalid, err)
	}

	if !v.Quantity.Valid() {
		return fmt.Errorf(
			"%w: quantity %q is not one of %v", bserr.ErrInvalid, v.Quantity, Quantities())
	}

	if v.IssuedAt.IsZero() || v.ValidAt.IsZero() {
		return fmt.Errorf(
			"%w: %s value for %s has issuance %v and valid time %v; both axes are "+
				"required", bserr.ErrInvalid, v.Quantity, v.SpanID, v.IssuedAt, v.ValidAt)
	}

	return nil
}

// Key returns the series key this value belongs to.
func (v SpanValue) Key() string { return ValueKey(v.SpanID, v.Quantity, v.IssuedAt) }

// Series key format. The same delimiter and issuance layout
// forecast.SampleKey uses, so that one convention spans both series and a
// reader of either can parse the other by eye.
const (
	keyDelimiter = "|"
	keyParts     = 3

	// IssuanceLayout formats the issuance inside a series key. Minute
	// precision, because no model this platform reads issues more often.
	IssuanceLayout = "20060102T1504Z"
)

// ValueKey composes the series key a value is stored under.
func ValueKey(spanID id.ID, q Quantity, issuedAt time.Time) string {
	return strings.Join([]string{
		spanID.String(),
		string(q),
		issuedAt.UTC().Format(IssuanceLayout),
	}, keyDelimiter)
}

// ParseValueKey takes a series key apart.
//
// # Errors
//
// Returns an error wrapping bserr.ErrInvalid when the key has the wrong shape,
// does not begin with a span identifier, names a quantity outside the
// vocabulary, or carries an issuance that is not in IssuanceLayout.
func ParseValueKey(key string) (id.ID, Quantity, time.Time, error) {
	piece := strings.Split(key, keyDelimiter)
	if len(piece) != keyParts {
		return id.Nil, "", time.Time{}, fmt.Errorf(
			"%w: series key %q has %d parts, want %d (span, quantity, issuance)",
			bserr.ErrInvalid, key, len(piece), keyParts)
	}

	spanID, err := gid.ParseSpan(piece[0])
	if err != nil {
		return id.Nil, "", time.Time{}, fmt.Errorf(
			"%w: series key %q does not begin with a span identifier: %w",
			bserr.ErrInvalid, key, err)
	}

	quantity := Quantity(piece[1])
	if !quantity.Valid() {
		return id.Nil, "", time.Time{}, fmt.Errorf(
			"%w: series key %q names quantity %q, which is not one of %v",
			bserr.ErrInvalid, key, piece[1], Quantities())
	}

	issuedAt, err := time.Parse(IssuanceLayout, piece[2])
	if err != nil {
		return id.Nil, "", time.Time{}, fmt.Errorf(
			"%w: series key %q carries issuance %q, which is not %s: %w",
			bserr.ErrInvalid, key, piece[2], IssuanceLayout, err)
	}

	return spanID, quantity, issuedAt, nil
}

// AssignmentStore holds which conductor each branch is strung with.
type AssignmentStore interface {
	// Put writes an assignment, replacing any the branch already had.
	//
	// Replace rather than create, because a survey correcting an assumption is
	// the normal case and the second write is the whole point. One assignment
	// per branch: two would leave nothing to say which one a rating used.
	Put(ctx context.Context, in conductor.Assignment) error

	// Get reads one assignment.
	Get(ctx context.Context, want id.ID) (conductor.Assignment, error)

	// ForBranch finds the assignment of a branch.
	//
	// The lookup the compute engine makes once per span, by way of the span's
	// route. Its absence is not an error the engine reports — it is a span
	// counted unrated, which is why this returns bserr.ErrNotFound rather than
	// an empty assignment.
	ForBranch(ctx context.Context, branch id.ID) (conductor.Assignment, error)

	// List returns assignments.
	List(ctx context.Context, p store.Page) ([]conductor.Assignment, error)

	// Delete removes an assignment.
	//
	// A branch with no assignment is a branch with no rating, which is a
	// meaningful state: better an absent rating than one conditioned on a
	// conductor somebody has decided is wrong.
	Delete(ctx context.Context, want id.ID) error
}

// Computation is one pass of the hazard models over one weather cycle.
//
// The row that makes a rating traceable: it names the cycle the weather came
// from, the provenance run that recorded the model version and dataset
// versions, and how much of the network the pass actually covered.
type Computation struct {
	// ID identifies this pass.
	ID id.ID `json:"id"`

	// CycleID names the weather cycle the samples were read from.
	CycleID id.ID `json:"cycle_id"`

	// RunID names the provenance run this pass recorded.
	//
	// The link that turns "which rating did this sweep use, and what weather
	// was it conditioned on" into a query rather than an investigation.
	RunID id.ID `json:"run_id"`

	// ComputedAt is when the pass ran.
	ComputedAt time.Time `json:"computed_at"`

	// Spans is how many spans produced values, and Unrated how many were
	// skipped for want of a conductor assignment.
	//
	// Both counts, not a ratio. A pass that rated nothing because no branch is
	// assigned and a pass that rated nothing because the cycle was empty are
	// different failures, and one number cannot tell them apart.
	Spans   int `json:"spans"`
	Unrated int `json:"unrated"`

	// Values is how many numbers were written.
	Values int `json:"values"`
}

// Validate reports whether a computation is storable.
func (c Computation) Validate() error {
	if c.ID.IsZero() || c.ID.Prefix() != hid.Computation {
		return fmt.Errorf(
			"%w: computation identifier %v is not a %q", bserr.ErrInvalid, c.ID, hid.Computation)
	}

	if c.CycleID.IsZero() {
		return fmt.Errorf(
			"%w: computation %s names no weather cycle, so nothing it produced "+
				"can be traced to a forecast", bserr.ErrInvalid, c.ID)
	}

	if c.ComputedAt.IsZero() {
		return fmt.Errorf("%w: computation %s has no time", bserr.ErrInvalid, c.ID)
	}

	if c.Spans < 0 || c.Unrated < 0 || c.Values < 0 {
		return fmt.Errorf(
			"%w: computation %s counted %d spans, %d unrated and %d values",
			bserr.ErrInvalid, c.ID, c.Spans, c.Unrated, c.Values)
	}

	return nil
}

// ComputationStore holds passes of the models.
type ComputationStore interface {
	// Create writes a new computation.
	Create(ctx context.Context, in Computation) error

	// Get reads one computation.
	Get(ctx context.Context, want id.ID) (Computation, error)

	// ForCycle reads every computation of a weather cycle, newest first.
	//
	// More than one is normal: a cycle re-computed after a conductor assignment
	// is corrected produces a second pass, and both are kept so that a rating
	// already consumed downstream still has the computation that made it.
	ForCycle(ctx context.Context, cycle id.ID) ([]Computation, error)

	// List returns computations, newest first.
	List(ctx context.Context, p store.Page) ([]Computation, error)
}

// ValueStore holds the numbers.
//
// Deliberately narrow, and deliberately keyed rather than filtered, for the
// reasons weather's SampleStore is: the backing table is a Timescale
// hypertable, which sits outside row-level security — Timescale's columnstore
// forbids it — and outside fabriq's raw-SQL guard, whose table list is a
// hardcoded constant this extension cannot add to. Tenant isolation there is
// this interface's implementations' own discipline, and keeping the surface
// this small is what makes that reviewable.
type ValueStore interface {
	// Write writes span values in bulk.
	//
	// Through fabriq's Timeseries port, which bypasses the outbox by design:
	// per-row events would melt it. "All entity writes go through fabriq"
	// holds; "all data writes" does not.
	Write(ctx context.Context, values []SpanValue) error

	// Read reads span values for the given series keys, over the half-open
	// window [from, to).
	//
	// Keys rather than a span-and-quantity filter, because the key is the
	// series identity. A caller composes the keys it wants and asks once,
	// rather than issuing one round trip per series.
	Read(ctx context.Context, keys []string, from, to time.Time) ([]SpanValue, error)
}
