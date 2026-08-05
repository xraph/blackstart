package bsctl

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/extensions/networkmodel/viewer"
	"github.com/xraph/blackstart/pkg/id"
)

// readPlacements loads bus coordinates from a CSV file.
//
// A separate file rather than something carried in the case, because MATPOWER
// has nowhere to put coordinates: the format has no field for them, and the
// ACTIVSg cases distribute them alongside the case rather than inside it.
// Keeping them separate also keeps them out of the content hash, which is
// deliberate — a substation moving fifty meters must not invalidate every
// result computed for the network.
//
// Columns are bus, latitude, longitude, and an optional elevation. Latitude
// first, matching every mapping tool and the coordinates the ACTIVSg
// distribution ships; the flip to (lon, lat) happens here.
//
// # Errors
//
// Returns an error naming the row that could not be read. A coordinate file
// silently half-loaded produces a network with some buses off the coast of
// Africa, which looks like a rendering bug.
func readPlacements(path string) (viewer.Placement, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading coordinates: %w", err)
	}
	defer func() { _ = file.Close() }()

	reader := csv.NewReader(file)
	// Variable, because the elevation column is optional and encoding/csv
	// otherwise refuses the whole file when one row omits it.
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	out := viewer.Placement{}

	for row := 1; ; row++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("coordinates row %d: %w", row, err)
		}

		if len(record) == 0 || strings.HasPrefix(record[0], "#") {
			continue
		}

		// A header line, skipped by looking rather than by counting: files
		// come with and without one, and skipping the first row
		// unconditionally would drop a bus from every file that has none.
		if row == 1 && !isNumeric(record[len(record)-1]) {
			continue
		}

		if len(record) < 3 {
			return nil, fmt.Errorf(
				"coordinates row %d has %d columns; bus,lat,lon[,elevation] are "+
					"needed", row, len(record))
		}

		lat, err := strconv.ParseFloat(strings.TrimSpace(record[1]), 64)
		if err != nil {
			return nil, fmt.Errorf("coordinates row %d: latitude %q is not a number",
				row, record[1])
		}

		lon, err := strconv.ParseFloat(strings.TrimSpace(record[2]), 64)
		if err != nil {
			return nil, fmt.Errorf("coordinates row %d: longitude %q is not a number",
				row, record[2])
		}

		var elevation float64

		if len(record) > 3 && strings.TrimSpace(record[3]) != "" {
			if elevation, err = strconv.ParseFloat(strings.TrimSpace(record[3]), 64); err != nil {
				return nil, fmt.Errorf(
					"coordinates row %d: elevation %q is not a number", row, record[3])
			}
		}

		if lat < -90 || lat > 90 {
			return nil, fmt.Errorf(
				"coordinates row %d: latitude %v is outside ±90; the columns are "+
					"bus,lat,lon and a longitude in the latitude column is the "+
					"usual cause", row, lat)
		}

		out[strings.TrimSpace(record[0])] = viewer.Coordinate{
			Lon: lon, Lat: lat, Elevation: elevation,
		}
	}

	return out, nil
}

// isNumeric reports whether a field parses as a number, which is how a header
// row is told from a data row.
func isNumeric(field string) bool {
	_, err := strconv.ParseFloat(strings.TrimSpace(field), 64)

	return err == nil
}

// placementsOf turns a parsed coordinate file into storable placements.
//
// Sorted by bus so two runs over one file write the same thing and a diff of
// the result means something — Go map iteration is deliberately unordered.
func placementsOf(in viewer.Placement, modelID id.ID) []model.Placement {
	buses := slices.Sorted(maps.Keys(in))
	out := make([]model.Placement, 0, len(buses))

	for _, bus := range buses {
		where := in[bus]

		out = append(out, model.Placement{
			ID:        nmid.NewPlacement(),
			ModelID:   modelID,
			Bus:       bus,
			Lon:       where.Lon,
			Lat:       where.Lat,
			Elevation: where.Elevation,
		})
	}

	return out
}

// placementCounts reports how a CSV's placements relate to a model's own bus
// identifiers: unplaced is how many of the model's buses no placement named
// at all, and unknown is how many placements name a bus the model does not
// have.
//
// Placement keys must be canonical bus-<n>, but the ACTIVSg distribution
// ships raw bus numbers, so a CSV in the wrong convention places zero of the
// model's buses and every row is unknown — exactly the state `modelPlaceCommand`
// must not report as plain success. Kept separate from that command so this
// arithmetic is testable without the engine, store and CLI plumbing needed
// to reach a model's stored encoding.
func placementCounts(placements []model.Placement, modelBuses map[string]bool) (unplaced, unknown int) {
	placedBuses := make(map[string]bool, len(placements))

	for _, p := range placements {
		placedBuses[p.Bus] = true

		if !modelBuses[p.Bus] {
			unknown++
		}
	}

	for bus := range modelBuses {
		if !placedBuses[bus] {
			unplaced++
		}
	}

	return unplaced, unknown
}
