# Agent instructions

This project is run by Skeleton Crew: a Project Manager, a Dev Manager, Devs and Reviewers.

## Bootstrap

Skeleton Crew pins `crew` version `v0.1.1`.

1. Run `crew --version`. If it prints `v0.1.1`, go to step 3.
2. Otherwise, install `crew` `v0.1.1` with one of the methods below. If `.crew/crew.toml` has an `install` line, use the method it names. If not, ask the user which method to use; `release` is the default. Show the user the exact commands before running them, and stop if any of them fails. If `~/.local/bin` is not on the user's `PATH`, tell them.

   On Linux and macOS, run the `sh` commands. On Windows, run the PowerShell commands in a PowerShell process of their own, whatever shell you use: save them to a temporary `.ps1` file and run `powershell -NoProfile -ExecutionPolicy Bypass -File <file>`.

   - **`release`:** downloads the release binary, verifies its checksum and its GitHub artifact attestation, and installs it.

     ```sh
     (
       os=$(uname -s | tr '[:upper:]' '[:lower:]') && arch=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') &&
       if command -v sha256sum >/dev/null; then sum=sha256sum; else sum='shasum -a 256'; fi &&
       cd "$(mktemp -d)" &&
       gh release download v0.1.1 --repo reubenbubu/skeleton-crew --pattern "crew-$os-$arch" --pattern SHA256SUMS &&
       line=$(grep " crew-$os-$arch\$" SHA256SUMS) && printf '%s\n' "$line" | $sum -c &&
       gh attestation verify "crew-$os-$arch" --repo reubenbubu/skeleton-crew \
         --signer-workflow reubenbubu/skeleton-crew/.github/workflows/release.yml --source-ref refs/tags/v0.1.1 &&
       mkdir -p ~/.local/bin && install -m 0755 "crew-$os-$arch" ~/.local/bin/crew
     )
     ```

     ```powershell
     $ErrorActionPreference = 'Stop'
     $a = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
     $bin = 'crew-windows-' + @{ AMD64 = 'amd64'; ARM64 = 'arm64' }[$a] + '.exe'
     $d = (New-Item -ItemType Directory -Path (Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName()))).FullName
     gh release download v0.1.1 --repo reubenbubu/skeleton-crew --pattern $bin --pattern SHA256SUMS --dir $d
     if ($LASTEXITCODE) { throw 'gh release download failed' }
     $want = foreach ($l in Get-Content "$d\SHA256SUMS") { $h, $n = $l -split '  ', 2; if ($n -eq $bin) { $h } }
     if ((Get-FileHash "$d\$bin" -Algorithm SHA256).Hash -ne $want) { throw "checksum mismatch: $bin" }
     gh attestation verify "$d\$bin" --repo reubenbubu/skeleton-crew `
       --signer-workflow reubenbubu/skeleton-crew/.github/workflows/release.yml --source-ref refs/tags/v0.1.1
     if ($LASTEXITCODE) { throw 'gh attestation verify failed' }
     New-Item -ItemType Directory -Force -Path "$HOME\.local\bin" | Out-Null
     Copy-Item "$d\$bin" "$HOME\.local\bin\crew.exe" -Force
     ```

   - **`source`:** clones the tag, builds it inside the official Go container image, checks that the build matches the release checksum byte for byte, and installs it. It needs a container engine but no Go toolchain. On Windows it builds with `scripts/self-build.ps1`.

     ```sh
     (
       os=$(uname -s | tr '[:upper:]' '[:lower:]') && arch=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') &&
       if command -v sha256sum >/dev/null; then sum=sha256sum; else sum='shasum -a 256'; fi &&
       d=$(mktemp -d) &&
       git clone --quiet --depth 1 --branch v0.1.1 https://github.com/reubenbubu/skeleton-crew "$d/src" &&
       "$d/src/scripts/self-build.sh" v0.1.1 &&
       gh release download v0.1.1 --repo reubenbubu/skeleton-crew --pattern SHA256SUMS --dir "$d" &&
       cd "$d/src/dist" &&
       line=$(grep " crew-$os-$arch\$" "$d/SHA256SUMS") && printf '%s\n' "$line" | $sum -c &&
       mkdir -p ~/.local/bin && install -m 0755 "crew-$os-$arch" ~/.local/bin/crew
     )
     ```

     ```powershell
     $ErrorActionPreference = 'Stop'
     $a = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
     $bin = 'crew-windows-' + @{ AMD64 = 'amd64'; ARM64 = 'arm64' }[$a] + '.exe'
     $d = (New-Item -ItemType Directory -Path (Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName()))).FullName
     git clone --quiet --depth 1 --branch v0.1.1 https://github.com/reubenbubu/skeleton-crew "$d\src"
     if ($LASTEXITCODE) { throw 'git clone failed' }
     powershell -NoProfile -ExecutionPolicy Bypass -File "$d\src\scripts\self-build.ps1" v0.1.1
     if ($LASTEXITCODE) { throw 'self-build failed' }
     gh release download v0.1.1 --repo reubenbubu/skeleton-crew --pattern SHA256SUMS --dir $d
     if ($LASTEXITCODE) { throw 'gh release download failed' }
     $want = foreach ($l in Get-Content "$d\SHA256SUMS") { $h, $n = $l -split '  ', 2; if ($n -eq $bin) { $h } }
     if ((Get-FileHash "$d\src\dist\$bin" -Algorithm SHA256).Hash -ne $want) { throw "checksum mismatch: $bin" }
     New-Item -ItemType Directory -Force -Path "$HOME\.local\bin" | Out-Null
     Copy-Item "$d\src\dist\$bin" "$HOME\.local\bin\crew.exe" -Force
     ```

   - **`go`:** for a user who already has Go. Go checks the module against its checksum database and installs `crew` into its own `bin` directory.

     ```sh
     GOTOOLCHAIN=go1.27.2 CGO_ENABLED=0 go install -trimpath -ldflags '-s -w -buildid= -X main.version=v0.1.1' github.com/reubenbubu/skeleton-crew/cmd/crew@v0.1.1
     ```

     ```powershell
     $env:GOTOOLCHAIN = 'go1.27.2'; $env:CGO_ENABLED = '0'
     go install -trimpath -ldflags '-s -w -buildid= -X main.version=v0.1.1' github.com/reubenbubu/skeleton-crew/cmd/crew@v0.1.1
     ```

3. If the project has no `.crew/crew.toml`:
   1. Ask the user which profile it uses: `python`, `node-ts`, `java-spring`, or their own stack. Use a built-in profile only when the user names it or accepts one you proposed. If they describe a project instead, propose a profile and wait for their answer. If no built-in profile fits, write a custom profile first at `.crew/profiles/<name>/`, as "Custom profiles" in the README of tag `v0.1.1` of <https://github.com/reubenbubu/skeleton-crew> describes, and show it to the user.
   2. `crew` has no `--help`, and `crew init` takes flags only. Its usage is in "What `crew init` does" in the same README: `crew init --profile <name> [--install release|source|go] [--harness <name>] [--repo <owner>/<name>] [--public] [--json]`.
   3. Before you run `crew init`, tell the user what it will do: create the GitHub repository (private unless `--public`) when there is no `origin`, naming the owner and repository; create the labels there; and push the setup commit to `main`. Run it only after the user confirms. Do the same before any other command that changes their GitHub account.

   Then go to [Every session](#every-session).

When the user asks to upgrade Skeleton Crew, run `crew upgrade`, then start again at step 1.

Never fetch Skeleton Crew from a branch, and never run a command the user has not seen.

## Every session

1. Run `crew doctor`. If it reports a missing credential, show the user the command it prints and ask them to run it in their own terminal. Never ask for a credential in chat.
2. Run `crew brief <role>` and follow it. The user talks to a Project Manager (`pm`) or a Dev Manager (`dm`). If the user has not said which, ask whether they want to talk to the PM or the DM, and offer only those two. Never offer Dev or Reviewer: the lead DM starts them with their own briefing.
