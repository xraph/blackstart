#!/usr/bin/env bash
#
# Enforce the advisory-only boundary described in docs/SAFETY.md.
#
# Blackstart produces forecasts and recommendations. It does not actuate. There
# is no write path to a SCADA, EMS, ADMS, or field device, no control API, and
# no placeholder anticipating one.
#
# This script is the declaration-level half of that enforcement. The other half
# is the forbidigo block in .golangci.yml, and the two are complementary:
#
#   - forbidigo catches *call sites*, including calls into third-party
#     libraries, which text search cannot resolve.
#   - It does NOT catch *declarations*. `func (c *client) WriteControlPoint()`
#     is invisible to it until something calls it.
#
# So this script scans source text for control-shaped identifiers anywhere they
# appear — including in comments, because docs/SAFETY.md commits to there being
# no evidence that actuation is planned, and a commented-out control path is
# exactly such evidence.
#
# Usage:  scripts/check-safety-boundary.sh [path ...]
# Exit:   0 clean, 1 violation found.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# Identifiers that name an actuation capability. Deliberately broad: a false
# positive costs a rename, a false negative costs the safety property.
PATTERNS=(
	'(Write|Send|Issue|Execute|Operate|Set)(Control|Command|Setpoint|Breaker|Point|Output|Tap)'
	'SelectBeforeOperate'
	'DirectOperate'
	'ControlRelayOutput'
	'CROB'                 # DNP3 Control Relay Output Block
	'(crob|controlRelay)'
)

# Paths exempt because they define or document the boundary rather than cross
# it. Tests and testdata are exempt because asserting a capability is absent
# requires naming it.
EXCLUDE_RE='^(docs/SAFETY\.md|\.golangci\.yml|scripts/check-safety-boundary\.sh|CONTRIBUTING\.md|README\.md|a private design note)$'

# Source extensions only. Lockfiles and generated stubs are out of scope.
# Written to a temp file rather than a bash array: macOS ships bash 3.2, which
# has no `mapfile`, and this script must run identically on a laptop and in CI.
FILELIST="$(mktemp)"
trap 'rm -f "$FILELIST"' EXIT

git ls-files -- '*.go' '*.rs' '*.ts' '*.tsx' '*.proto' '*.sql' 2>/dev/null |
	grep -Ev "$EXCLUDE_RE" >"$FILELIST" || true

COUNT=$(wc -l <"$FILELIST" | tr -d ' ')
if [ "$COUNT" -eq 0 ]; then
	echo "check-safety-boundary: no source files to scan"
	exit 0
fi

FOUND=0
for pattern in "${PATTERNS[@]}"; do
	matches=$(xargs -I{} grep -nEH "$pattern" {} <"$FILELIST" 2>/dev/null || true)
	if [ -n "$matches" ]; then
		if [ $FOUND -eq 0 ]; then
			echo "check-safety-boundary: FAILED"
			echo
			echo "Blackstart is advisory and does not actuate (docs/SAFETY.md)."
			echo "The following look like actuation capabilities:"
			echo
		fi
		echo "$matches" | sed 's/^/  /'
		FOUND=1
	fi
done

if [ $FOUND -eq 1 ]; then
	echo
	echo "If this is a read path whose name merely resembles a control"
	echo "operation, rename it. If it is a write path to an operational"
	echo "system, it does not belong in this repository."
	exit 1
fi

echo "check-safety-boundary: ok (${#FILES[@]} files scanned)"
