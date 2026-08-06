// Package cascade runs cascade simulations and records what they found.
//
// It owns no mathematics. The simulation runs in Rust and returns one result
// for the whole cascade — unary, unlike contingency's streamed screen,
// because there is no partial cascade worth rendering as it goes. This
// package adds the platform's half: minting an identifier, converting the
// response to the platform's units, recording provenance, and storing the
// run so it can be produced again from what was stored beside it.
//
// Advisory only. Nothing here writes to a control system, and the contract
// it calls defines no method that could. See docs/SAFETY.md.
package cascade

import (
	"context"
	"errors"
	"fmt"
	"time"

	cid "github.com/xraph/blackstart/extensions/cascade/id"
	"github.com/xraph/blackstart/extensions/cascade/run"
	"github.com/xraph/blackstart/extensions/cascade/store"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// Simulator is the subset of pkg/solver.Client this engine needs.
//
// Narrow on purpose: an engine that could reach the upload or solve methods
// would be able to do things its job does not require, and every method here
// is one the tests have to fake.
type Simulator interface {
	Simulate(
		ctx context.Context,
		model *solverv1.NetworkModel,
		request *solverv1.SimulateCascadeRequest,
	) (*solverv1.SimulateCascadeResponse, error)
}

// Engine holds the extension's business logic.
type Engine struct {
	store     store.Store
	simulator Simulator
	bus       *hooks.Bus

	// The extent half. All optional: a deployment that only simulates and
	// reads back runs needs none of them, and an engine that demanded a
	// model, branch or geometry reader to answer Simulate or Get would make
	// the read path depend on collaborators it has no use for.
	models   Models
	branches Branches
	geo      Geo

	// now is injectable so a test can pin the times a run records without
	// sleeping or comparing against a moving target.
	now func() time.Time
}

// EngineOption configures the engine.
//
// Named for the engine because the extension already has an Option, and two
// types called the same thing in one package would make every call site ask
// which one it meant.
type EngineOption func(*Engine)

// WithExtent supplies what Extent needs: a model reader, a branch reader and
// a geometry reader.
//
// All three or none — Extent needs every one of them to get from a run's
// model hash to a tripped branch's corridor, and a partial set would fail on
// the first call rather than at construction.
func WithExtent(m Models, b Branches, g Geo) EngineOption {
	return func(e *Engine) { e.models, e.branches, e.geo = m, b, g }
}

// NewEngine builds the engine.
//
// Unlike contingency.NewEngine, this takes no solver version: the response
// carries its own solver_version and model_hash, so recording them means
// reading what the solver said rather than configuring a second, possibly
// stale source of truth into the engine. The bus may be nil — a CLI
// invocation has no interest in domain events.
func NewEngine(s store.Store, simulator Simulator, bus *hooks.Bus, opts ...EngineOption) (*Engine, error) {
	if s == nil {
		return nil, errors.New("cascade: engine needs a store")
	}

	if simulator == nil {
		return nil, errors.New("cascade: engine needs a simulator")
	}

	e := &Engine{
		store:     s,
		simulator: simulator,
		bus:       bus,
		now:       time.Now,
	}

	for _, opt := range opts {
		opt(e)
	}

	return e, nil
}

// Request is one cascade simulation to run.
type Request struct {
	// Model is the network, in the contract's SI units.
	Model *solverv1.NetworkModel

	// CanonicalHash is the model's identity as networkmodel stores it —
	// canonical.Hash, the string a manifest pins. Supplied by the caller
	// because only the caller can: what arrives here is a wire model, and the
	// canonical encoding it was built from is already gone. The solver's own
	// hash comes back on the response and needs no help.
	//
	// Optional, and deliberately not required by Validate, for the reason
	// contingency.Request.CanonicalHash is not: this engine accepts any
	// *solverv1.NetworkModel, and a model networkmodel never stored has no
	// canonical hash to give. Empty here means unknown, and is recorded as
	// absent rather than as "".
	CanonicalHash string

	// Overrides adjust the model for this run without re-uploading it.
	Overrides []*solverv1.Override

	// Initiating is the branches taken out of service at t=0; everything
	// after is the simulator's consequence, not a further instruction.
	Initiating []uint32

	// Protection is the network-wide relay default; BranchProtection
	// overrides it per branch. Both nil means a relay at its own rating.
	Protection       *solverv1.ProtectionSettings
	BranchProtection []*solverv1.BranchProtection

	// Ufls is the under-frequency load-shedding schedule. Nil sheds nothing,
	// ever — shedding policy belongs to the utility, not this tool's default.
	Ufls *solverv1.UflsScheme

	// CoalesceWindowS, MaxSteps and HorizonS bound the run. Zero means the
	// server's own default for each.
	CoalesceWindowS float64
	MaxSteps        uint32
	HorizonS        float64
}

// Validate reports whether a request can be run.
func (r Request) Validate() error {
	if r.Model == nil {
		return errors.New("cascade: a request needs a network model")
	}

	if len(r.Model.GetBranches()) == 0 {
		return errors.New(
			"cascade: a model with no branches has nothing to cascade from")
	}

	return nil
}

// Simulate runs the cascade and records the result.
//
// The run is stored before it is returned, so a caller acting on the outcome
// can always point at the record behind it.
func (e *Engine) Simulate(ctx context.Context, in Request) (run.CascadeRun, error) {
	if err := in.Validate(); err != nil {
		return run.CascadeRun{}, err
	}

	started := e.now().UTC()

	request := &solverv1.SimulateCascadeRequest{
		Overrides:        in.Overrides,
		Initiating:       in.Initiating,
		Protection:       in.Protection,
		BranchProtection: in.BranchProtection,
		Ufls:             in.Ufls,
		CoalesceWindowS:  in.CoalesceWindowS,
		MaxSteps:         in.MaxSteps,
		HorizonS:         in.HorizonS,
	}

	response, err := e.simulator.Simulate(ctx, in.Model, request)
	if err != nil {
		return run.CascadeRun{}, fmt.Errorf("cascade: simulating: %w", err)
	}

	built := fromWire(response, in.Model)
	built.ID = cid.NewRun()
	// Set here rather than in fromWire, alongside the other three the response
	// cannot supply: the canonical hash came off the request, not the wire.
	built.CanonicalHash = optionalHash(in.CanonicalHash)
	built.StartedAt = started
	built.FinishedAt = e.now().UTC()

	stored, err := e.store.Runs().Create(ctx, built)
	if err != nil {
		return run.CascadeRun{}, fmt.Errorf("cascade: recording the run: %w", err)
	}

	e.emit(ctx, stored)

	return stored, nil
}

// Get retrieves a recorded run.
func (e *Engine) Get(ctx context.Context, runID id.ID) (run.CascadeRun, error) {
	return e.store.Runs().Get(ctx, runID)
}

// ListByModel returns what is known about one network, most recent first.
//
// The hash may be in either space — see run.Store.ListByModel. The canonical
// one is what a caller holding a scenario manifest has, and answering it is
// why a run carries two hashes.
func (e *Engine) ListByModel(
	ctx context.Context, modelHash string, limit int,
) ([]run.CascadeRun, error) {
	return e.store.Runs().ListByModel(ctx, modelHash, limit)
}

// optionalHash turns a caller's unset hash into an absent one.
//
// The empty string is how a Go struct literal says "I did not supply this",
// and it must not survive as a pointer to "": stored, that reads as a
// canonical hash which is known and blank, and every run from a caller that
// supplied nothing would list together as one network. See
// run.CascadeRun.CanonicalHash.
func optionalHash(given string) *string {
	if given == "" {
		return nil
	}

	return &given
}

// fromWire converts a simulation response into the domain shape.
//
// The per-unit boundary on the read path. The base comes from the model
// simulated, not a constant, so two models on different bases cannot be
// confused. ID, StartedAt and FinishedAt are the caller's to fill in.
func fromWire(response *solverv1.SimulateCascadeResponse, model *solverv1.NetworkModel) run.CascadeRun {
	sBase := model.GetSBaseVa()

	events := make([]run.Event, len(response.GetEvents()))
	for index, wire := range response.GetEvents() {
		events[index] = eventFromWire(wire)
	}

	islands := make([]run.Island, len(response.GetIslands()))
	for index, wire := range response.GetIslands() {
		islands[index] = islandFromWire(wire, sBase)
	}

	return run.CascadeRun{
		ModelHash:     response.GetModelHash(),
		SolverVersion: response.GetSolverVersion(),
		Outcome:       run.Outcome(outcomeTable[response.GetOutcome()]),
		SBaseVA:       sBase,
		// Branch identifiers, not a power-system quantity — crosses untouched,
		// the same reason Elapsed below does. Never nil: an empty repeated
		// field comes back nil from the generated getter, and a cascade with
		// no initiating outage — which the engine's own Request.Validate
		// allows — must serialize as "[]", not "null", or the wire's own
		// required field throws in a reader that assumes an array.
		Initiating: nonNilStrings(response.GetInitiating()),
		Events:     events,
		Islands:    islands,
		LoadLost:   divide(response.GetLoadLostW(), sBase),
		Steps:      response.GetSteps(),
		// Wall-clock seconds, not a power-system quantity. Dividing this by
		// sBase would be the same class of error as dividing a frequency by
		// it — a plausible-looking number on the wrong axis entirely.
		Elapsed: response.GetElapsedS(),
	}
}

// eventFromWire converts one logged occurrence.
func eventFromWire(wire *solverv1.CascadeEvent) run.Event {
	event := run.Event{
		At:      wire.GetAtS(), // seconds since the initiating outage, never divided
		Kind:    run.EventKind(eventKindTable[wire.GetKind()]),
		Element: wire.GetElement(),
		Detail:  wire.GetDetail(),
	}

	// An event before any split is not island-specific; reporting it against
	// island 0 would claim a split that had not happened yet.
	if wire.Island != nil {
		island := wire.GetIsland()
		event.Island = &island
	}

	return event
}

// islandFromWire converts one island's final state.
func islandFromWire(wire *solverv1.IslandState, sBase float64) run.Island {
	buses := wire.GetBuses()

	island := run.Island{
		Buses:     buses,
		BusCount:  len(buses),
		Condition: run.IslandCondition(conditionTable[wire.GetCondition()]),
		Verdict:   run.AcVerdict(verdictTable[wire.GetVerdict()]),
		Balance:   divide(wire.GetBalanceW(), sBase),
		Shed:      divide(wire.GetShedW(), sBase),
	}

	// Frequency, nadir and rate-of-change are HERTZ on the wire and stay
	// hertz here — the one place in this crossing where dividing by sBase
	// would be silent and plausible rather than obviously wrong. Each is
	// optional: no machine data means no frequency, not a frequency of zero.
	if wire.FrequencyHz != nil {
		frequency := wire.GetFrequencyHz()
		island.Frequency = &frequency
	}

	if wire.NadirHz != nil {
		nadir := wire.GetNadirHz()
		island.Nadir = &nadir
	}

	if wire.RocofHzS != nil {
		rocof := wire.GetRocofHzS()
		island.Rocof = &rocof
	}

	// Seconds and hertz both cross untouched. The only conversion this
	// extension performs is division by a power base, and neither of these
	// is a power. No trace means no machine ever reported on this island —
	// an absent curve, not one point sitting at zero.
	if trace := wire.GetTrace(); len(trace) > 0 {
		samples := make([]run.Sample, len(trace))
		for i, point := range trace {
			samples[i] = run.Sample{At: point.GetAtS(), Hz: point.GetHz()}
		}
		island.Trace = samples
	}

	return island
}

// These four tables share one defense: proto3 cannot tell an unset field
// from a deliberate zero, so *_UNSPECIFIED is left out of each rather than
// mapped to the first real value. A Go map returns "" for a key it lacks, so
// an unset enum comes back empty instead of reading as the most reassuring
// wrong answer available — the same guard screenabilityFromWire in
// extensions/contingency applies to its own single enum.
//
//nolint:gochecknoglobals // read-only lookup tables, never mutated
var (
	outcomeTable = map[solverv1.CascadeOutcome]string{
		solverv1.CascadeOutcome_CASCADE_OUTCOME_SETTLED:   string(run.OutcomeSettled),
		solverv1.CascadeOutcome_CASCADE_OUTCOME_TRUNCATED: string(run.OutcomeTruncated),
		solverv1.CascadeOutcome_CASCADE_OUTCOME_COLLAPSED: string(run.OutcomeCollapsed),
	}

	conditionTable = map[solverv1.IslandCondition]string{
		solverv1.IslandCondition_ISLAND_CONDITION_STABLE:     string(run.ConditionStable),
		solverv1.IslandCondition_ISLAND_CONDITION_RECOVERING: string(run.ConditionRecovering),
		solverv1.IslandCondition_ISLAND_CONDITION_SHEDDING:   string(run.ConditionShedding),
		solverv1.IslandCondition_ISLAND_CONDITION_COLLAPSED:  string(run.ConditionCollapsed),
	}

	verdictTable = map[solverv1.AcVerdict]string{
		solverv1.AcVerdict_AC_VERDICT_SECURE:     string(run.VerdictSecure),
		solverv1.AcVerdict_AC_VERDICT_VIOLATED:   string(run.VerdictViolated),
		solverv1.AcVerdict_AC_VERDICT_INSOLVABLE: string(run.VerdictInsolvable),
	}

	eventKindTable = map[solverv1.CascadeEventKind]string{
		solverv1.CascadeEventKind_CASCADE_EVENT_KIND_OVERLOAD: string(run.EventOverload),
		solverv1.CascadeEventKind_CASCADE_EVENT_KIND_TRIP:     string(run.EventTrip),
		solverv1.CascadeEventKind_CASCADE_EVENT_KIND_ISLAND:   string(run.EventIsland),
		solverv1.CascadeEventKind_CASCADE_EVENT_KIND_SHED:     string(run.EventShed),
		//nolint:misspell // matches the generated proto enum name
		solverv1.CascadeEventKind_CASCADE_EVENT_KIND_STABILISE: string(run.EventStabilize),
	}
)

// divide converts SI to per unit. A zero base means the model did not state
// one; returning the SI value rather than an infinity keeps the failure
// legible.
func divide(value, base float64) float64 {
	if base == 0 {
		return value
	}

	return value / base
}

// nonNilStrings turns a nil slice into an empty one.
//
// A proto getter returns nil, not an empty slice, for an unset or empty
// repeated field — and encoding/json renders a nil slice as `null`, not
// `[]`, regardless of any omitempty tag (which would drop the field
// entirely, not fix its value). CascadeRun.Initiating has no omitempty for
// exactly that reason: the field must always be present, as an array, so
// this is the one place that guarantees it.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}

// emit publishes a domain event, best effort and out of transaction: a
// subscriber that fails must not undo a simulation that succeeded.
func (e *Engine) emit(ctx context.Context, stored run.CascadeRun) {
	if e.bus == nil {
		return
	}

	e.bus.Emit(ctx, hooks.Event{
		Name:      "cascade.simulated",
		Extension: "cascade",
		Entity:    "CascadeRun",
		EntityID:  stored.ID.String(),
		Op:        hooks.OpCreate,
		At:        stored.FinishedAt,
		Payload:   stored,
	})
}
