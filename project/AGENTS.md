# Agent instructions

This project is run by Skeleton Crew: a Project Manager, a Dev Manager, Devs and Reviewers.

## Bootstrap

Skeleton Crew pins `crew` version `v0.1.1`.

1. Run `crew --version`. If it prints `v0.1.1`, go to step 3.
2. Otherwise, install `crew` `v0.1.1` with one of the methods below. If `.crew/crew.toml` has an `install` line, use the method it names. If not, ask the user which method to use; `release` is the default. Show the user the exact commands before running them, and stop if any of them fails. If `~/.local/bin` is not on the user's `PATH`, tell them.
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

   - **`source`:** clones the tag, builds it inside the official Go container image, checks that the build matches the release checksum byte for byte, and installs it. It needs a container engine but no Go toolchain.

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

   - **`go`:** for a user who already has Go. Go checks the module against its checksum database and installs `crew` into its own `bin` directory.

     ```sh
     GOTOOLCHAIN=go1.27.2 CGO_ENABLED=0 go install -trimpath -ldflags '-s -w -buildid= -X main.version=v0.1.1' github.com/reubenbubu/skeleton-crew/cmd/crew@v0.1.1
     ```

3. If the project has no `.crew/crew.toml`, ask the user which profile it uses (`python`, `node-ts`, `java-spring`, or a description of their own stack), then run `crew init --install <method>` with their answers and the method used in step 2. For their own stack, first write a custom profile at `.crew/profiles/<name>/`, as "Custom profiles" in the README of tag `v0.1.1` of <https://github.com/reubenbubu/skeleton-crew> describes, and pass `--profile <name>`. Otherwise go to [Every session](#every-session).

When the user asks to upgrade Skeleton Crew, run `crew upgrade`, then start again at step 1.

Never fetch Skeleton Crew from a branch, and never run a command the user has not seen.

## Every session

1. Run `crew doctor`. If it reports a missing credential, show the user the command it prints and ask them to run it in their own terminal. Never ask for a credential in chat.
2. Run `crew brief <role>` and follow it. If the user has not said which role you are, ask them.
