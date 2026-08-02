// Package matpower reads the subset of MATPOWER case files this project needs.
//
// A small hand-rolled lexer over the fields that matter — mpc.baseMVA, .bus,
// .gen, .branch — and deliberately not an attempt to parse MATLAB. A MATLAB
// parser would be a large piece of work whose failure mode is silently
// misreading a case, and the case files in circulation use a tiny, regular
// subset of the language: assignments of numeric matrices.
//
// What this rejects, it rejects loudly. A file it cannot read is better than a
// file it reads wrong, because a network with one transposed column still
// solves and still produces numbers.
package matpower

import (
	"fmt"
	"strconv"
	"strings"
)

// matrix is one `mpc.name = [ ... ];` assignment.
type matrix struct {
	// name is the field after `mpc.`.
	name string

	// rows are the numeric rows, each already split into columns.
	rows [][]float64

	// line is where the assignment started, for error messages that point at
	// the file rather than at this package.
	line int
}

// scalar is one `mpc.name = value;` assignment.
type scalar struct {
	name  string
	value float64
	line  int
}

// text is one `mpc.name = 'value';` assignment.
//
// Every real case file has at least `mpc.version = '2'`, and several name
// themselves. Treating a quoted value as a malformed number would reject every
// file in circulation — which the first draft of this lexer did, and which the
// tests caught only because their fixture was shaped like a real file rather
// than like the subset this reader needs.
type text struct {
	name  string
	value string
	line  int
}

// document is everything the lexer found.
type document struct {
	scalars  map[string]scalar
	texts    map[string]text
	matrices map[string]matrix
}

// lex reads a MATPOWER case file into assignments.
//
// Line-oriented rather than token-oriented, because the grammar this accepts
// is line-oriented: an assignment starts at `mpc.` and a matrix ends at `];`.
// A token-based lexer would be more general and would also accept files this
// has no business accepting.
func lex(source string) (document, error) {
	doc := document{
		scalars:  map[string]scalar{},
		texts:    map[string]text{},
		matrices: map[string]matrix{},
	}

	lines := strings.Split(source, "\n")

	for index := 0; index < len(lines); index++ {
		line := strip(lines[index])
		if line == "" {
			continue
		}

		name, rest, ok := assignment(line)
		if !ok {
			continue
		}

		if strings.HasPrefix(rest, "[") {
			consumed, m, err := readMatrix(name, lines, index, rest)
			if err != nil {
				return document{}, err
			}

			m.line = index + 1
			doc.matrices[name] = m
			index = consumed

			continue
		}

		if quoted, ok := readText(rest); ok {
			doc.texts[name] = text{name: name, value: quoted, line: index + 1}

			continue
		}

		value, err := readScalar(rest)
		if err != nil {
			return document{}, fmt.Errorf("matpower: line %d: mpc.%s: %w", index+1, name, err)
		}

		doc.scalars[name] = scalar{name: name, value: value, line: index + 1}
	}

	return doc, nil
}

// strip removes comments and surrounding whitespace.
//
// MATLAB comments start at `%`. A `%` inside a string would be mangled, and
// that is acceptable here: this grammar has no strings.
func strip(line string) string {
	if at := strings.IndexByte(line, '%'); at >= 0 {
		line = line[:at]
	}

	return strings.TrimSpace(line)
}

// assignment splits `mpc.name = rest` into its parts.
func assignment(line string) (name, rest string, ok bool) {
	const prefix = "mpc."

	if !strings.HasPrefix(line, prefix) {
		return "", "", false
	}

	equals := strings.IndexByte(line, '=')
	if equals < 0 {
		return "", "", false
	}

	name = strings.TrimSpace(line[len(prefix):equals])
	rest = strings.TrimSpace(line[equals+1:])

	return name, rest, name != ""
}

// readText reads a `'value';` right-hand side, reporting whether it was one.
//
// Single quotes only. MATLAB accepts double quotes too, and no case file in
// circulation uses them — accepting both would mean guessing at a grammar this
// reader deliberately does not implement.
func readText(rest string) (string, bool) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(rest), ";")
	trimmed = strings.TrimSpace(trimmed)

	if len(trimmed) < 2 || !strings.HasPrefix(trimmed, "'") || !strings.HasSuffix(trimmed, "'") {
		return "", false
	}

	return trimmed[1 : len(trimmed)-1], true
}

// readScalar reads a `value;` right-hand side.
func readScalar(rest string) (float64, error) {
	text := strings.TrimSuffix(strings.TrimSpace(rest), ";")

	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", text)
	}

	return value, nil
}

// readMatrix reads a `[ ... ];` right-hand side, which may span lines.
//
// Returns the index of the last line consumed, so the caller resumes after the
// closing bracket rather than re-reading rows as assignments.
func readMatrix(name string, lines []string, start int, first string) (int, matrix, error) {
	var (
		m       matrix
		builder strings.Builder
	)

	m.name = name

	builder.WriteString(strings.TrimPrefix(first, "["))

	index := start

	for {
		text := builder.String()

		if body, _, found := strings.Cut(text, "]"); found {
			rows, err := parseRows(name, body)
			if err != nil {
				return 0, matrix{}, fmt.Errorf("matpower: line %d: %w", start+1, err)
			}

			m.rows = rows

			return index, m, nil
		}

		index++

		if index >= len(lines) {
			return 0, matrix{}, fmt.Errorf(
				"matpower: line %d: mpc.%s opens a matrix that is never closed; "+
					"a `];` is missing", start+1, name)
		}

		next := strip(lines[index])

		// A new assignment inside a matrix body means the previous one was
		// never closed. Detected here rather than left to the number parser,
		// which would otherwise report `mpc.gen` as a malformed number and
		// point the reader at the wrong line entirely.
		if _, _, isAssignment := assignment(next); isAssignment {
			return 0, matrix{}, fmt.Errorf(
				"matpower: line %d: mpc.%s opens a matrix that is never closed; "+
					"line %d starts a new assignment, so a `];` is missing before it",
				start+1, name, index+1)
		}

		builder.WriteString("\n")
		builder.WriteString(next)
	}
}

// parseRows splits a matrix body into numeric rows.
//
// Rows are separated by newlines or semicolons and columns by whitespace or
// commas, which covers every case file in circulation without accepting
// MATLAB's full expression grammar.
func parseRows(name, body string) ([][]float64, error) {
	var rows [][]float64

	for _, raw := range strings.FieldsFunc(body, func(r rune) bool {
		return r == '\n' || r == ';'
	}) {
		fields := strings.FieldsFunc(raw, func(r rune) bool {
			return r == ' ' || r == '\t' || r == ',' || r == '\r'
		})

		if len(fields) == 0 {
			continue
		}

		row := make([]float64, 0, len(fields))

		for _, field := range fields {
			value, err := strconv.ParseFloat(field, 64)
			if err != nil {
				return nil, fmt.Errorf(
					"mpc.%s: %q is not a number; this reader accepts numeric "+
						"matrices only, not MATLAB expressions", name, field)
			}

			row = append(row, value)
		}

		rows = append(rows, row)
	}

	return rows, nil
}
