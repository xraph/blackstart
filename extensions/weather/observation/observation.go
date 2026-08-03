// Package observation is what a surface station actually measured.
//
// Distinct from a forecast in the one way that matters: an observation has no
// issuance time, because valid time and knowledge time are the same instant.
// A forecast says "at 09:00 we expected 14:00 to look like this"; an
// observation says "at 14:00 it looked like this".
//
// Modelling both as one type with an optional issuance would make that
// difference a nullable column rather than a property of the thing — and a
// nullable issuance is exactly the shape that lets a backtest read an
// observation as though it were a forecast that happened to be perfect.
package observation

import (
	"fmt"
	"strings"
	"time"

	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/weather/field"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Station is a surface observing site.
type Station struct {
	// ID identifies this station within the platform.
	ID id.ID `json:"id"`

	// Code is the publisher's own identifier — "KAUS".
	//
	// Kept rather than discarded once resolved. Reconciliation decisions have
	// to be auditable and reversible, and the source identifier is what makes
	// them so.
	Code string `json:"code"`

	// Name is the human-readable name.
	Name string `json:"name"`

	// Location is where it stands. Indexed through the same spatial port span
	// geometry uses, so "which stations are near this span" is one query
	// rather than a scan.
	Location shape.Point `json:"location"`
}

// Validate reports whether a station can be stored.
func (s Station) Validate() error {
	if s.ID.IsZero() {
		return fmt.Errorf("%w: a station needs an identifier", bserr.ErrInvalid)
	}

	if strings.TrimSpace(s.Code) == "" {
		return fmt.Errorf(
			"%w: station %s has no code; the code is how the observation feed "+
				"names it, and without one its readings cannot be matched to it",
			bserr.ErrInvalid, s.ID)
	}

	if err := s.Location.Validate(); err != nil {
		return fmt.Errorf("%w: station %s: %w", bserr.ErrInvalid, s.ID, err)
	}

	return nil
}

// Observation is one measured value at one station.
type Observation struct {
	// StationID names where.
	StationID id.ID `json:"station_id"`

	// Field names what.
	Field field.Field `json:"field"`

	// ObservedAt is when — both the valid time and the knowledge time.
	ObservedAt time.Time `json:"observed_at"`

	// Value is the magnitude, in the unit this field's spec names.
	Value float64 `json:"value"`
}

// Validate reports whether an observation can be stored.
func (o Observation) Validate() error {
	if o.StationID.IsZero() {
		return fmt.Errorf("%w: an observation needs a station", bserr.ErrInvalid)
	}

	if !o.Field.Valid() {
		return fmt.Errorf(
			"%w: %q is not a field in the vocabulary", bserr.ErrInvalid, o.Field)
	}

	if o.ObservedAt.IsZero() {
		return fmt.Errorf(
			"%w: observation of %q at station %s has no time; for an observation "+
				"that one instant is both when it was true and when it was known",
			bserr.ErrInvalid, o.Field, o.StationID)
	}

	return nil
}

// keyDelimiter separates a series key's parts, matching the forecast series so
// that a reader of either table sees one convention.
const keyDelimiter = "|"

// keyParts is how many pieces an observation series key has.
const keyParts = 2

// SampleKey composes the series key an observation is stored under.
//
// Two parts, not three. An observation carries no issuance, and a key with an
// empty third part would sort and prefix-match differently from one with two —
// which would make the two series behave differently under the same query for
// no reason a reader could see.
func SampleKey(stationID id.ID, f field.Field) string {
	return stationID.String() + keyDelimiter + string(f)
}

// ParseSampleKey takes a series key apart.
//
// The counterpart to SampleKey, and the reason a stored reading can be turned
// back into an Observation: the station and the field live only in the key,
// because the series table stores one key and one float per row.
//
// # Errors
//
// Returns an error wrapping bserr.ErrInvalid when the key has the wrong shape,
// does not begin with a station identifier, or names a field outside the
// vocabulary.
func ParseSampleKey(key string) (id.ID, field.Field, error) {
	piece := strings.Split(key, keyDelimiter)
	if len(piece) != keyParts {
		return id.Nil, "", fmt.Errorf(
			"%w: observation series key %q has %d parts, want %d (station, field)",
			bserr.ErrInvalid, key, len(piece), keyParts)
	}

	stationID, err := wid.ParseStation(piece[0])
	if err != nil {
		return id.Nil, "", fmt.Errorf(
			"%w: observation series key %q does not begin with a station "+
				"identifier: %w", bserr.ErrInvalid, key, err)
	}

	found := field.Field(piece[1])
	if !found.Valid() {
		return id.Nil, "", fmt.Errorf(
			"%w: observation series key %q names field %q, which is not in the "+
				"vocabulary", bserr.ErrInvalid, key, piece[1])
	}

	return stationID, found, nil
}
