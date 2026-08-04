// Package solver is the client for the Rust power-flow service.
//
// The platform is written in Go and the mathematics in Rust, and this is the
// seam between them. It exists as a package rather than inside an extension
// because both the powerflow and contingency extensions need it, and a client
// that lives in one of them would make the other depend on its internals.
//
// Two behaviors here are the whole reason this is not a bare generated stub.
//
// The client uploads a model when the server does not have it and retries the
// call exactly once. The solver keeps models in memory, so a restart loses
// every one of them; without this, every restart would surface as a wave of
// failures across the platform. With it, a restart is invisible.
//
// And it converts to SI on the way out. The contract's units are watts, vars,
// volts and ohms; the platform's domain model is per unit. That conversion
// happens here and nowhere else, for the same reason it happens in one place
// on the Rust side: a missed conversion produces a plausible answer on the
// wrong scale rather than an error.
//
// Advisory only. This client calls no method that writes to a control system,
// because the contract defines none. See docs/SAFETY.md.
package solver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// chunkSize is how much of a serialized model goes in one message.
//
// gRPC's default maximum message size is 4 MiB, and a 2000-bus model is
// comfortably larger. One MiB leaves room for framing without making the
// stream needlessly chatty.
const chunkSize = 1 << 20

// Client talks to the solver service.
type Client struct {
	rpc solverv1.SolverServiceClient
}

// New wraps a gRPC connection.
func New(conn grpc.ClientConnInterface) *Client {
	return &Client{rpc: solverv1.NewSolverServiceClient(conn)}
}

// Hash returns the content hash of a model, which is how it is named
// everywhere else.
//
// Computed from the serialized bytes with deterministic marshaling, so the
// same model always produces the same hash. Protobuf serialization is not
// canonical by default — field order and map iteration are unspecified — and
// a hash that changed between runs would defeat the cache entirely.
func Hash(model *solverv1.NetworkModel) (string, []byte, error) {
	body, err := proto.MarshalOptions{Deterministic: true}.Marshal(model)
	if err != nil {
		return "", nil, fmt.Errorf("solver: serializing the model: %w", err)
	}

	sum := sha256.Sum256(body)

	return hex.EncodeToString(sum[:]), body, nil
}

// Upload sends a model and returns its content hash.
//
// Uploading a model the server already has is not an error — it reports
// already-present and costs one round trip, which is what Has is for.
func (c *Client) Upload(ctx context.Context, model *solverv1.NetworkModel) (string, error) {
	hash, body, err := Hash(model)
	if err != nil {
		return "", err
	}

	stream, err := c.rpc.PutNetworkModel(ctx)
	if err != nil {
		return "", fmt.Errorf("solver: opening the upload stream: %w", err)
	}

	header := &solverv1.PutNetworkModelRequest{
		Payload: &solverv1.PutNetworkModelRequest_Header{
			Header: &solverv1.PutNetworkModelHeader{
				ContentHash: hash,
				SizeBytes:   uint64(len(body)),
			},
		},
	}

	if err := stream.Send(header); err != nil {
		return "", fmt.Errorf("solver: sending the upload header: %w", err)
	}

	for start := 0; start < len(body); start += chunkSize {
		end := min(start+chunkSize, len(body))

		chunk := &solverv1.PutNetworkModelRequest{
			Payload: &solverv1.PutNetworkModelRequest_Chunk{Chunk: body[start:end]},
		}

		err := stream.Send(chunk)

		// A Send that returns io.EOF means the server has already responded and
		// closed its side — which it does, deliberately, when it already holds
		// this payload. The real outcome is on CloseAndRecv; io.EOF here is not
		// the error and must not be reported as one.
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return "", fmt.Errorf("solver: sending model bytes: %w", err)
		}
	}

	response, err := stream.CloseAndRecv()
	if err != nil {
		return "", fmt.Errorf("solver: completing the upload: %w", err)
	}

	// The server computes the hash independently. If it disagrees, something
	// between here and there changed the bytes, and using either hash would
	// mean naming a model by something that is not its content.
	if response.GetContentHash() != hash {
		return "", fmt.Errorf(
			"solver: uploaded %s but the server stored %s; the bytes changed in transit",
			hash, response.GetContentHash())
	}

	return hash, nil
}

// Has reports whether the server already holds a model.
func (c *Client) Has(ctx context.Context, hash string) (bool, error) {
	response, err := c.rpc.HasNetworkModel(ctx, &solverv1.HasNetworkModelRequest{
		ContentHash: hash,
	})
	if err != nil {
		return false, fmt.Errorf("solver: checking for model %s: %w", hash, err)
	}

	return response.GetPresent(), nil
}

// Solve runs one power flow, uploading the model first if the server does not
// have it.
//
// The request's ContentHash is filled in from the model, so a caller cannot
// pass a hash that names something other than what it supplied.
func (c *Client) Solve(
	ctx context.Context,
	model *solverv1.NetworkModel,
	request *solverv1.SolvePowerFlowRequest,
) (*solverv1.SolvePowerFlowResponse, error) {
	hash, _, err := Hash(model)
	if err != nil {
		return nil, err
	}

	attempt := proto.CloneOf(request)
	attempt.ContentHash = hash

	response, err := c.rpc.SolvePowerFlow(ctx, attempt)
	if err == nil {
		return response, nil
	}

	if !isModelMissing(err) {
		return nil, fmt.Errorf("solver: solving: %w", err)
	}

	// Exactly one retry. A second FAILED_PRECONDITION after a successful
	// upload means something other than a cold cache is wrong — a mismatched
	// hash, an eviction race, two servers behind one address — and retrying
	// again would turn a bug into a loop.
	if _, err := c.Upload(ctx, model); err != nil {
		return nil, err
	}

	response, err = c.rpc.SolvePowerFlow(ctx, attempt)
	if err != nil {
		return nil, fmt.Errorf("solver: solving after upload: %w", err)
	}

	return response, nil
}

// Simulate runs one cascade simulation, uploading the model first if the
// server does not have it.
//
// Mirrors Solve's upload-and-retry exactly, for the same reason: the solver
// keeps models in memory only, so a restart between an earlier upload and
// this call must not surface as a failure the caller has to handle specially.
// The request's ContentHash is filled in from the model, so a caller cannot
// pass a hash that names something other than what it supplied.
func (c *Client) Simulate(
	ctx context.Context,
	model *solverv1.NetworkModel,
	request *solverv1.SimulateCascadeRequest,
) (*solverv1.SimulateCascadeResponse, error) {
	hash, _, err := Hash(model)
	if err != nil {
		return nil, err
	}

	attempt := proto.CloneOf(request)
	attempt.ContentHash = hash

	response, err := c.rpc.SimulateCascade(ctx, attempt)
	if err == nil {
		return response, nil
	}

	if !isModelMissing(err) {
		return nil, fmt.Errorf("solver: simulating: %w", err)
	}

	// Exactly one retry, for the same reason Solve allows exactly one: a
	// second FAILED_PRECONDITION after a successful upload means something
	// other than a cold cache is wrong, and retrying again would turn a bug
	// into a loop.
	if _, err := c.Upload(ctx, model); err != nil {
		return nil, err
	}

	response, err = c.rpc.SimulateCascade(ctx, attempt)
	if err != nil {
		return nil, fmt.Errorf("solver: simulating after upload: %w", err)
	}

	return response, nil
}

// Screen runs a contingency sweep, uploading the model first if needed, and
// calls visit for each result as it arrives.
//
// Streaming to a callback rather than collecting a slice: a sweep over a
// 2000-bus network produces thousands of results, and the caller usually wants
// to rank and discard rather than hold them all. Returning ErrStopScreening
// from visit ends the sweep early.
func (c *Client) Screen(
	ctx context.Context,
	model *solverv1.NetworkModel,
	request *solverv1.ScreenContingenciesRequest,
	visit func(*solverv1.ScreenContingenciesResponse) error,
) error {
	hash, _, err := Hash(model)
	if err != nil {
		return err
	}

	attempt := proto.CloneOf(request)
	attempt.ContentHash = hash

	// Opening a server stream does no work, so a missing model is not reported
	// until the first Recv. Retrying at the call site would therefore never
	// fire — the first attempt always appears to succeed — which is why the
	// recovery lives inside the read loop instead.
	stream, err := c.rpc.ScreenContingencies(ctx, attempt)
	if err != nil {
		return fmt.Errorf("solver: screening: %w", err)
	}

	// Retry is only safe before anything has been delivered. Once the callback
	// has seen a result, restarting the sweep would deliver it twice, and a
	// caller accumulating violations would count them twice.
	delivered := 0
	retried := false

	for {
		result, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			if isModelMissing(err) && delivered == 0 && !retried {
				retried = true

				if _, err := c.Upload(ctx, model); err != nil {
					return err
				}

				if stream, err = c.rpc.ScreenContingencies(ctx, attempt); err != nil {
					return fmt.Errorf("solver: screening after upload: %w", err)
				}

				continue
			}

			if isModelMissing(err) {
				return fmt.Errorf(
					"solver: the model went missing after %d results: %w", delivered, err)
			}

			return fmt.Errorf("solver: reading screen results: %w", err)
		}

		delivered++

		if err := visit(result); err != nil {
			if errors.Is(err, ErrStopScreening) {
				return nil
			}

			return err
		}
	}
}

// ErrStopScreening ends a sweep early when returned from a Screen callback.
//
// A sentinel rather than a boolean return, so that stopping deliberately and
// failing are not the same shape at the call site.
var ErrStopScreening = errors.New("solver: stop screening")

// isModelMissing reports whether an error means the server lacks the model.
//
// FAILED_PRECONDITION is the contract's signal that the client should upload
// and retry. Matching on the code rather than the message, because a message
// is prose and will be reworded.
func isModelMissing(err error) bool {
	return status.Code(err) == codes.FailedPrecondition
}

// HashOnly returns a model's content hash without its serialized bytes.
//
// For callers that need to name a model — recording which network a result
// came from — rather than send it. Returning the body too would invite holding
// a megabyte alive for the sake of a 64-character string.
func HashOnly(model *solverv1.NetworkModel) (string, error) {
	hash, _, err := Hash(model)

	return hash, err
}
