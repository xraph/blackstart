// Package scl imports IEC 61850 substation configuration files.
//
// One file yields two models: the electrical topology, written through the
// topology extension, and the communication topology, written here. They come
// from the same bytes and the same import, and the subscription entity is a
// reified graph edge — so both graphs are maintained by one write path with no
// second pipeline to keep in step.
//
// It is also the only source of genuine node-breaker data this project has.
// MATPOWER cases are bus-branch, so before SCL the reduction was fixture-only.
package scl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/extensions/scl/comms"
	"github.com/xraph/blackstart/extensions/scl/device"
	sclid "github.com/xraph/blackstart/extensions/scl/id"
	"github.com/xraph/blackstart/extensions/scl/mapping"
	"github.com/xraph/blackstart/extensions/scl/parse"
	"github.com/xraph/blackstart/extensions/scl/store"
	"github.com/xraph/blackstart/extensions/topology"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
)

// Engine holds the extension's business logic.
type Engine struct {
	store    store.Store
	topology *topology.Engine
	recorder provenance.Recorder
	bus      *hooks.Bus

	now func() time.Time
}

// NewEngine builds the engine.
//
// The topology engine may be nil, and that is deliberate: an ICD describes a
// device type and binds to no plant at all, so importing one needs no
// topology. A host that registers this extension without that one can still
// read device configurations.
func NewEngine(
	s store.Store, topo *topology.Engine, recorder provenance.Recorder, bus *hooks.Bus,
) (*Engine, error) {
	if s == nil {
		return nil, errors.New("scl: engine needs a store")
	}

	return &Engine{store: s, topology: topo, recorder: recorder, bus: bus, now: time.Now}, nil
}

// ImportRequest is one SCL file to read.
type ImportRequest struct {
	// Source is the file's contents.
	Source []byte

	// URL is where it came from, recorded in the provenance chain.
	URL string
}

// ImportResult is what an import produced.
type ImportResult struct {
	// Substations are the sites written, empty for an ICD.
	Substations []id.ID

	// Devices, Publications and Subscriptions count what was written.
	Devices       int
	Publications  int
	Subscriptions int

	// Unresolved counts subscriptions naming a device this file does not
	// configure. Not an error — an SCD may reference devices configured
	// elsewhere — but a number worth seeing, because a file where most
	// subscriptions are unresolved is usually a partial export rather than a
	// substation full of external references.
	Unresolved int

	// Skipped names equipment the topology mapping did not model, with the
	// reason. Reported so an operator comparing a device count against their
	// own tooling can see what was left out.
	Skipped []mapping.Skip

	// DatasetVersionID names the provenance record for the bytes.
	DatasetVersionID id.ID

	// Traceable says whether the chain back to the file was recorded.
	Traceable bool
}

// Import reads an SCL file and writes both models it describes.
//
// # Errors
//
// Returns an error when the file cannot be parsed, when the topology it
// describes is internally inconsistent, or when a write fails. A file
// referencing devices configured elsewhere is not an error: that is what an
// SCD of one substation in a larger system looks like.
func (e *Engine) Import(ctx context.Context, in ImportRequest) (ImportResult, error) {
	if len(in.Source) == 0 {
		return ImportResult{}, errors.New("scl: nothing to import")
	}

	parsed, err := parse.Decode(bytes.NewReader(in.Source))
	if err != nil {
		return ImportResult{}, err
	}

	out := ImportResult{}
	out.DatasetVersionID, out.Traceable = e.recordDataset(ctx, in)

	// The electrical model first, because a substation that cannot be mapped
	// is a file worth refusing before anything is written — and the
	// communication model is the part that survives an ICD with no plant.
	if err := e.importTopology(ctx, parsed, &out); err != nil {
		return ImportResult{}, err
	}

	if err := e.importComms(ctx, parsed, &out); err != nil {
		return ImportResult{}, err
	}

	e.emit(ctx, out)

	return out, nil
}

// importTopology writes the electrical model.
func (e *Engine) importTopology(
	ctx context.Context, parsed *parse.SCL, out *ImportResult,
) error {
	mapped, err := mapping.Substations(parsed)
	if err != nil {
		return err
	}

	if len(mapped) == 0 {
		return nil
	}

	if e.topology == nil {
		return fmt.Errorf(
			"scl: this file describes %d substation(s) but no topology engine is "+
				"configured; the electrical model would be silently discarded",
			len(mapped))
	}

	for _, result := range mapped {
		site, err := e.topology.Define(ctx, result.Definition)
		if err != nil {
			return fmt.Errorf("scl: writing substation %s: %w",
				result.Definition.Substation.Name, err)
		}

		out.Substations = append(out.Substations, site.ID)
		out.Skipped = append(out.Skipped, result.Skipped...)
	}

	return nil
}

// importComms writes the communication model.
//
// Devices before publications before subscriptions, because each names the one
// before it. A subscription written first would reference devices that do not
// exist yet, and the graph projection would carry an edge between two nodes
// that are not there.
func (e *Engine) importComms(
	ctx context.Context, parsed *parse.SCL, out *ImportResult,
) error {
	graph := comms.Build(parsed)

	byName := map[string]id.ID{}

	for _, ied := range parsed.IEDs {
		stored, err := e.store.Devices().PutIED(ctx, device.IED{
			ID:            sclid.NewIED(),
			Name:          ied.Name,
			Type:          ied.Type,
			Manufacturer:  ied.Manufacturer,
			ConfigVersion: ied.ConfigVersion,
			Description:   ied.Description,
		})
		if err != nil {
			return fmt.Errorf("scl: writing device %s: %w", ied.Name, err)
		}

		byName[ied.Name] = stored.ID
		out.Devices++
	}

	for _, publication := range graph.Publications {
		iedID, known := byName[publication.IEDName]
		if !known {
			continue
		}

		if err := e.store.Devices().PutPublication(ctx, device.Publication{
			ID:           sclid.NewPublication(),
			IEDID:        iedID,
			IEDName:      publication.IEDName,
			LDInst:       publication.LDInst,
			CBName:       publication.CBName,
			AppID:        publication.AppID,
			DataSet:      publication.DataSet,
			ConfRev:      publication.ConfRev,
			MAC:          publication.MAC,
			VLAN:         publication.VLAN,
			NetworkAppID: publication.NetworkAppID,
		}); err != nil {
			return fmt.Errorf("scl: writing publication %s/%s: %w",
				publication.IEDName, publication.CBName, err)
		}

		out.Publications++
	}

	for _, subscription := range graph.Subscriptions {
		subscriber, known := byName[subscription.SubscriberIED]
		if !known {
			continue
		}

		publisher, resolved := byName[subscription.PublisherIED]
		if !resolved {
			// The publisher is configured elsewhere. Counted and not written:
			// an edge needs both endpoints, and inventing a node for a device
			// this file does not describe would put a device in the graph that
			// nothing knows anything else about.
			out.Unresolved++

			continue
		}

		if err := e.store.Devices().PutSubscription(ctx, device.Subscription{
			ID:             sclid.NewSubscription(),
			SubscriberID:   subscriber,
			PublisherID:    publisher,
			Relation:       "SUBSCRIBES_" + subscription.Service,
			SubscriberPath: subscription.SubscriberPath,
			PublisherCB:    subscription.PublisherCB,
			Data:           subscription.Data,
			Resolved:       subscription.Resolved,
		}); err != nil {
			return fmt.Errorf("scl: writing subscription %s → %s: %w",
				subscription.SubscriberIED, subscription.PublisherIED, err)
		}

		out.Subscriptions++
	}

	return nil
}

// recordDataset writes the provenance for the bytes, reporting whether it
// managed to.
func (e *Engine) recordDataset(ctx context.Context, in ImportRequest) (id.ID, bool) {
	if e.recorder == nil {
		return id.Nil, false
	}

	stored, err := e.recorder.RecordDataset(ctx, record.DatasetVersion{
		URL:           in.URL,
		ContentHash:   hashOf(in.Source),
		SizeBytes:     int64(len(in.Source)),
		ParserVersion: parse.ParserVersion,
		RetrievedAt:   e.now().UTC(),
	})
	if err != nil {
		return id.Nil, false
	}

	return stored.ID, true
}

// Devices lists the devices this extension knows.
func (e *Engine) Devices(ctx context.Context, limit int) ([]device.IED, error) {
	return e.store.Devices().IEDs(ctx, limit)
}

// Device finds one device by the name every ExtRef resolves against.
func (e *Engine) Device(ctx context.Context, name string) (device.IED, error) {
	return e.store.Devices().IEDByName(ctx, name)
}

// Publications lists what a device publishes.
func (e *Engine) Publications(ctx context.Context, ied id.ID) ([]device.Publication, error) {
	return e.store.Devices().Publications(ctx, ied)
}

// Listens returns what a device subscribes to.
func (e *Engine) Listens(ctx context.Context, ied id.ID) ([]device.Subscription, error) {
	return e.store.Devices().SubscriptionsOf(ctx, ied)
}

// Listeners returns who subscribes to a device.
//
// The question the electrical model cannot answer: if this device is lost,
// whose logic stops working.
func (e *Engine) Listeners(ctx context.Context, ied id.ID) ([]device.Subscription, error) {
	return e.store.Devices().SubscribersTo(ctx, ied)
}

// emit publishes a domain event, best effort.
func (e *Engine) emit(ctx context.Context, out ImportResult) {
	if e.bus == nil {
		return
	}

	e.bus.Emit(ctx, hooks.Event{
		Name:      "scl.imported",
		Extension: "scl",
		Entity:    "SCLFile",
		Op:        hooks.OpCreate,
		At:        e.now().UTC(),
		Payload:   out,
	})
}

// hashOf is the content hash recorded in a provenance record.
func hashOf(body []byte) string {
	sum := sha256.Sum256(body)

	return hex.EncodeToString(sum[:])
}
