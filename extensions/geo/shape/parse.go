package shape

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseLineEWKT reads a line back from PostGIS extended well-known text.
//
// The inverse of Line.EWKT, and the reason the well-known text is kept on the
// row at all: the spatial index answers "what is near here", and the row is
// the durable copy the geometry is rebuilt from.
//
// Deliberately narrow. It reads what this package writes — an SRID prefix and
// a LINESTRING of three-ordinate coordinates — and refuses everything else
// rather than half-understanding the wider WKT grammar. A parser that
// silently accepted a POLYGON as a line would produce a route through the
// vertices of a shape.
//
// # Errors
//
// Returns an error naming what it found instead. A geometry that cannot be
// read is a stored row that has to be looked at, and a vague message turns
// that into a hunt.
func ParseLineEWKT(text string) (Line, error) {
	body := strings.TrimSpace(text)

	// The SRID prefix is optional on input and always written on output. A
	// geometry with no SRID is ambiguous rather than wrong — PostGIS treats it
	// as unknown — so it is accepted and assumed to be this package's one SRID.
	if rest, ok := strings.CutPrefix(body, "SRID="); ok {
		declared, remainder, found := strings.Cut(rest, ";")
		if !found {
			return nil, fmt.Errorf(
				"shape: %q has an SRID prefix with no geometry after it", text)
		}

		if declared != strconv.Itoa(SRID) {
			return nil, fmt.Errorf(
				"shape: geometry declares SRID %s; everything stored here is "+
					"SRID %d, and mixing them makes a distance come back in "+
					"degrees looking like a small number of meters", declared, SRID)
		}

		body = strings.TrimSpace(remainder)
	}

	inner, ok := strings.CutPrefix(body, "LINESTRING")
	if !ok {
		kind, _, _ := strings.Cut(body, "(")

		return nil, fmt.Errorf(
			"shape: %q is a %s, not a LINESTRING", text, strings.TrimSpace(kind))
	}

	inner = strings.TrimSpace(inner)

	inner, ok = strings.CutPrefix(inner, "(")
	if !ok || !strings.HasSuffix(inner, ")") {
		return nil, fmt.Errorf("shape: %q has unbalanced parentheses", text)
	}

	inner = strings.TrimSuffix(inner, ")")

	var out Line

	for index, part := range strings.Split(inner, ",") {
		point, err := parsePoint(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("shape: %q coordinate %d: %w", text, index+1, err)
		}

		out = append(out, point)
	}

	if err := out.Validate(); err != nil {
		return nil, err
	}

	return out, nil
}

// parsePoint reads one whitespace-separated coordinate triple.
func parsePoint(text string) (Point, error) {
	fields := strings.Fields(text)

	// Two is accepted so a geometry written by something other than this
	// package still reads, with elevation unknown rather than refused. Three
	// is what this package writes.
	if len(fields) != 2 && len(fields) != 3 {
		return Point{}, fmt.Errorf(
			"%q has %d ordinates; a coordinate is 'lon lat' or 'lon lat z'",
			text, len(fields))
	}

	values := make([]float64, len(fields))

	for index, field := range fields {
		parsed, err := strconv.ParseFloat(field, 64)
		if err != nil {
			return Point{}, fmt.Errorf("%q is not a number", field)
		}

		values[index] = parsed
	}

	out := Point{Lon: values[0], Lat: values[1]}
	if len(values) == 3 {
		out.Elevation = values[2]
	}

	return out, out.Validate()
}
