#!/usr/bin/env bash
#
# Enforce the per-commit limit on hand-written code.
#
# No commit adds more than ~500 lines of hand-written code. Larger units of
# work get decomposed. The commit history is an artifact of this project, and
# a 3,000-line commit records that something happened without recording what.
#
# "Hand-written" is defined by .gitattributes, not by a list kept here. The
# script asks `git check-attr linguist-generated` for every changed path, so
# generated stubs, downloaded test cases, golden files, and lockfiles are
# excluded automatically and the two definitions cannot drift apart.
#
# Usage:
#   scripts/check-commit-size.sh                  # HEAD only
#   scripts/check-commit-size.sh <base>..<head>   # every commit in a range
#
# Exit: 0 within limit, 1 over.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

LIMIT="${COMMIT_SIZE_LIMIT:-500}"
RANGE="${1:-HEAD~1..HEAD}"

# A repository with a single commit has no HEAD~1.
if ! git rev-parse --verify --quiet "${RANGE%%..*}" >/dev/null 2>&1; then
	RANGE="HEAD"
fi

if [ "$RANGE" = "HEAD" ]; then
	COMMITS=$(git rev-parse HEAD)
else
	COMMITS=$(git rev-list "$RANGE")
fi

if [ -z "$COMMITS" ]; then
	echo "check-commit-size: no commits in range $RANGE"
	exit 0
fi

STATUS=0

# is_excluded <path> — true when .gitattributes marks the path as something
# other than hand-written code.
#
# Note the value match. `git check-attr` reports `set` for a bare attribute
# (`foo`) but the literal value for an assignment (`foo=true` reports `true`).
# .gitattributes uses the assignment form, so matching only `set` silently
# matches nothing and every generated file counts against the limit. Accept
# both spellings so the script survives either style.
is_excluded() {
	local path="$1" attr
	for attr in linguist-generated linguist-vendored linguist-documentation; do
		if git check-attr "$attr" -- "$path" 2>/dev/null |
			grep -qE ": ${attr}: (set|true)$"; then
			return 0
		fi
	done

	return 1
}

for sha in $COMMITS; do
	subject=$(git log -1 --format='%s' "$sha")
	total=0
	detail=""

	# --numstat gives "<added> <deleted> <path>"; binary files give "-".
	while read -r added _deleted path; do
		[ -z "${path:-}" ] && continue
		[ "$added" = "-" ] && continue

		# Ask git, not a glob list kept in this script.
		if is_excluded "$path"; then
			continue
		fi

		total=$((total + added))
		detail="${detail}    ${added}\t${path}\n"
	done < <(git show --numstat --format= "$sha")

	if [ "$total" -gt "$LIMIT" ]; then
		if [ $STATUS -eq 0 ]; then
			echo "check-commit-size: FAILED"
			echo
		fi
		echo "  ${sha:0:9}  ${total} hand-written lines added (limit ${LIMIT})"
		echo "  ${subject}"
		echo
		printf "%b" "$detail"
		echo
		STATUS=1
	fi
done

if [ $STATUS -eq 1 ]; then
	echo "Decompose the work into smaller commits. If a file above is"
	echo "generated or vendored, mark it in .gitattributes rather than"
	echo "raising the limit."
	exit 1
fi

echo "check-commit-size: ok (limit ${LIMIT}, range ${RANGE})"
