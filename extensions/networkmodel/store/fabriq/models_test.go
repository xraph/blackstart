package fabriq

import (
	"reflect"
	"testing"
)

// The stored branch row reproduces Fabriq's structural contract and names the
// columns the migration creates.
//
// A grove tag that disagrees with the table compiles, round-trips in memory,
// and fails only against a real database — the slowest possible place to find
// out.
func TestBranchRowNamesItsColumns(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"ID":       "id,pk",
		"TenantID": "tenant_id,notnull",
		"Version":  "version,notnull",
		"ModelID":  "model_id,notnull",
		"Ordinal":  "ordinal,notnull",
		"FromBus":  "from_bus,notnull",
		"ToBus":    "to_bus,notnull",
		"Alias":    "alias",
	}

	row := reflect.TypeFor[branchRow]()

	for field, tag := range want {
		got, ok := row.FieldByName(field)
		if !ok {
			t.Errorf("branchRow has no %s field", field)

			continue
		}

		if got.Tag.Get("grove") != tag {
			t.Errorf("%s grove tag = %q, want %q", field, got.Tag.Get("grove"), tag)
		}
	}
}

func TestPlacementRowNamesItsColumns(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"ID":        "id,pk",
		"TenantID":  "tenant_id,notnull",
		"Version":   "version,notnull",
		"ModelID":   "model_id,notnull",
		"Bus":       "bus,notnull",
		"Lon":       "lon,notnull",
		"Lat":       "lat,notnull",
		"Elevation": "elevation,notnull",
	}

	row := reflect.TypeFor[placementRow]()

	for field, tag := range want {
		got, ok := row.FieldByName(field)
		if !ok {
			t.Errorf("placementRow has no %s field", field)

			continue
		}

		if got.Tag.Get("grove") != tag {
			t.Errorf("%s grove tag = %q, want %q", field, got.Tag.Get("grove"), tag)
		}
	}
}
