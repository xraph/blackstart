package fetch_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/weather/fetch"
)

func hrrrIndex(t *testing.T) fetch.Records {
	t.Helper()

	body, err := os.Open(filepath.Join("testdata", "hrrr.t12z.wrfsfcf01.grib2.idx"))
	if err != nil {
		t.Fatalf("opening the fixture: %v", err)
	}

	t.Cleanup(func() { _ = body.Close() })

	records, err := fetch.ParseIndex(body)
	if err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}

	return records
}

func TestParseIndexReadsARealSidecar(t *testing.T) {
	t.Parallel()

	records := hrrrIndex(t)

	if len(records) != 170 {
		t.Fatalf("parsed %d records, want the fixture's 170", len(records))
	}

	// Record 9 is the gust, at the offset the file states.
	got := records[8]

	if got.Number != 9 {
		t.Errorf("ninth record is numbered %d", got.Number)
	}

	if got.Abbrev != "GUST" || got.LevelText != "surface" {
		t.Errorf("ninth record is %q:%q, want GUST:surface", got.Abbrev, got.LevelText)
	}

	if got.Offset != 3231216 {
		t.Errorf("gust offset = %d, want 3231216", got.Offset)
	}
}

// A sidecar states offsets and no lengths, so a message's length is the next
// record's offset minus its own. That is the whole reason records are kept in
// order rather than in a map.
func TestLocateDerivesLengthFromTheNextOffset(t *testing.T) {
	t.Parallel()

	records := hrrrIndex(t)

	offset, length, ok := records.Locate("GUST:surface")
	if !ok {
		t.Fatal("GUST:surface not found in a sidecar that contains it")
	}

	if offset != 3231216 {
		t.Errorf("offset = %d, want 3231216", offset)
	}

	// The next record starts at 4460131.
	if length != 4460131-3231216 {
		t.Errorf("length = %d, want %d", length, 4460131-3231216)
	}
}

// The last record has no successor, so its length is unknowable from the
// sidecar. Zero means "to the end of the file", which is what an open-ended
// HTTP range expresses — a negative or a guess would be worse than either.
func TestTheLastRecordHasAnOpenEndedLength(t *testing.T) {
	t.Parallel()

	records := hrrrIndex(t)
	last := records[len(records)-1]

	_, length, ok := records.Locate(last.Abbrev + ":" + last.LevelText)
	if !ok {
		t.Fatal("the last record is not locatable")
	}

	if length != 0 {
		t.Errorf("last record length = %d, want 0 meaning open-ended", length)
	}
}

func TestLocateReportsAMissingKey(t *testing.T) {
	t.Parallel()

	records := hrrrIndex(t)

	if _, _, ok := records.Locate("GUST:10 m above ground"); ok {
		t.Error("a key absent from the sidecar was located")
	}

	if _, _, ok := records.Locate("nonsense"); ok {
		t.Error("a malformed key was located")
	}
}

// A sidecar carries several records under one abbreviation at different
// levels — TMP appears at a dozen pressure levels. Locating must key on both.
func TestLocateDistinguishesLevels(t *testing.T) {
	t.Parallel()

	records := hrrrIndex(t)

	surface, _, ok := records.Locate("TMP:surface")
	if !ok {
		t.Fatal("TMP:surface not found")
	}

	twoMetre, _, ok := records.Locate("TMP:2 m above ground")
	if !ok {
		t.Fatal("TMP:2 m above ground not found")
	}

	if surface == twoMetre {
		t.Error("two levels of TMP resolved to one offset")
	}
}

func TestParseIndexRejectsGarbage(t *testing.T) {
	t.Parallel()

	for name, text := range map[string]string{
		"no colons":     "this is not an index\n",
		"short record":  "1:0:d=2026080312\n",
		"bad offset":    "1:notanumber:d=2026080312:GUST:surface:1 hour fcst:\n",
		"bad number":    "x:0:d=2026080312:GUST:surface:1 hour fcst:\n",
		"empty content": "",
	} {
		if _, err := fetch.ParseIndex(strings.NewReader(text)); err == nil {
			t.Errorf("%s: ParseIndex accepted it", name)
		}
	}
}

// Records must come back in file order, because the length derivation depends
// on it. A sidecar whose offsets go backwards is corrupt.
func TestParseIndexRejectsOffsetsThatGoBackwards(t *testing.T) {
	t.Parallel()

	text := "1:100:d=2026080312:GUST:surface:1 hour fcst:\n" +
		"2:50:d=2026080312:TMP:surface:1 hour fcst:\n"

	if _, err := fetch.ParseIndex(strings.NewReader(text)); err == nil {
		t.Error("an index with decreasing offsets was accepted")
	}
}
