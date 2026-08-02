package fabric

import (
	"fmt"
	"strings"

	"github.com/xraph/blackstart/internal/entities"
)

// assertGraphConfigured refuses to start when entities declare a graph node
// but no graph engine is configured.
//
// This is the check Fabriq does not make. A GraphNode with no FalkorDB address
// is skipped silently — no error, no log line — so the system boots, accepts
// writes, and maintains a graph view that is simply empty. Given that "one
// write, many views" is the architectural claim this project exists to
// demonstrate, booting into a state where it is quietly false is worse than
// refusing to boot.
func (e *Extension) assertGraphConfigured() error {
	if !e.config.RequireGraph {
		return nil
	}

	declared := entities.GraphEntities(e.reg)
	if len(declared) == 0 {
		return nil
	}

	if e.config.Fabriq.FalkorDB.Addr != "" {
		return nil
	}

	return fmt.Errorf(
		"fabric: %d entities declare a graph projection (%s) but no graph "+
			"engine is configured; fabriq would skip them silently, leaving the "+
			"graph view empty with no error. Set falkordb.addr, or disable the "+
			"check with WithRequireGraph(false) if the graph is genuinely not "+
			"wanted in this deployment",
		len(declared), strings.Join(declared, ", "))
}
