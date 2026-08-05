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
// Every write goes through the command plane — one Create per row, chunked
// into transactions of putBatchChunk each — rather than around it. That is
// the opposite choice from hazard's and weather's value stores, and
// deliberately so: see the package doc for the tradeoff and for why the
// column shape here rules out their Timeseries-port shortcut.
type valueStore struct {
	base *fabriqstore.Base
}

// putBatchChunk bounds how many rows one transaction covers.
//
// Each row costs a SELECT ... FOR UPDATE, a single-row INSERT, an outbox
// INSERT and a pg_notify (see the package doc), all inside one transaction.
// An unchunked write of a realistic pass — components times forecast hours,
// plausibly into six figures — would hold that many statements and that many
// notifications open at once, and every row's SELECT ... FOR UPDATE lock
// would be held until the whole thing committed. 500 keeps a single
// transaction's lock footprint and notify burst small and short-lived while
// still turning a six-figure pass into a few hundred round trips rather than
// one per row.
const putBatchChunk = 500

func (s *valueStore) PutBatch(ctx context.Context, in []store.ComponentValue) error {
	if len(in) == 0 {
		return nil
	}

	// Validated before anything is written — every row, not just the first
	// chunk's — so a bad value near the end of a large batch is refused
	// before any chunk is committed rather than after the chunks ahead of it
	// already went through.
	for index, value := range in {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("risk: value %d: %w", index, err)
		}
	}

	// Each chunk is atomic (one transaction via Base.Batch); the call as a
	// whole is not. A failure partway through leaves every earlier chunk
	// durably committed, the failing chunk rolled back, and every later chunk
	// never attempted — the returned error names the row range that did not
	// land. Resubmitting the full slice is not a safe retry: PutBatch mints a
	// fresh row id per call, so replaying already-committed chunks appends
	// duplicate rows rather than upserting over them. A caller that retries
	// after a partial failure must resubmit only the unwritten suffix.
	for start := 0; start < len(in); start += putBatchChunk {
		end := min(start+putBatchChunk, len(in))

		cmds := make([]command.Command, 0, end-start)

		for _, value := range in[start:end] {
			cmds = append(cmds, command.Command{
				Entity:  ValueEntity,
				Op:      command.OpCreate,
				AggID:   newValueID().String(),
				Payload: valueToModel(value),
			})
		}

		if err := s.base.Batch(ctx, cmds); err != nil {
			return fmt.Errorf("risk: writing values %d-%d of %d: %w", start, end-1, len(in), err)
		}
	}

	return nil
}

func (s *valueStore) ForComponent(
	ctx context.Context, component id.ID, quantities []store.Quantity, issuedAt, from, to time.Time,
) ([]store.ComponentValue, error) {
	// A degenerate window (to at or before from) is treated as legitimately
	// empty, not invalid — matching extensions/risk/store/memory/memory.go's
	// ForComponent, the brief's named behavioral reference, which never
	// rejects the range and simply finds nothing in it (inWindow can never be
	// true when to <= from). Short-circuiting here reaches the same empty
	// result without a query the WHERE clause below would answer with zero
	// rows anyway.
	if len(quantities) == 0 || !to.After(from) {
		return []store.ComponentValue{}, nil
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
	//
	// Columns are named rather than SELECT * — matching every other read in
	// this codebase — so a later migration adding a column to this table
	// cannot silently change what a row scans into here.
	if err := s.base.Query(ctx, &rows,
		`SELECT component_id, quantity, issued_at, valid_at, probability, scalar
		 FROM `+ValueSeries+`
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
