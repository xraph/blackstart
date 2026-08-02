package hooks

import "testing"

// The bridge probes several shapes because nothing in Forge declares what an
// interceptor deposits. These tests exercise actorFromValue directly rather
// than through a Forge request, since constructing one needs a live router and
// the interesting behavior is entirely in the interpretation.

type fakeSubjectWithID struct{ id string }

func (f fakeSubjectWithID) GetID() string { return f.id }

type fakeSubjectNamed struct{ sub string }

func (f fakeSubjectNamed) Subject() string { return f.sub }

func TestActorFromValueRecognizesTheCommonShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		value  any
		wantID string
	}{
		{"bare string", "usr_01j9", "usr_01j9"},
		{"object with GetID", fakeSubjectWithID{id: "usr_02"}, "usr_02"},
		{"object with Subject", fakeSubjectNamed{sub: "usr_03"}, "usr_03"},
		{"claims with sub", map[string]any{"sub": "usr_04"}, "usr_04"},
		{"claims with id", map[string]any{"id": "usr_05"}, "usr_05"},
		{"claims with user_id", map[string]any{"user_id": "usr_06"}, "usr_06"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := actorFromValue(tt.value)
			if !ok {
				t.Fatalf("did not recognize %#v", tt.value)
			}

			if got.Kind != KindUser {
				t.Errorf("kind = %s, want user", got.Kind)
			}

			if got.ID != tt.wantID {
				t.Errorf("id = %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}

// The registered JWT claim wins over the hand-rolled ones, so a token
// carrying both does not depend on map iteration order.
func TestClaimsPreferTheRegisteredSubject(t *testing.T) {
	t.Parallel()

	got, ok := actorFromValue(map[string]any{
		"sub":     "registered",
		"id":      "handrolled",
		"user_id": "also-handrolled",
	})
	if !ok {
		t.Fatal("did not recognize the claims map")
	}

	if got.ID != "registered" {
		t.Errorf("id = %q; the registered `sub` claim should win so that "+
			"attribution does not depend on map ordering", got.ID)
	}
}

// Attributing an action to a subject we could not actually identify is worse
// than attributing it to the system: it looks authoritative while being made
// up.
func TestUnrecognizedValuesFallBackRatherThanBeingCoerced(t *testing.T) {
	t.Parallel()

	unrecognized := []any{
		nil,
		"",
		42,
		map[string]any{},
		map[string]any{"sub": ""},
		map[string]any{"name": "no identifier here"},
		struct{ Unrelated int }{1},
		fakeSubjectWithID{id: ""},
		fakeSubjectNamed{sub: ""},
	}

	for _, v := range unrecognized {
		got, ok := actorFromValue(v)
		if ok {
			t.Errorf("coerced %#v into an actor: %s", v, got)
		}

		if !got.IsSystem() {
			t.Errorf("the fallback for %#v was %s, want the system actor", v, got)
		}
	}
}

// A nil Forge context is what a background task or a unit test passes, and it
// must not panic.
func TestNilForgeContextYieldsSystem(t *testing.T) {
	t.Parallel()

	if got := ActorFromForgeContext(nil); !got.IsSystem() {
		t.Errorf("a nil request context yielded %s", got)
	}

	ctx := ContextWithActorFromForge(nil)
	if ctx == nil {
		t.Fatal("ContextWithActorFromForge returned nil")
	}

	if got := ActorFromContext(ctx); !got.IsSystem() {
		t.Errorf("a nil request context produced actor %s", got)
	}
}

// The whole point of this file is that these two keys appear exactly once in
// the repository. If either is ever spelled somewhere else, the quarantine has
// leaked.
func TestTheForgeKeysArePinnedHere(t *testing.T) {
	t.Parallel()

	if forgeUserKey != "user" || forgeAuthKey != "auth" {
		t.Errorf("the Forge context keys changed: %q, %q — every consumer of "+
			"Forge auth state reads these, and nothing in Forge declares them",
			forgeUserKey, forgeAuthKey)
	}
}
