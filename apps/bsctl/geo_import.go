package bsctl

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/xraph/forge/cli"

	"github.com/xraph/blackstart/extensions/geo/source"
)

// Sources by name, so the command line names a source rather than a URL.
//
// The default ArcGIS layer is EIA's republication of the HIFLD transmission
// data. HIFLD Open itself was discontinued in August 2025 — see the phase-3
// data survey, §1.4 — and this layer is the mirror that stayed up.
const (
	sourceOSM    = "osm"
	sourceArcGIS = "arcgis"

	defaultArcGIS = "https://services7.arcgis.com/FGr1D95XCGALKXqM/arcgis/rest/" +
		"services/Transmission_Lines/FeatureServer/0"
)

// geoImportCommand imports real geometry from a public source.
func geoImportCommand() cli.Command {
	return cli.NewCommand(
		"import",
		"Import transmission geometry from a public source",
		func(ctx cli.CommandContext) error {
			box, err := parseBoundingBox(ctx.String("bbox"))
			if err != nil {
				return err
			}

			fetcher, err := sourceFor(ctx.String("source"), ctx.String("url"))
			if err != nil {
				return err
			}

			limit := ctx.Int("limit")

			// Fetched before the application starts. A public endpoint that is
			// slow, rate-limited or down is the likeliest failure here, and
			// finding out after opening a database connection means holding one
			// open for the length of somebody else's timeout.
			lines, err := fetcher.Fetch(ctx.Context(), box, limit)
			if err != nil {
				return err
			}

			if len(lines) == 0 {
				return fmt.Errorf(
					"no lines in %s; the box may be outside the layer's coverage "+
						"rather than empty of transmission", ctx.String("bbox"))
			}

			ctx.Printf("fetched %d lines\n", len(lines))

			engine, cleanup, err := geoEngine(ctx.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			var (
				imported  int
				surveyed  int
				skipped   int
				lastError error
			)

			for _, line := range lines {
				circuit, route, err := engine.ImportLine(ctx.Context(), line)
				if err != nil {
					// One malformed way does not abandon the other thousand.
					// Counted and reported rather than swallowed: an import
					// that quietly stored 900 of 1000 lines looks exactly like
					// a region with 900 lines in it.
					skipped++
					lastError = err

					continue
				}

				imported++

				if route.Provenance.StructuresAreReal() {
					surveyed++
				}

				if ctx.Bool("verbose") {
					ctx.Printf("  %s  %-24s  %s  %.1f km\n",
						circuit.ID, line.ExternalID, route.Provenance, route.LengthM/1000)
				}
			}

			ctx.Printf("imported %d circuits, %d with real structures\n", imported, surveyed)

			if skipped > 0 {
				ctx.Warning(fmt.Sprintf(
					"%d lines were skipped; the last said: %v", skipped, lastError))
			}

			// Said at every import, because the distinction decides what a
			// per-span result may claim. A routed path is a real right-of-way
			// with imaginary structures on it, and a per-span failure
			// probability conditioned on one is a weaker claim than it looks.
			if imported > surveyed {
				ctx.Warning(fmt.Sprintf(
					"%d of these have interpolated structures: the right-of-way is "+
						"real, the towers along it are not", imported-surveyed))
			}

			// The carrier matters as much as the geometry. These circuits are
			// not in any solvable network, and nothing downstream should
			// discover that by finding an impedance missing.
			ctx.Warning(
				"imported circuits carry no electrical parameters; they cannot " +
					"appear in a power flow or a contingency")

			return nil
		},
		cli.WithUsage(
			"bsctl geo import --bbox minLat,minLon,maxLat,maxLon "+
				"[--source osm|arcgis] [--limit N]"),
		cli.WithFlag(cli.NewStringFlag(
			"bbox", "", "area to import: minLat,minLon,maxLat,maxLon", "")),
		cli.WithFlag(cli.NewStringFlag(
			"source", "", "osm for surveyed structures, arcgis for routed paths", sourceOSM)),
		cli.WithFlag(cli.NewStringFlag(
			"url", "", "ArcGIS feature layer, without /query", defaultArcGIS)),
		cli.WithFlag(cli.NewIntFlag("limit", "", "stop after this many lines", 100)),
		cli.WithFlag(cli.NewBoolFlag("verbose", "", "print every circuit", false)),
	)
}

// sourceFor picks a fetcher by name.
func sourceFor(name, url string) (source.Fetcher, error) {
	switch name {
	case sourceOSM:
		return source.Overpass{}, nil

	case sourceArcGIS:
		if url == "" {
			return nil, errors.New("--url is required for the arcgis source")
		}

		return source.ArcGISLayer{URL: url}, nil

	default:
		return nil, fmt.Errorf(
			"unknown source %q: osm publishes structures, arcgis publishes paths", name)
	}
}

// parseBoundingBox reads "minLat,minLon,maxLat,maxLon".
//
// Latitude first, matching `bsctl geo trace` and every mapping site, and the
// opposite of the (lon, lat) order the geometry is stored in. The flip happens
// here, at the boundary, which is the only place it can be got wrong exactly
// once instead of everywhere.
func parseBoundingBox(raw string) (source.BoundingBox, error) {
	if raw == "" {
		return source.BoundingBox{}, errors.New(
			"--bbox is required: every source here is national or global, and " +
				"fetching a continent to draw one storm is rude to a public endpoint")
	}

	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return source.BoundingBox{}, fmt.Errorf(
			"--bbox %q: want minLat,minLon,maxLat,maxLon", raw)
	}

	values := make([]float64, 4)

	for index, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return source.BoundingBox{}, fmt.Errorf("--bbox %q: %w", raw, err)
		}

		values[index] = value
	}

	box := source.BoundingBox{
		MinLat: values[0], MinLon: values[1],
		MaxLat: values[2], MaxLon: values[3],
	}

	if err := box.Validate(); err != nil {
		return source.BoundingBox{}, err
	}

	return box, nil
}
