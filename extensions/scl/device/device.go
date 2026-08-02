// Package device is the persisted form of what an SCL file says about
// communication.
package device

import (
	"context"
	"fmt"
	"strings"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// IED is one intelligent electronic device.
type IED struct {
	ID            id.ID  `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	Manufacturer  string `json:"manufacturer"`
	ConfigVersion string `json:"config_version"`
	Description   string `json:"description"`
}

// Validate reports whether a device can be stored.
func (d IED) Validate() error {
	if d.ID.IsZero() {
		return fmt.Errorf("%w: a device needs an identifier", bserr.ErrInvalid)
	}

	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf(
			"%w: a device needs a name; every ExtRef in the file resolves by it",
			bserr.ErrInvalid)
	}

	return nil
}

// Publication is one GOOSE control block with its network address.
type Publication struct {
	ID      id.ID  `json:"id"`
	IEDID   id.ID  `json:"ied_id"`
	IEDName string `json:"ied_name"`
	LDInst  string `json:"ld_inst"`
	CBName  string `json:"cb_name"`
	AppID   string `json:"app_id"`
	DataSet string `json:"data_set"`
	ConfRev int    `json:"conf_rev"`

	MAC          string `json:"mac"`
	VLAN         string `json:"vlan"`
	NetworkAppID string `json:"network_app_id"`
}

// Subscription is one device listening to another's published data.
type Subscription struct {
	ID           id.ID  `json:"id"`
	SubscriberID id.ID  `json:"subscriber_id"`
	PublisherID  id.ID  `json:"publisher_id"`
	Relation     string `json:"relation"`

	SubscriberPath string `json:"subscriber_path"`
	PublisherCB    string `json:"publisher_cb"`
	Data           string `json:"data"`
	Resolved       bool   `json:"resolved"`
}

// Store reads and writes the communication model.
type Store interface {
	PutIED(ctx context.Context, in IED) (IED, error)
	Get(ctx context.Context, want id.ID) (IED, error)
	IEDByName(ctx context.Context, name string) (IED, error)
	IEDs(ctx context.Context, limit int) ([]IED, error)

	PutPublication(ctx context.Context, in Publication) error
	Publications(ctx context.Context, ied id.ID) ([]Publication, error)

	PutSubscription(ctx context.Context, in Subscription) error

	// SubscriptionsOf is what a device listens to. Asked when configuring it.
	SubscriptionsOf(ctx context.Context, subscriber id.ID) ([]Subscription, error)

	// SubscribersTo is who listens to a device. Asked when deciding whether
	// losing it matters — which is the question the electrical model cannot
	// answer at all.
	SubscribersTo(ctx context.Context, publisher id.ID) ([]Subscription, error)
}
