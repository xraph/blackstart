// Package httperr maps domain errors onto HTTP ones.
//
// One place rather than one per controller, so every extension's API agrees on
// what a missing record looks like. Four extensions had grown an identical
// private copy of this before it was extracted; the fifth would have been the
// one that quietly disagreed.
//
// Separate from pkg/bserr because that package is deliberately transport-free —
// stores, engines and the solver client all return its sentinels, and none of
// them should have to compile against an HTTP framework to do it.
package httperr

import (
	"net/http"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/pkg/bserr"
)

// From turns a domain error into the HTTP error a Forge handler returns.
//
// The status comes from bserr.HTTPStatus, so the mapping lives with the
// sentinels rather than being restated here.
func From(err error) error {
	if err == nil {
		return nil
	}

	status := bserr.HTTPStatus(err)

	switch status {
	case http.StatusNotFound:
		return forge.NotFound(err.Error())
	case http.StatusBadRequest:
		return forge.BadRequest(err.Error())
	case http.StatusForbidden:
		return forge.Forbidden(err.Error())
	}

	return forge.NewHTTPError(status, err.Error())
}
