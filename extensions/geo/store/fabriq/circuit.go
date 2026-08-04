package fabriq

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/grove"

	"github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// CircuitTable is the circuits table.
const CircuitTable = "blackstart_geo_circuit"

// CircuitEntity is a transmission line somebody actually built.
const CircuitEntity = "geo_circuit"

// circuitRow is the circuits table.
//
// No electrical parameters, and that absence is the point rather than an
// omission — see the real-circuit-identity design, §3.1. A column here for
// impedance would be populated by a guess, and a guess in a column is
// indistinguishable from a measurement once it is stored.
type circuitRow struct {
	grove.BaseModel `grove:"table:blackstart_geo_circuit"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	ExternalID string  `grove:"external_id,notnull"`
	Source     string  `grove:"source,notnull"`
	VoltageKV  float64 `grove:"voltage_kv,notnull"`
	Operator   string  `grove:"operator"`
}

// Circuits returns the circuit store.
func (s *Store) Circuits() store.CircuitStore { return (*circuits)(s) }

type circuits Store

// Reconcile stores a circuit, or updates the one already carrying the same
// source and external identifier.
func (c *circuits) Reconcile(ctx context.Context, circuit asset.Circuit) (asset.Circuit, error) {
	if err := circuit.Validate(); err != nil {
		return asset.Circuit{}, err
	}

	existing, err := c.ByExternal(ctx, circuit.Source, circuit.ExternalID)
	if err != nil && !errors.Is(err, bserr.ErrNotFound) {
		return asset.Circuit{}, err
	}

	row := circuitRow{
		ExternalID: circuit.ExternalID,
		Source:     circuit.Source,
		VoltageKV:  circuit.VoltageKV,
		Operator:   circuit.Operator,
	}

	// No expected version. Two imports of the same bounding box racing is a
	// re-import, not a conflict: both carry the same source's view of the same
	// way, so last-writer-wins loses nothing. A version check here would fail
	// an import for agreeing with itself.
	//
	// Update rather than delete-and-recreate. The route already points at the
	// existing identifier, and recreating would break that reference for the
	// window between the two writes — on a re-import of thousands of lines,
	// that window is where a reader sees geometry carrying nothing.
	if err == nil {
		row.ID = existing.ID.String()

		if err := c.base.Update(ctx, CircuitEntity, existing.ID, row, nil); err != nil {
			return asset.Circuit{}, err
		}

		circuit.ID = existing.ID

		return circuit, nil
	}

	row.ID = circuit.ID.String()

	if err := c.base.Create(ctx, CircuitEntity, circuit.ID, row); err != nil {
		return asset.Circuit{}, err
	}

	return circuit, nil
}

// Get reads one circuit.
func (c *circuits) Get(ctx context.Context, want id.ID) (asset.Circuit, error) {
	var row circuitRow

	if err := c.base.Get(ctx, CircuitEntity, want, &row); err != nil {
		return asset.Circuit{}, err
	}

	return row.toDomain()
}

// ByExternal finds a circuit by the key its source used.
func (c *circuits) ByExternal(ctx context.Context, source, external string) (asset.Circuit, error) {
	var rows []circuitRow

	if err := c.base.Query(ctx, &rows,
		`SELECT * FROM `+CircuitTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND source = $1
		   AND external_id = $2
		 LIMIT 1`, source, external); err != nil {
		return asset.Circuit{}, err
	}

	if len(rows) == 0 {
		return asset.Circuit{}, fmt.Errorf(
			"%w: no circuit from %s with external identifier %s",
			bserr.ErrNotFound, source, external)
	}

	return rows[0].toDomain()
}

// List returns circuits.
func (c *circuits) List(ctx context.Context, limit int) ([]asset.Circuit, error) {
	if limit <= 0 {
		limit = 100
	}

	var rows []circuitRow

	if err := c.base.Query(ctx, &rows,
		`SELECT * FROM `+CircuitTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		 ORDER BY id DESC
		 LIMIT $1`, limit); err != nil {
		return nil, err
	}

	out := make([]asset.Circuit, 0, len(rows))

	for _, row := range rows {
		circuit, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, circuit)
	}

	return out, nil
}

// toDomain maps a stored circuit back.
func (c circuitRow) toDomain() (asset.Circuit, error) {
	circuitID, err := gid.ParseCircuit(c.ID)
	if err != nil {
		return asset.Circuit{}, fmt.Errorf("geo: stored circuit id %q: %w", c.ID, err)
	}

	return asset.Circuit{
		ID:         circuitID,
		ExternalID: c.ExternalID,
		Source:     c.Source,
		VoltageKV:  c.VoltageKV,
		Operator:   c.Operator,
	}, nil
}
