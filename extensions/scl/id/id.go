// Package id declares the SCL extension's identifier prefixes.
package id

import "github.com/xraph/blackstart/pkg/id"

// Owner is the extension these prefixes belong to.
const Owner = "scl"

// Identifier prefixes. Immutable once shipped.
const (
	// IED is an intelligent electronic device: a relay, a controller, a meter.
	IED id.Prefix = "ied"

	// Publication is one GOOSE control block with its network address.
	Publication id.Prefix = "gpub"

	// Subscription is one device listening to another's published data. A
	// reified edge — the row is the relationship.
	Subscription id.Prefix = "gsub"
)

//nolint:gochecknoinits // prefix registration is init-time by design
func init() {
	for _, p := range []struct {
		prefix id.Prefix
		name   string
	}{
		{IED, "IED"},
		{Publication, "Publication"},
		{Subscription, "Subscription"},
	} {
		id.MustRegister(p.prefix, Owner, p.name)
	}
}

// NewIED mints a device identifier.
func NewIED() id.ID { return id.MustNew(IED) }

// NewPublication mints a publication identifier.
func NewPublication() id.ID { return id.MustNew(Publication) }

// NewSubscription mints a subscription identifier.
func NewSubscription() id.ID { return id.MustNew(Subscription) }

// ParseIED reads a device identifier, rejecting any other type.
func ParseIED(s string) (id.ID, error) { return id.ParseWithPrefix(s, IED) }
