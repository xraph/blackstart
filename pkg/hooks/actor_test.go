package hooks

import (
	"context"
	"testing"
)

// The central design choice. A background task with no actor must produce a
// visibly-system-attributed record, not an error — a refused write in a cron
// job at 3am is far harder to discover than an honest "system" in the audit
// log.
func TestMissingActorIsSystemNotAFailure(t *testing.T) {
	t.Parallel()

	got := ActorFromContext(context.Background())

	if !got.IsSystem() {
		t.Errorf("an unattributed context yielded %s, want the system actor", got)
	}

	if got.String() != "system:system" {
		t.Errorf("System renders as %q", got.String())
	}
}

// The zero ActorKind is System for the same reason: a struct field nobody
// populated attributes to the system rather than to a user with an empty
// identifier, which would be a false attribution rather than an absent one.
func TestZeroKindIsSystem(t *testing.T) {
	t.Parallel()

	var zero ActorID

	if !zero.IsSystem() {
		t.Error("the zero ActorID is not the system actor; an unpopulated " +
			"field would otherwise read as some other kind of actor")
	}
}

func TestConstructors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		actor    ActorID
		wantKind ActorKind
		wantStr  string
	}{
		{User("usr_01j9"), KindUser, "user:usr_01j9"},
		{Service("ingest-worker"), KindService, "service:ingest-worker"},
		{Import("dsv_01j9"), KindImport, "import:dsv_01j9"},
		{System, KindSystem, "system:system"},
	}

	for _, tt := range tests {
		if tt.actor.Kind != tt.wantKind {
			t.Errorf("%s has kind %s, want %s", tt.actor, tt.actor.Kind, tt.wantKind)
		}

		if got := tt.actor.String(); got != tt.wantStr {
			t.Errorf("String() = %q, want %q", got, tt.wantStr)
		}
	}
}

// The import actor names a dataset version so that a row's provenance
// reference and its audit entry agree without a join.
func TestImportActorCarriesTheDatasetVersion(t *testing.T) {
	t.Parallel()

	a := Import("dsv_01j9x8k2m3n4p5q6r7s8t9v0w1")

	if a.Kind != KindImport {
		t.Errorf("kind is %s, want import", a.Kind)
	}

	if a.ID != "dsv_01j9x8k2m3n4p5q6r7s8t9v0w1" {
		t.Errorf("the dataset version was not preserved: %s", a.ID)
	}
}

func TestContextRoundTrip(t *testing.T) {
	t.Parallel()

	want := User("usr_01j9")

	got := ActorFromContext(WithActor(t.Context(), want))
	if got != want {
		t.Errorf("round trip: %s -> %s", want, got)
	}
}

// A nested boundary may narrow the actor — a request handler setting the
// caller, then an inner job attributing its own work to a service.
func TestInnerActorOverridesOuter(t *testing.T) {
	t.Parallel()

	outer := WithActor(t.Context(), User("usr_01j9"))
	inner := WithActor(outer, Service("solver"))

	if got := ActorFromContext(inner); got.Kind != KindService {
		t.Errorf("inner actor did not take effect: %s", got)
	}

	if got := ActorFromContext(outer); got.Kind != KindUser {
		t.Errorf("the outer context was mutated: %s", got)
	}
}

func TestActorKindString(t *testing.T) {
	t.Parallel()

	tests := map[ActorKind]string{
		KindSystem:    "system",
		KindUser:      "user",
		KindService:   "service",
		KindImport:    "import",
		ActorKind(99): "kind(99)",
	}

	for k, want := range tests {
		if got := k.String(); got != want {
			t.Errorf("ActorKind(%d) = %q, want %q", uint8(k), got, want)
		}
	}
}

// ActorID is comparable, so it works as a map key for per-actor metrics and
// in an == check without reflect.
func TestActorIDIsComparable(t *testing.T) {
	t.Parallel()

	counts := map[ActorID]int{}
	counts[User("a")]++
	counts[User("a")]++
	counts[User("b")]++

	if counts[User("a")] != 2 {
		t.Errorf("ActorID does not work as a map key: %v", counts)
	}
}
