package conductor

import (
	"fmt"
	"slices"
	"strings"

	hid "github.com/xraph/blackstart/extensions/hazard/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Basis is how a branch came to be assigned the conductor it has.
//
// It is on the row rather than inferred, because a rating conditioned on a
// surveyed conductor and one conditioned on a guess are different statements,
// and a consumer that cannot tell them apart will treat both as fact.
type Basis string

// The bases. Immutable once shipped: assignments store them.
const (
	// Surveyed means somebody looked. The conductor is what the line is
	// actually strung with.
	Surveyed Basis = "surveyed"

	// AssumedByVoltage means nobody looked, and [AssumeByVoltage] picked a
	// conductor typical of the line's voltage class.
	//
	// Ratings built on this are indicative. The whole point of recording it is
	// that a hazard result can say so.
	AssumedByVoltage Basis = "assumed_by_voltage"
)

// Bases returns every basis, in a stable order.
func Bases() []Basis { return []Basis{Surveyed, AssumedByVoltage} }

// Valid reports whether b is one of the declared bases.
func (b Basis) Valid() bool { return slices.Contains(Bases(), b) }

// String returns the basis as a plain string.
func (b Basis) String() string { return string(b) }

// Assignment binds one branch to one conductor.
//
// Its own record rather than a column on the branch, because the binding
// carries a fact belonging to neither end: how confident this platform is that
// the branch really is that conductor.
//
// One assignment per branch, not per span. A line is strung with one conductor
// end to end; where it is not, the second stringing is a different branch in
// the network model.
type Assignment struct {
	// ID names this assignment.
	ID id.ID `json:"id"`

	// BranchID is the electrical branch, matching geo.Route's BranchID — which
	// is how a span reaches its conductor: span to route, route to branch,
	// branch to here.
	BranchID id.ID `json:"branch_id"`

	// ConductorCode is a [Conductor.Code]. A code rather than an embedded
	// conductor, so that correcting a library figure corrects every rating
	// built on it rather than leaving stale copies on a thousand rows.
	ConductorCode string `json:"conductor_code"`

	// Basis is how this assignment was arrived at.
	Basis Basis `json:"basis"`

	// Note is free text for whoever surveyed it: a work order, a date, a
	// caveat. Never parsed.
	Note string `json:"note,omitempty"`
}

// Validate reports whether an assignment is usable.
//
// The conductor code is checked against the library here rather than at compute
// time, because a typo caught at assignment names the row that has it, while
// the same typo caught during a rating pass names a span three joins away.
func (a Assignment) Validate() error {
	if a.ID.IsZero() {
		return fmt.Errorf("%w: an assignment needs an identifier", bserr.ErrInvalid)
	}

	if got := a.ID.Prefix(); got != hid.Assignment {
		return fmt.Errorf(
			"%w: assignment identifier %s has prefix %q, not %q",
			bserr.ErrInvalid, a.ID, got, hid.Assignment)
	}

	if a.BranchID.IsZero() {
		return fmt.Errorf(
			"%w: assignment %s names no branch, so nothing can find it",
			bserr.ErrInvalid, a.ID)
	}

	if strings.TrimSpace(a.ConductorCode) == "" {
		return fmt.Errorf(
			"%w: assignment %s names no conductor", bserr.ErrInvalid, a.ID)
	}

	if _, err := ByCode(a.ConductorCode); err != nil {
		return fmt.Errorf("%w: assignment %s: %w", bserr.ErrInvalid, a.ID, err)
	}

	if !a.Basis.Valid() {
		return fmt.Errorf(
			"%w: assignment %s has basis %q; it must be one of %v",
			bserr.ErrInvalid, a.ID, a.Basis, Bases())
	}

	return nil
}

// Voltage thresholds for [AssumeByVoltage], in kilovolts.
//
// The classes are the usual transmission split: sub-transmission and the lower
// bulk voltages, the 230 kV class, and extra-high voltage.
const (
	drakeFromKV    = 200
	cardinalFromKV = 300
)

// AssumeByVoltage picks a conductor typical of a line's voltage class.
//
// This is a guess, and callers must record it as one — see [AssumedByVoltage].
// It exists because the alternative is no rating at all: the network model
// carries voltage for every branch and conductor type for none, and a platform
// that refused to rate anything unsurveyed would rate nothing.
//
// The defaults are defensible starting points from typical transmission
// practice, not authority. They deserve a second opinion from somebody who
// strings line for a living.
//
// # Errors
//
// Returns an error wrapping bserr.ErrInvalid when kv is not positive.
func AssumeByVoltage(kv float64) (string, error) {
	if kv <= 0 {
		return "", fmt.Errorf(
			"%w: %v kV is not a voltage class", bserr.ErrInvalid, kv)
	}

	switch {
	case kv < drakeFromKV:
		return "hawk", nil
	case kv < cardinalFromKV:
		return "drake", nil
	default:
		return "cardinal", nil
	}
}
