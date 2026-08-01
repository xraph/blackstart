package id

import (
	"errors"
	"fmt"
	"regexp"
)

// Prefix names an entity type. It is the part before the underscore.
type Prefix string

// prefixPattern is the TypeID specification's rule: lowercase ASCII letters
// and underscores, no leading or trailing underscore, at most 63 characters.
var prefixPattern = regexp.MustCompile(`^[a-z]([a-z_]{0,61}[a-z])?$`)

// Validate reports whether the prefix is well-formed.
//
// This duplicates a check the TypeID library also performs, deliberately: a
// prefix is a compile-time constant declared in an extension, so catching a
// malformed one at registration gives a clear error naming the prefix rather
// than a generation failure at the first write.
func (p Prefix) Validate() error {
	if p == "" {
		return errors.New("id: prefix must not be empty")
	}

	if !prefixPattern.MatchString(string(p)) {
		return fmt.Errorf(
			"id: prefix %q is malformed; TypeID prefixes are lowercase ASCII "+
				"letters and underscores, with no leading or trailing underscore, "+
				"at most 63 characters", p)
	}

	return nil
}

// String returns the prefix as a plain string.
func (p Prefix) String() string { return string(p) }
