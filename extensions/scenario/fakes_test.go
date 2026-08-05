package scenario

import (
	"context"

	"github.com/xraph/blackstart/extensions/cascade"
	cascaderun "github.com/xraph/blackstart/extensions/cascade/run"
	"github.com/xraph/blackstart/extensions/contingency"
	"github.com/xraph/blackstart/extensions/contingency/sweep"
	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	nmmodel "github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wxid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/pkg/id"
)

// theCycle is the forecast every manifest here pins. Its issuance is the
// knowledge-time axis of the rating lookup, which is the only reason the
// engine resolves a cycle at all.
var theCycle = forecast.Cycle{
	ID:       id.MustNew(wxid.Cycle),
	FeedID:   dsid.NewFeed(),
	Model:    forecast.ModelHRRR,
	IssuedAt: issued,
	Status:   forecast.StatusSampled,
}

// fakeModels answers with one stored model and its canonical bytes.
type fakeModels struct {
	found    nmmodel.NetworkModel
	foundErr error

	encoded    []byte
	encodedErr error
}

func (m *fakeModels) FindByHash(_ context.Context, _ string) (nmmodel.NetworkModel, error) {
	if m.foundErr != nil {
		return nmmodel.NetworkModel{}, m.foundErr
	}

	return m.found, nil
}

func (m *fakeModels) Encoding(_ context.Context, _ string) ([]byte, error) {
	if m.encodedErr != nil {
		return nil, m.encodedErr
	}

	return m.encoded, nil
}

// fakeCycles answers with one forecast cycle.
type fakeCycles struct {
	cycle forecast.Cycle
	err   error
}

func (c *fakeCycles) Cycle(_ context.Context, _ id.ID) (forecast.Cycle, error) {
	if c.err != nil {
		return forecast.Cycle{}, c.err
	}

	return c.cycle, nil
}

// fakeScreener records the request it was handed and answers with one sweep.
type fakeScreener struct {
	run sweep.Run
	err error

	request contingency.Request
	calls   int
}

func (s *fakeScreener) Screen(_ context.Context, in contingency.Request) (sweep.Run, error) {
	s.calls++
	s.request = in

	if s.err != nil {
		return sweep.Run{}, s.err
	}

	return s.run, nil
}

// fakeSimulator records the request it was handed and answers with one
// cascade.
type fakeSimulator struct {
	run cascaderun.CascadeRun
	err error

	request cascade.Request
	calls   int
}

func (s *fakeSimulator) Simulate(
	_ context.Context, in cascade.Request,
) (cascaderun.CascadeRun, error) {
	s.calls++
	s.request = in

	if s.err != nil {
		return cascaderun.CascadeRun{}, s.err
	}

	return s.run, nil
}

// fakes is the whole collaborator set, kept together so a test can reach the
// one it wants to break.
type fakes struct {
	models    *fakeModels
	cycles    *fakeCycles
	geo       *fakeGeo
	hazard    *fakeHazard
	screener  *fakeScreener
	simulator *fakeSimulator
}

func (f *fakes) collaborators() Collaborators {
	return Collaborators{
		Models:    f.models,
		Cycles:    f.cycles,
		Geo:       f.geo,
		Hazard:    f.hazard,
		Screener:  f.screener,
		Simulator: f.simulator,
	}
}

// newFakes wires a collaborator set that answers every call. A test that
// cares about one stage reaches in and breaks that one.
func newFakes() *fakes {
	return &fakes{
		models:    &fakeModels{},
		cycles:    &fakeCycles{cycle: theCycle},
		geo:       &fakeGeo{route: aRoute, spans: threeSpans},
		hazard:    &fakeHazard{ampacity: 900},
		screener:  &fakeScreener{},
		simulator: &fakeSimulator{},
	}
}
