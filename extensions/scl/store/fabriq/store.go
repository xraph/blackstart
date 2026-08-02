package fabriq

import (
	"context"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/blackstart/extensions/scl/device"
	"github.com/xraph/blackstart/extensions/scl/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// Store is the Fabriq-backed composite.
type Store struct {
	base *fabriqstore.Base
}

// New builds the store over a Fabriq facade.
func New(fab *fabriq.Fabriq, tenant string) (*Store, error) {
	base, err := fabriqstore.New(fab, tenant)
	if err != nil {
		return nil, fmt.Errorf("scl: building the store: %w", err)
	}

	return &Store{base: base}, nil
}

// StoreName identifies the store in health output and migration logs.
func (s *Store) StoreName() string { return "scl/fabriq" }

// Migrate is a no-op here; schema changes run through the group in
// migrations.go.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping reports whether the source of truth is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.base.Ping(ctx) }

// Close releases nothing: the Fabriq facade is owned by the fabric extension.
func (s *Store) Close() error { return nil }

// Devices returns the device store.
func (s *Store) Devices() device.Store { return (*devices)(s) }

type devices Store

// PutIED writes a device, or returns the one already stored under its name.
//
// Idempotent on name rather than failing, because re-importing a corrected SCD
// is the normal way a configuration is updated: the device is the same device,
// and minting a second identifier for it would orphan every subscription
// pointing at the first.
func (d *devices) PutIED(ctx context.Context, in device.IED) (device.IED, error) {
	if err := in.Validate(); err != nil {
		return device.IED{}, err
	}

	existing, err := d.IEDByName(ctx, in.Name)
	if err == nil {
		in.ID = existing.ID

		if err := d.base.Update(ctx, IEDEntity, existing.ID, toIEDRow(in), nil); err != nil {
			return device.IED{}, err
		}

		return in, nil
	}

	if err := d.base.Create(ctx, IEDEntity, in.ID, toIEDRow(in)); err != nil {
		return device.IED{}, err
	}

	return in, nil
}

// Get reads one device by identifier.
func (d *devices) Get(ctx context.Context, want id.ID) (device.IED, error) {
	var row iedRow

	if err := d.base.Get(ctx, IEDEntity, want, &row); err != nil {
		return device.IED{}, err
	}

	return row.toDomain()
}

// IEDByName finds a device by the name every ExtRef resolves against.
func (d *devices) IEDByName(ctx context.Context, name string) (device.IED, error) {
	var rows []iedRow

	if err := d.base.Query(ctx, &rows,
		`SELECT * FROM `+IEDTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND name = $1
		 LIMIT 1`, name); err != nil {
		return device.IED{}, err
	}

	if len(rows) == 0 {
		return device.IED{}, fmt.Errorf("%w: no device named %q", bserr.ErrNotFound, name)
	}

	return rows[0].toDomain()
}

// IEDs lists devices.
func (d *devices) IEDs(ctx context.Context, limit int) ([]device.IED, error) {
	if limit <= 0 {
		limit = 100
	}

	var rows []iedRow

	if err := d.base.Query(ctx, &rows,
		`SELECT * FROM `+IEDTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		 ORDER BY name ASC
		 LIMIT $1`, limit); err != nil {
		return nil, err
	}

	out := make([]device.IED, 0, len(rows))

	for _, row := range rows {
		mapped, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, mapped)
	}

	return out, nil
}

// PutPublication writes a control block and its address.
func (d *devices) PutPublication(ctx context.Context, in device.Publication) error {
	return d.base.Create(ctx, PublicationEntity, in.ID, publicationRow{
		ID:           in.ID.String(),
		IEDID:        in.IEDID.String(),
		IEDName:      in.IEDName,
		LDInst:       in.LDInst,
		CBName:       in.CBName,
		AppID:        in.AppID,
		DataSet:      in.DataSet,
		ConfRev:      in.ConfRev,
		MAC:          in.MAC,
		VLAN:         in.VLAN,
		NetworkAppID: in.NetworkAppID,
	})
}

// Publications lists what a device publishes.
func (d *devices) Publications(ctx context.Context, ied id.ID) ([]device.Publication, error) {
	var rows []publicationRow

	if err := d.base.Query(ctx, &rows,
		`SELECT * FROM `+PublicationTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND ied_id = $1
		 ORDER BY cb_name ASC`, ied.String()); err != nil {
		return nil, err
	}

	out := make([]device.Publication, 0, len(rows))

	for _, row := range rows {
		mapped, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, mapped)
	}

	return out, nil
}

// PutSubscription writes one edge.
func (d *devices) PutSubscription(ctx context.Context, in device.Subscription) error {
	return d.base.Create(ctx, SubscriptionEntity, in.ID, subscriptionRow{
		ID:             in.ID.String(),
		SubscriberID:   in.SubscriberID.String(),
		PublisherID:    in.PublisherID.String(),
		Relation:       in.Relation,
		SubscriberPath: in.SubscriberPath,
		PublisherCB:    in.PublisherCB,
		Data:           in.Data,
		Resolved:       in.Resolved,
	})
}

// SubscriptionsOf is what a device listens to.
func (d *devices) SubscriptionsOf(
	ctx context.Context, subscriber id.ID,
) ([]device.Subscription, error) {
	return d.edges(ctx, "subscriber_id", subscriber)
}

// SubscribersTo is who listens to a device.
func (d *devices) SubscribersTo(
	ctx context.Context, publisher id.ID,
) ([]device.Subscription, error) {
	return d.edges(ctx, "publisher_id", publisher)
}

// edges reads subscriptions from one end.
//
// The column name is a constant at every call site, never caller input — the
// two exported methods above are the only callers and both pass a literal.
func (d *devices) edges(
	ctx context.Context, column string, want id.ID,
) ([]device.Subscription, error) {
	var rows []subscriptionRow

	if err := d.base.Query(ctx, &rows,
		`SELECT * FROM `+SubscriptionTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND `+column+` = $1
		 ORDER BY id ASC`, want.String()); err != nil {
		return nil, err
	}

	out := make([]device.Subscription, 0, len(rows))

	for _, row := range rows {
		mapped, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, mapped)
	}

	return out, nil
}

// Compile-time check that this satisfies the composite.
var _ store.Store = (*Store)(nil)
