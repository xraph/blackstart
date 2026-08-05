// Package canonical is the one encoder that decides what a network *is*.
//
// It serves two jobs from one implementation, and that is the point: the
// content hash that names a model and the wire format the solver reads are the
// same bytes. Two encoders would eventually disagree, and the disagreement
// would look like a solver bug — the cache would hand back a result computed
// for a network that differs from the one just submitted.
//
// # What is hashed
//
// Only physics. Names, descriptions and geometry are excluded, so renaming a
// substation does not invalidate every cached solve of the network it sits in.
// The included fields are listed explicitly in encodeBus and encodeBranch
// rather than derived by reflection, because reflection over a struct means
// adding a display field silently changes every hash in the system.
//
// # Why not protobuf, or JSON
//
// Neither is canonical. Protobuf does not specify field order on the wire and
// its encoders are free to differ; JSON has no defined ordering for object
// keys and renders floats through decimal text that loses the last bits. This
// is a length-prefixed binary form with a fixed field order and IEEE-754
// big-endian floats, which is reproducible by construction rather than by
// convention.
package canonical

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strings"
)

// Version is the first byte of every encoding.
//
// A change to what is included or how it is laid out increments this, so an
// old hash and a new one can never collide by accident — and so a stored hash
// says which encoder produced it.
//
// 2 added Bus.VNominalV. A version-1 blob is unreadable by this build, and
// Decode says so rather than guessing: the bus record grew a field, so every
// byte after the first bus is at a different offset and reading one anyway
// would take a nominal voltage out of the next bus's identifier. The remedy
// is to re-import the source, which produces a new hash — deliberately, since
// a model that now states its buses' voltages is not the model that did not.
const Version byte = 2

// Bus is the physics of one bus.
//
// Deliberately not the domain entity. This carries what the equations read and
// nothing else, so that the domain model can grow a display name without
// touching a single hash.
type Bus struct {
	// ID is the stable identifier, which fixes ordering.
	ID string

	// Kind is 1 for PQ, 2 for PV, 3 for slack — the MATPOWER numbering, so an
	// imported case needs no reinterpretation.
	Kind uint8

	// Injections and voltage, per unit.
	PInjection float64
	QInjection float64
	VMagnitude float64
	VAngle     float64

	// Reactive limits, per unit. Infinite means unlimited.
	QMin float64
	QMax float64

	// Shunt admittance, per unit.
	GShunt float64
	BShunt float64

	// VNominalV is the voltage level this bus sits at, line-to-line RMS, in
	// VOLTS.
	//
	// SI, alone among the fields above. Everything else here is per unit
	// against Model.VBaseV, and the unit suffix marks the difference the way
	// Model.SBaseVA and Model.VBaseV do. Per unit is exactly what this field
	// cannot be: expressed against the model's single base it would read 1.0
	// on every bus of a mixed-voltage network and say nothing, and saying
	// something is the entire point — a network spanning 345 kV and 138 kV has
	// one per-unit base and two nominal voltages, and a consumer turning a
	// current into a power needs the bus's own.
	//
	// It does not change the per-unit system. Nothing in the solver reads it;
	// every impedance in this model is still expressed against Model.VBaseV
	// and still converted against it. A per-unit base per bus is a different
	// question — transformers put each end of a branch in its own zone — and
	// this field deliberately does not answer it.
	//
	// Zero means the bus states none. Not a bus at zero volts: there is no
	// such bus, and a consumer must read a non-positive value as unknown and
	// say so rather than convert against it. Zero rather than a NaN sentinel
	// because zero is Go's zero value — a builder with nothing to say produces
	// "unknown" without having to know the convention, which is the opposite
	// of what a sentinel asks of everyone who ever constructs a Bus.
	//
	// Hashed, like every other field here, which is why Version moved. Two
	// networks identical in per unit and different in what their buses are
	// nominally at are different networks, and Import deduplicates on this
	// hash: a collision would hand back the first network's voltages and rate
	// the second one's lines against them.
	VNominalV float64
}

// StatedNominalV is the voltage this bus states it sits at, and whether it
// states one at all.
//
// The one definition of "stated", on the type that owns the field, because
// there were three: two consumers checked `> 0` and one checked `> 0` and
// finite, so a bus holding +Inf — which a builder computing a voltage by
// division produces — read as *stated* in two of the three. It would have
// crossed the wire, reached the viewer, and failed there in encoding/json,
// which refuses to marshal an infinity: an error naming JSON rather than
// voltage, from an endpoint, about a bus nobody was looking at.
//
// Zero is the absent value and is not the only one refused. Any non-positive
// or non-finite number is a bus that states nothing: there is no bus at zero
// volts, none at a negative voltage, and none at an infinite one, so none of
// them is a measurement that could be confused with a real reading.
func (b Bus) StatedNominalV() (float64, bool) {
	return StatedNominalV(b.VNominalV)
}

// StatedNominalV is the same predicate as the method above, for a caller
// holding the volts without a canonical.Bus around them.
//
// It exists because the solver's wire type is generated and cannot carry a
// method, and extensions/scenario reads the voltage off that type — so the
// alternative to exporting this is scenario spelling the rule a fourth time,
// which is what the method exists to stop.
func StatedNominalV(volts float64) (float64, bool) {
	if volts > 0 && !math.IsInf(volts, 1) {
		return volts, true
	}

	return 0, false
}

// Branch is the physics of one branch.
type Branch struct {
	// ID is the stable identifier, which fixes ordering.
	ID string

	// FromID and ToID name the buses by identifier.
	//
	// By identifier rather than by index, because Encode sorts the bus list
	// and the caller cannot know the resulting order. A branch carrying
	// indices into its own unsorted list would silently point at the wrong
	// buses the moment anything was reordered — and the model would still
	// encode, still hash, and describe a different network.
	FromID string
	ToID   string

	// Impedance and charging, per unit.
	R float64
	X float64
	B float64

	// Rating, per unit. Zero means unrated.
	Rating float64

	// InService decides whether the branch is in the network at all, so it is
	// physics rather than presentation and is hashed.
	InService bool

	// Transformer ratio and phase shift.
	Tap   float64
	Shift float64
}

// Model is a complete network, ready to encode.
type Model struct {
	// SBaseVA and VBaseV are the per-unit base.
	//
	// Hashed, because the same numbers on a different base describe a
	// different network — and a cache keyed on a hash that ignored the base
	// would return one network's answer for another's question.
	SBaseVA float64
	VBaseV  float64

	// Buses and Branches in any order. Encode sorts them.
	Buses    []Bus
	Branches []Branch
}

// Encode produces the canonical bytes for a model.
//
// Entities are sorted by identifier first, so the caller's ordering cannot
// leak into the result. Everything is length-prefixed, so a decoder never has
// to guess where a field ends, and floats go out as IEEE-754 big-endian bits
// rather than decimal text — text loses the last bits and two encoders would
// round differently.
//
// # Errors
//
// Returns an error when a branch names a bus that is not in the model, since
// the index it would encode is meaningless.
func Encode(m Model) ([]byte, error) {
	buses := slices.Clone(m.Buses)
	slices.SortFunc(buses, func(left, right Bus) int {
		return strings.Compare(left.ID, right.ID)
	})

	index := make(map[string]uint32, len(buses))
	for position, bus := range buses {
		if _, seen := index[bus.ID]; seen {
			return nil, fmt.Errorf("canonical: bus %q appears twice", bus.ID)
		}

		if err := checkNominalV(bus); err != nil {
			return nil, err
		}

		index[bus.ID] = uint32(position)
	}

	branches := slices.Clone(m.Branches)
	slices.SortFunc(branches, func(left, right Branch) int {
		return strings.Compare(left.ID, right.ID)
	})

	var out []byte

	out = append(out, Version)
	out = appendFloat(out, m.SBaseVA)
	out = appendFloat(out, m.VBaseV)

	out = appendUint32(out, uint32(len(buses)))
	for _, bus := range buses {
		out = encodeBus(out, bus)
	}

	out = appendUint32(out, uint32(len(branches)))
	for _, branch := range branches {
		encoded, err := encodeBranch(out, branch, index)
		if err != nil {
			return nil, err
		}

		out = encoded
	}

	return out, nil
}

// checkNominalV refuses a nominal voltage that no bus could have.
//
// Zero is legal and means the bus states none. Everything else non-positive or
// non-finite is refused rather than tolerated, and the reason is the hash
// rather than the physics. Every consumer reads −138 kV, NaN and +Inf as
// "states nothing" — the same as zero — so a model carrying one behaves
// identically to the model with a zero there, and hashes differently. That is
// the deduplication collision the field was hashed to prevent, running
// backwards: two networks nobody can tell apart, stored as two, with two
// content hashes and two sets of results citing them.
//
// Refused at Encode rather than at the importer because Encode is what every
// path goes through. A model assembled in memory and hashed without ever
// touching a file would otherwise skip the check entirely.
func checkNominalV(b Bus) error {
	if b.VNominalV == 0 {
		return nil
	}

	if _, stated := b.StatedNominalV(); !stated {
		return fmt.Errorf(
			"canonical: bus %q states a nominal voltage of %v V, which no bus "+
				"can be at; zero is how a bus says it states none, and every "+
				"other unusable value reads as zero downstream while hashing "+
				"to a different model",
			b.ID, b.VNominalV)
	}

	return nil
}

// Hash is the content hash of a model, hex-encoded lowercase.
//
// What names a model everywhere else: the solver cache, a provenance record, a
// result's model_hash field.
//
// # Errors
//
// Returns an error when the model cannot be encoded.
func Hash(m Model) (string, error) {
	encoded, err := Encode(m)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(encoded)

	return hex.EncodeToString(sum[:]), nil
}

// encodeBus writes one bus's physics.
//
// The field list is explicit rather than reflected, because reflection over a
// struct means adding a display field silently changes every hash in the
// system. Adding a field here is a deliberate act that should come with a
// Version bump.
func encodeBus(out []byte, b Bus) []byte {
	out = appendString(out, b.ID)
	out = append(out, b.Kind)
	out = appendFloat(out, b.PInjection)
	out = appendFloat(out, b.QInjection)
	out = appendFloat(out, b.VMagnitude)
	out = appendFloat(out, b.VAngle)
	out = appendFloat(out, b.QMin)
	out = appendFloat(out, b.QMax)
	out = appendFloat(out, b.GShunt)
	out = appendFloat(out, b.BShunt)

	// Appended after the per-unit fields rather than placed beside VMagnitude,
	// so the layout a version-1 reader knew is a prefix of this one. That does
	// not make the two compatible — Decode refuses a version it does not know,
	// and must — but it keeps the diff to this function one line and leaves
	// nothing for a reader to misalign.
	return appendFloat(out, b.VNominalV)
}

// encodeBranch writes one branch's physics, resolving its bus references.
func encodeBranch(out []byte, b Branch, index map[string]uint32) ([]byte, error) {
	from, ok := index[b.FromID]
	if !ok {
		return nil, fmt.Errorf(
			"canonical: branch %q names bus %q, which is not in the model",
			b.ID, b.FromID)
	}

	to, ok := index[b.ToID]
	if !ok {
		return nil, fmt.Errorf(
			"canonical: branch %q names bus %q, which is not in the model",
			b.ID, b.ToID)
	}

	out = appendString(out, b.ID)
	out = appendUint32(out, from)
	out = appendUint32(out, to)
	out = appendFloat(out, b.R)
	out = appendFloat(out, b.X)
	out = appendFloat(out, b.B)
	out = appendFloat(out, b.Rating)

	if b.InService {
		out = append(out, 1)
	} else {
		out = append(out, 0)
	}

	out = appendFloat(out, b.Tap)

	return appendFloat(out, b.Shift), nil
}

// appendString writes a length-prefixed string.
func appendString(out []byte, s string) []byte {
	out = appendUint32(out, uint32(len(s)))

	return append(out, s...)
}

// appendUint32 writes a big-endian length or index.
func appendUint32(out []byte, v uint32) []byte {
	return binary.BigEndian.AppendUint32(out, v)
}

// appendFloat writes an IEEE-754 double, big-endian.
//
// The bits rather than a decimal rendering. Text loses the last bits and two
// encoders round differently; the bits are exactly what the solver will
// compute with.
//
// NaN is normalized to one bit pattern. IEEE-754 permits many, and two models
// identical in every respect except which NaN they carry would otherwise hash
// differently — a distinction with no physical meaning.
func appendFloat(out []byte, f float64) []byte {
	bits := math.Float64bits(f)

	if math.IsNaN(f) {
		bits = math.Float64bits(math.NaN())
	}

	// Negative zero is normalized too, for the same reason: -0.0 and 0.0 are
	// equal in every comparison the solver makes and describe the same network.
	if f == 0 {
		bits = 0
	}

	return binary.BigEndian.AppendUint64(out, bits)
}
