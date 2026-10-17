# Runs the go command inside the pinned official Go container image, so the
# host needs no Go toolchain, as scripts/go.sh does, from PowerShell. The
# repository is mounted at /src, and the image is the one scripts/go.sh pins.
#
#   powershell -File scripts\go.ps1 test ./...
#   $env:GOOS = 'windows'; powershell -File scripts\go.ps1 build ./cmd/crew

$m = Select-String -Path (Join-Path $PSScriptRoot 'go.sh') -Pattern "^image='([^']+)'$"
if (-not $m) {
	[Console]::Error.WriteLine('no image in scripts/go.sh')
	exit 1
}
$image = $m.Matches[0].Groups[1].Value

$engine = 'podman', 'docker' | Where-Object { Get-Command $_ -ErrorAction SilentlyContinue } | Select-Object -First 1
if (-not $engine) {
	[Console]::Error.WriteLine('no container engine: install podman or docker')
	exit 1
}

$root = Split-Path -Parent $PSScriptRoot
$os = if ($env:GOOS) { $env:GOOS } else { 'linux' }
# A 32-bit PowerShell on 64-bit Windows names the machine's architecture in
# PROCESSOR_ARCHITEW6432.
$a = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
$arch = if ($env:GOARCH) { $env:GOARCH } else { @{ AMD64 = 'amd64'; ARM64 = 'arm64' }[$a] }

& $engine run --rm -v "${root}:/src:Z" -w /src `
	-e CGO_ENABLED=0 -e GOTOOLCHAIN=local -e GOFLAGS=-buildvcs=false `
	-e "GOOS=$os" -e "GOARCH=$arch" `
	$image go @args
exit $LASTEXITCODE
