package fabriqstore

import (
	"errors"
	"fmt"
	"testing"

	"github.com/xraph/fabriq/core/fabriqerr"
	"github.com/xraph/fabriq/core/tenant"

	"github.com/xraph/blackstart/pkg/bserr"
)

func TestTranslate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   error
		want error
	}{
		{"not found", fabriqerr.ErrNotFound, bserr.ErrNotFound},
		{"version conflict", fabriqerr.ErrVersionConflict, bserr.ErrConflict},
		{"query timeout", fabriqerr.ErrQueryTimeout, bserr.ErrUnavailable},
		{"projection lag", fabriqerr.ErrProjectionLag, bserr.ErrUnavailable},
		{"store not configured", fabriqerr.ErrStoreNotConfigured, bserr.ErrUnsupported},
		{"raw sql unsupported", fabriqerr.ErrRawSQLUnsupported, bserr.ErrUnsupported},
		{"missing tenant", tenant.ErrNoTenant, bserr.ErrInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Translate(tt.in)
			if !errors.Is(got, tt.want) {
				t.Errorf("Translate(%v) does not match %v", tt.in, tt.want)
			}
		})
	}
}

// The original is wrapped rather than replaced, so code that genuinely needs
// the Fabriq-level distinction can still ask for it.
func TestTranslatePreservesTheOriginal(t *testing.T) {
	t.Parallel()

	got := Translate(fabriqerr.ErrVersionConflict)

	if !errors.Is(got, fabriqerr.ErrVersionConflict) {
		t.Error("the Fabriq sentinel was lost in translation")
	}

	if !errors.Is(got, bserr.ErrConflict) {
		t.Error("the shared kind was not attached")
	}
}

func TestTranslateSeesThroughWrapping(t *testing.T) {
	t.Parallel()

	deep := fmt.Errorf("loading bus: %w",
		fmt.Errorf("relational get: %w", fabriqerr.ErrNotFound))

	if !errors.Is(Translate(deep), bserr.ErrNotFound) {
		t.Error("Translate did not see a sentinel through two layers of wrapping")
	}
}

func TestTranslatePassesThroughNilAndUnknown(t *testing.T) {
	t.Parallel()

	if Translate(nil) != nil {
		t.Error("Translate turned nil into an error")
	}

	unknown := errors.New("something else entirely")
	if got := Translate(unknown); !errors.Is(got, unknown) {
		t.Error("an unrecognized error was not passed through unchanged")
	}

	// And it maps to 500, because an unclassified failure is a defect.
	if got := bserr.HTTPStatus(Translate(unknown)); got != 500 {
		t.Errorf("an unrecognized error mapped to %d, want 500", got)
	}
}

// A missing tenant is a wiring defect, never a client mistake: blackstart
// stamps a constant, so reaching this means a call path bypassed Scoped.
// Reporting it as a 400 would file our bug under the caller's mistakes.
func TestMissingTenantIsAnInternalErrorNotAClientError(t *testing.T) {
	t.Parallel()

	got := Translate(tenant.ErrNoTenant)

	if bserr.IsClientError(got) {
		t.Error("a missing tenant was reported as the client's fault; it means " +
			"a store call bypassed Scoped, which is our defect")
	}

	if status := bserr.HTTPStatus(got); status != 500 {
		t.Errorf("missing tenant mapped to %d, want 500", status)
	}
}

// The retry advice has to survive translation, since it is what a client
// actually acts on.
func TestTranslatedErrorsCarryUsefulRetryAdvice(t *testing.T) {
	t.Parallel()

	for _, in := range []error{fabriqerr.ErrQueryTimeout, fabriqerr.ErrVersionConflict} {
		if !bserr.IsRetryable(Translate(in)) {
			t.Errorf("%v should translate to something retryable", in)
		}
	}

	for _, in := range []error{fabriqerr.ErrNotFound, fabriqerr.ErrStoreNotConfigured} {
		if bserr.IsRetryable(Translate(in)) {
			t.Errorf("%v should not translate to something retryable; repeating "+
				"it changes nothing", in)
		}
	}
}
