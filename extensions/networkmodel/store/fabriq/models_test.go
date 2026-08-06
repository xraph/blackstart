package fabriq

import (
	"reflect"
	"testing"
)

// The row's grove tags must reproduce Fabriq's structural contract — id,
// tenant_id, version — and match the DDL in migrations.go column for column.
// A mismatch here fails at insert against a real database and nowhere else.
//
// Unqualified, not fabriq.BusTable: this file is package fabriq itself
// (white-box, like the branch and placement tests above it), and a package
// cannot import itself to qualify its own identifiers.
func TestTheBusRowDeclaresItsTable(t *testing.T) {
	if BusTable != "blackstart_networkmodel_bus" {
		t.Fatalf("bus table is %q", BusTable)
	}

	if BusEntity != "networkmodel_bus" {
		t.Fatalf("bus entity is %q", BusEntity)
	}
}

// The registry refuses a spec whose Model does not declare the structural
// columns, so an entity added without a spec is an extension that fails to
// compose rather than a table nobody writes to.
func TestTheContributorDeclaresTheBusEntity(t *testing.T) {
	var found bool

	for _, spec := range (Contributor{}).Specs() {
		if spec.Name == BusEntity {
			found = true
		}
	}

	if !found {
		t.Fatal("the contributor declares no bus entity")
	}
}

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
