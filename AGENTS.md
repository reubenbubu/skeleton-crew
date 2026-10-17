# Agent instructions

These instructions are for agents working on Skeleton Crew itself. The file shipped into users' projects is [`project/AGENTS.md`](project/AGENTS.md); do not confuse the two.

## Source of truth

[README.md](README.md) records the agreed design. Implement what it describes and nothing more. When a design decision changes, update the README in the same change. Do not add features, commands, options or configuration that the README does not describe; propose them to the user first.

## Principles

- **Simple wins.** Skeleton Crew runs inside the user's agent harness. Do not add daemons, services, background processes, databases or a control plane. GitHub Issues and git hold all state.
- **Harness-neutral.** Assume only that a harness reads `AGENTS.md`, runs shell commands and can start subagents. Never require harness plugins, hooks or MCP servers. Optional harness wiring is written by the user's agent against the contract in the README; `crew` ships no harness adapters.
- **Stack-agnostic core.** Language and framework knowledge belongs in profiles, never in the core.
- **Every command earns its place.** The number of `crew` commands does not matter; each must have a clear purpose.
- **Scripts do the deterministic work; the DM handles the exceptions.** `crew` covers the normal path and the invariants. When it meets a situation outside the normal path, it changes nothing, stops and reports what it found. The DM handles the case with judgement, and may use `git` and `gh` directly, but must still respect the invariants. A corner case that keeps recurring is a candidate for a `crew` release.
- **Never a victim of its own workings.** No lock, limit or internal state may leave the crew unable to progress. Every one needs a recovery path that takes at most a confirmation from the user.
- **Zero tinkering for users.** Defaults must work. Users should never need to edit prompts or configuration.
- **Stand alone.** Describe what Skeleton Crew is. Do not refer to earlier projects, prior versions or the history of the design.

## Layout

| Path | Contents |
| --- | --- |
| `project/AGENTS.md` | The seed file users copy into their projects |
| `cmd/crew/` | The CLI entry point |
| `internal/` | CLI implementation packages |
| `profiles/` | Built-in profiles, embedded into the binary |
| `.github/workflows/release.yml` | The release workflow: builds, checksums, attests and publishes a tagged release |
| `scripts/` | `go.sh` runs Go in the pinned container image; `test.sh` runs the tests; `test-containers.sh` runs the tests that need a container engine; `self-build.sh` builds `crew`; `go.ps1` and `self-build.ps1` do what `go.sh` and `self-build.sh` do, from PowerShell |
| `docs/sessions/` | Session recaps: state, open questions and next steps for picking work back up |

## Navigating the code

- Use the codebase-memory MCP server for structural questions about the code, both in the main checkout and in every worktree: `search_graph` to find symbols, `trace_path` for callers and callees, `get_code_snippet` for source. Use grep and file reads for literals, configuration and files that are not code.
- A worktree's code differs from the main checkout's. Before querying in a worktree, index it with `index_repository` on the worktree's own path, and query its project, never the main checkout's.
- Call `check_index_coverage` for every file a conclusion relies on, and read the lines it reports as missed.

## Building and testing

- `crew` is written in Go, using the standard library only. The module path is `github.com/reubenbubu/skeleton-crew`.
- Go is pinned to 1.27.2, and the container image to `golang:1.27.2` by digest in `scripts/go.sh`. Change both together. `scripts/go.ps1` reads the image from `scripts/go.sh`.
- Every build uses `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false` and `-ldflags "-s -w -buildid= -X main.version=<version>"`, so builds of the same commit are byte-identical.
- The user's machine needs no Go toolchain; `scripts/self-build.sh` compiles inside the official Go container image. Run tests with `scripts/test.sh`; it fails if any test takes longer than one second.
- `crew` supports Linux, macOS and Windows. Keep the code portable: build paths with `filepath`, convert with `filepath.ToSlash` for output and git, and never run a host shell; `sh -c` runs only inside containers. A test that needs a stand-in for a program `crew` runs copies the test binary, as `internal/github/exec_test.go` does, rather than writing a shell script. The release workflow runs the tests on Windows, and runs only them when started by hand. Keep `scripts/go.ps1` and `scripts/self-build.ps1` in step with their `sh` versions, and keep them and the bootstrap's PowerShell blocks within Windows PowerShell 5.1.
- Commands take time from the injectable clock in `internal/clock` and never call the `time` package for the current time or for waiting. Every polling interval is a parameter.
- Tests that need a container engine carry the `containers` build tag, so a plain `go test ./...` needs no containers. Only a binary built with that tag lets `compose.Default` run in a test. `scripts/test-containers.sh` builds those test binaries in the Go container image and runs them on the host, where the engine is; they are exempt from the one-second limit. Keep them few and small.
- Automated tests must not touch real GitHub repositories. All GitHub access goes through `internal/github`, which runs `gh api`. Tests give it the in-memory fake in `internal/github/fakegithub`, which stands in for `gh` and answers with GitHub's JSON and errors. Its `Hook` lets a test change state between two requests, for example to simulate a label race.
- In a test binary, `crew` refuses to run a `gh` outside the temporary directory and allows git only local remotes, so a test that would reach a real GitHub host fails. Git tests use local bare repositories as `origin`. A test that needs a GitHub URL as `origin` sets `GIT_CONFIG_GLOBAL` to a file whose `url.<base>.insteadOf` points that URL at a local bare repository.
- Field tests run the whole pipeline against a real repository: the agent plays the user, sends requests to a PM or DM session, and checks the tickets, PRs, gates, reviews and merges that result. Each field test creates its own private repository with a randomized name and deletes it when the test is done. Never use one of the user's existing repositories.
- A field test that checks a harness, or a harness version, records it in "Validated harnesses" in the README.
- A field test runs a binary from `scripts/self-build.sh <version>`, placed first on `PATH` from a scratch directory, never in `~/.local/bin`, where the bootstrap would take it for a verified install. The PM and DM sessions run headless in the project's directory, for example `claude -p "<request>" --session-id <id>` and then `--resume <id>` for each later message. A headless session cannot be woken during its turn, so its DM skips the wiring self-test and starts Devs and Reviewers as foreground subagents; the agent running the test does the self-test itself as lead.

## Releasing

Release only when the user asks. Versions are `v<major>.<minor>.<patch>`: a new minor version for new behaviour, a new patch version for fixes only.

1. Start on `main`, clean and level with `origin/main`. `scripts/test.sh` passes; run `scripts/test-containers.sh` too when the gate, compose or profiles changed. When the code changed since the last release, run the Windows tests first with `gh workflow run release.yml --ref main`, which runs only the `windows` job, and follow it with `gh run watch`.
2. Replace every mention of the previous version in `project/AGENTS.md` with the new one, and commit it as `Pin crew <version> in the seed AGENTS.md.` The tagged commit must pin its own version, so the seed file at a tag installs that tag.
3. Tag a commit that holds the pin with an annotated tag, and push `main` and the tag together:

   ```sh
   git tag -a <version> -m <version>
   git push origin main <version>
   ```

4. The tag starts `.github/workflows/release.yml`. Follow it with `gh run watch` until it ends. The `windows` job must pass before the `release` job builds, attests and publishes.
5. If the workflow fails before it publishes, no release exists: fix the cause on `main`, delete the tag locally and on `origin` (`git push origin :refs/tags/<version>`), and tag again. Never move or delete the tag of a published release; publish a new patch version instead.
6. Verify the release as the bootstrap does, from a scratch directory: download `crew-linux-amd64` and `SHA256SUMS`, check the checksum, run `gh attestation verify` with `--signer-workflow reubenbubu/skeleton-crew/.github/workflows/release.yml` and `--source-ref refs/tags/<version>`, and check that `scripts/self-build.sh <version>` from a fresh clone of the tag matches `SHA256SUMS` byte for byte.
7. Record the release in the session recap: the version, its UTC publish time, the tagged commit and what was verified.

## Output contract

`crew` output is for agents. Keep it terse and stable: no API structure, IDs, colours or decoration. Times are absolute UTC timestamps, never relative. Provide `--json` for structured output. Never alter ticket bodies or comments.

## Security

- Never handle, print or store user credentials. `crew doctor` tells the user which command to run in their own terminal.
- The bootstrap in `project/AGENTS.md` must pin an exact release and verify what it downloads. Never fetch from a moving branch.
