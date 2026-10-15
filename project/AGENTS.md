# Agent instructions

This project is run by Skeleton Crew: a Project Manager, a Dev Manager, Devs and Reviewers.

## Bootstrap

Skeleton Crew pins `crew` version `v0.1.0` (not yet released).

1. Run `crew --version`. If it prints `v0.1.0`, go to [Every session](#every-session).
2. Otherwise, ask the user how to install `crew`. Show them the exact command before running it:
   - **GitHub release (default):** download the `v0.1.0` binary from <https://github.com/reubenbubu/skeleton-crew/releases/tag/v0.1.0>, then verify its checksum and its GitHub artifact attestation before installing it.
   - **Build from source:** clone tag `v0.1.0` of <https://github.com/reubenbubu/skeleton-crew> and run its self-build script, which builds inside the official Go container image.
3. If the project has no `crew.toml`, ask the user which profile it uses (`python`, `node-ts`, `java-spring`, or a description of their own stack), then run `crew init` with their answers.

Never fetch Skeleton Crew from a branch, and never run a command the user has not seen.

## Every session

1. Run `crew doctor`. If it reports a missing credential, show the user the command it prints and ask them to run it in their own terminal. Never ask for a credential in chat.
2. Run `crew brief <role>` and follow it. If the user has not said which role you are, ask them.
