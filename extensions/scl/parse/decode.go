package parse

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ParserVersion identifies this reader in a provenance record.
//
// A dataset is only as reproducible as the code that read it: the same bytes
// mean different things to different parsers, so a fix here changes what a
// file says and the version moves with it.
const ParserVersion = "scl/0.1.0"

// Limits bound what a decoder will accept.
//
// This is a public repository and XML parsers are a classic denial-of-service
// surface. The specific hazards for Go's decoder are not the ones a Java
// codebase guards against — see Decode — but depth and volume are real, and a
// bound that is never hit costs nothing.
type Limits struct {
	// MaxDepth caps element nesting. SCL's deepest legitimate path is around
	// fifteen elements; a hundred is far beyond any real file and far below
	// what would exhaust a goroutine stack.
	MaxDepth int

	// MaxTokens caps total tokens read. A large SCD is a few million; fifty
	// million is generous and still terminates.
	MaxTokens int
}

// DefaultLimits are what Decode uses.
func DefaultLimits() Limits {
	return Limits{MaxDepth: 100, MaxTokens: 50_000_000}
}

// ErrDoctype is returned for a document carrying a document type declaration.
var ErrDoctype = errors.New(
	"scl: the document has a DOCTYPE declaration; SCL files are schema-defined " +
		"and have no legitimate use for one, and refusing it is cheaper than " +
		"reasoning about what a parser does with the entities it may declare")

// Decode reads an SCL document.
//
// # Hardening
//
// Go's encoding/xml does not resolve external entities and does not expand
// custom internal ones — an undefined entity is a parse error, not a fetch. So
// the classic XXE and billion-laughs attacks do not apply here, and guarding
// against them specifically would be borrowed vigilance rather than a real
// defense. What does apply is unbounded nesting, which recurses, and unbounded
// input, which does not terminate. Both are bounded.
//
// A DOCTYPE is refused outright. Not because Go would act on it, but because a
// schema-defined format has no legitimate use for one — its presence means the
// file is either not an SCL file or was produced by something worth knowing
// about before trusting its contents.
//
// # Errors
//
// Returns an error naming what was wrong. A file that cannot be read is better
// than one read wrong: a substation with a mis-parsed terminal still reduces,
// and produces a network nobody drew.
func Decode(r io.Reader) (*SCL, error) {
	return DecodeWithLimits(r, DefaultLimits())
}

// DecodeWithLimits reads an SCL document under caller-supplied bounds.
func DecodeWithLimits(r io.Reader, limits Limits) (*SCL, error) {
	if limits.MaxDepth <= 0 {
		limits.MaxDepth = DefaultLimits().MaxDepth
	}

	if limits.MaxTokens <= 0 {
		limits.MaxTokens = DefaultLimits().MaxTokens
	}

	// Scanned first, then decoded from the buffered copy. Two passes over the
	// bytes rather than one, and worth it: a custom xml.TokenReader that
	// enforced the limits mid-decode would have to reimplement the decoder's
	// own namespace handling to stay correct.
	buffered, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("scl: reading the document: %w", err)
	}

	if err := scan(buffered, limits); err != nil {
		return nil, err
	}

	var out SCL

	decoder := xml.NewDecoder(strings.NewReader(string(buffered)))

	// Nil rather than a resolver: an SCL file naming an external entity is one
	// asking this process to fetch something, and the answer is no. Go would
	// refuse anyway; stating it means the refusal survives a future change to
	// the decoder's defaults.
	decoder.Entity = nil

	if err := decoder.Decode(&out); err != nil {
		return nil, fmt.Errorf("scl: %w", err)
	}

	if out.XMLName.Local != "SCL" {
		return nil, fmt.Errorf(
			"scl: the root element is <%s>, not <SCL>", out.XMLName.Local)
	}

	return &out, nil
}

// scan walks the token stream enforcing the limits, without building anything.
func scan(document []byte, limits Limits) error {
	decoder := xml.NewDecoder(strings.NewReader(string(document)))
	decoder.Entity = nil

	var depth, tokens int

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("scl: %w", err)
		}

		tokens++
		if tokens > limits.MaxTokens {
			return fmt.Errorf(
				"scl: the document has more than %d tokens; a large SCD is a few "+
					"million, so this is either not one or is not worth reading",
				limits.MaxTokens)
		}

		switch typed := token.(type) {
		case xml.Directive:
			// Directives are DOCTYPE and friends. Checked case-insensitively
			// after trimming, because a hostile file will not write it tidily.
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(string(typed))), "DOCTYPE") {
				return ErrDoctype
			}

		case xml.StartElement:
			depth++
			if depth > limits.MaxDepth {
				return fmt.Errorf(
					"scl: elements nest more than %d deep; SCL's deepest real path "+
						"is about fifteen, so this is a document built to exhaust a "+
						"parser rather than to describe a substation",
					limits.MaxDepth)
			}

		case xml.EndElement:
			depth--
		}
	}
}
