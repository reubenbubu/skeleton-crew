# Builds crew inside the pinned official Go container image, as
# scripts/self-build.sh does, from PowerShell. Its output is byte for byte the
# same as that of scripts/self-build.sh for the same commit and target.
#
#   powershell -File scripts\self-build.ps1 [<version>]
#
# The version defaults to the exact tag of HEAD, or dev when HEAD is not
# tagged or the working tree has changes. The binary is written to
# dist/crew-<os>-<arch>, with .exe for windows; it builds for Windows on this
# machine's architecture unless GOOS and GOARCH say otherwise.

$root = Split-Path -Parent $PSScriptRoot
if ($args.Count -gt 0) {
	$version = $args[0]
} elseif (-not (git -C $root status --porcelain)) {
	$version = git -C $root describe --tags --exact-match 2>$null
	if ($LASTEXITCODE) { $version = 'dev' }
} else {
	$version = 'dev'
}
if ($version -notmatch '^[A-Za-z0-9._+-]+\z') {
	[Console]::Error.WriteLine("invalid version: $version")
	exit 1
}
$os = if ($env:GOOS) { $env:GOOS } else { 'windows' }
# A 32-bit PowerShell on 64-bit Windows names the machine's architecture in
# PROCESSOR_ARCHITEW6432.
$a = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
$arch = if ($env:GOARCH) { $env:GOARCH } else { @{ AMD64 = 'amd64'; ARM64 = 'arm64' }[$a] }
$out = "dist/crew-$os-$arch"
if ($os -eq 'windows') { $out += '.exe' }

New-Item -ItemType Directory -Force -Path (Join-Path $root 'dist') | Out-Null
$saved = $env:GOOS, $env:GOARCH
try {
	$env:GOOS, $env:GOARCH = $os, $arch
	& (Join-Path $PSScriptRoot 'go.ps1') build -trimpath `
		-ldflags "-s -w -buildid= -X main.version=$version" `
		-o $out ./cmd/crew
	if ($LASTEXITCODE) { exit $LASTEXITCODE }
} finally {
	$env:GOOS, $env:GOARCH = $saved
}
$out
