package reduce

import (
	"cmp"
	"slices"
)

// Reduce collapses a node-breaker snapshot into a bus-branch model.
//
// Every set of connectivity nodes joined by closed zero-impedance devices
// becomes one bus. Devices with impedance survive as branches between the
// buses their terminals landed in; generators and loads become injections.
//
// # Numbering
//
// Bus numbers are assigned deterministically, and this is load-bearing rather
// than tidy. The numbers feed the solver's content hash, so a reduction that
// numbered the same network two ways would produce two hashes for one network
// and defeat the model cache — and would make every golden file noise.
//
// The rule: sort each class's members lexicographically and take the smallest
// as the class key; sort classes by voltage level and then by that key; number
// from 1. Nothing in that depends on map iteration, on the order equipment
// appeared in the snapshot, or on how the union-find happened to link things.
//
// # Errors
//
// Returns [Error] when the snapshot does not describe a network: a dangling
// terminal, a duplicate identifier, or a device with the wrong number of
// terminals for its kind. Those are checked before any merging, because the
// same problem found halfway through produces a partial reduction and a
// message pointing at the wrong place.
func Reduce(in Snapshot, states SwitchStates) (Reduction, error) {
	if err := in.validate(); err != nil {
		return Reduction{}, err
	}

	level := make(map[string]string, len(in.Nodes))
	for _, node := range in.Nodes {
		level[node.ID] = node.VoltageLevelID
	}

	sets := newUnionFind(in.Nodes)

	// Equipment is walked in snapshot order. Union-find's result does not
	// depend on the order things are merged — that is the point of the
	// structure — but walking deterministically keeps a debugger session
	// reproducible, which is worth the nothing it costs.
	for _, item := range in.Equipment {
		if !item.Kind.Switchable() || !states.Closed(item.ID) {
			continue
		}

		from, to := item.Terminals[0], item.Terminals[1]

		// A closed switch between two voltage levels would be a modelling
		// error — the levels are separated by a transformer, which has
		// impedance — and merging across it would produce one bus at two
		// voltages, which no solver can represent.
		if level[from] != level[to] {
			return Reduction{}, Error{
				Subject: item.ID,
				Reason: "a closed " + string(item.Kind) + " joins voltage levels " +
					level[from] + " and " + level[to] + "; only a transformer " +
					"connects levels, and a transformer has impedance",
			}
		}

		sets.union(from, to)
	}

	buses := numberBuses(sets, level)

	busOf := make(map[string]int, len(in.Nodes))
	for _, bus := range buses {
		for _, node := range bus.Nodes {
			busOf[node] = bus.Number
		}
	}

	branches, injections := attach(in, states, busOf)

	return Reduction{
		Buses:      buses,
		Branches:   branches,
		Injections: injections,
		Islands:    findIslands(buses, branches),
	}, nil
}

// numberBuses turns equivalence classes into numbered buses.
func numberBuses(sets *unionFind, level map[string]string) []Bus {
	members := make(map[string][]string, len(sets.parent))

	for node := range sets.parent {
		root := sets.find(node)
		members[root] = append(members[root], node)
	}

	buses := make([]Bus, 0, len(members))

	for _, nodes := range members {
		// Sorted before the key is taken, so the key is the smallest member
		// rather than whichever node the union-find happened to root at —
		// which depends on merge order and would make numbering unstable.
		sortNodeIDs(nodes)

		buses = append(buses, Bus{
			VoltageLevelID: level[nodes[0]],
			Nodes:          nodes,
		})
	}

	// Voltage level first so a reduced model reads in a sensible order, then
	// the class key. Both are total, so the sort is deterministic without
	// relying on stability.
	slices.SortFunc(buses, func(left, right Bus) int {
		if by := cmp.Compare(left.VoltageLevelID, right.VoltageLevelID); by != 0 {
			return by
		}

		return cmp.Compare(left.Nodes[0], right.Nodes[0])
	})

	for index := range buses {
		buses[index].Number = index + 1
	}

	return buses
}

// attach places impedance devices and injections onto buses.
func attach(
	in Snapshot, states SwitchStates, busOf map[string]int,
) ([]Branch, []Injection) {
	var (
		branches   []Branch
		injections []Injection
	)

	for _, item := range in.Equipment {
		switch {
		case item.Kind.Impedance():
			branches = append(branches, Branch{
				EquipmentID: item.ID,
				Kind:        item.Kind,
				From:        busOf[item.Terminals[0]],
				To:          busOf[item.Terminals[1]],
			})

		case item.Kind == KindGenerator || item.Kind == KindLoad:
			injections = append(injections, Injection{
				EquipmentID: item.ID,
				Kind:        item.Kind,
				Bus:         busOf[item.Terminals[0]],
			})

		case item.Kind.Switchable() && !states.Closed(item.ID):
			// An open switch is not a branch and not an error. It is the
			// reason two buses exist where one would otherwise be, and that
			// fact is already carried by the numbering.

		default:
			// A closed switch, already merged.
		}
	}

	// Snapshot order is already deterministic, but sorting by identifier makes
	// the output independent of how the snapshot was assembled — which matters
	// once snapshots come from a database rather than a literal.
	slices.SortFunc(branches, func(left, right Branch) int {
		return cmp.Compare(left.EquipmentID, right.EquipmentID)
	})

	slices.SortFunc(injections, func(left, right Injection) int {
		return cmp.Compare(left.EquipmentID, right.EquipmentID)
	})

	return branches, injections
}

// findIslands groups buses connected to each other and to nothing else.
//
// De-energized islands are reported rather than deleted. A substation section
// isolated by open breakers is a real part of the network in a real state, and
// dropping it would mean the reduction quietly disagrees with the model it came
// from about how many buses exist.
func findIslands(buses []Bus, branches []Branch) [][]int {
	if len(buses) == 0 {
		return nil
	}

	// A second union-find, this time over bus numbers and impedance branches.
	// The first one merged across closed switches; this one asks what is still
	// connected after the merging is done, which is a different question.
	parent := make(map[int]int, len(buses))
	for _, bus := range buses {
		parent[bus.Number] = bus.Number
	}

	var find func(int) int

	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}

		return parent[x]
	}

	for _, branch := range branches {
		if branch.From == 0 || branch.To == 0 {
			continue
		}

		left, right := find(branch.From), find(branch.To)
		if left != right {
			parent[left] = right
		}
	}

	groups := map[int][]int{}
	for _, bus := range buses {
		root := find(bus.Number)
		groups[root] = append(groups[root], bus.Number)
	}

	islands := make([][]int, 0, len(groups))
	for _, members := range groups {
		slices.Sort(members)
		islands = append(islands, members)
	}

	// Ordered by first member, so the list reads the same way every run.
	slices.SortFunc(islands, func(left, right []int) int {
		return cmp.Compare(left[0], right[0])
	})

	return islands
}

// unionFind is the disjoint-set structure the merging runs on.
//
// Path compression without union by rank: a substation has tens of nodes, not
// millions, and the second optimisation would buy nothing measurable while
// adding a field whose only effect is to change which node ends up as a root —
// which is exactly the thing numbering must not depend on.
type unionFind struct {
	parent map[string]string
}

func newUnionFind(nodes []ConnectivityNode) *unionFind {
	parent := make(map[string]string, len(nodes))
	for _, node := range nodes {
		parent[node.ID] = node.ID
	}

	return &unionFind{parent: parent}
}

func (u *unionFind) find(x string) string {
	if u.parent[x] != x {
		u.parent[x] = u.find(u.parent[x])
	}

	return u.parent[x]
}

func (u *unionFind) union(left, right string) {
	a, b := u.find(left), u.find(right)
	if a != b {
		u.parent[a] = b
	}
}
