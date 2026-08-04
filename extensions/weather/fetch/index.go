// Package fetch retrieves weather products from the public registry.
//
// The unit of retrieval is one field at one forecast hour, not one file. HRRR
// publishes a 151 MB product per hour and this platform wants a few megabytes
// of it, so every fetch begins with the index sidecar the publisher ships
// beside the data: ten kilobytes listing the byte offset of every message in
// the file.
//
// That ratio is why this package exists rather than a plain HTTP GET. It is
// also why a dataset version's hash means what it says — the hash covers the
// bytes actually read and parsed, not a file that was downloaded and mostly
// discarded.
package fetch

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xraph/blackstart/pkg/bserr"
)

// Record is one message's entry in an index sidecar.
type Record struct {
	// Number is the message's position in the file, from one.
	Number int

	// Offset is where the message begins, in bytes from the start of the file.
	Offset int64

	// Abbrev and LevelText are what the field vocabulary matches on — "GUST"
	// and "surface". Together they are the only identity a sidecar carries;
	// the numeric GRIB2 coordinates appear nowhere in it.
	Abbrev    string
	LevelText string

	// Forecast is the validity description — "1 hour fcst", "0-1 hour acc
	// fcst". Kept because it distinguishes an instantaneous field from an
	// accumulation, which is a difference in what the number means.
	Forecast string
}

// Key is what the field vocabulary's IndexKey matches.
func (r Record) Key() string { return r.Abbrev + ":" + r.LevelText }

// Records is a sidecar's contents, in file order.
//
// Order is not incidental. A sidecar states offsets and no lengths, so a
// message's length is the next record's offset minus its own — which is only
// derivable while the records are still in sequence.
type Records []Record

// index positions within a sidecar record, which is
// `number:offset:date:ABBREV:LEVEL:forecast:`.
const (
	fieldNumber = 0
	fieldOffset = 1
	fieldAbbrev = 3
	fieldLevel  = 4
	fieldFcst   = 5
	fieldCount  = 6
)

// ParseIndex reads a sidecar.
//
// # Errors
//
// Returns an error wrapping bserr.ErrInvalid when a line has the wrong shape,
// carries an unparseable number, or when offsets do not increase — all of
// which mean the length derivation below would produce nonsense rather than
// fail.
func ParseIndex(from io.Reader) (Records, error) {
	var (
		records Records
		scanner = bufio.NewScanner(from)
		line    int
	)

	for scanner.Scan() {
		line++

		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}

		record, err := parseRecord(text)
		if err != nil {
			return nil, fmt.Errorf("weather: index line %d: %w", line, err)
		}

		// Offsets must increase, because the next one is how this one's length
		// is known. A sidecar that goes backwards would yield a negative
		// length, and a negative length in a range request is a confusing
		// failure a long way from here.
		if len(records) > 0 && record.Offset < records[len(records)-1].Offset {
			return nil, fmt.Errorf(
				"%w: index line %d has offset %d after %d; a sidecar's offsets "+
					"increase, and this one's lengths cannot be derived",
				bserr.ErrInvalid, line, record.Offset,
				records[len(records)-1].Offset)
		}

		records = append(records, record)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("weather: reading the index: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf(
			"%w: the index is empty; a product with no messages is not one this "+
				"platform can use", bserr.ErrInvalid)
	}

	return records, nil
}

func parseRecord(text string) (Record, error) {
	part := strings.Split(text, ":")
	if len(part) < fieldCount {
		return Record{}, fmt.Errorf(
			"%w: %q has %d fields, want at least %d",
			bserr.ErrInvalid, text, len(part), fieldCount)
	}

	number, err := strconv.Atoi(part[fieldNumber])
	if err != nil {
		return Record{}, fmt.Errorf(
			"%w: record number %q: %w", bserr.ErrInvalid, part[fieldNumber], err)
	}

	offset, err := strconv.ParseInt(part[fieldOffset], 10, 64)
	if err != nil {
		return Record{}, fmt.Errorf(
			"%w: offset %q: %w", bserr.ErrInvalid, part[fieldOffset], err)
	}

	return Record{
		Number:    number,
		Offset:    offset,
		Abbrev:    part[fieldAbbrev],
		LevelText: part[fieldLevel],
		Forecast:  part[fieldFcst],
	}, nil
}

// Locate finds a field's byte range by its index key.
//
// The length is the next record's offset minus this one's. For the last
// record there is no next offset, so the length is zero — meaning "to the end
// of the file", which is exactly what an open-ended HTTP range expresses.
// Guessing a length there would be worse than saying so.
func (r Records) Locate(key string) (offset, length int64, ok bool) {
	for index, record := range r {
		if record.Key() != key {
			continue
		}

		if index+1 < len(r) {
			return record.Offset, r[index+1].Offset - record.Offset, true
		}

		return record.Offset, 0, true
	}

	return 0, 0, false
}

// Keys returns every index key present, in file order.
//
// Useful when a field is missing and the question is what the product does
// carry — an ingest failing with "GUST:10 m above ground not found" is far
// less useful without it.
func (r Records) Keys() []string {
	out := make([]string, 0, len(r))

	for _, record := range r {
		out = append(out, record.Key())
	}

	return out
}
