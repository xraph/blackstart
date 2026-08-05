package fragility

// osmKindToClass maps the OpenStreetMap power-structure vocabulary to this
// package's closed set of curve classes.
//
// This lives here, at the point of use, rather than on asset.Structure.Kind
// itself: Kind is free text because source vocabularies differ between
// OpenStreetMap, ArcGIS and anything else a route might come from, and
// forcing them into one enumeration at the point of storage would discard
// the distinction the source drew. The coercion to a closed set belongs
// where an unmapped value can be counted and reported, not baked into the
// asset model every consumer of Kind has to carry.
//
// "portal" is deliberately absent, and not for the same reason "monopole"
// and "H-frame" are absent. Those two have no curve because no fragility
// literature covers them (see the [Class] doc). "portal" has no curve
// because there is no fragility.Portal — the search for a citable curve for
// portal (H-frame) structures came up empty, so the class itself was never
// added to the closed set; adding a mapping to it here would not compile,
// and inventing the constant just to make it compile would be exactly the
// kind of plausible-looking-but-unmeasured curve this package exists to
// avoid. power=portal is not a rare tag: the phase-3 survey measured it at
// roughly 4% of line-way nodes, so a portal structure reports false here —
// a known, quantified gap, not an oversight.
var osmKindToClass = map[string]Class{
	"tower": Lattice,
	"pole":  Pole,
}

// ClassOf maps a structure's recorded kind to the closed set of curve
// classes, reporting false for anything not in that set.
//
// An unrecognized kind is not coerced to the nearest class: a wrong class
// swapped in silently is a wrong curve applied to a real structure, and it
// looks identical to a correct one once the probability is stored. Callers
// are expected to count and report the false case rather than default it.
func ClassOf(kind string) (Class, bool) {
	c, ok := osmKindToClass[kind]

	return c, ok
}
