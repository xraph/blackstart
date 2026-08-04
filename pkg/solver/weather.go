package solver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc"

	weatherv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/weather/v1"
)

// GridClient talks to the weather grid service.
//
// A separate type in the same package, because it is a separate contract
// reached over the same connection. One Rust binary serves both, so a caller
// holding a connection can build either client from it without a second dial —
// but the two contracts share nothing else, and a single client type would
// imply they do.
type GridClient struct {
	rpc weatherv1.WeatherGridServiceClient
}

// NewGrid wraps a gRPC connection.
func NewGrid(conn grpc.ClientConnInterface) *GridClient {
	return &GridClient{rpc: weatherv1.NewWeatherGridServiceClient(conn)}
}

// GridHash returns the content hash of a GRIB2 message.
//
// The same hash the retrieval's DatasetVersion records, computed the same way:
// the sha256 of the bytes as they arrived, hex-encoded lowercase. That
// identity is the point — a grid sampled here and a dataset version recorded
// in provenance name the same bytes, so a sample traces to a retrieval without
// a lookup table in between.
//
// Note the difference from Hash, which serializes a protobuf message first. A
// GRIB2 message is already bytes; there is nothing to canonicalize and nothing
// that could make the same file hash two ways.
func GridHash(message []byte) string {
	sum := sha256.Sum256(message)

	return hex.EncodeToString(sum[:])
}

// GridDescription is what a decoded message turned out to be.
//
// Returned so a caller can confirm the bytes it fetched are the field it asked
// for. A fetcher locates a message by the abbreviation and level text in an
// index sidecar, which is a different vocabulary from these coordinates —
// matching only the index would fetch the right bytes and decode them as the
// wrong field.
type GridDescription struct {
	// Category and Number are the WMO code table 4.2 coordinates within
	// discipline 0.
	Category uint32
	Number   uint32

	// LevelType is WMO code table 4.5, and LevelValue its value in the unit
	// that type implies — already descaled.
	LevelType  uint32
	LevelValue float64

	// NX and NY are the grid's dimensions, and Projection is "lambert" or
	// "latlon".
	NX         uint32
	NY         uint32
	Projection string
}

// UploadGrid sends a GRIB2 message and returns its content hash and what it
// decoded to.
//
// Uploading a grid the server already has is not an error — it reports
// already-present after one round trip, which is what HasGrid is for.
func (c *GridClient) UploadGrid(
	ctx context.Context, message []byte,
) (string, GridDescription, error) {
	hash := GridHash(message)

	stream, err := c.rpc.PutGrid(ctx)
	if err != nil {
		return "", GridDescription{}, fmt.Errorf(
			"solver: opening the grid upload stream: %w", err)
	}

	header := &weatherv1.PutGridRequest{
		Payload: &weatherv1.PutGridRequest_Header{
			Header: &weatherv1.PutGridHeader{
				ContentHash: hash,
				SizeBytes:   uint64(len(message)),
			},
		},
	}

	if err := stream.Send(header); err != nil {
		return "", GridDescription{}, fmt.Errorf(
			"solver: sending the grid upload header: %w", err)
	}

	for start := 0; start < len(message); start += chunkSize {
		end := min(start+chunkSize, len(message))

		chunk := &weatherv1.PutGridRequest{
			Payload: &weatherv1.PutGridRequest_Chunk{Chunk: message[start:end]},
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
			return "", GridDescription{}, fmt.Errorf("solver: sending grid bytes: %w", err)
		}
	}

	response, err := stream.CloseAndRecv()
	if err != nil {
		return "", GridDescription{}, fmt.Errorf(
			"solver: completing the grid upload: %w", err)
	}

	// The server computes the hash independently. If it disagrees, something
	// between here and there changed the bytes, and using either hash would
	// mean naming a grid by something that is not its content — and the
	// provenance record would point at a retrieval this is not.
	if response.GetContentHash() != hash {
		return "", GridDescription{}, fmt.Errorf(
			"solver: uploaded grid %s but the server stored %s; the bytes changed "+
				"in transit", hash, response.GetContentHash())
	}

	return hash, describeFrom(response.GetDescription()), nil
}

// HasGrid reports whether the server already holds a grid, and what it is.
func (c *GridClient) HasGrid(
	ctx context.Context, hash string,
) (bool, GridDescription, error) {
	response, err := c.rpc.HasGrid(ctx, &weatherv1.HasGridRequest{ContentHash: hash})
	if err != nil {
		return false, GridDescription{}, fmt.Errorf(
			"solver: checking for grid %s: %w", hash, err)
	}

	return response.GetPresent(), describeFrom(response.GetDescription()), nil
}

// GridPoint is somewhere to sample.
type GridPoint struct {
	// Lon is degrees east in ±180, Lat degrees north — the same convention
	// geo's shape.Point uses, so a span's midpoint converts without thought.
	Lon float64
	Lat float64
}

// GridSample is one point's result.
type GridSample struct {
	// Present says whether the point fell inside the grid's domain.
	//
	// A separate field rather than a sentinel, because no number could mean
	// absent here: zero is a real gust and a real precipitation rate. HRRR
	// covers CONUS only, so absence is the ordinary case for a span outside
	// it — not an error, and never a zero.
	Present bool

	// Value is the sampled value in the unit the source reported. Meaningless
	// unless Present.
	Value float64
}

// SampleGrid reads a cached grid at a set of points.
//
// Results come back in the order the points were given, which is what lets a
// caller pair them with its own spans without echoing an identifier through
// the wire.
//
// # Errors
//
// Returns an error when the grid is not cached. That is a real condition
// rather than a transient one: the server holds grids in memory, so a restart
// loses them and the caller has the bytes to re-upload. Unlike Solve, this
// does not retry automatically — the caller owns the blob and re-reading it
// from the byte plane is its decision, not this client's.
func (c *GridClient) SampleGrid(
	ctx context.Context, hash string, points []GridPoint,
) ([]GridSample, error) {
	if len(points) == 0 {
		return nil, nil
	}

	wire := make([]*weatherv1.Point, 0, len(points))

	for _, point := range points {
		wire = append(wire, &weatherv1.Point{Lon: point.Lon, Lat: point.Lat})
	}

	response, err := c.rpc.SampleGrid(ctx, &weatherv1.SampleGridRequest{
		ContentHash: hash,
		Points:      wire,
	})
	if err != nil {
		return nil, fmt.Errorf("solver: sampling grid %s: %w", hash, err)
	}

	got := response.GetSamples()

	// The contract pairs results with requests by position, so a length
	// mismatch means the pairing is already wrong and every value would be
	// attributed to the wrong span.
	if len(got) != len(points) {
		return nil, fmt.Errorf(
			"solver: asked grid %s for %d points and got %d samples; results are "+
				"paired by position, so this would attribute values to the wrong spans",
			hash, len(points), len(got))
	}

	out := make([]GridSample, 0, len(got))

	for _, sample := range got {
		out = append(out, GridSample{
			Present: sample.GetPresent(),
			Value:   sample.GetValue(),
		})
	}

	return out, nil
}

// describeFrom maps the wire description into the package's own type.
func describeFrom(from *weatherv1.GridDescription) GridDescription {
	if from == nil {
		return GridDescription{}
	}

	return GridDescription{
		Category:   from.GetCategory(),
		Number:     from.GetNumber(),
		LevelType:  from.GetLevelType(),
		LevelValue: from.GetLevelValue(),
		NX:         from.GetNx(),
		NY:         from.GetNy(),
		Projection: from.GetProjection(),
	}
}
