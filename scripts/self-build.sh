#!/bin/sh
# Builds crew inside the pinned official Go container image, so the host needs
# no Go toolchain. The build is reproducible: the same commit gives the same
# bytes as any other build made with these settings.
#
#   scripts/self-build.sh [<version>]
#
# The version defaults to the exact tag of HEAD, or dev when HEAD is not
# tagged or the working tree has changes. The binary is written to
# dist/crew-<os>-<arch>, with .exe for windows; set GOOS and GOARCH to
# cross-build. scripts/self-build.ps1 does the same from PowerShell.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
if [ $# -gt 0 ]; then
	version=$1
elif [ -z "$(git -C "$root" status --porcelain)" ]; then
	version=$(git -C "$root" describe --tags --exact-match 2>/dev/null || echo dev)
else
	version=dev
fi
case $version in
'' | *[!A-Za-z0-9._+-]*)
	echo "invalid version: $version" >&2
	exit 1
	;;
esac
os=${GOOS:-$(uname -s | tr '[:upper:]' '[:lower:]')}
arch=${GOARCH:-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')}
out=dist/crew-$os-$arch
[ "$os" = windows ] && out=$out.exe

mkdir -p "$root/dist"
GOOS=$os GOARCH=$arch "$root/scripts/go.sh" build -trimpath \
	-ldflags "-s -w -buildid= -X main.version=$version" \
	-o "$out" ./cmd/crew
echo "$out"
