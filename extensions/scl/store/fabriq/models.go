// Package fabriq is the Fabriq-backed implementation of the SCL store.
//
// The subscription is a reified graph edge: the row *is* the relationship, and
// Fabriq projects it as one. That is what makes two overlaid graphs fall out
// of a single write path — the electrical topology from one set of entities
// and the communication topology from this one, both maintained by the same
// outbox with no second pipeline to keep in step.
package fabriq

import (
	"github.com/xraph/grove"
)

// Table names, prefixed with the extension so a shared schema stays legible.
const (
	IEDTable          = "blackstart_scl_ied"
	PublicationTable  = "blackstart_scl_publication"
	SubscriptionTable = "blackstart_scl_subscription"
)

// Entity names as registered with Fabriq.
const (
	IEDEntity          = "scl_ied"
	PublicationEntity  = "scl_publication"
	SubscriptionEntity = "scl_subscription"
)

// iedRow is one intelligent electronic device.
type iedRow struct {
	grove.BaseModel `grove:"table:blackstart_scl_ied"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	Name          string `grove:"name,notnull"`
	Type          string `grove:"type"`
	Manufacturer  string `grove:"manufacturer"`
	ConfigVersion string `grove:"config_version"`
	Description   string `grove:"description"`
}

// publicationRow is one GOOSE control block with its address.
type publicationRow struct {
	grove.BaseModel `grove:"table:blackstart_scl_publication"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	IEDID   string `grove:"ied_id,notnull"`
	IEDName string `grove:"ied_name,notnull"`
	LDInst  string `grove:"ld_inst,notnull"`
	CBName  string `grove:"cb_name,notnull"`
	AppID   string `grove:"app_id"`
	DataSet string `grove:"data_set"`
	ConfRev int    `grove:"conf_rev"`

	MAC          string `grove:"mac"`
	VLAN         string `grove:"vlan"`
	NetworkAppID string `grove:"network_app_id"`
}

// subscriptionRow is one device listening to another's published data.
//
// A reified edge. SubscriberID and PublisherID are the endpoints Fabriq
// matches in the graph, and Relation carries the edge type — one column,
// because SCL has more than one service and a Report subscription is a
// different relationship from a GOOSE one even between the same two devices.
type subscriptionRow struct {
	grove.BaseModel `grove:"table:blackstart_scl_subscription"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	SubscriberID string `grove:"subscriber_id,notnull"`
	PublisherID  string `grove:"publisher_id,notnull"`

	// Relation is the graph edge type, derived from the service.
	Relation string `grove:"relation,notnull"`

	SubscriberPath string `grove:"subscriber_path"`
	PublisherCB    string `grove:"publisher_cb"`
	Data           string `grove:"data"`

	// Resolved says whether both ends were found in the file. Carried as an
	// edge property so a graph query can ask for confirmed subscriptions only
	// — an unresolved one is the subscriber's assertion, not a fact.
	Resolved bool `grove:"resolved,notnull"`
}
