package canonical

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// ErrTruncated is what a short or corrupt encoding produces.
var ErrTruncated = errors.New("canonical: the encoding ends mid-field")

// Decode reads a model back from its canonical bytes.
//
// The inverse of Encode, and the reason it exists is not symmetry: the stored
// blob is the only copy of a network once its source file has moved on, and a
// viewer, an export or a re-solve all need the model rather than its hash. The
// Rust solver decodes the same format, so the round-trip here is also the Go
// side of a cross-language agreement.
//
// Bus references come back as identifiers rather than indices, so a decoded
// model re-encodes to the same bytes. Encode sorts by identifier, and the
// indices it wrote are positions in that sorted order — which decoding
// preserves by reading the buses in the order they were written.
//
// # Errors
//
// Returns ErrTruncated when the encoding ends mid-field, and a named error for
// an unknown version or an out-of-range bus index. A decoder that guessed at
// either would produce a plausible network from corrupt bytes.
func Decode(encoded []byte) (Model, error) {
	r := &reader{buf: encoded}

	version, err := r.byteValue()
	if err != nil {
		return Model{}, err
	}

	if version != Version {
		return Model{}, fmt.Errorf(
			"canonical: encoding is version %d and this build reads version %d; "+
				"the physics fields changed between them, so reading it anyway "+
				"would produce a different network from the one that was stored",
			version, Version)
	}

	out := Model{}

	if out.SBaseVA, err = r.float(); err != nil {
		return Model{}, err
	}

	if out.VBaseV, err = r.float(); err != nil {
		return Model{}, err
	}

	busCount, err := r.uint32()
	if err != nil {
		return Model{}, err
	}

	out.Buses = make([]Bus, 0, min(busCount, uint32(len(encoded))))

	for range busCount {
		bus, err := r.bus()
		if err != nil {
			return Model{}, err
		}

		out.Buses = append(out.Buses, bus)
	}

	branchCount, err := r.uint32()
	if err != nil {
		return Model{}, err
	}

	out.Branches = make([]Branch, 0, min(branchCount, uint32(len(encoded))))

	for range branchCount {
		branch, err := r.branch(out.Buses)
		if err != nil {
			return Model{}, err
		}

		out.Branches = append(out.Branches, branch)
	}

	if r.at != len(r.buf) {
		return Model{}, fmt.Errorf(
			"canonical: %d bytes remain after the last branch; the encoding "+
				"describes fewer entities than it contains",
			len(r.buf)-r.at)
	}

	return out, nil
}

// reader walks the encoding, refusing to read past its end.
//
// A cursor rather than repeated slicing, because every field would otherwise
// need its own bounds check and the one that was forgotten would panic on
// corrupt input rather than reporting it.
type reader struct {
	buf []byte
	at  int
}

func (r *reader) byteValue() (byte, error) {
	if r.at+1 > len(r.buf) {
		return 0, ErrTruncated
	}

	out := r.buf[r.at]
	r.at++

	return out, nil
}

func (r *reader) uint32() (uint32, error) {
	if r.at+4 > len(r.buf) {
		return 0, ErrTruncated
	}

	out := binary.BigEndian.Uint32(r.buf[r.at:])
	r.at += 4

	return out, nil
}

func (r *reader) float() (float64, error) {
	if r.at+8 > len(r.buf) {
		return 0, ErrTruncated
	}

	out := math.Float64frombits(binary.BigEndian.Uint64(r.buf[r.at:]))
	r.at += 8

	return out, nil
}

func (r *reader) text() (string, error) {
	length, err := r.uint32()
	if err != nil {
		return "", err
	}

	if r.at+int(length) > len(r.buf) {
		return "", ErrTruncated
	}

	out := string(r.buf[r.at : r.at+int(length)])
	r.at += int(length)

	return out, nil
}

func (r *reader) bus() (Bus, error) {
	var (
		out Bus
		err error
	)

	if out.ID, err = r.text(); err != nil {
		return Bus{}, err
	}

	if out.Kind, err = r.byteValue(); err != nil {
		return Bus{}, err
	}

	// In encodeBus's order, which is what makes the round trip a round trip.
	// VNominalV is last because it was added last; it is VOLTS where the rest
	// are per unit, and reading it into the wrong slot would put a five-figure
	// number where a per-unit one belongs.
	for _, field := range []*float64{
		&out.PInjection, &out.QInjection, &out.VMagnitude, &out.VAngle,
		&out.QMin, &out.QMax, &out.GShunt, &out.BShunt, &out.VNominalV,
	} {
		if *field, err = r.float(); err != nil {
			return Bus{}, err
		}
	}

	return out, nil
}

func (r *reader) branch(buses []Bus) (Branch, error) {
	var (
		out Branch
		err error
	)

	if out.ID, err = r.text(); err != nil {
		return Branch{}, err
	}

	if out.FromID, err = r.busRef(buses, out.ID, "from"); err != nil {
		return Branch{}, err
	}

	if out.ToID, err = r.busRef(buses, out.ID, "to"); err != nil {
		return Branch{}, err
	}

	for _, field := range []*float64{&out.R, &out.X, &out.B, &out.Rating} {
		if *field, err = r.float(); err != nil {
			return Branch{}, err
		}
	}

	inService, err := r.byteValue()
	if err != nil {
		return Branch{}, err
	}

	out.InService = inService != 0

	for _, field := range []*float64{&out.Tap, &out.Shift} {
		if *field, err = r.float(); err != nil {
			return Branch{}, err
		}
	}

	return out, nil
}

// busRef resolves an encoded index back to a bus identifier.
func (r *reader) busRef(buses []Bus, branchID, end string) (string, error) {
	index, err := r.uint32()
	if err != nil {
		return "", err
	}

	if int(index) >= len(buses) {
		return "", fmt.Errorf(
			"canonical: branch %q names bus %d at its %s end, and the encoding "+
				"holds %d buses", branchID, index, end, len(buses))
	}

	return buses[index].ID, nil
}
