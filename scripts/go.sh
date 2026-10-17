#!/bin/sh
# Runs the go command inside the pinned official Go container image, so the
# host needs no Go toolchain. The repository is mounted at /src.
# scripts/go.ps1 does the same from PowerShell.
#
#   scripts/go.sh test ./...
#   GOOS=darwin GOARCH=arm64 scripts/go.sh build ./cmd/crew
set -eu

image='docker.io/library/golang:1.27.2@sha256:820153096fda4de6ac1b7194d12e416b7dd8f4e9a33d17e0345b111b945162d8'

if command -v podman >/dev/null 2>&1; then
	engine=podman
elif command -v docker >/dev/null 2>&1; then
	engine=docker
else
	echo 'no container engine: install podman or docker' >&2
	exit 1
fi

root=$(cd "$(dirname "$0")/.." && pwd)

# Rootless podman maps the container's root to the caller. Docker does not, so
# run as the caller to keep files written into the repository theirs.
user=
if [ "$engine" = docker ]; then
	user="--user $(id -u):$(id -g) -e HOME=/tmp"
fi

# shellcheck disable=SC2086
exec "$engine" run --rm $user \
	-v "$root:/src:Z" -w /src \
	-e CGO_ENABLED=0 -e GOTOOLCHAIN=local -e GOFLAGS=-buildvcs=false \
	-e GOOS="${GOOS:-linux}" -e GOARCH="${GOARCH:-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')}" \
	"$image" go "$@"
