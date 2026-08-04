package solver_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/xraph/blackstart/pkg/solver"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// fake is a solver that records what it was asked and can be told to behave
// like one that has just restarted.
//
// A fake rather than the real Rust service: what is under test here is the
// client's retry and hashing behavior, and running an actual solver would
// make these tests depend on a cargo build and on the numerics being correct,
// neither of which is the subject.
type fake struct {
	solverv1.UnimplementedSolverServiceServer

	models map[string][]byte

	solves    int
	simulates int
	screens   int
	uploads   int

	// forgetOnce drops every model exactly once, when the next solve or screen
	// arrives. That is what a restarted solver looks like to a client holding
	// a hash it uploaded before the restart.
	forgetOnce bool

	// refuseUploads makes every upload succeed on the wire and store nothing,
	// which is how a client's retry can be observed failing a second time
	// rather than quietly recovering.
	refuseUploads bool
}

func newFake() *fake {
	return &fake{models: map[string][]byte{}}
}

func (f *fake) PutNetworkModel(stream solverv1.SolverService_PutNetworkModelServer) error {
	f.uploads++

	var (
		hash string
		body []byte
	)

	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		switch payload := message.GetPayload().(type) {
		case *solverv1.PutNetworkModelRequest_Header:
			hash = payload.Header.GetContentHash()
		case *solverv1.PutNetworkModelRequest_Chunk:
			body = append(body, payload.Chunk...)
		}
	}

	_, present := f.models[hash]
	if !f.refuseUploads {
		f.models[hash] = body
	}

	return stream.SendAndClose(&solverv1.PutNetworkModelResponse{
		ContentHash:    hash,
		AlreadyPresent: present,
	})
}

func (f *fake) HasNetworkModel(
	_ context.Context, request *solverv1.HasNetworkModelRequest,
) (*solverv1.HasNetworkModelResponse, error) {
	_, present := f.models[request.GetContentHash()]

	return &solverv1.HasNetworkModelResponse{Present: present}, nil
}

// forget empties the cache the first time it is called after forgetOnce is set.
func (f *fake) forget() {
	if f.forgetOnce {
		f.models = map[string][]byte{}
		f.forgetOnce = false
	}
}

func (f *fake) SolvePowerFlow(
	_ context.Context, request *solverv1.SolvePowerFlowRequest,
) (*solverv1.SolvePowerFlowResponse, error) {
	f.forget()
	f.solves++

	if _, present := f.models[request.GetContentHash()]; !present {
		return nil, status.Error(codes.FailedPrecondition, "upload it with PutNetworkModel")
	}

	return &solverv1.SolvePowerFlowResponse{
		ModelHash:     request.GetContentHash(),
		SolverVersion: "fake",
	}, nil
}

func (f *fake) SimulateCascade(
	_ context.Context, request *solverv1.SimulateCascadeRequest,
) (*solverv1.SimulateCascadeResponse, error) {
	f.forget()
	f.simulates++

	if _, present := f.models[request.GetContentHash()]; !present {
		return nil, status.Error(codes.FailedPrecondition, "upload it with PutNetworkModel")
	}

	return &solverv1.SimulateCascadeResponse{
		ModelHash:     request.GetContentHash(),
		SolverVersion: "fake",
	}, nil
}

func (f *fake) ScreenContingencies(
	request *solverv1.ScreenContingenciesRequest,
	stream solverv1.SolverService_ScreenContingenciesServer,
) error {
	f.forget()
	f.screens++

	if _, present := f.models[request.GetContentHash()]; !present {
		return status.Error(codes.FailedPrecondition, "upload it with PutNetworkModel")
	}

	for i := range 3 {
		result := &solverv1.ScreenContingenciesResponse{
			Contingency: &solverv1.Contingency{
				Id:     "branch",
				Branch: uint32(i),
			},
			Screenability: solverv1.Screenability_SCREENABILITY_SCREENED,
		}

		if err := stream.Send(result); err != nil {
			return err
		}
	}

	return nil
}

// dial stands the fake up over an in-process listener.
func dial(t *testing.T, server *fake) *solver.Client {
	t.Helper()

	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	solverv1.RegisterSolverServiceServer(grpcServer, server)

	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dialing the fake: %v", err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	return solver.New(conn)
}

func model() *solverv1.NetworkModel {
	return &solverv1.NetworkModel{
		Name:     "two-bus",
		SBaseVa:  100e6,
		VBaseV:   138e3,
		Buses:    []*solverv1.Bus{{Id: "s"}, {Id: "l"}},
		Branches: []*solverv1.Branch{{Id: "line", FromBus: 0, ToBus: 1}},
	}
}

// The same model must always hash to the same value, or the cache is useless.
// Protobuf serialization is not canonical by default, which is why Hash sets
// Deterministic rather than relying on Marshal.
func TestTheSameModelAlwaysHashesTheSame(t *testing.T) {
	first, body, err := solver.Hash(model())
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if len(first) != 64 {
		t.Errorf("hash %q is not a hex sha256", first)
	}

	if len(body) == 0 {
		t.Error("Hash returned no bytes to upload")
	}

	for range 20 {
		again, _, err := solver.Hash(model())
		if err != nil {
			t.Fatalf("Hash: %v", err)
		}

		if again != first {
			t.Fatalf("the same model hashed to %q then %q", first, again)
		}
	}
}

func TestADifferentModelHashesDifferently(t *testing.T) {
	base, _, _ := solver.Hash(model())

	changed := model()
	changed.Buses[1].PInjectionW = -1e6

	other, _, err := solver.Hash(changed)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if base == other {
		t.Error("changing an injection did not change the hash")
	}
}

func TestUploadStoresTheModelUnderItsHash(t *testing.T) {
	server := newFake()
	client := dial(t, server)

	hash, err := client.Upload(t.Context(), model())
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	present, err := client.Has(t.Context(), hash)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}

	if !present {
		t.Error("the server does not have the model that was just uploaded")
	}
}

// The behavior this client exists for. The solver keeps models in memory, so
// a restart loses all of them; without an automatic re-upload every restart
// would surface as a wave of failures across the platform.
func TestASolveReUploadsAfterTheServerForgets(t *testing.T) {
	server := newFake()
	client := dial(t, server)

	if _, err := client.Upload(t.Context(), model()); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	server.forgetOnce = true

	response, err := client.Solve(t.Context(), model(), &solverv1.SolvePowerFlowRequest{
		Method: solverv1.Method_METHOD_AC,
	})
	if err != nil {
		t.Fatalf("Solve should have recovered from a restarted server: %v", err)
	}

	if response.GetSolverVersion() != "fake" {
		t.Errorf("got %q back", response.GetSolverVersion())
	}

	if server.solves != 2 {
		t.Errorf("solved %d times, want one failure and one retry", server.solves)
	}

	if server.uploads != 2 {
		t.Errorf("uploaded %d times, want the original plus the recovery", server.uploads)
	}
}

// Exactly one retry. A second failure after a successful upload means
// something other than a cold cache is wrong, and retrying again would turn a
// bug into a loop.
func TestASolveRetriesOnlyOnce(t *testing.T) {
	server := newFake()
	client := dial(t, server)

	// Accept the upload on the wire and store nothing, so the retry fails the
	// same way the first attempt did.
	server.refuseUploads = true

	_, err := client.Solve(t.Context(), model(), &solverv1.SolvePowerFlowRequest{
		Method: solverv1.Method_METHOD_AC,
	})
	if err == nil {
		t.Fatal("Solve succeeded against a server that never stored the model")
	}

	if server.solves != 2 {
		t.Errorf("solved %d times, want exactly one retry", server.solves)
	}
}

// A caller must not be able to supply a hash naming something other than the
// model it passed, or the two could describe different networks.
func TestSolveNamesTheModelItWasGiven(t *testing.T) {
	server := newFake()
	client := dial(t, server)

	hash, err := client.Upload(t.Context(), model())
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	response, err := client.Solve(t.Context(), model(), &solverv1.SolvePowerFlowRequest{
		Method: solverv1.Method_METHOD_DC,
		// Deliberately wrong, and deliberately ignored.
		ContentHash: "not-the-model-you-passed",
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}

	if response.GetModelHash() != hash {
		t.Errorf("solved %q, want the hash of the model passed in, %q",
			response.GetModelHash(), hash)
	}
}

// Simulate shares Solve's upload-and-retry, proven the same way: a model
// present before the server forgets it must still simulate successfully
// after, with exactly one extra upload and one retried call.
func TestSimulateReUploadsAfterTheServerForgets(t *testing.T) {
	server := newFake()
	client := dial(t, server)

	if _, err := client.Upload(t.Context(), model()); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	server.forgetOnce = true

	response, err := client.Simulate(t.Context(), model(), &solverv1.SimulateCascadeRequest{})
	if err != nil {
		t.Fatalf("Simulate should have recovered from a restarted server: %v", err)
	}

	if response.GetSolverVersion() != "fake" {
		t.Errorf("got %q back", response.GetSolverVersion())
	}

	if server.simulates != 2 {
		t.Errorf("simulated %d times, want one failure and one retry", server.simulates)
	}

	if server.uploads != 2 {
		t.Errorf("uploaded %d times, want the original plus the recovery", server.uploads)
	}
}

// A caller must not be able to supply a hash naming something other than the
// model it passed, same as Solve.
func TestSimulateNamesTheModelItWasGiven(t *testing.T) {
	server := newFake()
	client := dial(t, server)

	hash, err := client.Upload(t.Context(), model())
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	response, err := client.Simulate(t.Context(), model(), &solverv1.SimulateCascadeRequest{
		// Deliberately wrong, and deliberately ignored.
		ContentHash: "not-the-model-you-passed",
	})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	if response.GetModelHash() != hash {
		t.Errorf("simulated %q, want the hash of the model passed in, %q",
			response.GetModelHash(), hash)
	}
}

func TestScreenVisitsEveryResult(t *testing.T) {
	server := newFake()
	client := dial(t, server)

	if _, err := client.Upload(t.Context(), model()); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	var seen []uint32

	err := client.Screen(t.Context(), model(), &solverv1.ScreenContingenciesRequest{},
		func(result *solverv1.ScreenContingenciesResponse) error {
			seen = append(seen, result.GetContingency().GetBranch())

			return nil
		})
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}

	if len(seen) != 3 {
		t.Errorf("visited %v, want three results", seen)
	}
}

func TestScreenReUploadsAfterTheServerForgets(t *testing.T) {
	server := newFake()
	client := dial(t, server)

	if _, err := client.Upload(t.Context(), model()); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	server.forgetOnce = true

	count := 0
	err := client.Screen(t.Context(), model(), &solverv1.ScreenContingenciesRequest{},
		func(*solverv1.ScreenContingenciesResponse) error {
			count++

			return nil
		})
	if err != nil {
		t.Fatalf("Screen should have recovered from a restarted server: %v", err)
	}

	if count != 3 {
		t.Errorf("visited %d results after recovery, want three", count)
	}
}

// Stopping early is the normal case on a large sweep, so it has its own
// sentinel rather than sharing a shape with failure.
func TestScreenStopsWhenAskedTo(t *testing.T) {
	server := newFake()
	client := dial(t, server)

	if _, err := client.Upload(t.Context(), model()); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	count := 0
	err := client.Screen(t.Context(), model(), &solverv1.ScreenContingenciesRequest{},
		func(*solverv1.ScreenContingenciesResponse) error {
			count++

			return solver.ErrStopScreening
		})
	if err != nil {
		t.Fatalf("stopping early should not be an error: %v", err)
	}

	if count != 1 {
		t.Errorf("visited %d results after asking to stop at the first", count)
	}
}

// An error from the callback is the caller's, and must reach the caller
// unchanged rather than being folded into a transport failure.
func TestScreenReturnsTheCallbacksOwnError(t *testing.T) {
	server := newFake()
	client := dial(t, server)

	if _, err := client.Upload(t.Context(), model()); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	sentinel := errors.New("the caller's problem")

	err := client.Screen(t.Context(), model(), &solverv1.ScreenContingenciesRequest{},
		func(*solverv1.ScreenContingenciesResponse) error {
			return sentinel
		})

	if !errors.Is(err, sentinel) {
		t.Errorf("got %v, want the callback's own error", err)
	}
}
