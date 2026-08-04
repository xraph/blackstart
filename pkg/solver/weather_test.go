package solver_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	weatherv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/weather/v1"
)

// fakeGrid records what it was asked and answers from a map.
//
// A fake rather than the real Rust service, matching the solver client's
// tests: what is under test here is the client's chunking, hashing and
// position pairing. Whether GRIB2 decodes correctly is settled in bs-grib,
// against ecCodes, and running a cargo build from a Go test would couple two
// things that are better kept apart.
type fakeGrid struct {
	weatherv1.UnimplementedWeatherGridServiceServer

	grids map[string][]byte

	// samples is what SampleGrid returns, and shortSamples makes it return one
	// fewer than asked — the pairing failure the client has to catch.
	samples      []*weatherv1.Sample
	shortSamples bool
}

func (f *fakeGrid) PutGrid(stream weatherv1.WeatherGridService_PutGridServer) error {
	var (
		header *weatherv1.PutGridHeader
		body   []byte
	)

	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return err
		}

		if sent := message.GetHeader(); sent != nil {
			header = sent

			if _, ok := f.grids[sent.GetContentHash()]; ok {
				return stream.SendAndClose(&weatherv1.PutGridResponse{
					ContentHash:    sent.GetContentHash(),
					AlreadyPresent: true,
				})
			}

			continue
		}

		body = append(body, message.GetChunk()...)
	}

	if header == nil {
		return status.Error(codes.InvalidArgument, "no header")
	}

	sum := sha256.Sum256(body)
	computed := hex.EncodeToString(sum[:])

	f.grids[computed] = body

	return stream.SendAndClose(&weatherv1.PutGridResponse{
		ContentHash: computed,
		Description: &weatherv1.GridDescription{
			Category: 2, Number: 22, LevelType: 1,
			Nx: 1799, Ny: 1059, Projection: "lambert",
		},
	})
}

func (f *fakeGrid) HasGrid(
	_ context.Context, request *weatherv1.HasGridRequest,
) (*weatherv1.HasGridResponse, error) {
	_, ok := f.grids[request.GetContentHash()]

	return &weatherv1.HasGridResponse{Present: ok}, nil
}

func (f *fakeGrid) SampleGrid(
	_ context.Context, request *weatherv1.SampleGridRequest,
) (*weatherv1.SampleGridResponse, error) {
	if _, ok := f.grids[request.GetContentHash()]; !ok {
		return nil, status.Error(codes.FailedPrecondition, "not cached")
	}

	if f.shortSamples {
		return &weatherv1.SampleGridResponse{
			Samples: make([]*weatherv1.Sample, len(request.GetPoints())-1),
		}, nil
	}

	if f.samples != nil {
		return &weatherv1.SampleGridResponse{Samples: f.samples}, nil
	}

	out := make([]*weatherv1.Sample, 0, len(request.GetPoints()))

	for range request.GetPoints() {
		out = append(out, &weatherv1.Sample{Present: true, Value: 1.5})
	}

	return &weatherv1.SampleGridResponse{Samples: out}, nil
}

func gridHarness(t *testing.T, server *fakeGrid) *solver.GridClient {
	t.Helper()

	if server.grids == nil {
		server.grids = map[string][]byte{}
	}

	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()

	weatherv1.RegisterWeatherGridServiceServer(grpcServer, server)

	go func() { _ = grpcServer.Serve(listener) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dialing: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
		grpcServer.Stop()
	})

	return solver.NewGrid(conn)
}

// The hash is the one a DatasetVersion records: the sha256 of the bytes as
// they arrived, with nothing serialized or canonicalized first.
func TestGridHashIsTheHashOfTheBytes(t *testing.T) {
	t.Parallel()

	message := []byte("GRIB not really but bytes are bytes")

	sum := sha256.Sum256(message)

	if got := solver.GridHash(message); got != hex.EncodeToString(sum[:]) {
		t.Errorf("GridHash = %q, want the plain sha256 of the bytes", got)
	}
}

func TestUploadGridRoundTrips(t *testing.T) {
	t.Parallel()

	client := gridHarness(t, &fakeGrid{})

	// Larger than one chunk, so the chunking path is the one under test.
	message := make([]byte, (1<<20)+1234)
	for i := range message {
		message[i] = byte(i % 251)
	}

	hash, described, err := client.UploadGrid(t.Context(), message)
	if err != nil {
		t.Fatalf("UploadGrid: %v", err)
	}

	if hash != solver.GridHash(message) {
		t.Errorf("upload returned %q, want the message's own hash", hash)
	}

	if described.Projection != "lambert" || described.NX != 1799 {
		t.Errorf("description = %+v, want the fake's lambert grid", described)
	}
}

// A grid the server already holds costs one round trip and is not an error.
func TestUploadGridIsIdempotent(t *testing.T) {
	t.Parallel()

	message := []byte("some grib bytes")
	server := &fakeGrid{grids: map[string][]byte{solver.GridHash(message): message}}
	client := gridHarness(t, server)

	hash, _, err := client.UploadGrid(t.Context(), message)
	if err != nil {
		t.Fatalf("UploadGrid: %v", err)
	}

	if hash != solver.GridHash(message) {
		t.Errorf("hash = %q", hash)
	}
}

func TestSampleGridPreservesOrder(t *testing.T) {
	t.Parallel()

	message := []byte("some grib bytes")

	server := &fakeGrid{
		grids: map[string][]byte{solver.GridHash(message): message},
		samples: []*weatherv1.Sample{
			{Present: true, Value: 14.2},
			{Present: false},
			{Present: true, Value: 18.9},
		},
	}

	client := gridHarness(t, server)

	got, err := client.SampleGrid(t.Context(), solver.GridHash(message), []solver.GridPoint{
		{Lon: -97.67, Lat: 30.19},
		{Lon: 0, Lat: 0},
		{Lon: -95.37, Lat: 29.76},
	})
	if err != nil {
		t.Fatalf("SampleGrid: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d samples, want 3", len(got))
	}

	if !got[0].Present || got[0].Value != 14.2 {
		t.Errorf("first sample = %+v", got[0])
	}

	// The middle point is outside the domain. Absence must survive the wire as
	// absence rather than arriving as a zero that reads like a calm.
	if got[1].Present {
		t.Errorf("second sample reported present: %+v", got[1])
	}

	if got[1].Value != 0 {
		t.Errorf("an absent sample carried the value %v", got[1].Value)
	}

	if !got[2].Present || got[2].Value != 18.9 {
		t.Errorf("third sample = %+v", got[2])
	}
}

// Results are paired with requests by position, so a length mismatch would
// attribute every value to the wrong span. It has to be caught here rather
// than surfacing as weather at the wrong place.
func TestSampleGridRefusesAShortResponse(t *testing.T) {
	t.Parallel()

	message := []byte("some grib bytes")

	server := &fakeGrid{
		grids:        map[string][]byte{solver.GridHash(message): message},
		shortSamples: true,
	}

	client := gridHarness(t, server)

	_, err := client.SampleGrid(t.Context(), solver.GridHash(message), []solver.GridPoint{
		{Lon: -97.67, Lat: 30.19},
		{Lon: -95.37, Lat: 29.76},
	})
	if err == nil {
		t.Fatal("a response with fewer samples than points was accepted")
	}
}

func TestSampleGridOnAnUncachedGridFails(t *testing.T) {
	t.Parallel()

	client := gridHarness(t, &fakeGrid{})

	_, err := client.SampleGrid(t.Context(), "nosuchhash", []solver.GridPoint{
		{Lon: -97.67, Lat: 30.19},
	})
	if err == nil {
		t.Fatal("sampling an uncached grid succeeded")
	}
}

func TestSampleGridWithNoPointsAsksNothing(t *testing.T) {
	t.Parallel()

	client := gridHarness(t, &fakeGrid{})

	got, err := client.SampleGrid(t.Context(), "nosuchhash", nil)
	if err != nil {
		t.Fatalf("SampleGrid with no points: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("got %d samples for no points", len(got))
	}
}

func TestHasGrid(t *testing.T) {
	t.Parallel()

	message := []byte("some grib bytes")
	server := &fakeGrid{grids: map[string][]byte{solver.GridHash(message): message}}
	client := gridHarness(t, server)

	present, _, err := client.HasGrid(t.Context(), solver.GridHash(message))
	if err != nil {
		t.Fatalf("HasGrid: %v", err)
	}

	if !present {
		t.Error("a cached grid reported absent")
	}

	present, _, err = client.HasGrid(t.Context(), "nosuchhash")
	if err != nil {
		t.Fatalf("HasGrid: %v", err)
	}

	if present {
		t.Error("an uncached grid reported present")
	}
}

// The server closes its side as soon as the header names a grid it already
// holds — which is the whole point of sending the hash first. A client still
// pushing chunks then sees io.EOF from Send, and that is not an error: the
// real outcome is on CloseAndRecv.
//
// This needs a payload larger than one chunk to reproduce. A single-chunk
// upload races the short-circuit and passes either way, which is why the
// earlier idempotency test did not catch it and a live service did.
func TestUploadGridToleratesAnEarlyServerResponse(t *testing.T) {
	t.Parallel()

	message := make([]byte, (3<<20)+7)
	for i := range message {
		message[i] = byte(i % 251)
	}

	server := &fakeGrid{grids: map[string][]byte{solver.GridHash(message): message}}
	client := gridHarness(t, server)

	hash, _, err := client.UploadGrid(t.Context(), message)
	if err != nil {
		t.Fatalf("UploadGrid against a server that already had it: %v", err)
	}

	if hash != solver.GridHash(message) {
		t.Errorf("hash = %q", hash)
	}
}
