package bserr

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

// The pattern the package exists to support: an extension wraps a shared kind,
// and both questions can be asked of the result.
var errBusNotFound = fmt.Errorf("topology: bus not found: %w", ErrNotFound)

func TestWrappingWorksAtBothLevels(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("loading network model nmdl_01: %w", errBusNotFound)

	if !errors.Is(err, errBusNotFound) {
		t.Error("the specific sentinel was lost through wrapping")
	}

	if !errors.Is(err, ErrNotFound) {
		t.Error("the shared kind was lost through wrapping; transport would " +
			"have to import the extension to classify this")
	}

	if got := HTTPStatus(err); got != http.StatusNotFound {
		t.Errorf("HTTPStatus = %d, want 404", got)
	}
}

func TestHTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		want int
	}{
		{nil, http.StatusOK},
		{ErrNotFound, http.StatusNotFound},
		{ErrConflict, http.StatusConflict},
		{ErrInvalid, http.StatusBadRequest},
		{ErrUnsupported, http.StatusUnprocessableEntity},
		{ErrPermission, http.StatusForbidden},
		{ErrUnavailable, http.StatusServiceUnavailable},
		{ErrInternal, http.StatusInternalServerError},
		{errors.New("something nobody classified"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		if got := HTTPStatus(tt.err); got != tt.want {
			t.Errorf("HTTPStatus(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
}

// An unclassified error is a defect, not a client mistake. Reporting it as a
// 400 would hide it among ordinary bad requests in the logs.
func TestUnclassifiedErrorsAreServerErrors(t *testing.T) {
	t.Parallel()

	mystery := errors.New("something nobody classified")

	if got := HTTPStatus(mystery); got != http.StatusInternalServerError {
		t.Errorf("an unclassified error mapped to %d; it should be 500 so it "+
			"shows up as a defect rather than as a client mistake", got)
	}

	if IsClientError(mystery) {
		t.Error("an unclassified error was reported as the client's fault")
	}
}

func TestKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want error
	}{
		{"wrapped not found", errBusNotFound, ErrNotFound},
		{"double wrapped", fmt.Errorf("outer: %w", errBusNotFound), ErrNotFound},
		{"conflict", fmt.Errorf("x: %w", ErrConflict), ErrConflict},
		{"unclassified falls back to internal", errors.New("?"), ErrInternal},
		{"nil falls back to internal", nil, ErrInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Kind(tt.err); !errors.Is(got, tt.want) {
				t.Errorf("Kind(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// Log level follows this. Logging routine client errors at error level is how
// alert fatigue starts.
func TestIsClientError(t *testing.T) {
	t.Parallel()

	clientFaults := []error{ErrNotFound, ErrConflict, ErrInvalid, ErrUnsupported, ErrPermission}
	for _, err := range clientFaults {
		if !IsClientError(err) {
			t.Errorf("%v should be a client error", err)
		}
	}

	ourFaults := []error{ErrUnavailable, ErrInternal, errors.New("?")}
	for _, err := range ourFaults {
		if IsClientError(err) {
			t.Errorf("%v should not be a client error", err)
		}
	}
}

func TestIsRetryable(t *testing.T) {
	t.Parallel()

	for _, err := range []error{ErrUnavailable, ErrConflict} {
		if !IsRetryable(err) {
			t.Errorf("%v should be retryable", err)
		}
	}

	for _, err := range []error{ErrNotFound, ErrInvalid, ErrPermission, ErrUnsupported} {
		if IsRetryable(err) {
			t.Errorf("%v should not be retryable; repeating it changes nothing", err)
		}
	}
}

// The kinds must stay distinct, or errors.Is on one matches another and the
// status mapping becomes arbitrary.
func TestKindsAreDistinct(t *testing.T) {
	t.Parallel()

	all := []error{
		ErrNotFound, ErrConflict, ErrInvalid, ErrUnsupported,
		ErrPermission, ErrUnavailable, ErrInternal,
	}

	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}

			if errors.Is(a, b) {
				t.Errorf("%v matches %v; the kinds must stay distinct", a, b)
			}
		}
	}
}
