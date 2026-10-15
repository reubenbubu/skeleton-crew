# Agent instructions

These instructions are for agents working on Skeleton Crew itself. The file shipped into users' projects is [`project/AGENTS.md`](project/AGENTS.md); do not confuse the two.

## Source of truth

[README.md](README.md) records the agreed design. Implement what it describes and nothing more. When a design decision changes, update the README in the same change. Do not add features, commands, options or configuration that the README does not describe; propose them to the user first.

## Principles

- **Simple wins.** Skeleton Crew runs inside the user's agent harness. Do not add daemons, services, background processes, databases or a control plane. GitHub Issues and git hold all state.
- **Harness-neutral.** Assume only that a harness reads `AGENTS.md`, runs shell commands and can start subagents. Never require harness plugins, hooks or MCP servers.
- **Stack-agnostic core.** Language and framework knowledge belongs in profiles, never in the core.
- **Every command earns its place.** The number of `crew` commands does not matter; each must have a clear purpose.
- **Zero tinkering for users.** Defaults must work. Users should never need to edit prompts or configuration.
- **Stand alone.** Describe what Skeleton Crew is. Do not refer to earlier projects, prior versions or the history of the design.

## Layout

| Path | Contents |
| --- | --- |
| `project/AGENTS.md` | The seed file users copy into their projects |
| `cmd/crew/` | The CLI entry point |
| `internal/` | CLI implementation packages |
| `profiles/` | Built-in profiles, embedded into the binary |

## Building and testing

- `crew` is written in Go and builds with `CGO_ENABLED=0` and `-trimpath`, so builds are reproducible.
- The user's machine needs no Go toolchain; the self-build path compiles inside the official Go container image.
- Tests must not touch real GitHub repositories. Use fakes or a local test server for the GitHub API.

## Output contract

`crew` output is for agents. Keep it terse and stable: no API structure, IDs, colours or decoration. Provide `--json` for structured output. Never alter ticket bodies or comments.

## Security

- Never handle, print or store user credentials. `crew doctor` tells the user which command to run in their own terminal.
- The bootstrap in `project/AGENTS.md` must pin an exact release and verify what it downloads. Never fetch from a moving branch.
