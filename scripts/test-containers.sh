#!/bin/sh
# Runs the tests that need a container engine: those behind the containers
# build tag. The test binaries are built in the pinned Go container image and
# run on the host, where the container engine is.
#
#   scripts/test-containers.sh [<test name pattern>]
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
bin="$root/dist/containers"
rm -rf "$bin"
mkdir -p "$bin"
status=0
for dir in $(grep -rl --include='*_test.go' '^//go:build containers' "$root/internal" | xargs -r -n1 dirname | sort -u); do
	pkg=${dir#"$root"/}
	name=$(printf '%s' "$pkg" | tr / -)
	"$root/scripts/go.sh" test -c -tags containers -o "/src/dist/containers/$name.test" "./$pkg"
	(cd "$dir" && "$bin/$name.test" -test.v -test.count=1 -test.run "${1:-.}") || status=1
done
exit $status
