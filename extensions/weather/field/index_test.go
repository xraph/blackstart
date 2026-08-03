package field_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/model"
)

// The vocabulary is checked against real index files rather than against the
// code tables it was assembled from.
//
// This is the test that would have caught the first version's errors. That
// one had the gust at height-above-ground 10 m, the lightning threat at the
// surface, and a scalar wind field — three keys that appear in no NOAA index
// anywhere, and which every table-based test passed anyway.
//
// The fixtures live in testdata with their provenance, so the check runs
// offline and keeps running when the cycle they came from has long rolled out
// of NOAA's archive. Not under data/weather: .gitignore reserves that for the
// bulk products fetched on demand and catalogued by content hash, which is a
// different thing from a fixture a test reads.
func TestEverySpecAppearsInARealIndex(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		model model.Model
		file  string
	}{
		{model.HRRR, "hrrr.t12z.wrfsfcf01.grib2.idx"},
		{model.GFS, "gfs.t12z.pgrb2.0p25.f001.idx"},
	} {
		keys := indexKeys(t, tc.file)

		for _, f := range field.Fields() {
			spec, err := field.SpecFor(f, tc.model)
			if err != nil {
				// A field this model does not publish is expected to be
				// absent; that it is absent is asserted below.
				continue
			}

			if !keys[spec.IndexKey()] {
				t.Errorf("%s: %q resolves to %q, which appears in no record of %s",
					tc.model, f, spec.IndexKey(), tc.file)
			}
		}
	}
}

// The three HRRR-only fields must really be absent from the GFS index. If one
// turned up, SpecFor would be refusing a field the product actually carries —
// a silent gap rather than a loud error.
func TestUnpublishedFieldsAreAbsentFromTheGFSIndex(t *testing.T) {
	t.Parallel()

	keys := indexKeys(t, "gfs.t12z.pgrb2.0p25.f001.idx")

	for _, f := range []field.Field{
		field.Lightning, field.FreezingRain, field.SnowAccum,
	} {
		spec, err := field.SpecFor(f, model.HRRR)
		if err != nil {
			t.Fatalf("SpecFor(%q, hrrr): %v", f, err)
		}

		if keys[spec.IndexKey()] {
			t.Errorf("%q keys on %q, which GFS does publish; SpecFor refuses it "+
				"for no reason", f, spec.IndexKey())
		}
	}
}

// indexKeys reads an index sidecar into the set of "ABBREV:LEVEL" keys it
// contains.
//
// A record is `number:offset:date:ABBREV:LEVEL:forecast:` — colon-separated,
// with the level text itself free of colons.
func indexKeys(t *testing.T, name string) map[string]bool {
	t.Helper()

	path := filepath.Join("testdata", name)

	handle, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening the index fixture %s: %v", path, err)
	}

	defer func() { _ = handle.Close() }()

	const (
		abbrevAt = 3
		levelAt  = 4
		minParts = 6
	)

	keys := map[string]bool{}
	scanner := bufio.NewScanner(handle)

	for scanner.Scan() {
		part := strings.Split(scanner.Text(), ":")
		if len(part) < minParts {
			continue
		}

		keys[part[abbrevAt]+":"+part[levelAt]] = true
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	if len(keys) == 0 {
		t.Fatalf("%s yielded no records; the fixture or the parse is wrong", path)
	}

	return keys
}
