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
| `scripts/` | `go.sh` runs Go in the pinned container image; `test.sh` runs the tests; `test-containers.sh` runs the tests that need a container engine; `self-build.sh` builds `crew` |
| `docs/sessions/` | Session recaps: state, open questions and next steps for picking work back up |

## Building and testing

- `crew` is written in Go, using the standard library only. The module path is `github.com/reubenbubu/skeleton-crew`.
- Go is pinned to 1.27.2, and the container image to `golang:1.27.2` by digest in `scripts/go.sh`. Change both together.
- Every build uses `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false` and `-ldflags "-s -w -buildid= -X main.version=<version>"`, so builds of the same commit are byte-identical.
- The user's machine needs no Go toolchain; `scripts/self-build.sh` compiles inside the official Go container image. Run tests with `scripts/test.sh`; it fails if any test takes longer than one second.
- Commands take time from the injectable clock in `internal/clock` and never call the `time` package for the current time or for waiting. Every polling interval is a parameter.
- Tests that need a container engine carry the `containers` build tag, so a plain `go test ./...` needs no containers. Only a binary built with that tag lets `compose.Default` run in a test. `scripts/test-containers.sh` builds those test binaries in the Go container image and runs them on the host, where the engine is; they are exempt from the one-second limit. Keep them few and small.
- Automated tests must not touch real GitHub repositories. All GitHub access goes through `internal/github`, which runs `gh api`. Tests give it the in-memory fake in `internal/github/fakegithub`, which stands in for `gh` and answers with GitHub's JSON and errors. Its `Hook` lets a test change state between two requests, for example to simulate a label race.
- In a test binary, `crew` refuses to run a `gh` outside the temporary directory and allows git only local remotes, so a test that would reach a real GitHub host fails. Git tests use local bare repositories as `origin`. A test that needs a GitHub URL as `origin` sets `GIT_CONFIG_GLOBAL` to a file whose `url.<base>.insteadOf` points that URL at a local bare repository.
- Field tests run the whole pipeline against a real repository: the agent plays the user, sends requests to a PM or DM session, and checks the tickets, PRs, gates, reviews and merges that result. Each field test creates its own private repository with a randomized name and deletes it when the test is done. Never use one of the user's existing repositories.

## Output contract

`crew` output is for agents. Keep it terse and stable: no API structure, IDs, colours or decoration. Times are absolute UTC timestamps, never relative. Provide `--json` for structured output. Never alter ticket bodies or comments.

## Security

- Never handle, print or store user credentials. `crew doctor` tells the user which command to run in their own terminal.
- The bootstrap in `project/AGENTS.md` must pin an exact release and verify what it downloads. Never fetch from a moving branch.
