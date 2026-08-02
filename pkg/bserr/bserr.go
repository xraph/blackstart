// Package bserr provides the small set of error kinds every extension shares.
//
// Extensions do not return these sentinels directly. They wrap them, so that
// errors.Is works at two levels — the specific fact and the general kind:
//
//	var ErrBusNotFound = fmt.Errorf("topology: bus not found: %w", bserr.ErrNotFound)
//
//	errors.Is(err, topology.ErrBusNotFound)  // was it this exactly?
//	errors.Is(err, bserr.ErrNotFound)        // is it a 404?
//
// The transport layer needs only the second question, so it never imports an
// extension to map a status code. A handler in `api` translating a store error
// would otherwise have to know every sentinel every store might return, which
// is how transport code ends up depending on domains it has no business
// knowing about.
//
// The set is deliberately small. An error kind that maps to the same status
// code and the same operator response as an existing one is not a new kind.
package bserr

import (
	"errors"
	"net/http"
)

// The shared error kinds.
//
//nolint:gochecknoglobals // sentinel errors, the standard Go idiom
var (
	// ErrNotFound means the addressed thing does not exist, or does not exist
	// at the requested point in time. The second case is ordinary here: a bus
	// that was decommissioned last year genuinely does not exist as of today.
	ErrNotFound = errors.New("not found")

	// ErrConflict means the write collided with another. Usually an optimistic
	// concurrency failure, or an attempt to create something that exists.
	ErrConflict = errors.New("conflict")

	// ErrInvalid means the request was understood and is wrong — a malformed
	// identifier, a negative impedance, a valid time after its end.
	ErrInvalid = errors.New("invalid")

	// ErrUnsupported means the operation is not implemented for this input.
	// A parser meeting a schema version it does not handle, a solver asked for
	// a method it does not provide.
	ErrUnsupported = errors.New("unsupported")

	// ErrPermission means the actor may not do this.
	ErrPermission = errors.New("permission denied")

	// ErrUnavailable means a dependency is down or a deadline passed. Retrying
	// may work; nothing about the request itself is wrong.
	ErrUnavailable = errors.New("unavailable")

	// ErrInternal means a defect. It exists so that transport can distinguish
	// "we broke" from "you asked for something impossible", and so that the
	// two get different log levels.
	ErrInternal = errors.New("internal error")
)

// HTTPStatus maps an error to a status code.
//
// An unrecognized error maps to 500, which is the right default: an error kind
// nobody classified is a defect, not a client mistake, and reporting it as one
// would hide it in the request logs.
func HTTPStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	case errors.Is(err, ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, ErrUnsupported):
		return http.StatusUnprocessableEntity
	case errors.Is(err, ErrPermission):
		return http.StatusForbidden
	case errors.Is(err, ErrUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// Kind returns the shared sentinel an error belongs to, or ErrInternal.
//
// Useful for metrics labels and log fields, where the full error text has too
// many distinct values to aggregate usefully.
func Kind(err error) error {
	for _, kind := range []error{
		ErrNotFound, ErrConflict, ErrInvalid,
		ErrUnsupported, ErrPermission, ErrUnavailable,
	} {
		if errors.Is(err, kind) {
			return kind
		}
	}

	return ErrInternal
}

// IsClientError reports whether the error is the caller's fault.
//
// The distinction drives log level: a client error is routine and belongs at
// info, while everything else is a defect or an outage and belongs at error.
// Logging the former at error level is how alert fatigue starts.
func IsClientError(err error) bool {
	status := HTTPStatus(err)

	return status >= http.StatusBadRequest && status < http.StatusInternalServerError
}

// IsRetryable reports whether repeating the request might succeed without
// changing it.
func IsRetryable(err error) bool {
	return errors.Is(err, ErrUnavailable) || errors.Is(err, ErrConflict)
}
