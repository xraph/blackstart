// Package fabriqstore is the base every extension's Fabriq-backed store
// embeds.
//
// It exists so that Fabriq's API surface is touched from one package rather
// than from a dozen. Fabriq is version 0.0.6 with an explicitly unfrozen API;
// routing every call through here means a breaking change upstream is a
// one-package fix rather than a sweep.
package fabriqstore

import (
	"errors"
	"fmt"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/pgdriver"
)

// PgOrErr returns the PostgreSQL driver behind a grove handle.
//
// This is the ONLY place in blackstart permitted to reach for the concrete
// driver, and it is the reason `pgdriver.Unwrap` is banned by a forbidigo rule
// in .golangci.yml.
//
// Grove's own README recommends Unwrap. That recommendation is wrong here:
//
//	func Unwrap(db *grove.DB) *PgDB {
//	    pgdb, ok := db.Driver().(*PgDB)
//	    if !ok {
//	        panic("pgdriver: driver is not a *PgDB")
//	    }
//	    return pgdb
//	}
//
// It panics. A store that calls it cannot be exercised against any other
// driver, cannot be handed a fake in a test, and turns a configuration mistake
// — a DSN pointing at the wrong engine — into a crash at first query rather
// than an error at startup.
//
// The checked form costs one `ok` and gives an error that names what was found
// instead.
func PgOrErr(gdb *grove.DB) (*pgdriver.PgDB, error) {
	if gdb == nil {
		return nil, errors.New("blackstart: no database handle")
	}

	drv := gdb.Driver()
	if drv == nil {
		return nil, errors.New("blackstart: database handle has no driver")
	}

	pg, ok := drv.(*pgdriver.PgDB)
	if !ok {
		return nil, fmt.Errorf(
			"blackstart: PostgreSQL is required but the database handle uses "+
				"the %q driver; blackstart depends on PostGIS for network "+
				"geometry and on btree_gist for the bitemporal exclusion "+
				"constraint, neither of which any other engine provides",
			drv.Name())
	}

	return pg, nil
}
