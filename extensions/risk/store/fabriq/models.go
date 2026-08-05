package fabriq

import (
	"fmt"
	"time"

	"github.com/xraph/grove"

	riskid "github.com/xraph/blackstart/extensions/risk/id"
	"github.com/xraph/blackstart/extensions/risk/store"
	"github.com/xraph/blackstart/pkg/id"
)

// valueModel is the persistence shape of one computed probability.
type valueModel struct {
	grove.BaseModel `grove:"table:blackstart_risk_value"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	ComponentID string    `grove:"component_id,notnull"`
	Quantity    string    `grove:"quantity,notnull"`
	IssuedAt    time.Time `grove:"issued_at,notnull"`
	ValidAt     time.Time `grove:"valid_at,notnull"`

	// Probability and Scalar carry no notnull tag. Zero is a legitimate
	// probability — a component the model is confident will not fail in the
	// hour is exactly the row a zero-rejecting write would discard — and the
	// same reasoning that keeps hazard's pass counters untagged applies here.
	Probability float64 `grove:"probability"`
	Scalar      float64 `grove:"scalar"`
}

func valueToModel(v store.ComponentValue) *valueModel {
	return &valueModel{
		ComponentID: v.ComponentID.String(),
		Quantity:    string(v.Quantity),
		IssuedAt:    v.IssuedAt.UTC(),
		ValidAt:     v.ValidAt.UTC(),
		Probability: v.Probability,
		Scalar:      v.Scalar,
	}
}

func valueFromModel(m valueModel) (store.ComponentValue, error) {
	componentID, err := id.Parse(m.ComponentID)
	if err != nil {
		return store.ComponentValue{}, fmt.Errorf(
			"risk: stored component id %q: %w", m.ComponentID, err)
	}

	return store.ComponentValue{
		ComponentID: componentID,
		Quantity:    store.Quantity(m.Quantity),
		IssuedAt:    m.IssuedAt.UTC(),
		ValidAt:     m.ValidAt.UTC(),
		Probability: m.Probability,
		Scalar:      m.Scalar,
	}, nil
}

// newValueID mints the row's identifier. See riskid.Value for why this
// extension mints one at all despite minting no component identifiers.
func newValueID() id.ID { return riskid.NewValue() }
