package bitemporal

import "fmt"

// Interval arithmetic for versioned facts.
//
// Every bitemporal write in the platform goes through one of the four
// functions below. They are the only place intervals are computed, because
// getting this arithmetic wrong produces data that reads correctly today and
// makes every backtest silently invalid — the worst possible failure shape.
//
// The storage model is a current table plus a history table. The current table holds each entity's
// present belief: its knowledge interval is open. When that belief is
// superseded, the prior image is archived to history with its knowledge
// interval closed, in the same transaction as the new current row.
//
// The two axes close for different reasons, and conflating them is the usual
// mistake:
//
//   - Closing a KNOWLEDGE interval means "we stopped believing this". The
//     fact may still have been true; we simply have a better record now.
//   - Closing a VALID interval means "this stopped being true of the world".
//     We may have believed it correctly the whole time.

// Record is one version's position on both axes.
type Record struct {
	// Valid is when the fact was true of the physical world.
	Valid Interval

	// Knowledge is when this system believed it.
	Knowledge Interval
}

// Validate reports whether both intervals are well-formed.
func (r Record) Validate() error {
	if err := r.Valid.Validate(); err != nil {
		return fmt.Errorf("bitemporal: valid interval: %w", err)
	}

	if err := r.Knowledge.Validate(); err != nil {
		return fmt.Errorf("bitemporal: knowledge interval: %w", err)
	}

	return nil
}

// IsCurrent reports whether this record is the present belief — that is,
// whether its knowledge interval is still open.
func (r Record) IsCurrent() bool { return r.Knowledge.IsOpen() }

// Matches reports whether this record answers the given read. Both axes must
// contain their respective instant.
//
// This mirrors the SQL predicate exactly, and a test asserts the two agree.
// Having the rule in Go as well as in SQL is what makes the arithmetic
// testable without a database.
func (r Record) Matches(a AsOf) bool {
	return r.Valid.Contains(a.ValidAt) && r.Knowledge.Contains(a.KnownAt)
}

// String renders both intervals.
func (r Record) String() string {
	return "valid" + r.Valid.String() + " known" + r.Knowledge.String()
}

// Open records a fact learned for the first time.
//
// The fact becomes true at validFrom, which may be in the past — a substation
// commissioned last year, entered today. Its validity is open-ended until
// something closes it, and so is the belief.
func Open(validFrom, learnedAt Instant) (Record, error) {
	r := Record{
		Valid:     StartingAt(validFrom),
		Knowledge: StartingAt(learnedAt),
	}

	if err := r.Validate(); err != nil {
		return Record{}, err
	}

	return r, nil
}

// Supersede replaces a record's content, belief, or validity.
//
// It returns the archive — the prior image with its knowledge interval closed
// — and the replacement, believed from learnedAt onward. Both must be written
// in one transaction; a gap between them is a window in which the entity has
// no current version, and a crash inside it loses the fact entirely.
//
// This is the general primitive. Correct and Close are the two named cases
// that cover almost every real use, and are preferred at call sites because
// their names say which axis is moving.
func Supersede(prev Record, nextValid Interval, learnedAt Instant) (archive, next Record, err error) {
	if err := prev.Validate(); err != nil {
		return Record{}, Record{}, fmt.Errorf("bitemporal: prior record: %w", err)
	}

	if !prev.IsCurrent() {
		return Record{}, Record{}, fmt.Errorf(
			"bitemporal: cannot supersede %s: its knowledge interval is already "+
				"closed, so it is an archived version rather than the current belief",
			prev)
	}

	if !learnedAt.After(prev.Knowledge.From) {
		return Record{}, Record{}, fmt.Errorf(
			"bitemporal: cannot supersede a record believed from %s at the earlier "+
				"or equal instant %s; knowledge time moves forward",
			prev.Knowledge.From, learnedAt)
	}

	closed, err := From(prev.Knowledge.From, learnedAt)
	if err != nil {
		return Record{}, Record{}, fmt.Errorf("bitemporal: closing knowledge: %w", err)
	}

	archive = Record{Valid: prev.Valid, Knowledge: closed}

	next = Record{Valid: nextValid, Knowledge: StartingAt(learnedAt)}
	if err := next.Validate(); err != nil {
		return Record{}, Record{}, fmt.Errorf("bitemporal: replacement: %w", err)
	}

	return archive, next, nil
}

// Correct records that a fact's content was wrong, without changing when it
// was true.
//
// This is the amended outage report, the revised generator rating, the
// re-surveyed structure position. The valid interval is unchanged; only the
// belief moves. It is also the operation that makes point-in-time reads
// necessary: after a correction, a read at the original knowledge time still
// returns the original content.
func Correct(prev Record, learnedAt Instant) (archive, next Record, err error) {
	return Supersede(prev, prev.Valid, learnedAt)
}

// Close records that a fact stopped being true of the world at endedAt.
//
// The knowledge interval still closes — the old open-ended version is no
// longer what we believe — but the replacement carries a bounded valid
// interval rather than an open one. A decommissioned line, a breaker that
// opened, a rating that expired.
//
// endedAt may be in the past, and usually is: the system learns at learnedAt
// that something ended earlier.
func Close(prev Record, endedAt, learnedAt Instant) (archive, next Record, err error) {
	bounded, err := From(prev.Valid.From, endedAt)
	if err != nil {
		return Record{}, Record{}, fmt.Errorf(
			"bitemporal: closing validity at %s for a fact valid from %s: %w",
			endedAt, prev.Valid.From, err)
	}

	return Supersede(prev, bounded, learnedAt)
}

// Reopen records that a fact previously believed to have ended is in fact
// still true — a decommissioning that was canceled, an outage record closed
// in error.
func Reopen(prev Record, learnedAt Instant) (archive, next Record, err error) {
	if prev.Valid.IsOpen() {
		return Record{}, Record{}, fmt.Errorf(
			"bitemporal: %s is already open-ended; there is nothing to reopen", prev)
	}

	return Supersede(prev, StartingAt(prev.Valid.From), learnedAt)
}
