package contingency_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/contingency"
	cid "github.com/xraph/blackstart/extensions/contingency/id"
	"github.com/xraph/blackstart/extensions/contingency/store/memory"
	"github.com/xraph/blackstart/extensions/contingency/sweep"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/solver"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// fakeScreener streams a fixed list of results and records what it was asked.
//
// The mathematics is stubbed: the engine's job is collecting the stream,
// converting units, and recording provenance, and running the real screen
// would make these tests depend on a cargo build and on the numerics.
type fakeScreener struct {
	results []*solverv1.ScreenContingenciesResponse
	err     error

	sawRequest *solverv1.ScreenContingenciesRequest
	calls      int
}

func (f *fakeScreener) Screen(
	_ context.Context,
	_ *solverv1.NetworkModel,
	request *solverv1.ScreenContingenciesRequest,
	visit func(*solverv1.ScreenContingenciesResponse) error,
) error {
	f.calls++
	f.sawRequest = request

	if f.err != nil {
		return f.err
	}

	for _, result := range f.results {
		if err := visit(result); err != nil {
			return err
		}
	}

	return nil
}

func model() *solverv1.NetworkModel {
	return &solverv1.NetworkModel{
		Name:    "three-bus",
		SBaseVa: 100e6,
		VBaseV:  138e3,
		Buses: []*solverv1.Bus{
			{Id: "s", Kind: solverv1.BusKind_BUS_KIND_SLACK},
			{Id: "a", Kind: solverv1.BusKind_BUS_KIND_PQ},
		},
		Branches: []*solverv1.Branch{
			{Id: "s-a", FromBus: 0, ToBus: 1, Tap: 1},
			{Id: "a-s", FromBus: 1, ToBus: 0, Tap: 1},
		},
	}
}

func screenedWire(branch uint32, worst float64, violations ...*solverv1.Violation) *solverv1.ScreenContingenciesResponse {
	return &solverv1.ScreenContingenciesResponse{
		Contingency:   &solverv1.Contingency{Id: "line", Branch: branch},
		Screenability: solverv1.Screenability_SCREENABILITY_SCREENED,
		Violations:    violations,
		WorstLoading:  &worst,
	}
}

func radialWire(branch uint32) *solverv1.ScreenContingenciesResponse {
	return &solverv1.ScreenContingenciesResponse{
		Contingency:   &solverv1.Contingency{Id: "spur", Branch: branch},
		Screenability: solverv1.Screenability_SCREENABILITY_RADIAL,
		// No WorstLoading. That absence has to survive the crossing.
	}
}

func engine(t *testing.T, screener contingency.Screener, bus *hooks.Bus) *contingency.Engine {
	t.Helper()

	e, err := contingency.NewEngine(memory.New(), screener, "0.1.0-test", bus)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return e
}

func TestAnEngineNeedsItsCollaboratorsAndAVersion(t *testing.T) {
	screener := &fakeScreener{}

	if _, err := contingency.NewEngine(nil, screener, "v", nil); err == nil {
		t.Error("NewEngine accepted a nil store")
	}

	if _, err := contingency.NewEngine(memory.New(), nil, "v", nil); err == nil {
		t.Error("NewEngine accepted a nil screener")
	}

	// A run that cannot name what produced it is not reproducible, so the
	// version is required rather than defaulted to "unknown".
	if _, err := contingency.NewEngine(memory.New(), screener, "", nil); err == nil {
		t.Error("NewEngine accepted an empty solver version")
	}
}

// The absence this whole design protects. A contingency the screen could not
// evaluate has no loading, and a zero would let it rank as the safest case.
func TestAnAbsentLoadingSurvivesTheCrossing(t *testing.T) {
	screener := &fakeScreener{results: []*solverv1.ScreenContingenciesResponse{
		screenedWire(0, 0.8),
		radialWire(1),
	}}

	run, err := engine(t, screener, nil).Screen(t.Context(), contingency.Request{Model: model()})
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}

	if run.Candidates[0].WorstLoading == nil {
		t.Error("a screened contingency lost its loading")
	}

	if run.Candidates[1].WorstLoading != nil {
		t.Errorf("a radial contingency arrived with a loading of %v",
			*run.Candidates[1].WorstLoading)
	}

	if run.Candidates[1].Screenability != sweep.Radial {
		t.Errorf("screenability came across as %q", run.Candidates[1].Screenability)
	}
}

// Proto3 cannot distinguish an unset enum from a deliberate zero. Reading it
// as Screened would report a contingency nobody evaluated as one that came
// back clean, which is the exact inversion this extension exists to prevent.
func TestAnUnsetScreenabilityIsNotReadAsScreened(t *testing.T) {
	screener := &fakeScreener{results: []*solverv1.ScreenContingenciesResponse{{
		Contingency: &solverv1.Contingency{Id: "mystery", Branch: 0},
		// Screenability deliberately left unset.
	}}}

	run, err := engine(t, screener, nil).Screen(t.Context(), contingency.Request{Model: model()})
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}

	candidate := run.Candidates[0]

	if candidate.Screenability.Evaluated() {
		t.Error("an unset screenability was read as evaluated")
	}

	if !candidate.Concerning() {
		t.Error("a contingency with no screenability was reported as unconcerning")
	}
}

// Violations arrive in SI and are stored in per unit, against the base the
// model declared. Loading is already a ratio and must not be divided.
func TestViolationsComeBackInPerUnit(t *testing.T) {
	screener := &fakeScreener{results: []*solverv1.ScreenContingenciesResponse{
		screenedWire(0, 1.4, &solverv1.Violation{
			Branch:   1,
			PFlowW:   140e6,
			RatingVa: 100e6,
			Loading:  1.4,
		}),
	}}

	run, err := engine(t, screener, nil).Screen(t.Context(), contingency.Request{Model: model()})
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}

	violation := run.Candidates[0].Violations[0]

	if near := violation.PFlow; near < 1.39 || near > 1.41 {
		t.Errorf("flow is %v, want 1.4 per unit; SI escaped", near)
	}

	if near := violation.Rating; near < 0.99 || near > 1.01 {
		t.Errorf("rating is %v, want 1.0 per unit", near)
	}

	if violation.Loading != 1.4 {
		t.Errorf("loading is %v, want the 1.4 the screen reported", violation.Loading)
	}
}

// A run must name the model and the solver, or its findings cannot be
// reproduced.
func TestARunRecordsWhatProducedIt(t *testing.T) {
	screener := &fakeScreener{results: []*solverv1.ScreenContingenciesResponse{
		screenedWire(0, 0.5),
	}}

	run, err := engine(t, screener, nil).Screen(t.Context(), contingency.Request{Model: model()})
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}

	if run.ModelHash == "" {
		t.Error("the run names no model")
	}

	if run.SolverVersion != "0.1.0-test" {
		t.Errorf("solver version %q", run.SolverVersion)
	}

	if run.ID.IsZero() {
		t.Error("the run has no identifier")
	}

	if run.StartedAt.IsZero() || run.FinishedAt.IsZero() {
		t.Error("the run is not bounded in time")
	}
}

func TestTheRequestReachesTheScreenerIntact(t *testing.T) {
	screener := &fakeScreener{}

	contingencies := []*solverv1.Contingency{{Id: "first", Branch: 1}}

	if _, err := engine(t, screener, nil).Screen(t.Context(), contingency.Request{
		Model:              model(),
		Contingencies:      contingencies,
		ViolationThreshold: 1.1,
	}); err != nil {
		t.Fatalf("Screen: %v", err)
	}

	if got := screener.sawRequest.GetViolationThreshold(); got != 1.1 {
		t.Errorf("threshold arrived as %v", got)
	}

	if got := screener.sawRequest.GetContingencies(); len(got) != 1 || got[0].GetId() != "first" {
		t.Errorf("the contingency ordering did not survive: %v", got)
	}
}

func TestAnInvalidRequestNeverReachesTheScreener(t *testing.T) {
	cases := map[string]contingency.Request{
		"no model":           {},
		"no branches":        {Model: &solverv1.NetworkModel{SBaseVa: 100e6}},
		"negative threshold": {Model: model(), ViolationThreshold: -1},
	}

	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			screener := &fakeScreener{}

			if _, err := engine(t, screener, nil).Screen(t.Context(), request); err == nil {
				t.Fatalf("Screen accepted a request with %s", name)
			}

			if screener.calls != 0 {
				t.Errorf("an invalid request reached the screener %d times", screener.calls)
			}
		})
	}
}

// A screen failure must leave nothing behind. A partial run would sit in the
// store looking like a completed analysis.
func TestAScreenFailureStoresNothing(t *testing.T) {
	screener := &fakeScreener{err: errors.New("the solver fell over")}
	e := engine(t, screener, nil)

	_, err := e.Screen(t.Context(), contingency.Request{Model: model()})
	if err == nil {
		t.Fatal("Screen succeeded against a failing screener")
	}

	if !strings.Contains(err.Error(), "fell over") {
		t.Errorf("the screener's own error was lost: %v", err)
	}

	hash, hashErr := solver.HashOnly(model())
	if hashErr != nil {
		t.Fatalf("hashing: %v", hashErr)
	}

	found, err := e.ListByModel(t.Context(), hash, 0)
	if err != nil {
		t.Fatalf("ListByModel: %v", err)
	}

	if len(found) != 0 {
		t.Errorf("a failed screen left %d runs behind", len(found))
	}
}

func TestARunIsStoredBeforeItIsReturned(t *testing.T) {
	screener := &fakeScreener{results: []*solverv1.ScreenContingenciesResponse{
		screenedWire(0, 0.5),
	}}
	e := engine(t, screener, nil)

	run, err := e.Screen(t.Context(), contingency.Request{Model: model()})
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}

	stored, err := e.Get(t.Context(), run.ID)
	if err != nil {
		t.Fatalf("the run that was returned is not in the store: %v", err)
	}

	if stored.ID != run.ID {
		t.Errorf("stored %s, returned %s", stored.ID, run.ID)
	}
}

func TestAMissingRunIsNotFound(t *testing.T) {
	e := engine(t, &fakeScreener{}, nil)

	if _, err := e.Get(t.Context(), cid.NewRun()); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestAScreenEmitsADomainEvent(t *testing.T) {
	bus := hooks.NewBus()

	var seen []hooks.Event
	bus.Subscribe("contingency.screened", func(_ context.Context, e hooks.Event) error {
		seen = append(seen, e)

		return nil
	})

	screener := &fakeScreener{results: []*solverv1.ScreenContingenciesResponse{
		screenedWire(0, 0.5),
	}}

	run, err := engine(t, screener, bus).Screen(t.Context(), contingency.Request{Model: model()})
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}

	if len(seen) != 1 {
		t.Fatalf("got %d events, want one", len(seen))
	}

	if seen[0].EntityID != run.ID.String() {
		t.Errorf("the event names %q, want %q", seen[0].EntityID, run.ID)
	}
}

// A nil bus is the normal case for a CLI invocation.
func TestANilBusIsFine(t *testing.T) {
	screener := &fakeScreener{results: []*solverv1.ScreenContingenciesResponse{
		screenedWire(0, 0.5),
	}}

	if _, err := engine(t, screener, nil).Screen(
		t.Context(), contingency.Request{Model: model()},
	); err != nil {
		t.Fatalf("Screen with no bus: %v", err)
	}
}
