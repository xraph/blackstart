package fetch

import (
	"fmt"
	"strings"
	"time"

	"github.com/xraph/blackstart/extensions/weather/model"
	"github.com/xraph/blackstart/pkg/bserr"
)

// Cadence is how often a model publishes, in hours.
//
// HRRR runs every hour; GFS runs at 00, 06, 12 and 18Z. Everything about
// enumerating cycles falls out of these two numbers.
func Cadence(m model.Model) (time.Duration, error) {
	switch m {
	case model.HRRR:
		return time.Hour, nil
	case model.GFS:
		return 6 * time.Hour, nil
	default:
		return 0, fmt.Errorf(
			"%w: %q is not a model this platform knows", bserr.ErrInvalid, m)
	}
}

// IsCycle reports whether an instant is one at which a model publishes.
//
// Truncated to the hour first: a cycle is identified by its hour, and an
// issuance carrying minutes is a caller's rounding error rather than a
// different run.
func IsCycle(m model.Model, at time.Time) bool {
	cadence, err := Cadence(m)
	if err != nil {
		return false
	}

	at = at.UTC()

	if at.Minute() != 0 || at.Second() != 0 || at.Nanosecond() != 0 {
		return false
	}

	return time.Duration(at.Hour())*time.Hour%cadence == 0
}

// LatestCycle returns the most recent issuance at or before an instant.
//
// The most recent *issuance*, which is not the most recent *published*
// product — a model runs for the better part of an hour before its output
// appears. A caller walking backwards from here will find the newest few
// unpublished, which is exactly what ErrNotPublished is for.
func LatestCycle(m model.Model, at time.Time) (time.Time, error) {
	cadence, err := Cadence(m)
	if err != nil {
		return time.Time{}, err
	}

	at = at.UTC().Truncate(time.Hour)

	for time.Duration(at.Hour())*time.Hour%cadence != 0 {
		at = at.Add(-time.Hour)
	}

	return at, nil
}

// Cycles enumerates a model's issuances in [from, to], newest first.
//
// Newest first because that is the order a backfill wants: the current
// forecast matters now, and the older ones are filling a record. Reversing it
// at the call site would be easy to forget in exactly the pass where order is
// the point.
func Cycles(m model.Model, from, to time.Time) ([]time.Time, error) {
	cadence, err := Cadence(m)
	if err != nil {
		return nil, err
	}

	latest, err := LatestCycle(m, to)
	if err != nil {
		return nil, err
	}

	var out []time.Time

	for at := latest; !at.Before(from.UTC()); at = at.Add(-cadence) {
		out = append(out, at)
	}

	return out, nil
}

// IndexURL is where a product's sidecar lives.
//
// Both publishers use the same convention: the data file's name with .idx
// appended.
func IndexURL(productURL string) string { return productURL + ".idx" }

// ProductURL composes where one forecast hour of one cycle lives.
//
// The path shapes live here rather than in a substitution template on the feed
// row. A template in a database column is a small language with no tests, no
// type checking and no way to fail before an unattended worker fetches
// something wrong — and the shapes change about as often as the models do.
//
// # Errors
//
// Returns an error for a model this does not know, an issuance that is not one
// of that model's cycles, or a negative forecast hour.
func ProductURL(
	feedURL string, m model.Model, issuedAt time.Time, hour int,
) (string, error) {
	if hour < 0 {
		return "", fmt.Errorf(
			"%w: forecast hour %d; a forecast cannot precede its own run",
			bserr.ErrInvalid, hour)
	}

	issuedAt = issuedAt.UTC()

	if !IsCycle(m, issuedAt) {
		return "", fmt.Errorf(
			"%w: %s publishes no cycle at %s", bserr.ErrInvalid, m,
			issuedAt.Format(time.RFC3339))
	}

	root := strings.TrimSuffix(feedURL, "/")
	day := issuedAt.Format("20060102")
	run := issuedAt.Format("15")

	switch m {
	case model.HRRR:
		// hrrr.20260803/conus/hrrr.t12z.wrfsfcf01.grib2
		return fmt.Sprintf("%s/hrrr.%s/conus/hrrr.t%sz.wrfsfcf%02d.grib2",
			root, day, run, hour), nil

	case model.GFS:
		// gfs.20260803/12/atmos/gfs.t12z.pgrb2.0p25.f001
		return fmt.Sprintf("%s/gfs.%s/%s/atmos/gfs.t%sz.pgrb2.0p25.f%03d",
			root, day, run, run, hour), nil

	default:
		return "", fmt.Errorf(
			"%w: %q is not a model this platform knows", bserr.ErrInvalid, m)
	}
}
