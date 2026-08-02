package fabriqstore

import (
	"errors"
	"fmt"

	"github.com/xraph/fabriq/core/fabriqerr"
	"github.com/xraph/fabriq/core/tenant"

	"github.com/xraph/blackstart/pkg/bserr"
)

// Translate maps a Fabriq error onto blackstart's shared error kinds.
//
// It is applied at every boundary where a Fabriq call returns, so that
// transport can classify a failure without importing Fabriq — and so that a
// Fabriq API change alters this function rather than every HTTP handler.
//
// The original error is wrapped rather than replaced, so errors.Is against a
// Fabriq sentinel still works for code that genuinely needs the distinction.
func Translate(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, fabriqerr.ErrNotFound):
		return fmt.Errorf("%w: %w", bserr.ErrNotFound, err)

	case errors.Is(err, fabriqerr.ErrVersionConflict):
		return fmt.Errorf("%w: %w", bserr.ErrConflict, err)

	case errors.Is(err, fabriqerr.ErrQueryTimeout),
		errors.Is(err, fabriqerr.ErrProjectionLag):
		return fmt.Errorf("%w: %w", bserr.ErrUnavailable, err)

	case errors.Is(err, fabriqerr.ErrStoreNotConfigured),
		errors.Is(err, fabriqerr.ErrRawSQLUnsupported):
		// Not the caller's fault and not retryable: a capability was used
		// that this deployment did not configure. Unsupported rather than
		// unavailable, because waiting will not help.
		return fmt.Errorf("%w: %w", bserr.ErrUnsupported, err)

	case errors.Is(err, tenant.ErrNoTenant):
		// A missing tenant is never a client error. Fabriq requires one on
		// every entry point, and blackstart stamps a constant, so reaching
		// this means a code path bypassed Scoped — a wiring defect that
		// should read as a 500 and be fixed, not surfaced as a bad request.
		return fmt.Errorf(
			"%w: tenant context missing; a store call bypassed Scoped: %w",
			bserr.ErrInternal, err)

	default:
		return err
	}
}
