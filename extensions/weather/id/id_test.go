package id_test

import (
	"testing"

	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/pkg/id"
)

// The prefixes must be registered by the time anything uses them, which is
// what the init function is for. Registering from init means a collision with
// another extension stops the process at startup with both owners named.
func TestPrefixesAreRegisteredToThisExtension(t *testing.T) {
	t.Parallel()

	for _, p := range []id.Prefix{wid.Cycle, wid.Grid, wid.Station} {
		got, ok := id.Lookup(p)
		if !ok {
			t.Errorf("prefix %q is not registered", p)

			continue
		}

		if got.Owner != wid.Owner {
			t.Errorf("prefix %q is owned by %q, want %q", p, got.Owner, wid.Owner)
		}
	}
}

func TestConstructorsMintTheRightType(t *testing.T) {
	t.Parallel()

	if got := wid.NewCycle().Prefix(); got != wid.Cycle {
		t.Errorf("NewCycle minted a %q identifier", got)
	}

	if got := wid.NewGrid().Prefix(); got != wid.Grid {
		t.Errorf("NewGrid minted a %q identifier", got)
	}

	if got := wid.NewStation().Prefix(); got != wid.Station {
		t.Errorf("NewStation minted a %q identifier", got)
	}
}

// The typed parsers are the point: a grid identifier reaching a cycle lookup
// should fail as "wrong type" rather than later as "not found", which sends
// the reader hunting in the wrong place.
func TestParsersRejectTheOtherTypes(t *testing.T) {
	t.Parallel()

	cycle := wid.NewCycle()
	grid := wid.NewGrid()
	station := wid.NewStation()

	for _, tt := range []struct {
		name  string
		parse func(string) (id.ID, error)
		own   string
		other []string
	}{
		{"cycle", wid.ParseCycle, cycle.String(), []string{grid.String(), station.String()}},
		{"grid", wid.ParseGrid, grid.String(), []string{cycle.String(), station.String()}},
		{"station", wid.ParseStation, station.String(), []string{cycle.String(), grid.String()}},
	} {
		if _, err := tt.parse(tt.own); err != nil {
			t.Errorf("Parse%s rejected its own identifier: %v", tt.name, err)
		}

		for _, other := range tt.other {
			if _, err := tt.parse(other); err == nil {
				t.Errorf("Parse%s accepted %q", tt.name, other)
			}
		}
	}
}

// Prefixes are immutable once shipped: changing one invalidates every stored
// identifier using it, every provenance record naming it, and every series key
// built from it — the last of which would silently read back as empty rather
// than failing. Pinning the literals makes a change fail here instead.
func TestPrefixValuesAreFrozen(t *testing.T) {
	t.Parallel()

	if wid.Cycle != "wxcyc" || wid.Grid != "wxgrd" || wid.Station != "wxstn" {
		t.Errorf("a shipped prefix changed: cycle=%q grid=%q station=%q; every "+
			"stored identifier using the old value would become unparseable",
			wid.Cycle, wid.Grid, wid.Station)
	}
}
