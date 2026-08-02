package networkmodel_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/networkmodel"
	"github.com/xraph/blackstart/extensions/networkmodel/store/memory"
	"github.com/xraph/blackstart/extensions/provenance"
	provmemory "github.com/xraph/blackstart/extensions/provenance/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
)

const twoBus = `
mpc.version = '2';
mpc.baseMVA = 100;
mpc.bus = [
	1	3	0	0	0	0	1	1.06	0	138	1	1.06	0.94;
	2	1	50	20	0	0	1	1.00	0	138	1	1.06	0.94;
];
mpc.gen = [
	1	50	0	100	-100	1.06	100	1	200	0;
];
mpc.branch = [
	1	2	0.01	0.1	0.02	200	0	0	0	0	1;
];
`

func engine(t *testing.T, recorder provenance.Recorder, bus *hooks.Bus) *networkmodel.Engine {
	t.Helper()

	e, err := networkmodel.NewEngine(memory.New(), recorder, bus)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return e
}

// A real recorder over its in-memory store, not a fake. The chain this engine
// writes has to be readable afterwards, and a fake that accepted anything
// would let a malformed record through.
func recorder(t *testing.T) *provenance.Engine {
	t.Helper()

	r, err := provenance.NewEngine(provmemory.New(), nil)
	if err != nil {
		t.Fatalf("provenance.NewEngine: %v", err)
	}

	return r
}

func TestAnEngineNeedsAStore(t *testing.T) {
	if _, err := networkmodel.NewEngine(nil, nil, nil); err == nil {
		t.Error("NewEngine accepted a nil store")
	}
}

func TestImportingACaseStoresAModel(t *testing.T) {
	e := engine(t, recorder(t), nil)

	got, err := e.Import(t.Context(), networkmodel.ImportRequest{
		Source: []byte(twoBus),
		URL:    "file:///cases/two-bus.m",
		Name:   "two-bus",
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if got.Model.BusCount != 2 || got.Model.BranchCount != 1 {
		t.Errorf("model has %d buses and %d branches",
			got.Model.BusCount, got.Model.BranchCount)
	}

	if len(got.Model.ContentHash) != 64 {
		t.Errorf("content hash %q is not a hex sha256", got.Model.ContentHash)
	}

	if got.AlreadyPresent {
		t.Error("a first import reported the model as already present")
	}

	if !got.Traceable {
		t.Error("an import with a recorder reported an incomplete chain")
	}
}

// Re-importing an unchanged file is a normal thing to do and must produce no
// new model — otherwise every re-import invalidates the solver cache for a
// network that did not change.
func TestReimportingAnUnchangedFileBuildsNothingNew(t *testing.T) {
	e := engine(t, recorder(t), nil)

	first, err := e.Import(t.Context(), networkmodel.ImportRequest{Source: []byte(twoBus)})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	second, err := e.Import(t.Context(), networkmodel.ImportRequest{Source: []byte(twoBus)})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if !second.AlreadyPresent {
		t.Error("a re-import did not report the model as already present")
	}

	if second.Model.ID != first.Model.ID {
		t.Errorf("a re-import produced a new model: %s then %s",
			first.Model.ID, second.Model.ID)
	}

	found, err := e.List(t.Context(), 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(found) != 1 {
		t.Errorf("got %d models after two imports of one file", len(found))
	}
}

// A changed file is a different network and must produce a different model.
func TestAChangedFileBuildsANewModel(t *testing.T) {
	e := engine(t, recorder(t), nil)

	first, err := e.Import(t.Context(), networkmodel.ImportRequest{Source: []byte(twoBus)})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	changed := strings.Replace(twoBus, "0.01	0.1", "0.02	0.2", 1)

	second, err := e.Import(t.Context(), networkmodel.ImportRequest{Source: []byte(changed)})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if second.Model.ContentHash == first.Model.ContentHash {
		t.Error("changing a branch impedance did not change the content hash")
	}

	if second.AlreadyPresent {
		t.Error("a changed file was reported as already present")
	}
}

// The stored bytes are what a solve uploads. Read back rather than re-encoded,
// so the bytes a result was computed from are the bytes that were stored.
func TestTheStoredEncodingIsReadableByHash(t *testing.T) {
	e := engine(t, nil, nil)

	got, err := e.Import(t.Context(), networkmodel.ImportRequest{Source: []byte(twoBus)})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	encoded, err := e.Encoding(t.Context(), got.Model.ContentHash)
	if err != nil {
		t.Fatalf("Encoding: %v", err)
	}

	if len(encoded) == 0 {
		t.Fatal("the stored encoding is empty")
	}

	if encoded[0] != 1 {
		t.Errorf("the encoding does not begin with the canonical version byte")
	}
}

func TestSomethingThatIsNotACaseIsRefusedWithItsReason(t *testing.T) {
	e := engine(t, nil, nil)

	for name, source := range map[string]string{
		"empty":    "",
		"a CSV":    "bus,voltage\n1,1.0\n",
		"raw JSON": `{"buses": []}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := e.Import(t.Context(), networkmodel.ImportRequest{Source: []byte(source)})
			if err == nil {
				t.Fatalf("Import accepted %s", name)
			}
		})
	}

	// And the message names the format this build reads, so a caller with a
	// PSS/E file learns why rather than guessing.
	_, err := e.Import(t.Context(), networkmodel.ImportRequest{Source: []byte("nonsense")})
	if err == nil || !strings.Contains(err.Error(), "MATPOWER") {
		t.Errorf("the message does not say what format is read: %v", err)
	}
}

func TestAMissingModelIsNotFound(t *testing.T) {
	e := engine(t, nil, nil)

	if _, err := e.Get(t.Context(), id.MustNew("nmdl")); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestBuildingAModelEmitsADomainEvent(t *testing.T) {
	bus := hooks.NewBus()

	var seen []string
	bus.SubscribeAll(func(_ context.Context, e hooks.Event) error {
		seen = append(seen, e.Name)

		return nil
	})

	e := engine(t, nil, bus)

	if _, err := e.Import(t.Context(), networkmodel.ImportRequest{Source: []byte(twoBus)}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	if len(seen) != 1 || seen[0] != "networkmodel.built" {
		t.Errorf("events %v", seen)
	}
}
