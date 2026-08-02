package bitemporal

import (
	"testing"
	"time"
)

func at(day, hour int) Instant {
	return At(time.Date(2026, 2, day, hour, 0, 0, 0, time.UTC))
}

// THE test. Everything else in this package exists to make this behavior
// possible.
//
// A rating is recorded on the 10th. On the 20th someone discovers it was
// wrong and files a correction, backdated so the corrected value applies from
// the 15th. A model that ran on the 16th saw the original value. Scoring that
// model must return the original value, not the correction — otherwise the
// model appears to have known something it could not have known, and its
// measured skill is inflated by exactly the amount of the correction.
func TestACorrectionDoesNotLeakBackwardsInKnowledgeTime(t *testing.T) {
	t.Parallel()

	const (
		originallyLearned = 10
		correctionApplies = 15
		modelRan          = 16
		correctionFiled   = 20
	)

	// v1: believed from the 10th, valid from the 10th, open-ended.
	v1, err := Open(at(originallyLearned, 0), at(originallyLearned, 0))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// On the 20th we learn the true value applied from the 15th. The old
	// version's validity is closed at the 15th; the new one takes over there.
	archived, superseded, err := Close(v1, at(correctionApplies, 0), at(correctionFiled, 0))
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	v2, err := Open(at(correctionApplies, 0), at(correctionFiled, 0))
	if err != nil {
		t.Fatalf("Open v2: %v", err)
	}

	versions := []Record{archived, superseded, v2}

	t.Run("the model's own view is unchanged by the later correction", func(t *testing.T) {
		t.Parallel()

		asOf := PointInTime(at(modelRan, 0), at(modelRan, 0))

		matched := matching(versions, asOf)
		if len(matched) != 1 {
			t.Fatalf("expected exactly one version to match %s, got %d: %v",
				asOf, len(matched), matched)
		}

		if !matched[0].Knowledge.From.Equal(at(originallyLearned, 0)) {
			t.Errorf("a read as of the 16th returned a version believed from %s; "+
				"the correction filed on the 20th has leaked backwards",
				matched[0].Knowledge.From)
		}
	})

	t.Run("a report today sees the corrected value", func(t *testing.T) {
		t.Parallel()

		asOf := PointInTime(at(modelRan, 0), at(correctionFiled+1, 0))

		matched := matching(versions, asOf)
		if len(matched) != 1 {
			t.Fatalf("expected exactly one version to match %s, got %d: %v",
				asOf, len(matched), matched)
		}

		if !matched[0].Knowledge.From.Equal(at(correctionFiled, 0)) {
			t.Errorf("a read with present knowledge returned a version believed "+
				"from %s; it should see the correction",
				matched[0].Knowledge.From)
		}
	})
}

// At any point on either axis, exactly one version answers. Two would make a
// query non-deterministic; none would lose the fact.
func TestExactlyOneVersionMatchesAtEveryPoint(t *testing.T) {
	t.Parallel()

	v1, err := Open(at(1, 0), at(1, 0))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	archivedV1, correctedV1, err := Correct(v1, at(5, 0))
	if err != nil {
		t.Fatalf("Correct: %v", err)
	}

	archivedV2, endedV2, err := Close(correctedV1, at(10, 0), at(12, 0))
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	versions := []Record{archivedV1, archivedV2, endedV2}

	for _, validDay := range []int{2, 6, 9} {
		for _, knownDay := range []int{2, 6, 9, 13, 20} {
			if knownDay < validDay {
				continue // a read cannot know about a moment before it happened
			}

			asOf := PointInTime(at(validDay, 0), at(knownDay, 0))

			if got := len(matching(versions, asOf)); got != 1 {
				t.Errorf("%s matched %d versions, want exactly 1", asOf, got)
			}
		}
	}

	// After the fact ends, nothing is valid — which is correct, not a bug.
	afterEnd := PointInTime(at(11, 0), at(20, 0))
	if got := len(matching(versions, afterEnd)); got != 0 {
		t.Errorf("%s matched %d versions; the fact ended on the 10th", afterEnd, got)
	}
}

func TestOpen(t *testing.T) {
	t.Parallel()

	got, err := Open(at(1, 0), at(3, 0))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if !got.Valid.IsOpen() || !got.Knowledge.IsOpen() {
		t.Errorf("a new fact should be open on both axes: %s", got)
	}

	if !got.IsCurrent() {
		t.Error("a new fact should be the current belief")
	}

	// Backdating is normal: a substation commissioned last year, entered today.
	if !got.Valid.From.Before(got.Knowledge.From) {
		t.Error("Open should accept a valid time earlier than the learn time")
	}
}

// Correct moves only the knowledge axis. Close moves the valid axis too.
// Conflating the two is the usual mistake, so each is pinned.
func TestCorrectMovesOnlyTheKnowledgeAxis(t *testing.T) {
	t.Parallel()

	v1, err := Open(at(1, 0), at(1, 0))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	archive, next, err := Correct(v1, at(5, 0))
	if err != nil {
		t.Fatalf("Correct: %v", err)
	}

	if !next.Valid.From.Equal(v1.Valid.From) || !next.Valid.To.Equal(v1.Valid.To) {
		t.Errorf("Correct changed the valid interval: %s -> %s", v1.Valid, next.Valid)
	}

	if !archive.Knowledge.To.Equal(at(5, 0)) {
		t.Errorf("the archived version's belief should end at the correction: %s", archive)
	}

	if archive.IsCurrent() {
		t.Error("the archived version should not be the current belief")
	}

	if !next.IsCurrent() {
		t.Error("the replacement should be the current belief")
	}
}

func TestCloseBoundsTheValidInterval(t *testing.T) {
	t.Parallel()

	v1, err := Open(at(1, 0), at(1, 0))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, next, err := Close(v1, at(10, 0), at(12, 0))
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	if next.Valid.IsOpen() {
		t.Error("Close left the valid interval open")
	}

	if !next.Valid.To.Equal(at(10, 0)) {
		t.Errorf("valid interval ends at %s, want the 10th", next.Valid.To)
	}

	if !next.Knowledge.IsOpen() {
		t.Error("Close should leave the replacement as the current belief")
	}
}

func TestCloseRejectsAnEndBeforeTheStart(t *testing.T) {
	t.Parallel()

	v1, err := Open(at(10, 0), at(10, 0))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, _, err := Close(v1, at(5, 0), at(12, 0)); err == nil {
		t.Error("closed a fact before it began")
	}
}

func TestReopen(t *testing.T) {
	t.Parallel()

	v1, err := Open(at(1, 0), at(1, 0))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, ended, err := Close(v1, at(10, 0), at(12, 0))
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, reopened, err := Reopen(ended, at(15, 0))
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}

	if !reopened.Valid.IsOpen() {
		t.Error("Reopen did not reopen the valid interval")
	}

	if !reopened.Valid.From.Equal(at(1, 0)) {
		t.Errorf("Reopen changed the start of validity to %s", reopened.Valid.From)
	}

	if _, _, err := Reopen(reopened, at(20, 0)); err == nil {
		t.Error("reopened an already-open record")
	}
}

// Knowledge time moves forward. Superseding a record at an instant at or
// before the moment it came to be believed would produce an empty knowledge
// interval, which can never match a query.
func TestSupersedeRequiresForwardKnowledgeTime(t *testing.T) {
	t.Parallel()

	v1, err := Open(at(1, 0), at(5, 0))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	for _, when := range []Instant{at(5, 0), at(4, 0)} {
		if _, _, err := Correct(v1, when); err == nil {
			t.Errorf("superseded a record believed from the 5th at %s", when)
		}
	}
}

// Only the current belief can be superseded. Superseding an archived version
// would fork history into two conflicting branches.
func TestSupersedeRejectsAnArchivedRecord(t *testing.T) {
	t.Parallel()

	v1, err := Open(at(1, 0), at(1, 0))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	archive, _, err := Correct(v1, at(5, 0))
	if err != nil {
		t.Fatalf("Correct: %v", err)
	}

	if _, _, err := Correct(archive, at(9, 0)); err == nil {
		t.Error("superseded an already-archived version; history would fork")
	}
}

func matching(versions []Record, a AsOf) []Record {
	var out []Record

	for _, v := range versions {
		if v.Matches(a) {
			out = append(out, v)
		}
	}

	return out
}
