# Skeleton Crew

Skeleton Crew is a small team of AI agents that takes software from a requirement to merged, tested and deployed code. You give requirements to a Project Manager agent and discuss implementation with a Dev Manager agent. The rest of the team plans, builds, reviews and merges the work through GitHub.

It runs inside the agent harness you already use. There is no daemon, no service to install and no separate control plane. GitHub Issues hold the tickets, git holds the code, and a single CLI, `crew`, does the deterministic work.

> **Status: design stage.** This README records the agreed design. Nothing is released yet.

## The crew

| Role | You talk to it about | It does |
| --- | --- | --- |
| Project Manager (PM) | Requirements, priorities, status | Turns requirements into well-formed tickets and keeps them refined |
| Dev Manager (DM) | Implementation, architecture, trade-offs, debugging | Makes every technical decision, starts workers, triages problems and merges |
| Dev | Never; it reports to the DM | Implements one ticket in its own worktree and opens a PR |
| Reviewer | Never; it reports to the DM | Reviews every PR with fresh context and posts a verdict |

The PM owns *what and why*. The DM owns *how*. The DM never waits for you or the PM to make a technical decision, and never writes product code itself. It edits the repository only to resolve trivial merge conflicts and to record project knowledge.

## How it works

1. You send a requirement to the PM in conversation, or open a GitHub issue yourself.
2. The PM refines it with you until it has clear acceptance criteria, then marks it eligible.
3. The DM picks the next eligible ticket, chooses a model tier and starts a Dev.
4. The Dev works in its own git worktree and container environment, commits, and opens a PR.
5. `crew` runs the gate on the exact pushed commit. A Reviewer checks the PR against the ticket.
6. The DM merges approved PRs one at a time. If continuous deployment is configured, a GitHub Actions workflow deploys `main`.

## Getting started

Copy one file, [`project/AGENTS.md`](project/AGENTS.md), into your project and open your agent harness there. That file is the only thing you carry between projects.

The file holds a short bootstrap block pinned to an exact `crew` release. On first use the agent:

1. Checks whether the pinned `crew` version is installed.
2. If not, asks you how to install it:
   - **GitHub release (default):** downloads the pinned release binary and verifies its checksum and GitHub artifact attestation.
   - **Build from source:** clones the pinned tag and builds it inside the official Go container image, so you need no Go toolchain. Builds are reproducible, so a local build matches the release checksum byte for byte.
   - If you already have Go: `go install` at the pinned tag.
3. Asks you which profile the project uses, then runs `crew init` with your answers. `crew init` takes flags only; the agent has the conversation with you.
4. Runs `crew doctor`. Anything that needs a credential, such as GitHub authentication, is reported as a command for you to run in your own terminal. Agents never handle secrets.

The bootstrap block never fetches anything from a moving branch, and it shows the exact commands it runs. `crew upgrade` moves the pin to a newer release.

### Requirements

- Git, a container engine (Docker or Podman, with compose) and a GitHub account.
- An agent harness that reads `AGENTS.md`, runs shell commands, and lets an agent start subagents without your intervention.
- Linux or macOS. On Windows, use WSL.

No harness plugins, hooks or MCP servers are required.

## Tickets

Tickets are GitHub Issues. Each actionable ticket carries these labels:

| Label | Values |
| --- | --- |
| `stage:N:slug` | Exactly one; see [Ticket lifecycle](#ticket-lifecycle) |
| `priority:` | `low`, `medium`, `high`, `critical` |
| `kind:` | `feature`, `fix`, `chore` |
| `tier:` | The rank of the tier used for the current attempt, for example `tier:2` |
| `blocked` | Optional flag: waiting on something external or on you |

`critical` exists only to skip the queue. Agents set it only when you explicitly ask for it.

### Ticket lifecycle

The stage is always one clearly defined state. Its number never decreases; `crew` refuses any backward move. Stages that share a number form a loop.

| Stage | Owner | Meaning |
| --- | --- | --- |
| `1:intake` | PM | Created, for example by you on GitHub; waiting for the PM |
| `2:drafting` | PM | The PM is refining it, alone or with you |
| `3:eligible` | DM | Ready to be worked on |
| `4:in-progress` | Dev | A Dev is implementing it |
| `4:waiting-review` | Reviewer | The PR is ready and the gate has passed; waiting for the Reviewer |
| `4:review-rejected` | DM | The Reviewer rejected it; the DM starts a new attempt |
| `5:review-approved` | DM | Approved; the build-and-review loop is over |
| `6:pending-merge` | DM | Queued for merge into `main` |
| `7:merged` | Deploy workflow | Merged; deployment starts if configured |
| `8:deploying` | Deploy workflow | The deployment workflow is running |
| `9:done` | — | Finished and closed |
| `0:cancelled` | — | Will not be done; closed as not planned |

- Without continuous deployment, a ticket goes from `7:merged` straight to `9:done`.
- `blocked` is a flag, not a stage. A blocked ticket keeps its stage and is skipped by selection.
- Every stage has one owner and an expected dwell time. `crew status` flags tickets that sit too long, for example `#12 4:waiting-review since 2026-10-15T09:12Z: no reviewer`. This catches dropped handoffs without any background process.
- **Merge conflicts** at `6:pending-merge` stay at stage 6. The DM resolves trivial conflicts such as lockfiles and version lines itself. Otherwise it starts a Dev for the rebase fix. If resolving the conflict would change behaviour, the DM cancels the ticket and opens a new one.
- **Deployment failures:** if the pipeline or environment failed, the DM triages it and asks you only for credentials. If the code caused it, the original ticket goes to `9:done` and the DM opens a linked `fix` ticket at `high`. Fixes always go forward.

### Comments and terse views

Agents never call the GitHub API or `gh` directly. They go through `crew`, which returns compact views. It drops node IDs, URLs, label colours, reactions and other API structure. Labels become fields such as `stage=4:in-progress`. Times are absolute UTC timestamps, never relative ("2h ago" goes stale as soon as it is read). Ticket bodies and comments are never altered.

```
$ crew ticket show 12
#12 Login with email
stage=4:in-progress priority=high kind=feature tier=2 pr=#31
---
<body, verbatim>
--- timeline
2026-10-14T16:40Z eligible · 2026-10-15T08:05Z in-progress · 2026-10-15T09:12Z PR #31
--- comments
[decision] DM 2026-10-15T08:10Z: Reuse the session table; no new migration.
[question] DEV 2026-10-15T08:52Z: Should expired links show an error page or redirect?
[human] user 2026-10-15T09:30Z: Redirect to login with a notice.
```

Routine progress is recorded as label changes, which GitHub keeps in the issue timeline, not as comments. Agents post comments only through `crew ticket comment --kind <kind>`. The kind is stored as an invisible marker in the comment:

| Kind | Shown by `crew ticket show` |
| --- | --- |
| `decision`, `question`, `answer`, `review` | In full |
| `log` | Collapsed into the timeline |
| Human comments | In full |

`--all` shows the full thread.

## Selecting work

`crew ticket next` lists the eligible tickets that are not blocked and whose dependencies are done. They are sorted by priority, then by age.

- By default the DM takes the first one.
- The DM may skip down the list when the top ticket would collide with work in flight, for example two tickets that both change the same module. It records the reason as a `log` comment.
- If every candidate would collide, the slot stays idle until a merge lands.

## Parallelism

Several Devs can work at once, up to a cap in `crew.toml` (default 3). Each ticket in flight has its own:

- git worktree and branch,
- compose project (`crew-<ticket>`) with automatically assigned ports.

Merges into `main` happen one at a time. The DM rebases the remaining PRs when `main` moves.

A `critical` ticket gets one extra slot immediately when all slots are busy. Nothing in flight is interrupted.

## Models

The DM chooses a tier for each Dev based on the ticket's complexity. A tier is a numbered rank with a model and an effort level. You control the cost and quality trade-off in `crew.toml`. These are the defaults:

```toml
[tiers.1]
name   = "light"
model  = "<model id>"
effort = "medium"

[tiers.2]
name   = "standard"
model  = "<model id>"
effort = "medium"

[tiers.3]
name   = "heavy"
model  = "<model id>"
effort = "high"

[models]
reviewer = 2   # rank the Reviewer always uses
max_tier = 3   # escalation stops here; defaults to the highest rank
```

- Ranks run from 1 upwards with no gaps. You may define as many as you like; `crew doctor` checks the list.
- `name` is optional. It helps the DM and you tell ranks apart.
- Each failed attempt, such as a review rejection or a Dev reporting it is stuck, moves the next attempt up one rank automatically.
- When a ticket fails at `max_tier`, the DM decides what to do: split the ticket, rethink the approach, or ask you.
- You set the model and effort of the PM and DM yourself, in your harness session. `crew` sets them for Devs and Reviewers.
- `crew init` fills in default models for your harness. You can also tell the DM in conversation, for example "use cheaper models" or "use rank 3 for #12".
- Effort levels are `low`, `medium`, `high` and `max`. `crew` maps them to each harness's own settings.
- If a harness cannot set effort per subagent, only the model applies. If it cannot set the model either, every agent uses the session's settings.

## Review and merge

- Every ticket gets a Reviewer. It starts with fresh context: the ticket, the acceptance criteria and the diff, not the Dev's reasoning.
- The Reviewer posts its verdict as a PR review. The verdict is advisory; the DM decides whether to merge.
- The gate decides whether code can merge. See [Gates](#gates).
- The DM merges by pushing the gated commit to `main` as a fast-forward. GitHub then marks the PR as merged. There are no merge commits, and GitHub's own rebase merge is not used because it rewrites commits, so `main` would hold commits the gate never tested.

## Gates

There is no CI gate. `crew` runs the gate locally, and the result does not depend on any agent's word.

- When a Dev submits its work with `crew ticket submit`, `crew` checks out the exact pushed commit into a clean temporary worktree and runs the profile's `build` and `test` through compose.
- On success the ticket moves to `4:waiting-review`. On failure it stays at `4:in-progress` and the Dev gets the failing output.
- `crew` posts the result as a commit status (`crew/gate`) on that commit, so it shows on the PR. Commit statuses work on every GitHub plan and need no Actions.
- At `6:pending-merge`, if `main` has moved, `crew` rebases the branch and runs the gate again on the new commit before merging. Two PRs that pass separately cannot break `main` together.
- Gate runs use the local machine, so the parallel cap also limits gate load.
- Users may add their own CI. `crew` does not depend on it.

## Staying in sync with GitHub

The local repository always matches GitHub.

- Every `crew` command that reads `main` or a branch fetches from GitHub first.
- Local `main` is never committed to. It only fast-forwards to `origin/main`. If it has diverged, `crew` refuses and reports why.
- Every commit `crew` makes on a ticket branch is pushed immediately, so a lost session loses no work.

## Profiles and containers

The core of Skeleton Crew knows nothing about languages or frameworks. A **profile** describes a stack:

- commands: `setup`, `test`, `build`, `run`, `health`
- a `Containerfile` and a `compose.yaml` for the app and its services
- an optional deployment workflow (GitHub Actions)
- default conventions and architecture decisions
- a starter scaffold

Built-in profiles: **python**, **node-ts**, **java-spring**. At bootstrap you can choose one of them or describe your own stack, and the DM writes a custom profile for it.

The application always runs in containers, never directly on the host. Agents run on the host through your harness, but everything the code needs runs through compose: tests, builds, the app and its database. Your machine needs no language toolchains.

## Deployment

Skeleton Crew never deploys anything itself. A profile may include a GitHub Actions deployment workflow that runs on every push to `main`. The crew only reads that workflow's status to move tickets through `8:deploying` to `9:done`. Without a deployment workflow, merging finishes the ticket.

## Prompts and project knowledge

You never need to edit configuration or prompts. Everything works with defaults, and the agents adapt to your project as they go.

- **Rules** are built into `crew` and delivered through `crew brief <role>`. They cover role duties, the workflow and the hard rules. Agents cannot change them; they change only with a new release.
- **Project knowledge** lives in the repository and is written by the agents: architecture notes, conventions, gotchas, decisions, a product glossary and your stated preferences.
  - Devs add what they learn in their ticket's PR.
  - The DM records decisions from conversations with you in knowledge-only PRs.
  - Knowledge is curated, not an append-only log, and kept within a size budget.
- `crew brief dev <ticket>` includes the relevant knowledge, so every new agent starts informed.
- If project knowledge contradicts a built-in rule, the rule wins, and `crew doctor` reports the conflict.

## The `crew` CLI

`crew` is a single Go binary with no runtime dependencies. Built-in profiles, templates and role rules are embedded in it, so no framework code is copied into your project. Commands print terse, stable output for agents, and structured output with `--json`.

## Not yet designed

- The bootstrap conversation in detail: `crew init`, labels and repository setup.
- The full `crew` command list and its output contract.
- Session recovery: what the DM does at startup when tickets are mid-flight.
- Where project knowledge lives and how briefings select from it.
