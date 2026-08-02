package hooks

import (
	"errors"
	"strings"
	"testing"
)

// PanicError has to render something useful whatever was thrown. A panic
// value is any, and the message ends up in an operator's log at the moment
// they are trying to work out which subscriber misbehaved.
func TestPanicErrorRendersAnyPanicValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"string", "subscriber exploded", "subscriber exploded"},
		{"error", errors.New("boom"), "boom"},
		{"integer", 42, "non-string panic value"},
		{"nil", nil, "non-string panic value"},
		{"struct", struct{ A int }{1}, "non-string panic value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := &PanicError{Value: tt.value}

			msg := err.Error()
			if !strings.Contains(msg, tt.want) {
				t.Errorf("Error() = %q, want it to contain %q", msg, tt.want)
			}

			if !strings.Contains(msg, "subscriber panicked") {
				t.Errorf("Error() = %q, should say a subscriber panicked", msg)
			}
		})
	}
}

func TestPanicErrorIsAnError(t *testing.T) {
	t.Parallel()

	var err error = &PanicError{Value: "x"}

	var target *PanicError
	if !errors.As(err, &target) {
		t.Error("PanicError is not recoverable with errors.As")
	}

	if target.Value != "x" {
		t.Errorf("the panic value was lost: %v", target.Value)
	}
}

// The operations are a closed vocabulary. OpRun exists because most of what
// travels on this bus is not an entity change at all.
func TestOperationsAreDistinct(t *testing.T) {
	t.Parallel()

	all := []Op{OpCreate, OpUpdate, OpDelete, OpRun}

	seen := map[Op]bool{}
	for _, op := range all {
		if op == "" {
			t.Error("an operation has an empty value")
		}

		if seen[op] {
			t.Errorf("duplicate operation %q", op)
		}

		seen[op] = true
	}
}
