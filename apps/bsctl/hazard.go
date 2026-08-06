package bsctl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"

	"github.com/xraph/blackstart/extensions/geo"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/hazard"
	"github.com/xraph/blackstart/extensions/hazard/conductor"
	hid "github.com/xraph/blackstart/extensions/hazard/id"
	hzstore "github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/extensions/networkmodel"
	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// hazardCommand groups the hazard subcommands.
func hazardCommand() cli.Command {
	return cli.NewCommand(
		"hazard",
		"Conductor loading and dynamic line ratings",
		func(ctx cli.CommandContext) error {
			ctx.Println("usage: bsctl hazard <conductors|assign|assume|assignments|compute|rating|loading>")

			return nil
		},
		cli.WithSubcommand(hazardConductorsCommand()),
		cli.WithSubcommand(hazardAssignCommand()),
		cli.WithSubcommand(hazardAssumeCommand()),
		cli.WithSubcommand(hazardAssignmentsCommand()),
		cli.WithSubcommand(hazardComputeCommand()),
		cli.WithSubcommand(hazardRatingCommand()),
		cli.WithSubcommand(hazardLoadingCommand()),
	)
}

// hazardConductorsCommand prints the library.
//
// No engine and no database: the library is compiled in, so this answers even
// against a stack that is not running.
func hazardConductorsCommand() cli.Command {
	return cli.NewCommand(
		"conductors",
		"List the conductor library",
		func(ctx cli.CommandContext) error {
			ctx.Printf("%-10s %-28s %8s %8s %10s %10s\n",
				"CODE", "NAME", "DIAM_M", "AREA_MM2", "R25_OHM/M", "MAX_C")

			for _, got := range conductor.Library() {
				ctx.Printf("%-10s %-28s %8.5f %8.1f %10.3e %10.0f\n",
					got.Code, got.Name, got.DiameterM, got.AreaMM2,
					got.ResistanceLowOhmPerM, got.MaxTempC)
			}

			return nil
		},
	)
}

// hazardAssignCommand binds a branch to a conductor.
func hazardAssignCommand() cli.Command {
	return cli.NewCommand(
		"assign",
		"Say which conductor a branch is strung with",
		func(ctx cli.CommandContext) error {
			engine, cleanup, err := hazardEngine(ctx.Context())
			if err != nil {
				return err
			}

			defer cleanup()

			branch, err := id.Parse(ctx.String("branch"))
			if err != nil {
				return fmt.Errorf("bsctl: --branch: %w", err)
			}

			code := ctx.String("conductor")
			basis := conductor.Basis(ctx.String("basis"))

			// An explicit conductor and an assumed basis contradict each other,
			// and so do the reverse. Refusing beats silently believing one of
			// them, because the basis is the whole record of how much the
			// rating can be trusted.
			if code == "" {
				kv := float64(ctx.Int("kv"))
				if kv <= 0 {
					return errors.New(
						"bsctl: give --conductor, or --kv so a conductor can be " +
							"assumed from the voltage class")
				}

				code, err = conductor.AssumeByVoltage(kv)
				if err != nil {
					return err
				}

				basis = conductor.AssumedByVoltage

				ctx.Printf("assuming %s for %.0f kV\n", code, kv)
			}

			assignment := conductor.Assignment{
				ID:            hid.NewAssignment(),
				CarrierID:     branch,
				ConductorCode: code,
				Basis:         basis,
				Note:          ctx.String("note"),
			}

			if err := engine.Assign(ctx.Context(), assignment); err != nil {
				return err
			}

			ctx.Printf("assignment %s\n", assignment.ID)
			ctx.Printf("branch     %s\n", assignment.CarrierID)
			ctx.Printf("conductor  %s\n", assignment.ConductorCode)
			ctx.Printf("basis      %s\n", assignment.Basis)

			return nil
		},
		cli.WithFlag(cli.NewStringFlag("branch", "", "electrical branch identifier", "")),
		cli.WithFlag(cli.NewStringFlag("conductor", "", "conductor code from the library", "")),
		cli.WithFlag(cli.NewStringFlag(
			"basis", "", "surveyed or assumed_by_voltage", string(conductor.Surveyed))),
		cli.WithFlag(cli.NewIntFlag(
			"kv", "", "voltage class in kV, to assume a conductor when none is given", 0)),
		cli.WithFlag(cli.NewStringFlag("note", "", "free text for whoever surveyed it", "")),
	)
}

// hazardAssignmentsCommand lists assignments.
func hazardAssignmentsCommand() cli.Command {
	return cli.NewCommand(
		"assignments",
		"List which conductor each branch is strung with",
		func(ctx cli.CommandContext) error {
			engine, cleanup, err := hazardEngine(ctx.Context())
			if err != nil {
				return err
			}

			defer cleanup()

			found, err := engine.Assignments(
				ctx.Context(), pkgstore.Page{Limit: ctx.Int("limit")})
			if err != nil {
				return err
			}

			if len(found) == 0 {
				ctx.Println("no assignments; every span is unrated")

				return nil
			}

			for _, got := range found {
				ctx.Printf("%s  %-24s %-10s %s\n",
					got.ID, got.CarrierID, got.ConductorCode, got.Basis)
			}

			return nil
		},
		cli.WithFlag(cli.NewIntFlag("limit", "", "maximum results", 50)),
	)
}

// hazardComputeCommand runs the models over one weather cycle.
func hazardComputeCommand() cli.Command {
	return cli.NewCommand(
		"compute",
		"Run the hazard models over one weather cycle",
		func(ctx cli.CommandContext) error {
			engine, cleanup, err := hazardEngine(ctx.Context())
			if err != nil {
				return err
			}

			defer cleanup()

			cycle, err := wid.ParseCycle(ctx.String("cycle"))
			if err != nil {
				return fmt.Errorf("bsctl: --cycle: %w", err)
			}

			result, err := engine.Compute(ctx.Context(), hazard.ComputeRequest{
				CycleID: cycle,
			})
			if err != nil {
				return err
			}

			ctx.Printf("computation %s\n", result.ComputationID)
			ctx.Printf("run         %s\n", orNone(result.RunID))
			ctx.Printf("spans       %d\n", result.Spans)
			ctx.Printf("unrated     %d\n", result.Unrated)
			ctx.Printf("values      %d\n", result.Values)
			ctx.Printf("covered     %d of %d span-hours\n", result.Covered, result.Total)

			if result.Unrated > 0 {
				ctx.Printf(
					"\n%d spans produced nothing because their carrier has no "+
						"conductor. `bsctl hazard assume` assigns one to every "+
						"carrier that states a voltage; the rest need `bsctl hazard "+
						"assign`, because nothing here guesses a voltage.\n",
					result.Unrated)
			}

			return nil
		},
		cli.WithFlag(cli.NewStringFlag("cycle", "", "weather cycle identifier (wxcyc_…)", "")),
	)
}

// hazardRatingCommand prints a span's thermal rating.
func hazardRatingCommand() cli.Command {
	return cli.NewCommand(
		"rating",
		"Show a span's dynamic line rating",
		func(ctx cli.CommandContext) error {
			return showSpanValues(ctx, []hzstore.Quantity{hzstore.QuantityAmpacity})
		},
		spanValueFlags()...,
	)
}

// hazardLoadingCommand prints a span's mechanical loading.
func hazardLoadingCommand() cli.Command {
	return cli.NewCommand(
		"loading",
		"Show a span's wind and ice loading",
		func(ctx cli.CommandContext) error {
			return showSpanValues(ctx, []hzstore.Quantity{
				hzstore.QuantityWindLoad,
				hzstore.QuantityIceThickness,
				hzstore.QuantityIceLoad,
				hzstore.QuantityCombinedLoad,
			})
		},
		spanValueFlags()...,
	)
}

func spanValueFlags() []cli.CommandOption {
	return []cli.CommandOption{
		cli.WithFlag(cli.NewStringFlag("span", "", "span identifier (span_…)", "")),
		cli.WithFlag(cli.NewStringFlag("issued", "", "forecast issuance, RFC 3339", "")),
	}
}

// showSpanValues serves both read subcommands, which differ only in which
// series they ask for.
func showSpanValues(ctx cli.CommandContext, quantities []hzstore.Quantity) error {
	engine, cleanup, err := hazardEngine(ctx.Context())
	if err != nil {
		return err
	}

	defer cleanup()

	span, err := gid.ParseSpan(ctx.String("span"))
	if err != nil {
		return err
	}

	issued, err := time.Parse(time.RFC3339, ctx.String("issued"))
	if err != nil {
		return fmt.Errorf("bsctl: --issued must be an RFC 3339 timestamp: %w", err)
	}

	// A generous window rather than a required one: at the command line the
	// useful question is "what did this run say", and a forecast horizon is
	// days at most. The API keeps it required, because a window it chose would
	// give the same request a different answer tomorrow.
	const horizon = 15 * 24 * time.Hour

	values, err := engine.SpanValues(
		ctx.Context(), span, quantities, issued, issued, issued.Add(horizon))
	if err != nil {
		return err
	}

	if len(values) == 0 {
		ctx.Println("no values for that span and issuance")

		return nil
	}

	for _, got := range values {
		ctx.Printf("%s  %-15s %12.4f %s\n",
			got.ValidAt.Format(time.RFC3339), got.Quantity,
			got.Value, unitOf(got.Quantity))
	}

	return nil
}

// unitOf names what a quantity is measured in, for display only.
func unitOf(q hzstore.Quantity) string {
	switch q {
	case hzstore.QuantityAmpacity:
		return "A"
	case hzstore.QuantityWindLoad, hzstore.QuantityCombinedLoad:
		return "N/m"
	case hzstore.QuantityIceLoad:
		return "kg/m"
	case hzstore.QuantityIceThickness:
		return "m"
	default:
		return ""
	}
}

// orNone renders an absent identifier as a word rather than as an empty
// column, so that "this pass recorded no provenance run" reads as a fact.
func orNone(want id.ID) string {
	if want.IsZero() {
		return "none (no provenance recorder in this deployment)"
	}

	return want.String()
}

// hazardEngine boots the application and resolves the engine.
func hazardEngine(ctx context.Context) (*hazard.Engine, func(), error) {
	app, err := App()
	if err != nil {
		return nil, nil, err
	}

	if err := app.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("starting the application: %w", err)
	}

	cleanup := func() { _ = app.Stop(context.WithoutCancel(ctx)) }

	engine, err := forge.Inject[*hazard.Engine](app.Container())
	if err != nil {
		cleanup()

		return nil, nil, fmt.Errorf("resolving the hazard engine: %w", err)
	}

	return engine, cleanup, nil
}

// hazardAssumeCommand gives a conductor to every carrier that states a voltage.
func hazardAssumeCommand() cli.Command {
	return cli.NewCommand(
		"assume",
		"Assume a conductor for every carrier whose voltage is known",
		func(ctx cli.CommandContext) error {
			hazardEngine, geoEngine, modelEngine, cleanup, err := hazardGeoAndModel(ctx.Context())
			if err != nil {
				return err
			}

			defer cleanup()

			candidates, err := assumeCandidates(
				ctx.Context(), hazardEngine, geoEngine, modelEngine, ctx.Int("limit"))
			if err != nil {
				return err
			}

			dryRun := ctx.Bool("dry-run")

			plan, err := hazardEngine.Assume(ctx.Context(), candidates, dryRun)
			if err != nil {
				return err
			}

			if dryRun {
				ctx.Println("dry run — nothing was written")
			}

			for _, got := range plan.Assume {
				ctx.Printf("%-30s %s\n", got.CarrierID, got.ConductorCode)
			}

			// Every number, every time. A command reporting only what it did is
			// how somebody comes to believe the map is fuller than it is — and
			// the skip reasons are not interchangeable: no voltage is a gap in
			// the source data, already assigned is a decision somebody made,
			// and a transformer is neither — it is a carrier this command
			// correctly refuses to string a conductor onto.
			ctx.Printf("\nassigned          %d\n", len(plan.Assume))
			ctx.Printf("no voltage        %d\n", len(plan.NoVoltage))
			ctx.Printf("transformers      %d\n", len(plan.Transformers))
			ctx.Printf("already assigned  %d\n", len(plan.AlreadyAssigned))

			return nil
		},
		cli.WithUsage("bsctl hazard assume [--dry-run] [--limit N]"),
		cli.WithFlag(cli.NewBoolFlag(
			"dry-run", "", "report what would be assigned without writing", false)),
		cli.WithFlag(cli.NewIntFlag(
			"limit", "", "how many routes to consider", 1000)),
	)
}

// branchVoltagesKV converts a branch's two end voltages into the kilovolts a
// conductor.Candidate carries.
//
// The one conversion site: model.Bus.VNominalV is VOLTS and Candidate is
// kilovolts, and doing this in two places is how the two drift.
//
// "States a voltage" is canonical.StatedNominalV and is not re-spelled here.
// That function exists because this question had been answered three different
// ways, and a bus holding +Inf read as stated in two of them.
func branchVoltagesKV(from, to model.Bus) (float64, float64) {
	return statedKV(from), statedKV(to)
}

// statedKV is one bus's voltage in kilovolts, or zero when it states none.
//
// Zero rather than an error: PlanAssumptions already reads zero as "the source
// did not say", which is a skip rather than a default, and raising here would
// abandon a pass over every other carrier for one quiet bus.
func statedKV(bus model.Bus) float64 {
	volts, stated := canonical.StatedNominalV(bus.VNominalV)
	if !stated {
		return 0
	}

	return volts / 1e3
}

// assumeCandidates collects the carriers a pass could assign.
//
// Both carrier kinds, uniformly. A circuit states its own voltage from the
// source it was imported from; a branch runs at the voltage its buses sit at,
// which the model records per bus since the canonical encoding started
// carrying it. Neither is an operator-supplied default, which is the thing
// this command refuses to record as assumed_by_voltage.
//
// A branch whose ends sit at different voltages is a transformer.
// PlanAssumptions decides that, not this function — collecting is not
// deciding, and the rule that stops this command inventing conductors for
// transformers belongs with the other rules, where it is testable without a
// store.
func assumeCandidates(
	ctx context.Context, hz *hazard.Engine, gx *geo.Engine, nm *networkmodel.Engine, limit int,
) ([]conductor.Candidate, error) {
	routes, err := gx.Routes(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("bsctl: reading routes: %w", err)
	}

	candidates := make([]conductor.Candidate, 0, len(routes))

	for _, route := range routes {
		// Absence is not an error here. ForCarrier missing is the ordinary
		// answer for a carrier nobody has assigned, which is the whole set
		// this command exists to shrink.
		_, err := hz.AssignmentForCarrier(ctx, route.CarrierID)
		assigned := err == nil

		if err != nil && !errors.Is(err, bserr.ErrNotFound) {
			return nil, fmt.Errorf(
				"bsctl: reading the assignment of %s: %w", route.CarrierID, err)
		}

		candidate := conductor.Candidate{CarrierID: route.CarrierID, Assigned: assigned}

		if gid.IsCircuit(route.CarrierID) {
			circuit, err := gx.Circuit(ctx, route.CarrierID)
			if err != nil {
				return nil, fmt.Errorf(
					"bsctl: reading circuit %s: %w", route.CarrierID, err)
			}

			candidate.VoltageKV = circuit.VoltageKV
		} else {
			fromKV, toKV, err := branchEndVoltages(ctx, nm, route.CarrierID)
			if err != nil {
				return nil, err
			}

			candidate.VoltageKV = fromKV
			candidate.FarEndVoltageKV = toKV
		}

		candidates = append(candidates, candidate)
	}

	return candidates, nil
}

// branchEndVoltages reads the voltages a branch's two buses sit at, in kV.
//
// Three lookups — the branch, then each end — rather than one join, because
// that is the shape the store offers and a pass over fourteen routes is not
// where this platform's time goes.
//
// A bus row that is absent yields zero rather than an error. A model imported
// before buses were projected has no bus rows at all, and that is a gap in the
// record rather than a fault: it lands the branch in NoVoltage, counted, which
// is the honest report. A model that cannot be read at all is a different
// thing and does fail.
func branchEndVoltages(
	ctx context.Context, nm *networkmodel.Engine, carrier id.ID,
) (float64, float64, error) {
	branch, err := nm.Store().Branches().Get(ctx, carrier)
	if err != nil {
		if errors.Is(err, bserr.ErrNotFound) {
			return 0, 0, nil
		}

		return 0, 0, fmt.Errorf("bsctl: reading branch %s: %w", carrier, err)
	}

	from, err := busOrAbsent(ctx, nm, branch.ModelID, branch.FromBus)
	if err != nil {
		return 0, 0, err
	}

	to, err := busOrAbsent(ctx, nm, branch.ModelID, branch.ToBus)
	if err != nil {
		return 0, 0, err
	}

	fromKV, toKV := branchVoltagesKV(from, to)

	return fromKV, toKV, nil
}

// busOrAbsent reads one bus, treating not-found as a bus that states nothing.
func busOrAbsent(
	ctx context.Context, nm *networkmodel.Engine, modelID id.ID, ordinal string,
) (model.Bus, error) {
	bus, err := nm.Store().Buses().ByOrdinal(ctx, modelID, ordinal)
	if err != nil {
		if errors.Is(err, bserr.ErrNotFound) {
			return model.Bus{}, nil
		}

		return model.Bus{}, fmt.Errorf(
			"bsctl: reading bus %s of %s: %w", ordinal, modelID, err)
	}

	return bus, nil
}

// hazardGeoAndModel resolves all three engines from one application.
//
// One application, not two or three. Each helper starts its own, and starting
// the stack more than once in one command opens multiple connection pools and
// runs the migration orchestrator concurrently against the same database —
// the same reasoning as modelAndGeo, for the same one-application pattern.
func hazardGeoAndModel(
	ctx context.Context,
) (*hazard.Engine, *geo.Engine, *networkmodel.Engine, func(), error) {
	app, err := App()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	if err := app.Start(ctx); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("starting the application: %w", err)
	}

	cleanup := func() { _ = app.Stop(context.WithoutCancel(ctx)) }

	hz, err := forge.Inject[*hazard.Engine](app.Container())
	if err != nil {
		cleanup()

		return nil, nil, nil, nil, fmt.Errorf("resolving the hazard engine: %w", err)
	}

	gx, err := forge.Inject[*geo.Engine](app.Container())
	if err != nil {
		cleanup()

		return nil, nil, nil, nil, fmt.Errorf("resolving the geo engine: %w", err)
	}

	nm, err := forge.Inject[*networkmodel.Engine](app.Container())
	if err != nil {
		cleanup()

		return nil, nil, nil, nil, fmt.Errorf("resolving the model engine: %w", err)
	}

	return hz, gx, nm, cleanup, nil
}
