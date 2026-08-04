package fetch_test

import (
	"testing"
	"time"

	"github.com/xraph/blackstart/extensions/weather/fetch"
	"github.com/xraph/blackstart/extensions/weather/model"
)

func at(hour int) time.Time {
	return time.Date(2026, 8, 3, hour, 0, 0, 0, time.UTC)
}

// The URLs were confirmed against the live buckets: these exact paths are what
// the byte-range fetches in this package's own verification used.
func TestProductURLsMatchThePublishedPaths(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		model model.Model
		root  string
		hour  int
		want  string
	}{
		"hrrr": {
			model: model.HRRR,
			root:  "https://noaa-hrrr-bdp-pds.s3.amazonaws.com",
			hour:  1,
			want: "https://noaa-hrrr-bdp-pds.s3.amazonaws.com/hrrr.20260803/conus/" +
				"hrrr.t12z.wrfsfcf01.grib2",
		},
		"gfs": {
			model: model.GFS,
			root:  "https://noaa-gfs-bdp-pds.s3.amazonaws.com",
			hour:  1,
			want: "https://noaa-gfs-bdp-pds.s3.amazonaws.com/gfs.20260803/12/atmos/" +
				"gfs.t12z.pgrb2.0p25.f001",
		},
		"hrrr at hour 18": {
			model: model.HRRR,
			root:  "https://noaa-hrrr-bdp-pds.s3.amazonaws.com",
			hour:  18,
			want: "https://noaa-hrrr-bdp-pds.s3.amazonaws.com/hrrr.20260803/conus/" +
				"hrrr.t12z.wrfsfcf18.grib2",
		},
		"gfs at hour 120": {
			model: model.GFS,
			root:  "https://noaa-gfs-bdp-pds.s3.amazonaws.com",
			hour:  120,
			want: "https://noaa-gfs-bdp-pds.s3.amazonaws.com/gfs.20260803/12/atmos/" +
				"gfs.t12z.pgrb2.0p25.f120",
		},
	} {
		got, err := fetch.ProductURL(tc.root, tc.model, at(12), tc.hour)
		if err != nil {
			t.Errorf("%s: %v", name, err)

			continue
		}

		if got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, tc.want)
		}
	}
}

// A trailing slash on the feed row must not produce a doubled one in the path.
func TestProductURLTrimsATrailingSlash(t *testing.T) {
	t.Parallel()

	got, err := fetch.ProductURL(
		"https://example.gov/hrrr/", model.HRRR, at(12), 1)
	if err != nil {
		t.Fatalf("ProductURL: %v", err)
	}

	if want := "https://example.gov/hrrr/hrrr.20260803/conus/hrrr.t12z.wrfsfcf01.grib2"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestIndexURL(t *testing.T) {
	t.Parallel()

	if got := fetch.IndexURL("https://example.gov/a.grib2"); got != "https://example.gov/a.grib2.idx" {
		t.Errorf("IndexURL = %s", got)
	}
}

// GFS runs four times a day. Asking for a cycle at 13Z is a caller's mistake,
// and composing a URL for it would produce a 404 far from its cause.
func TestProductURLRefusesAnIssuanceThatIsNotACycle(t *testing.T) {
	t.Parallel()

	if _, err := fetch.ProductURL("https://x.gov", model.GFS, at(13), 1); err == nil {
		t.Error("GFS accepted a 13Z issuance")
	}

	// HRRR runs hourly, so 13Z is a real cycle for it.
	if _, err := fetch.ProductURL("https://x.gov", model.HRRR, at(13), 1); err != nil {
		t.Errorf("HRRR refused a 13Z issuance: %v", err)
	}
}

func TestProductURLRefusesANegativeHour(t *testing.T) {
	t.Parallel()

	if _, err := fetch.ProductURL("https://x.gov", model.HRRR, at(12), -1); err == nil {
		t.Error("a negative forecast hour was accepted")
	}
}

func TestIsCycle(t *testing.T) {
	t.Parallel()

	for hour := range 24 {
		if !fetch.IsCycle(model.HRRR, at(hour)) {
			t.Errorf("HRRR should publish at %02dZ", hour)
		}

		want := hour%6 == 0

		if got := fetch.IsCycle(model.GFS, at(hour)); got != want {
			t.Errorf("IsCycle(GFS, %02dZ) = %v, want %v", hour, got, want)
		}
	}

	// A cycle is identified by its hour. An issuance carrying minutes is a
	// caller's rounding error rather than a different run.
	withMinutes := time.Date(2026, 8, 3, 12, 30, 0, 0, time.UTC)

	if fetch.IsCycle(model.HRRR, withMinutes) {
		t.Error("an issuance with minutes was accepted as a cycle")
	}
}

func TestLatestCycleRoundsBackwards(t *testing.T) {
	t.Parallel()

	// 13:45Z: HRRR's latest is 13Z, GFS's is 12Z.
	now := time.Date(2026, 8, 3, 13, 45, 0, 0, time.UTC)

	got, err := fetch.LatestCycle(model.HRRR, now)
	if err != nil {
		t.Fatalf("LatestCycle: %v", err)
	}

	if !got.Equal(at(13)) {
		t.Errorf("HRRR latest = %s, want 13Z", got)
	}

	got, err = fetch.LatestCycle(model.GFS, now)
	if err != nil {
		t.Fatalf("LatestCycle: %v", err)
	}

	if !got.Equal(at(12)) {
		t.Errorf("GFS latest = %s, want 12Z", got)
	}
}

// Newest first, because that is the order a bounded backfill wants: the
// current forecast matters now and the older ones are filling a record.
func TestCyclesAreNewestFirst(t *testing.T) {
	t.Parallel()

	got, err := fetch.Cycles(model.GFS, at(0), at(23))
	if err != nil {
		t.Fatalf("Cycles: %v", err)
	}

	want := []time.Time{at(18), at(12), at(6), at(0)}

	if len(got) != len(want) {
		t.Fatalf("got %d cycles, want %d", len(got), len(want))
	}

	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Errorf("cycle %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestCyclesOverAnHourlyModel(t *testing.T) {
	t.Parallel()

	got, err := fetch.Cycles(model.HRRR, at(20), at(23))
	if err != nil {
		t.Fatalf("Cycles: %v", err)
	}

	if len(got) != 4 {
		t.Fatalf("got %d HRRR cycles in four hours, want 4", len(got))
	}

	if !got[0].Equal(at(23)) {
		t.Errorf("first cycle = %s, want 23Z", got[0])
	}
}

func TestAnUnknownModelIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := fetch.Cadence("ecmwf"); err == nil {
		t.Error("Cadence accepted a model outside the set")
	}

	if _, err := fetch.ProductURL("https://x.gov", "ecmwf", at(12), 1); err == nil {
		t.Error("ProductURL accepted a model outside the set")
	}
}
