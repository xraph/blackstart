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
// It is applied at every boundary where a Fabriq call returns, so transport can
// classify a failure without importing Fabriq — and so a Fabriq API change
// alters this function rather than every HTTP handler.
//
// The structured code is consulted first. Fabriq's own documentation is
// explicit that "HTTP and RPC boundaries switch on Code rather than matching on
// error-message substrings", and the code space is much wider than the handful
// of exported sentinels: a unique-index violation arrives as CodeAlreadyExists
// with no matching sentinel at all. An earlier version of this function checked
// only the sentinels and silently passed that through as an internal error,
// which the integration suite caught by asserting a duplicate slug surfaces as
// a conflict.
//
// The original error is wrapped rather than replaced, so errors.Is against a
// Fabriq sentinel still works for code that needs the distinction.
func Translate(err error) error {
	if err == nil {
		return nil
	}

	if kind := kindForCode(fabriqerr.CodeOf(err)); kind != nil {
		return fmt.Errorf("%w: %w", kind, err)
	}

	if kind := kindForSentinel(err); kind != nil {
		return fmt.Errorf("%w: %w", kind, err)
	}

	if errors.Is(err, tenant.ErrNoTenant) {
		// A missing tenant is never a client error. Fabriq requires one at
		// every entry point and blackstart stamps a constant, so reaching this
		// means a code path bypassed Scoped — a wiring defect that should read
		// as a 500 and be fixed, not surfaced as a bad request.
		return fmt.Errorf(
			"%w: tenant context missing; a store call bypassed Scoped: %w",
			bserr.ErrInternal, err)
	}

	return err
}

// kindForCode maps Fabriq's structured code onto a shared kind, or nil when
// the error carries no code.
func kindForCode(code fabriqerr.Code) error {
	switch code {
	case fabriqerr.CodeNotFound:
		return bserr.ErrNotFound

	case fabriqerr.CodeAlreadyExists,
		fabriqerr.CodeConstraintViolation,
		fabriqerr.CodeVersionConflict,
		// A serialization failure is a lost race, not a bad request: the same
		// write may well succeed on retry, which is what the conflict kind
		// tells a caller.
		fabriqerr.CodeSerialization:
		return bserr.ErrConflict

	case fabriqerr.CodeInvalidInput:
		return bserr.ErrInvalid

	case fabriqerr.CodeUnauthorized, fabriqerr.CodePermissionDenied:
		return bserr.ErrPermission

	case fabriqerr.CodeUnavailable, fabriqerr.CodeTimeout:
		return bserr.ErrUnavailable

	case fabriqerr.CodeNotConfigured:
		// Not the caller's fault and not retryable: a capability was used that
		// this deployment did not configure. Waiting will not help.
		return bserr.ErrUnsupported

	case fabriqerr.CodeSchemaMismatch:
		// The model and the table disagree, which means a migration was missed.
		// That is a deployment defect rather than anything the caller did.
		return bserr.ErrInternal

	case fabriqerr.CodeInternal:
		return bserr.ErrInternal

	default:
		return nil
	}
}

// kindForSentinel covers the errors Fabriq exports as plain sentinels, which
// carry no structured code.
func kindForSentinel(err error) error {
	switch {
	case errors.Is(err, fabriqerr.ErrNotFound):
		return bserr.ErrNotFound

	case errors.Is(err, fabriqerr.ErrVersionConflict):
		return bserr.ErrConflict

	case errors.Is(err, fabriqerr.ErrQueryTimeout), errors.Is(err, fabriqerr.ErrProjectionLag):
		return bserr.ErrUnavailable

	case errors.Is(err, fabriqerr.ErrStoreNotConfigured),
		errors.Is(err, fabriqerr.ErrRawSQLUnsupported):
		return bserr.ErrUnsupported

	default:
		return nil
	}
}
