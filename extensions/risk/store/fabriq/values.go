package fabriq

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/xraph/fabriq/core/command"

	"github.com/xraph/blackstart/extensions/risk/store"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// valueStore writes and reads the probability series.
//
// Every write goes through the command plane — one Create per row, batched
// into one transaction — rather than around it. That is the opposite choice
// from hazard's and weather's value stores, and deliberately so: see the
// package doc for why the column shape here rules out their Timeseries-port
// shortcut, and why going through the normal path is what buys back RLS.
type valueStore struct {
	base *fabriqstore.Base
}

func (s *valueStore) PutBatch(ctx context.Context, in []store.ComponentValue) error {
	if len(in) == 0 {
		return nil
	}

	// Validated before anything is written. Base.Batch runs every command in
	// one transaction, so refusing the batch up front — rather than partway
	// through building it — is what keeps a bad value from being the
	// difference between all of it and none.
	cmds := make([]command.Command, 0, len(in))

	for index, value := range in {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("risk: value %d: %w", index, err)
		}

		cmds = append(cmds, command.Command{
			Entity:  ValueEntity,
			Op:      command.OpCreate,
			AggID:   newValueID().String(),
			Payload: valueToModel(value),
		})
	}

	return s.base.Batch(ctx, cmds)
}

func (s *valueStore) ForComponent(
	ctx context.Context, component id.ID, quantities []store.Quantity, issuedAt, from, to time.Time,
) ([]store.ComponentValue, error) {
	if len(quantities) == 0 {
		return []store.ComponentValue{}, nil
	}

	if !to.After(from) {
		return nil, fmt.Errorf(
			"risk: reading %s over [%s, %s): the window ends at or before it "+
				"begins, so it selects nothing", ValueSeries,
			from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	}

	args := make([]any, 0, len(quantities)+4)
	args = append(args, component.String(), issuedAt.UTC(), from.UTC(), to.UTC())

	placeholders := make([]byte, 0, len(quantities)*4)

	for index, quantity := range quantities {
		if index > 0 {
			placeholders = append(placeholders, ',')
		}

		args = append(args, string(quantity))
		placeholders = append(placeholders, '$')
		placeholders = append(placeholders, strconv.Itoa(len(args))...)
	}

	var rows []valueModel

	// The issuance is an equality, not a bound: this reads one series — the
	// pass identified by issuedAt — rather than filtering the newest pass
	// across every issuance, which is what ForComponent's contract requires
	// and what the memory store's map key already encodes structurally.
	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+ValueSeries+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND component_id = $1
		   AND issued_at = $2
		   AND valid_at >= $3 AND valid_at < $4
		   AND quantity IN (`+string(placeholders)+`)
		 ORDER BY valid_at ASC`, args...); err != nil {
		return nil, err
	}

	out := make([]store.ComponentValue, 0, len(rows))

	for _, row := range rows {
		got, err := valueFromModel(row)
		if err != nil {
			return nil, fmt.Errorf("risk: reading %s: %w", ValueSeries, err)
		}

		out = append(out, got)
	}

	return out, nil
}
