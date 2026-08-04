package shape_test

import (
	"math"
	"testing"

	"github.com/xraph/blackstart/extensions/geo/shape"
)

// A digitized right-of-way with a deliberate corner: south-west across the
// South Plains, then a turn to the west. A resampler that straightens this has
// moved the conductor off the ridge it really crosses.
func cornered() shape.Line {
	return shape.Line{
		{Lon: -101.76, Lat: 34.54},
		{Lon: -102.10, Lat: 34.20},
		{Lon: -102.38, Lat: 34.20},
	}
}

func TestResampleKeepsEveryDigitizedVertex(t *testing.T) {
	out, err := shape.Resample(cornered(), 5000)
	if err != nil {
		t.Fatalf("Resample: %v", err)
	}

	// Each input vertex must survive somewhere in the output, or a turn the
	// digitiser recorded has been resampled into a straight line.
	for _, want := range cornered() {
		found := false

		for _, got := range out {
			if math.Abs(got.Lon-want.Lon) < 1e-9 && math.Abs(got.Lat-want.Lat) < 1e-9 {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("vertex %.4f,%.4f was resampled away; the corner is real", want.Lon, want.Lat)
		}
	}
}

func TestResampleNeverExceedsTheNominalSpan(t *testing.T) {
	const nominal = 400.0

	out, err := shape.Resample(cornered(), nominal)
	if err != nil {
		t.Fatalf("Resample: %v", err)
	}

	for index, span := range out.Spans() {
		length := span[0].DistanceTo(span[1])

		// Ceil division is what guarantees this. Rounding to nearest would let
		// a 1.9x segment become one 1.9x span, which is the outcome asking for
		// a nominal was meant to prevent.
		if length > nominal+1 {
			t.Errorf("span %d is %.0f m, over the %.0f m nominal", index, length, nominal)
		}
	}
}

func TestResampleFollowsThePathRatherThanTheChord(t *testing.T) {
	out, err := shape.Resample(cornered(), 400)
	if err != nil {
		t.Fatalf("Resample: %v", err)
	}

	// The straight line between the ends is shorter than the routed path. If
	// the resampler had drawn a great circle the two would agree, and the
	// conductor would be somewhere it is not.
	ends := cornered()
	chord := ends[0].DistanceTo(ends[len(ends)-1])

	var routed float64
	for _, span := range out.Spans() {
		routed += span[0].DistanceTo(span[1])
	}

	if routed <= chord*1.02 {
		t.Errorf("routed length %.0f m is not meaningfully longer than the chord %.0f m; "+
			"the path was flattened", routed, chord)
	}
}

func TestResampleDropsRepeatedVertices(t *testing.T) {
	repeated := shape.Line{
		{Lon: -101.76, Lat: 34.54},
		{Lon: -101.76, Lat: 34.54},
		{Lon: -102.10, Lat: 34.20},
	}

	out, err := shape.Resample(repeated, 100000)
	if err != nil {
		t.Fatalf("Resample: %v", err)
	}

	for index, span := range out.Spans() {
		// A zero-length span is not a thing that can fail and has no bearing to
		// resolve a crosswind against.
		if span[0].DistanceTo(span[1]) == 0 {
			t.Errorf("span %d has zero length", index)
		}
	}
}

func TestResampleRejectsWhatCannotBeDivided(t *testing.T) {
	for _, tc := range []struct {
		name    string
		line    shape.Line
		nominal float64
	}{
		{"one point", shape.Line{{Lon: -101, Lat: 34}}, 400},
		{"zero nominal", cornered(), 0},
		{"negative nominal", cornered(), -400},
		{"NaN nominal", cornered(), math.NaN()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := shape.Resample(tc.line, tc.nominal); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// A path already denser than the nominal is left alone rather than thinned.
func TestResampleDoesNotThinADensePath(t *testing.T) {
	dense := shape.Line{
		{Lon: -101.7600, Lat: 34.5400},
		{Lon: -101.7610, Lat: 34.5405},
		{Lon: -101.7620, Lat: 34.5410},
	}

	out, err := shape.Resample(dense, 5000)
	if err != nil {
		t.Fatalf("Resample: %v", err)
	}

	if len(out) != len(dense) {
		t.Errorf("a path denser than the nominal was resampled to %d points, want %d",
			len(out), len(dense))
	}
}
