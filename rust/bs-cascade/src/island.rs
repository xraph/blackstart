//! Islands: the connected components a case falls apart into.
//!
//! A cascade opens branches. `Case::validate` and the power-flow reduction
//! both assume exactly one connected network with exactly one slack — the
//! moment an outage splits the network, that assumption is false for the
//! whole case even though it may still be true for each piece. `Islands`
//! answers "what are the pieces", and `Subcase` turns one piece back into
//! something solvable on its own.
//!
//! The partition here must agree with `findIslands` in
//! `extensions/topology/reduce/reduce.go`: same union-find walk order, same
//! member ordering, same island ordering. Two implementations of "what is
//! connected to what" that could disagree are worse than one implementation
//! duplicated, because a disagreement produces a plausible wrong answer
//! instead of a build failure.

use bs_case::{Bus, BusKind, Case};

/// The connected components of a case's bus/branch graph.
///
/// Only in-service branches connect buses — an open breaker is exactly the
/// event that creates a new island, so treating it as still connecting would
/// make this type unable to see the thing it exists to detect.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Islands {
    /// Island index for each bus, by bus index.
    pub of_bus: Vec<usize>,
    /// Bus indices in each island, sorted, islands ordered by lowest member.
    pub members: Vec<Vec<usize>>,
}

impl Islands {
    /// Partitions a case's buses by in-service connectivity.
    ///
    /// Union-find over a plain `Vec<usize>` rather than a map: bus indices are
    /// already a dense `0..n_bus` range, so a map would add indirection for
    /// nothing. Branches are walked in array order and out-of-service ones are
    /// skipped before union, matching `findIslands` in
    /// `extensions/topology/reduce/reduce.go` exactly, so the Go and Rust
    /// sides cannot disagree about what an island is.
    #[must_use]
    pub fn of(case: &Case) -> Self {
        let n = case.n_bus();
        let mut parent: Vec<usize> = (0..n).collect();

        for branch in &case.branches {
            if branch.in_service {
                union(&mut parent, branch.from, branch.to);
            }
        }

        // Buses walked in index order, and a new island opened the first time
        // its root is seen. That single pass gives members sorted within each
        // island and islands ordered by their lowest member, both for free —
        // no separate sort needed anywhere.
        let mut of_bus = vec![0_usize; n];
        let mut island_of_root = vec![usize::MAX; n];
        let mut members: Vec<Vec<usize>> = Vec::new();

        for (bus, of_bus_entry) in of_bus.iter_mut().enumerate() {
            let root = find(&mut parent, bus);

            let island = if island_of_root[root] == usize::MAX {
                let island = members.len();
                island_of_root[root] = island;
                members.push(Vec::new());
                island
            } else {
                island_of_root[root]
            };

            members[island].push(bus);
            *of_bus_entry = island;
        }

        Self { of_bus, members }
    }

    /// Number of islands.
    #[must_use]
    pub fn count(&self) -> usize {
        self.members.len()
    }
}

/// Finds `x`'s root, compressing the path so the next lookup is O(1).
fn find(parent: &mut [usize], x: usize) -> usize {
    if parent[x] != x {
        parent[x] = find(parent, parent[x]);
    }
    parent[x]
}

/// Merges the sets containing `a` and `b`.
///
/// No union by rank: at the sizes a single case's islands reach, the second
/// optimisation buys nothing measurable, and it would change which bus ends
/// up as the root — a thing nothing here depends on, but not a variable worth
/// introducing for free.
fn union(parent: &mut [usize], a: usize, b: usize) {
    let ra = find(parent, a);
    let rb = find(parent, b);
    if ra != rb {
        parent[ra] = rb;
    }
}

/// One island, extracted into a standalone solvable [`Case`].
///
/// `Case::validate` requires exactly one slack bus and the power-flow
/// reduction deletes exactly one row and column for it — both are correct for
/// a whole connected network and both are wrong for an arbitrary subset of
/// one. `Subcase` is where an island's bus and branch indices get remapped
/// into a dense `0..n` range of their own, so the rest of the solve path can
/// stay ignorant of the fact that it is looking at a fragment.
#[derive(Debug, Clone, PartialEq)]
pub struct Subcase {
    /// The island as a standalone case, indexed from zero.
    pub case: Case,
    /// Full-case bus index for each subcase bus index.
    pub bus_of: Vec<usize>,
    /// Full-case branch index for each subcase branch index.
    pub branch_of: Vec<usize>,
}

impl Subcase {
    /// Extracts one island as a standalone case.
    ///
    /// `members` must be sorted ascending — [`Islands::of`] already produces
    /// them that way, and that order is what makes "lowest-index member"
    /// well-defined without a separate scan.
    ///
    /// The lowest-index member that is PV or slack becomes the subcase's one
    /// slack bus; every other PV-or-slack member is demoted to PV, since a
    /// second slack would make the extracted case fail `validate` the same
    /// way the un-split original would have. An island with no PV or slack
    /// member has no machine to hold a reference angle or absorb a mismatch —
    /// it is de-energized load with nothing driving it — so there is no
    /// meaningful subcase and this returns `None`.
    #[must_use]
    pub fn extract(case: &Case, members: &[usize]) -> Option<Self> {
        debug_assert!(
            members.is_sorted(),
            "members must be sorted ascending; Islands::of already guarantees this, and \
             an unsorted slice would silently pick the wrong reference bus below"
        );

        let bus_of = members.to_vec();

        let mut local_of_full = vec![usize::MAX; case.n_bus()];
        for (local, &full) in bus_of.iter().enumerate() {
            local_of_full[full] = local;
        }

        let reference = bus_of
            .iter()
            .position(|&full| matches!(case.buses[full].kind, BusKind::Pv | BusKind::Slack))?;

        let mut buses: Vec<Bus> = bus_of
            .iter()
            .map(|&full| case.buses[full].clone())
            .collect();
        for (local, bus) in buses.iter_mut().enumerate() {
            if local == reference {
                bus.kind = BusKind::Slack;
            } else if bus.kind == BusKind::Slack {
                bus.kind = BusKind::Pv;
            }
        }

        let mut branches = Vec::new();
        let mut branch_of = Vec::new();

        for (index, branch) in case.branches.iter().enumerate() {
            if !branch.in_service {
                continue;
            }

            let from = local_of_full[branch.from];
            let to = local_of_full[branch.to];
            if from == usize::MAX || to == usize::MAX {
                continue;
            }

            let mut remapped = branch.clone();
            remapped.from = from;
            remapped.to = to;
            branches.push(remapped);
            branch_of.push(index);
        }

        let sub_case = Case::new(case.name.clone(), case.base, buses, branches);

        Some(Self {
            case: sub_case,
            bus_of,
            branch_of,
        })
    }

    /// The subcase-local index of its one slack bus.
    ///
    /// # Panics
    ///
    /// Never, for a `Subcase` produced by [`Subcase::extract`] — extraction
    /// always designates exactly one slack. The `expect` exists to surface a
    /// bug in that invariant loudly rather than let a mis-built `Subcase`
    /// solve against the wrong bus.
    #[must_use]
    pub fn reference(&self) -> usize {
        self.case
            .slack_index()
            .expect("Subcase::extract always designates exactly one slack")
    }

    /// Writes subcase-indexed flows onto a full-case-indexed buffer.
    ///
    /// This is the one place a subcase branch index turns back into a full
    /// case branch index. Every other caller should go through it rather than
    /// re-deriving the mapping, because a second, slightly different
    /// remapping is how a flow ends up reported against the wrong branch — a
    /// failure that looks like a plausible number instead of a crash.
    ///
    /// # Panics
    ///
    /// Panics if `flows` is not one entry per subcase branch, or `into` is
    /// not one entry per full-case branch.
    pub fn expand_flows(&self, flows: &[f64], into: &mut [f64]) {
        assert_eq!(
            flows.len(),
            self.branch_of.len(),
            "flows must have one entry per subcase branch"
        );
        assert!(
            self.branch_of.iter().all(|&full| full < into.len()),
            "into must already be sized to the full case"
        );

        for (local, &full) in self.branch_of.iter().enumerate() {
            into[full] = flows[local];
        }
    }
}
