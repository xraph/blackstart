package scenario

import (
	"testing"

	"github.com/xraph/blackstart/extensions/scenario/store/memory"
)

// An engine missing a collaborator is refused at construction. A pipeline
// with a nil stage would fail every run at the stage it cannot reach, which
// is a deployment mistake reported as a run failure.
func TestAnEngineNeedsEveryCollaborator(t *testing.T) {
	t.Parallel()

	full := newFakes().collaborators()

	for _, without := range []struct {
		name string
		with Collaborators
	}{
		{"models", Collaborators{
			Cycles: full.Cycles, Geo: full.Geo, Hazard: full.Hazard,
			Screener: full.Screener, Simulator: full.Simulator,
		}},
		{"hazard", Collaborators{
			Models: full.Models, Cycles: full.Cycles, Geo: full.Geo,
			Screener: full.Screener, Simulator: full.Simulator,
		}},
		{"simulator", Collaborators{
			Models: full.Models, Cycles: full.Cycles, Geo: full.Geo,
			Hazard: full.Hazard, Screener: full.Screener,
		}},
	} {
		if _, err := NewEngine(memory.New(), without.with, nil); err == nil {
			t.Errorf("an engine with no %s was accepted", without.name)
		}
	}

	if _, err := NewEngine(nil, full, nil); err == nil {
		t.Error("an engine with no store was accepted")
	}
}
