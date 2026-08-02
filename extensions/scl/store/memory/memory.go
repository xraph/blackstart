// Package memory is the in-memory implementation of the SCL store.
//
// Not a throwaway mock. The engine is tested against this rather than a
// hand-written double per test, so it has to behave like the real store in the
// ways the engine depends on: idempotent device writes keyed on name, both
// subscription directions, and the same errors.
package memory

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/xraph/blackstart/extensions/scl/device"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Store is the in-memory composite.
type Store struct {
	mu sync.RWMutex

	ieds          map[id.ID]device.IED
	byName        map[string]id.ID
	publications  map[id.ID][]device.Publication
	subscriptions []device.Subscription
}

// New builds an empty store.
func New() *Store {
	return &Store{
		ieds:         map[id.ID]device.IED{},
		byName:       map[string]id.ID{},
		publications: map[id.ID][]device.Publication{},
	}
}

// Migrate is a no-op: there is no schema to create.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping always succeeds.
func (s *Store) Ping(context.Context) error { return nil }

// Close releases nothing.
func (s *Store) Close() error { return nil }

// Devices returns the device store.
func (s *Store) Devices() device.Store { return (*devices)(s) }

type devices Store

// PutIED writes a device, reusing the identifier already stored under its name.
//
// Idempotent on name like the durable store, and for the same reason:
// re-importing a corrected SCD is how a configuration is updated, and minting
// a second identifier would orphan every subscription pointing at the first.
// A counter-based fake would let a test pass that the real store fails.
func (d *devices) PutIED(_ context.Context, in device.IED) (device.IED, error) {
	if err := in.Validate(); err != nil {
		return device.IED{}, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if existing, ok := d.byName[in.Name]; ok {
		in.ID = existing
	}

	d.ieds[in.ID] = in
	d.byName[in.Name] = in.ID

	return in, nil
}

func (d *devices) IEDByName(_ context.Context, name string) (device.IED, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	found, ok := d.byName[name]
	if !ok {
		return device.IED{}, fmt.Errorf("%w: no device named %q", bserr.ErrNotFound, name)
	}

	return d.ieds[found], nil
}

func (d *devices) IEDs(_ context.Context, limit int) ([]device.IED, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	out := slices.Collect(maps.Values(d.ieds))

	// By name, matching the durable store. A listing whose order moved between
	// calls would make every golden built from one unstable.
	slices.SortFunc(out, func(left, right device.IED) int {
		return cmp.Compare(left.Name, right.Name)
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

func (d *devices) PutPublication(_ context.Context, in device.Publication) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.ieds[in.IEDID]; !ok {
		return fmt.Errorf(
			"%w: publication names device %s, which does not exist",
			bserr.ErrInvalid, in.IEDID)
	}

	d.publications[in.IEDID] = append(d.publications[in.IEDID], in)

	return nil
}

func (d *devices) Publications(_ context.Context, ied id.ID) ([]device.Publication, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	out := slices.Clone(d.publications[ied])

	slices.SortFunc(out, func(left, right device.Publication) int {
		return cmp.Compare(left.CBName, right.CBName)
	})

	return out, nil
}

func (d *devices) PutSubscription(_ context.Context, in device.Subscription) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Both ends must exist. An edge to a device that was never written is
	// exactly what the graph projection cannot represent, and letting one
	// through here would make the memory store accept what the real one
	// rejects.
	for _, end := range []id.ID{in.SubscriberID, in.PublisherID} {
		if _, ok := d.ieds[end]; !ok {
			return fmt.Errorf(
				"%w: subscription names device %s, which does not exist",
				bserr.ErrInvalid, end)
		}
	}

	d.subscriptions = append(d.subscriptions, in)

	return nil
}

func (d *devices) SubscriptionsOf(
	_ context.Context, subscriber id.ID,
) ([]device.Subscription, error) {
	return d.edges(func(s device.Subscription) bool { return s.SubscriberID == subscriber }), nil
}

func (d *devices) SubscribersTo(
	_ context.Context, publisher id.ID,
) ([]device.Subscription, error) {
	return d.edges(func(s device.Subscription) bool { return s.PublisherID == publisher }), nil
}

// edges collects subscriptions matching a predicate, in a stable order.
func (d *devices) edges(match func(device.Subscription) bool) []device.Subscription {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var out []device.Subscription

	for _, subscription := range d.subscriptions {
		if match(subscription) {
			out = append(out, subscription)
		}
	}

	slices.SortFunc(out, func(left, right device.Subscription) int {
		return cmp.Compare(left.ID.String(), right.ID.String())
	})

	return out
}
