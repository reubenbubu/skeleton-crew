#!/bin/sh
# Runs the automated tests in the pinned Go container image and fails if any
# test takes longer than one second.
#
#   scripts/test.sh [<go test args>]
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
[ $# -eq 0 ] && set -- ./...
out=$("$root/scripts/go.sh" test -v -count=1 "$@") || { printf '%s\n' "$out"; exit 1; }
printf '%s\n' "$out" | grep -E '^(ok|FAIL|---)' || true
slow=$(printf '%s\n' "$out" | awk '/^ *--- (PASS|FAIL|SKIP):/ { d = $NF; gsub(/[()s]/, "", d); if (d + 0 > 1) print }')
if [ -n "$slow" ]; then
	printf 'tests slower than one second:\n%s\n' "$slow" >&2
	exit 1
fi
