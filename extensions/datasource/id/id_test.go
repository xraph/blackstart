package id_test

import (
	"testing"

	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	"github.com/xraph/blackstart/pkg/id"
)

// The prefixes must be registered by the time anything uses them, which is
// what the init function is for. Registering from init means a collision with
// another extension stops the process at startup with both owners named.
func TestPrefixesAreRegisteredToThisExtension(t *testing.T) {
	t.Parallel()

	for _, p := range []id.Prefix{dsid.Source, dsid.Feed} {
		got, ok := id.Lookup(p)
		if !ok {
			t.Errorf("prefix %q is not registered", p)

			continue
		}

		if got.Owner != dsid.Owner {
			t.Errorf("prefix %q is owned by %q, want %q", p, got.Owner, dsid.Owner)
		}
	}
}

func TestConstructorsMintTheRightType(t *testing.T) {
	t.Parallel()

	if got := dsid.NewSource().Prefix(); got != dsid.Source {
		t.Errorf("NewSource minted a %q identifier", got)
	}

	if got := dsid.NewFeed().Prefix(); got != dsid.Feed {
		t.Errorf("NewFeed minted a %q identifier", got)
	}
}

// The typed parsers are the point: a source identifier reaching a feed lookup
// should fail as "wrong type" rather than later as "not found", which sends
// the reader hunting in the wrong place.
func TestParsersRejectTheOtherType(t *testing.T) {
	t.Parallel()

	source := dsid.NewSource()
	feed := dsid.NewFeed()

	if _, err := dsid.ParseSource(source.String()); err != nil {
		t.Errorf("ParseSource rejected a source identifier: %v", err)
	}

	if _, err := dsid.ParseFeed(feed.String()); err != nil {
		t.Errorf("ParseFeed rejected a feed identifier: %v", err)
	}

	if _, err := dsid.ParseSource(feed.String()); err == nil {
		t.Error("ParseSource accepted a feed identifier")
	}

	if _, err := dsid.ParseFeed(source.String()); err == nil {
		t.Error("ParseFeed accepted a source identifier")
	}
}

// Prefixes are immutable once shipped: changing one invalidates every stored
// identifier using it. Pinning the literals makes a change fail here rather
// than in production data.
func TestPrefixValuesAreFrozen(t *testing.T) {
	t.Parallel()

	if dsid.Source != "dsrc" || dsid.Feed != "dfeed" {
		t.Errorf("a shipped prefix changed: source=%q feed=%q; every stored "+
			"identifier using the old value would become unparseable",
			dsid.Source, dsid.Feed)
	}
}
