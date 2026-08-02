package fabriq

import (
	"context"
	"fmt"

	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/grove/migrate"
)

// GroupName identifies this extension's migration group.
const GroupName = "scl"

// Group returns the migration group, ordered after Fabriq's own.
func Group() *migrate.Group {
	g := migrate.NewGroup(GroupName, migrate.DependsOn(fabriqmigrations.GroupName))

	g.MustRegister(migration0001Tables)

	return g
}

// Version strings are capped at 14 characters: grove stores them in a
// VARCHAR(14), and a longer one fails at insert rather than at registration.
var migration0001Tables = &migrate.Migration{
	Name:    "scl_tables",
	Version: "20260802000040",
	Comment: "IED, GOOSE publication and subscription tables with tenancy and RLS",
	Up:      up0001,
	Down:    down0001,
}

func up0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`CREATE TABLE IF NOT EXISTS ` + IEDTable + ` (
			id             TEXT PRIMARY KEY,
			tenant_id      TEXT NOT NULL,
			version        BIGINT NOT NULL,
			name           TEXT NOT NULL,
			type           TEXT NOT NULL DEFAULT '',
			manufacturer   TEXT NOT NULL DEFAULT '',
			config_version TEXT NOT NULL DEFAULT '',
			description    TEXT NOT NULL DEFAULT ''
		)`,

		`CREATE INDEX IF NOT EXISTS ` + IEDTable + `_tenant_idx
			ON ` + IEDTable + ` (tenant_id)`,

		// An IED name is unique within a configuration by IEC 61850-6, and
		// every ExtRef resolves by it. Two devices under one name would make
		// every subscription between them ambiguous.
		`CREATE UNIQUE INDEX IF NOT EXISTS ` + IEDTable + `_name_idx
			ON ` + IEDTable + ` (tenant_id, name)`,

		`CREATE TABLE IF NOT EXISTS ` + PublicationTable + ` (
			id             TEXT PRIMARY KEY,
			tenant_id      TEXT NOT NULL,
			version        BIGINT NOT NULL,
			ied_id         TEXT NOT NULL,
			ied_name       TEXT NOT NULL,
			ld_inst        TEXT NOT NULL,
			cb_name        TEXT NOT NULL,
			app_id         TEXT NOT NULL DEFAULT '',
			data_set       TEXT NOT NULL DEFAULT '',
			conf_rev       INTEGER NOT NULL DEFAULT 0,
			mac            TEXT NOT NULL DEFAULT '',
			vlan           TEXT NOT NULL DEFAULT '',
			network_app_id TEXT NOT NULL DEFAULT ''
		)`,

		`CREATE INDEX IF NOT EXISTS ` + PublicationTable + `_tenant_idx
			ON ` + PublicationTable + ` (tenant_id)`,

		// The key SCL itself joins on, and the key a subscription resolves
		// against.
		`CREATE UNIQUE INDEX IF NOT EXISTS ` + PublicationTable + `_cb_idx
			ON ` + PublicationTable + ` (tenant_id, ied_name, ld_inst, cb_name)`,

		// Two control blocks sharing a MAC and APPID are two streams a
		// subscriber cannot tell apart on the wire. Not unique, because it is
		// a real misconfiguration that must be importable in order to be
		// reported — indexed so it can be found.
		`CREATE INDEX IF NOT EXISTS ` + PublicationTable + `_wire_idx
			ON ` + PublicationTable + ` (tenant_id, mac, network_app_id)`,

		`CREATE TABLE IF NOT EXISTS ` + SubscriptionTable + ` (
			id              TEXT PRIMARY KEY,
			tenant_id       TEXT NOT NULL,
			version         BIGINT NOT NULL,
			subscriber_id   TEXT NOT NULL,
			publisher_id    TEXT NOT NULL,
			relation        TEXT NOT NULL,
			subscriber_path TEXT NOT NULL DEFAULT '',
			publisher_cb    TEXT NOT NULL DEFAULT '',
			data            TEXT NOT NULL DEFAULT '',
			resolved        BOOLEAN NOT NULL DEFAULT FALSE
		)`,

		`CREATE INDEX IF NOT EXISTS ` + SubscriptionTable + `_tenant_idx
			ON ` + SubscriptionTable + ` (tenant_id)`,

		// Both directions are indexed because both questions get asked: "what
		// does this device listen to" when configuring it, and "who listens to
		// this device" when deciding whether losing it matters.
		`CREATE INDEX IF NOT EXISTS ` + SubscriptionTable + `_subscriber_idx
			ON ` + SubscriptionTable + ` (tenant_id, subscriber_id)`,

		`CREATE INDEX IF NOT EXISTS ` + SubscriptionTable + `_publisher_idx
			ON ` + SubscriptionTable + ` (tenant_id, publisher_id)`,

		// Row-level security, forced. Without FORCE the table owner is exempt
		// and tenant isolation is silently a no-op for exactly the connection
		// most likely to be used by mistake.
		`ALTER TABLE ` + IEDTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + IEDTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + IEDTable,
		`CREATE POLICY tenant_isolation ON ` + IEDTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))`,

		`ALTER TABLE ` + PublicationTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + PublicationTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + PublicationTable,
		`CREATE POLICY tenant_isolation ON ` + PublicationTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))`,

		`ALTER TABLE ` + SubscriptionTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + SubscriptionTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + SubscriptionTable,
		`CREATE POLICY tenant_isolation ON ` + SubscriptionTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))`,
	})
}

func down0001(ctx context.Context, exec migrate.Executor) error {
	// Dropped in reverse dependency order: subscriptions name devices, and
	// publications name devices, so both go before the devices themselves.
	return execAll(ctx, exec, []string{
		`DROP TABLE IF EXISTS ` + SubscriptionTable,
		`DROP TABLE IF EXISTS ` + PublicationTable,
		`DROP TABLE IF EXISTS ` + IEDTable,
	})
}

// execAll runs statements in order, naming the one that failed.
func execAll(ctx context.Context, exec migrate.Executor, statements []string) error {
	for index, statement := range statements {
		if _, err := exec.Exec(ctx, statement); err != nil {
			return fmt.Errorf("scl: statement %d: %w", index+1, err)
		}
	}

	return nil
}
