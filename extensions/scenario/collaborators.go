package scenario

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/blackstart/extensions/cascade"
	cascaderun "github.com/xraph/blackstart/extensions/cascade/run"
	"github.com/xraph/blackstart/extensions/contingency"
	"github.com/xraph/blackstart/extensions/contingency/sweep"
	"github.com/xraph/blackstart/extensions/networkmodel"
	nmmodel "github.com/xraph/blackstart/extensions/networkmodel/model"
	scnstore "github.com/xraph/blackstart/extensions/scenario/store"
	"github.com/xraph/blackstart/extensions/weather"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
)

// ModelReader is the subset of networkmodel.Engine this needs.
//
// Two methods rather than one because a run needs both halves of a stored
// model: the bytes the content hash names, and the row that says what the
// network is called. The name is not decoration — it goes into the wire model
// the solver hashes, so resolving the same model under a different name would
// produce a different solver-side hash and a cache miss on every run.
type ModelReader interface {
	// FindByHash finds a stored model by its content hash.
	FindByHash(ctx context.Context, hash string) (nmmodel.NetworkModel, error)

	// Encoding reads the canonical bytes a content hash names.
	Encoding(ctx context.Context, hash string) ([]byte, error)
}

// CycleReader is the subset of weather.Engine this needs.
//
// One method, and it exists for one field: a cycle's issuance. The manifest
// pins the cycle by identifier and the valid time by value, and hazard's
// series are keyed by issuance — so the cycle is what turns "this forecast"
// into the knowledge-time axis of the rating lookup.
type CycleReader interface {
	// Cycle reads one model run.
	Cycle(ctx context.Context, want id.ID) (forecast.Cycle, error)
}

// Screener is the subset of contingency.Engine this needs.
//
// Narrow on purpose, following cascade's Simulator: an engine that could
// reach the sweep store directly would be able to record a run this one did
// not produce, and every method here is one the tests have to fake.
type Screener interface {
	Screen(ctx context.Context, in contingency.Request) (sweep.Run, error)
}

// Simulator is the subset of cascade.Engine this needs.
type Simulator interface {
	Simulate(ctx context.Context, in cascade.Request) (cascaderun.CascadeRun, error)
}

// The real engines satisfy them, checked here rather than asserted in prose:
// a comment claiming the shapes match is enforced by nothing, and the two
// drift the first time one of those extensions changes a signature.
var (
	_ ModelReader = (*networkmodel.Engine)(nil)
	_ CycleReader = (*weather.Engine)(nil)
	_ Screener    = (*contingency.Engine)(nil)
	_ Simulator   = (*cascade.Engine)(nil)
)

// Collaborators is everything outside this extension the pipeline calls.
//
// A struct rather than seven positional parameters. Each field is an
// interface with one or two methods, several of them shaped alike, and a call
// site that reads NewEngine(store, models, cycles, geo, hazard, screener,
// simulator, bus) is one nobody can check by eye — which matters here because
// getting the wrong reader in the wrong slot produces a run that executes and
// is wrong rather than one that fails.
type Collaborators struct {
	// Models resolves the manifest's pinned network.
	Models ModelReader

	// Cycles resolves the manifest's pinned forecast to its issuance.
	Cycles CycleReader

	// Geo and Hazard are the two halves of the rating chain: a branch's route
	// and spans on one side, the ampacity computed for those spans on the
	// other. Neither can complete it alone.
	Geo    GeoReader
	Hazard HazardReader

	// Screener and Simulator are the two engines a scenario orchestrates.
	// Each produces and stores its own record; this extension holds their
	// identifiers and recomputes nothing.
	Screener  Screener
	Simulator Simulator
}

// Validate reports whether every collaborator is present.
func (c Collaborators) Validate() error {
	// A slice rather than a map, so the message reads the same every time.
	required := []struct {
		name    string
		present bool
	}{
		{"a model reader", c.Models != nil},
		{"a cycle reader", c.Cycles != nil},
		{"a geometry reader", c.Geo != nil},
		{"a hazard reader", c.Hazard != nil},
		{"a contingency screener", c.Screener != nil},
		{"a cascade simulator", c.Simulator != nil},
	}

	missing := make([]string, 0, len(required))

	for _, one := range required {
		if !one.present {
			missing = append(missing, one.name)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf(
			"scenario: the engine needs %v; a pipeline missing a stage would "+
				"fail every run at the stage it cannot reach", missing)
	}

	return nil
}

// Engine holds the extension's business logic.
type Engine struct {
	store        scnstore.Store
	collaborator Collaborators
	bus          *hooks.Bus

	// now is injectable so a test can pin the times a run records without
	// sleeping or comparing against a moving target.
	now func() time.Time
}

// NewEngine builds the engine.
//
// The bus may be nil — a CLI invocation has no interest in domain events.
func NewEngine(s scnstore.Store, in Collaborators, bus *hooks.Bus) (*Engine, error) {
	if s == nil {
		return nil, errors.New("scenario: engine needs a store")
	}

	if err := in.Validate(); err != nil {
		return nil, err
	}

	return &Engine{store: s, collaborator: in, bus: bus, now: time.Now}, nil
}
