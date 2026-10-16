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

### What `crew init` does

1. **Repository:** uses the existing git repository and its `origin`. If there is none, it runs `git init` and creates the GitHub repository, private by default.
2. **Labels:** creates the `stage:`, `priority:`, `kind:`, `tier:` and `testing:` labels and `blocked`. Running it again is safe.
3. **Crew files:** writes `.crew/crew.toml`, with default models for the detected harness, seeds `.crew/docs/` from the profile, and adds `.crew/worktrees/` and `.crew/logs/` to `.gitignore`.
4. **Application files:** for a new project only, writes the profile's `Containerfile`, `compose.yaml` and starter scaffold.
5. **Commit:** commits only the files it created and pushes them to `main`. Your own uncommitted changes are left alone.
6. **Check:** runs `crew doctor`, including one gate run.

### Existing projects

Skeleton Crew works on a project that already has code. For an existing codebase, the agent suggests a profile from what it finds, and `crew init` skips the scaffold and keeps any existing `Containerfile` and `compose.yaml`.

The DM then opens an **onboarding ticket**, which goes through the normal flow. A Dev studies the codebase and writes `architecture.md`, `conventions.md` and the first ADRs from what is really there. If the project has no `Containerfile` or `compose.yaml`, the Dev adds them, and it makes the gate pass.

### Requirements

- Git, a container engine (Docker or Podman, with compose) and a GitHub account.
- An agent harness that reads `AGENTS.md`, runs shell commands, and lets an agent start subagents without your intervention.
- Linux or macOS. On Windows, use WSL.

No harness plugins, hooks or MCP servers are required. [Harness wiring](#keeping-the-lead-dm-running) lets the lead DM work without prompting. The lead DM checks for it itself; it needs nothing installed when the harness can wake an agent on a background command's exit.

## Tickets

Tickets are GitHub Issues. Each actionable ticket carries these labels:

| Label | Values |
| --- | --- |
| `stage:N:slug` | Exactly one; see [Ticket lifecycle](#ticket-lifecycle) |
| `priority:` | `low`, `medium`, `high`, `critical` |
| `kind:` | `feat`, `fix`, `chore` |
| `tier:` | The rank of the tier used for the current attempt, for example `tier:2` |
| `testing:` | Optional: overrides the project's testing level for this ticket |
| `blocked` | Optional flag: waiting on something external or on you |

`critical` exists only to skip the queue. Agents set it only when you explicitly ask for it.

### Ticket body

`crew ticket new` writes this skeleton, and `crew ticket ready` checks that the goal and the acceptance criteria are filled in:

```markdown
## Goal
## Acceptance criteria
- [#12-AC1] ...
## Out of scope
## Assumptions
## Non-functional
## Risks
```

### Parent tickets

The PM splits a large requirement into a parent ticket and smaller child tickets with `crew ticket new --parent <n>`. Parents use GitHub's own sub-issues. A parent has no stage, is never worked on directly, and is closed by `crew status` once all its children are done or cancelled.

### Ticket lifecycle

The stage is always one clearly defined state. Its number never decreases; `crew` refuses any backward move. Stages that share a number form a loop.

| Stage | Owner | Meaning |
| --- | --- | --- |
| `1:intake` | PM | Created, for example by you on GitHub; waiting for the PM. `crew status` labels new unlabelled issues with this stage. |
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
stage=4:in-progress priority=high kind=feat tier=2 pr=#31
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

## Status

`crew status` shows each role what needs its attention. The PM and the DM run it at the start of every session.

- **Everyone:** questions waiting for you, from `blocked` tickets and tickets that reached the escalation limit.
- **PM:** new `1:intake` tickets, tickets in `2:drafting`, and DM decisions the PM has not acknowledged with `crew ticket ack`.
- **Lead DM:** unanswered Dev questions, the board, stale handoffs and recovery proposals. It also advances `7:merged` through `8:deploying` to `9:done` from the deployment workflow's status.

Open issues without a stage label, such as issues you create on GitHub, are labelled `1:intake` the first time `crew status` sees them.

## Selecting work

`crew ticket next` lists the eligible tickets that are not blocked and whose dependencies are done. Dependencies use GitHub's own issue dependencies ("blocked by"), which `crew ticket edit` sets. They are sorted by priority, then by age.

- By default the DM takes the first one.
- The DM may skip down the list when the top ticket would collide with work in flight, for example two tickets that both change the same module. It records the reason as a `log` comment.
- If every candidate would collide, the slot stays idle until a merge lands.

## Parallelism

Several Devs can work at once, up to a cap in `.crew/crew.toml` (default 3). Each ticket in flight has its own:

- git branch and worktree; see [Conventions](#conventions),
- compose project (`crew-<ticket>`) with automatically assigned ports.

Merges into `main` happen one at a time. Branches are rebased only at merge time, at `6:pending-merge`. Devs never rebase while they work.

A `critical` ticket gets one extra slot immediately when all slots are busy. Nothing in flight is interrupted.

## Models

The DM chooses a tier for each Dev based on the ticket's complexity. A tier is a numbered rank with a model and an effort level. You control the cost and quality trade-off in `.crew/crew.toml`. These are the defaults:

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

[escalation]
top_attempts = 2   # retries at max_tier before the ticket comes to you
```

- Ranks run from 1 upwards with no gaps. You may define as many as you like; `crew doctor` checks the list.
- `name` is optional. It helps the DM and you tell ranks apart.
- Each failed attempt, such as a review rejection or a Dev reporting it is stuck, moves the next attempt up one rank automatically.
- A Dev that needs a decision runs `crew ticket ask`. That is not a failed attempt. The DM answers with an `answer` comment, or takes a requirements question to the PM or to you, and then starts a Dev at the same rank.
- When a ticket fails at `max_tier`, the DM decides what to do: rethink the approach and retry, split the ticket, or ask you.
- Retries at `max_tier` are limited by `[escalation] top_attempts` (default 2). `crew` counts them from the ticket's label history. Past the limit, `crew` refuses to start another Dev, sets `blocked` and posts a `question` comment summarising what was tried, so the ticket comes to you.
- Splitting does not use the retry budget, because it changes the plan. The new tickets start at `2:drafting`, so the PM writes their acceptance criteria, and their first attempt uses rank 1. A ticket created by a split cannot be split again; if it reaches the limit, it comes to you.
- You set the model and effort of the PM and DM yourself, in your harness session. `crew` sets them for Devs and Reviewers.
- `crew init` fills in default models for your harness. You can also tell the DM in conversation, for example "use cheaper models" or "use rank 3 for #12".
- Effort levels are `low`, `medium`, `high` and `max`. `crew` maps them to each harness's own settings.
- If a harness cannot set effort per subagent, only the model applies. If it cannot set the model either, every agent uses the session's settings.

## Testing

Agents tend to write too many tests. You choose how much testing the crew adds, in `.crew/crew.toml`:

```toml
[testing]
level = "standard"   # minimal | standard | thorough
```

| Level | Devs write |
| --- | --- |
| `minimal` | One test per acceptance criterion, happy path only. No tests for trivial code such as getters, wiring or configuration. |
| `standard` | Each acceptance criterion, plus the error paths and edge cases that could realistically break. Unit tests only for non-trivial logic. |
| `thorough` | Everything in `standard`, plus boundary values, failure modes and integration tests across components. |

- Every test names the acceptance criterion or risk it covers. A test without a reason is excess.
- The level is part of every Dev and Reviewer briefing.
- The Reviewer rejects missing coverage **and** over-testing: redundant cases, tests of trivial code, or tests beyond the level.
- A ticket can override the project level with a `testing:` label, for example `testing:thorough` on a payment ticket. The PM or DM sets it when you ask, and the DM may raise it for clearly risky work.
- You can also just tell the DM, for example "keep tests lean".

## Test-driven development

Agents make mistakes, and tests written first are the guardrail that catches them. TDD is mandatory at every testing level. The level only decides how many tests go beyond the acceptance criteria.

### The PM sets the expectations

- Every ticket has acceptance criteria with IDs that are unique across the project, such as `#12-AC1` and `#12-AC2`.
- Criteria describe observable behaviour, not implementation, so they can be tested.
- `crew ticket ready` refuses a ticket without them.
- The PM never renumbers criteria. A changed requirement revises or removes a criterion, and the tests that cover it are updated in the same ticket.

### Devs write failing tests first

`crew` checks each TDD step by running the tests, not by trusting the commit message:

| Command | `crew` requires |
| --- | --- |
| `crew commit --red "<message>"` | The commit changes only test files, and the tests **fail** |
| `crew commit --green "<message>"` | The tests pass |
| `crew commit --refactor "<message>"` | The tests stay green, and no behaviour tests are added |

- Every test names the criterion it covers, such as `#12-AC1`.
- At `crew ticket submit`, the gate checks that every criterion of the ticket has a test that failed before it passed.
- `crew` records each red result as a `log` comment on the ticket, with the test names and a failure excerpt. The record is keyed by test, not by commit, so it survives rebases. The Reviewer checks that the test failed for the right reason: an assertion about the behaviour, not a typo or a missing import.
- Changes with no behaviour, such as documentation, configuration and knowledge files, use a plain `crew commit`. The Reviewer judges whether a change really has no behaviour.
- Red and green steps run the profile's fast `test-changed` command. The full suite runs at the gate.

### Obsolete tests are maintained

- Tests name their criteria, so `crew` finds tests whose criterion was removed, revised or belongs to a cancelled ticket. The gate flags them, and the ticket that changed the requirement updates or deletes them in the same PR.
- The gate refuses skipped or disabled tests.
- A commit that changes or deletes an existing test must give the reason, such as the criterion revision. The Reviewer rejects any test weakened just to make it pass.

## Conventions

| Item | Format | Example |
| --- | --- | --- |
| Branch | `<kind>/<ticket>/<slug>` | `feat/12/login-with-email` |
| Worktree | `.crew/worktrees/<kind>/<ticket>/<slug>` | `.crew/worktrees/feat/12/login-with-email` |
| Commit | `[<kind>:#<ticket>] <phase>: <message>` | `[feat:#12] red: login rejects an unknown email` |

- `<kind>` is `feat`, `fix` or `chore`.
- `<phase>` is `red`, `green` or `refactor`, and is left out for changes with no behaviour.
- `crew` creates branches and worktrees and writes the commit prefix, so agents never type it. `#<ticket>` makes GitHub link the commit to the issue.

## Review and merge

- Every ticket gets a Reviewer. It starts with fresh context: the ticket, the acceptance criteria and the diff, not the Dev's reasoning.
- The Reviewer posts its verdict as a PR review. The verdict is binding: an approval goes to merge, and a rejection means a new attempt.
- A rejection the DM believes is wrong is an exception for the DM to judge, but the DM cannot overrule the Reviewer alone. It runs `crew ticket appeal <n> "<reason>"`, which moves the ticket from `4:review-rejected` back to `4:waiting-review` for a second Reviewer. That Reviewer starts fresh and sees the first rejection and the DM's reason. If it approves, the ticket goes to `5:review-approved`. If it rejects, the rejection stands.
- An appeal is not a failed attempt and does not change the rank. Each attempt allows one appeal.
- The gate decides whether code can merge. See [Gates](#gates).
- The DM merges by pushing the gated commit to `main` as a fast-forward. GitHub then marks the PR as merged. There are no merge commits, and GitHub's own rebase merge is not used because it rewrites commits, so `main` would hold commits the gate never tested.

## Gates

There is no CI gate. `crew` runs the gate locally, and the result does not depend on any agent's word.

- When a Dev submits its work with `crew ticket submit`, `crew` checks out the exact pushed commit into a clean temporary worktree and runs, through compose:
  1. `build`
  2. `lint`: formatting, static analysis and architecture rules
  3. `test`: the full suite
  4. the ticket's compose stack, waiting until `health` succeeds
  5. `e2e` against that running stack
- A profile leaves out the steps that do not apply to its stack.
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

## Lead DM and conversational DMs

You can have several DM sessions open at once, but only one of them, the **lead**, acts on work.

- A DM session starts with `crew dm claim`. If no one holds the lead, it gets it, along with a token that its lead-only commands carry.
- Only the lead starts Devs and Reviewers, merges, cancels and runs recovery. `crew` refuses these from any other session with `not-lead`.
- Every other DM is **conversational**. It discusses implementation with you, records decisions, creates tickets from your requests and opens knowledge-only PRs. It never starts or merges work.
- `crew dm release` gives up the lead when the lead session ends normally.

### The lead can never lock the system

No internal mechanism may stop the crew from making progress. For the lead this means:

- **Takeover always works.** Any DM can take the lead with `crew dm claim --takeover` once you confirm. The old token stops working immediately. A dead session or a lost token costs at most one confirmation from you.
- A lead that has lost its own token takes over from itself the same way.
- If the old lead was still alive, its next lead-only command gets `not-lead` and it continues as a conversational DM. No work is lost, because all state is on GitHub.
- Every state change is a check-and-set on the stage label, such as "move #12 from 5 to 6 only if it is still at 5". If two DMs race during a takeover, one succeeds and the other gets `stage-changed`. Nothing happens twice.
- A missing or unreadable lock file counts as "no lead", with a warning, so the next claim succeeds.
- `crew` never decides on its own that a session is dead. Nothing moves without an open session, and an open session means you are there to confirm.

## Keeping the lead DM running

An agent session only acts when it has a turn. Without help, the lead DM stops when it runs out of work and waits for a prompt, even after new work becomes eligible. Keeping it in a long polling loop is not acceptable either, because every idle check would grow its context.

`crew` ships no harness adapters. It provides one command to wait on, and it defines what any wiring must achieve. The lead DM checks its harness every time it starts and uses the lightest wiring that works.

### Waiting for events

`crew events` reports what changed since the previous call. It is the single signal the wiring triggers on.

`crew events --wait` blocks until there is something unread, then exits without printing anything or marking anything read. Some harnesses can run a command in the background and wake the agent when it exits. In such a harness this is the whole wiring, and nothing is installed:

1. The lead DM starts `crew events --wait` in the background and ends its turn.
2. When something happens, the command exits and the harness wakes the lead DM.
3. The lead DM runs `crew events`, does the work, and starts `crew events --wait` again.

Waiting costs the lead DM's context nothing, because the waiting happens in the shell.

Some harnesses run background commands in a long-lived service of their own, so a wait can outlive the session that started it. Two rules make a leftover wait harmless:

- `--wait` never marks events read, so a leftover wait cannot swallow an event meant for the next lead DM.
- Each `--wait` replaces the previous one. A wait that has been replaced exits quietly, so at most one wait runs, and a leftover stops as soon as a new lead DM starts waiting.

### Checking the harness

Every time a lead DM session starts, it checks its wiring:

1. It identifies the harness it is running in.
2. It looks the harness up in `.crew/crew.toml`:
   - **Recorded as supporting `crew events --wait`:** it uses it.
   - **Recorded with its own wiring:** it uses that wiring.
   - **Not recorded:** it runs the self-test.
3. **Self-test.** It tells you: "If I have not continued within a minute, send me any message." It starts `crew events --wait --test` in the background, which exits after a few seconds as if an event had arrived, and ends its turn.
   - **Woken by the harness:** it records that the harness supports `crew events --wait`.
   - **Not woken:** when you send a message, it sees that the test event fired without waking it. It writes its own wiring with the harness's features, shows it to you and installs it only when you confirm. Then it records that wiring.

Records are kept per harness, so a project used from several harnesses has one record for each. A record can go stale, for example when a harness update removes a feature. The lead DM then stops waking by itself. When you next prompt it, it finds events that should have woken it, runs the self-test again and updates the record.

### What wiring must do

Whether it is `crew events --wait` or the lead DM's own, the wiring must:

1. **Be event-driven.** The lead DM wakes only when something happened: a Dev or Reviewer finished, a ticket became eligible, a question was answered, or a handoff went stale. Context then grows with work done, not with time. Compaction is safe because all state is on GitHub.
2. **Live inside the harness.** No cron jobs, system services or other processes outside the harness. A wait left behind when a session ends may keep running inside the harness until it is replaced.
3. **Leave you in control.** It stops when the session closes or when you say so, and never blocks you from talking to a DM.
4. **Be verifiable.** `crew doctor` checks that `.crew/crew.toml` records wiring for the harness in use. The self-test is the live check.

Without wiring, everything still works: the lead DM acts when you prompt it.

The lead DM is one long session. Its context grows only with the work it does, and compaction is safe. To save tokens, you can start a fresh lead DM session at any time: it takes the lead and picks up where the old one left off, as described in [Session recovery](#session-recovery). Devs and Reviewers still running in the old session stop when you close it, and the new lead DM restarts them.

The recommended setup is one lead DM session that does the work, and a conversational DM session that you talk to.

## Session recovery

Devs and Reviewers run inside the lead DM's harness session and stop when it ends. Every commit is pushed immediately and every stage is a label, so GitHub always holds the full state. When a lead DM starts, `crew status` proposes one action per ticket:

| Found at | Recovery |
| --- | --- |
| `4:in-progress` | If a Dev question is unanswered, answer it first. Then start a new Dev at the same rank. An interrupted session is not a failed attempt. The Dev continues from the branch, the ticket comments and the existing worktree, so uncommitted edits survive. |
| `4:waiting-review` | Start a Reviewer. |
| `4:review-rejected` | Start a Dev one rank up, as normal escalation. |
| `5:review-approved` | Queue the merge. |
| `6:pending-merge` | Run the gate again if `main` has moved, then merge. |
| `7:merged`, `8:deploying` | Read the deployment workflow's status and advance. |

Worktrees and compose projects of tickets no longer in stages 4 to 6 are removed.

## Profiles and containers

The core of Skeleton Crew knows nothing about languages or frameworks. A **profile** describes a stack:

- commands: `setup`, `build`, `lint`, `test`, `test-changed` (only the tests affected by a change, for fast TDD cycles), `run`, `health`, `e2e`
- globs that identify test files
- a `Containerfile` and a `compose.yaml` for the app and its services
- an optional deployment workflow (GitHub Actions)
- default conventions and architecture decisions
- a starter scaffold
- database migration rules where the stack has a database: naming, forward-only changes, and renumbering on rebase if the naming needs it. Applied migrations are never edited; fixes go forward. Every gate run starts from a fresh database.

Built-in profiles: **python**, **node-ts**, **java-spring**. At bootstrap you can choose one of them or describe your own stack, and the DM writes a custom profile for it.

The application always runs in containers, never directly on the host. Agents run on the host through your harness, but everything the code needs runs through compose: tests, builds, the app and its database. Your machine needs no language toolchains.

## Deployment

Skeleton Crew never deploys anything itself. A profile may include a GitHub Actions deployment workflow that runs on every push to `main`. The crew only reads that workflow's status to move tickets through `8:deploying` to `9:done`. Without a deployment workflow, merging finishes the ticket.

## Prompts and project knowledge

You never need to edit configuration or prompts. Everything works with defaults, and the agents adapt to your project as they go.

- **Rules** are built into `crew` and delivered through `crew brief <role>`. They cover role duties, the workflow and the hard rules. Agents cannot change them; they change only with a new release.
- **Project knowledge** is written by the agents as work happens. Devs add what they learn in their ticket's PR, and the Reviewer checks it with the code. The DM records decisions from conversations with you in knowledge-only PRs.
- If project knowledge contradicts a built-in rule, the rule wins, and `crew doctor` reports the conflict.

### Layout

Skeleton Crew adds only `AGENTS.md` and `.crew/` to the root of your project. The one exception is [harness wiring](#keeping-the-lead-dm-running) that the lead DM writes itself, which lives where the harness requires it, such as `.claude/` or `.opencode/`. Every harness's wiring is recorded in `.crew/crew.toml`.

```
AGENTS.md                 the seed file
.crew/
  crew.toml               configuration
  logs/                   full gate and test output; not committed, pruned automatically
  worktrees/              one worktree per ticket in flight; not committed
  docs/
    product.md            PM: what the product is, its users, glossary, your standing preferences
    architecture.md       DM: modules, boundaries, data model
    conventions.md        DM: coding patterns, naming, testing style, gotchas
    adr/                  DM: one short architecture decision record per decision
      0001-<slug>.md
```

- `crew init` seeds these files from the chosen profile.
- Files that belong to the application, not the crew, go where their ecosystem expects them: the `Containerfile` and `compose.yaml` at the root, and the optional deployment workflow in `.github/workflows/`. The application must not depend on `.crew/`.
- The PM owns `product.md`. The DM curates the rest.
- Knowledge is curated, not an append-only log. `product.md`, `architecture.md` and `conventions.md` together stay within a size budget. `crew doctor` warns when they exceed it, and the DM condenses them, moving detail into ADRs.
- To make the crew use your own documentation, such as an existing `docs/` folder, ask the DM. It records an ADR that tells agents where those documents are and when to read them.

### Briefings

`crew brief <role> [<ticket>]` assembles everything an agent needs:

- **In full:** the role rules, the ticket, `product.md`, `architecture.md` and `conventions.md`.
- **As an index:** the ADRs, one line each with number, title and summary. The agent opens the ones relevant to its work.
- **Code search:** if the harness provides a code index or code search tool, agents prefer it for finding code and fall back to plain search. `crew` does not detect such tools; subagents normally inherit them from the harness. Results must point into the agent's own worktree. An index of the main checkout shows `main`, not the ticket's branch, so the agent re-indexes its worktree or reads the files directly.

## The `crew` CLI

`crew` is a single Go binary with no runtime dependencies. Built-in profiles, templates and role rules are embedded in it, so no framework code is copied into your project. Commands print terse, stable output for agents, and structured output with `--json`. Times are absolute UTC timestamps.

`crew` does the deterministic work: the normal path and the invariants, such as stage order, the gate before merge, TDD checks, staying in sync with GitHub and keeping secrets out. When it meets something outside the normal path, such as an unusual git state or an unexpected GitHub response, it changes nothing, stops and reports what it found. The DM then handles the case with judgement. It may use `git` and `gh` directly, unlike Devs and Reviewers, but it must still respect the invariants, and it records what it did as a `decision` comment.

There is no generic "set stage" command. Each stage change has exactly one command, and that command checks that the move is legal. This is how `crew` keeps stage numbers from going backwards.

### Setup

| Command | Does |
| --- | --- |
| `crew init` | Sets up the project; see [What `crew init` does](#what-crew-init-does) |
| `crew doctor` | Checks tools, authentication, labels, configuration, tier ranks, knowledge size and harness wiring; runs one gate |
| `crew upgrade` | Moves the pinned version to a newer release, using the install method you chose |
| `crew brief <role> [<ticket>]` | Prints an agent's briefing, with the model and effort to use |

### Project Manager

| Command | Stage change |
| --- | --- |
| `crew ticket new [--parent <n>]` | Creates a ticket at `2:drafting`, or at `1:intake` with `--intake`; with `--parent`, as a child of ticket `n` |
| `crew ticket triage <n>` | `1:intake` to `2:drafting` |
| `crew ticket edit <n>` | Changes body, priority, kind, testing level or dependencies; no stage change |
| `crew ticket ack <n>` | Marks the DM decisions on a ticket as seen by the PM |
| `crew ticket ready <n>` | `2:drafting` to `3:eligible`; refused without numbered acceptance criteria |

### Lead DM

| Command | Stage change |
| --- | --- |
| `crew dm claim [--takeover]` | Takes the lead |
| `crew dm release` | Gives up the lead |
| `crew events [--wait [--test]]` | Reports what changed since the previous call; the signal for harness wiring. `--wait` blocks until something is unread, without marking it read; `--test` exits after a few seconds as if an event had arrived |
| `crew ticket next` | Lists eligible tickets in order |
| `crew ticket start <n> --tier <rank>` | `3:eligible` to `4:in-progress`; creates the worktree, branch, compose project and a draft PR |
| `crew ticket appeal <n> "<reason>"` | `4:review-rejected` to `4:waiting-review` for a second, independent Reviewer; one appeal per attempt |
| `crew ticket retry <n>` | `4:review-rejected` to `4:in-progress`, one rank up; enforces `top_attempts` |
| `crew ticket split <n>` | Creates child tickets at `2:drafting`, for the PM to complete, and cancels the parent |
| `crew merge <n>` | `5:review-approved` to `6:pending-merge` to `7:merged`; gates again if `main` moved, then fast-forwards `main` |
| `crew ticket cancel <n>` | Any open stage to `0:cancelled` |

### Dev

Run inside the ticket's worktree.

| Command | Does |
| --- | --- |
| `crew commit --red\|--green\|--refactor "<message>"` | Checks the TDD phase, commits and pushes immediately; see [Test-driven development](#test-driven-development) |
| `crew commit "<message>"` | Commits a change with no behaviour, such as documentation or configuration, and pushes immediately |
| `crew env up`, `crew env down`, `crew env logs` | Manages the ticket's compose project |
| `crew run <command>` | Runs a profile command, such as `test` or `build`, in its container |
| `crew ticket submit` | Runs the gate on the exact pushed commit; `4:in-progress` to `4:waiting-review` on success |
| `crew ticket ask "<question>"` | Posts a `question` for the DM and ends the Dev's run; no stage change, and not a failed attempt |
| `crew ticket stuck "<reason>"` | `4:in-progress` to `4:review-rejected`; counts as a failed attempt |

### Reviewer

| Command | Stage change |
| --- | --- |
| `crew review <n> --approve` | Posts the PR review; `4:waiting-review` to `5:review-approved` |
| `crew review <n> --reject "<reason>"` | Posts the PR review; `4:waiting-review` to `4:review-rejected` |

### Anyone

| Command | Does |
| --- | --- |
| `crew status` | Shows what needs the caller's attention; see [Status](#status) |
| `crew ticket list` | One line per ticket |
| `crew ticket show <n> [--all]` | The terse ticket view |
| `crew ticket comment <n> --kind <kind>` | Posts a tagged comment |
| `crew ticket block <n> "<reason>"`, `crew ticket unblock <n>` | Sets or clears the `blocked` flag |

## Later

Low-priority features that are agreed to be useful but are not part of the first release.

| Feature | Notes |
| --- | --- |
| Project versioning | SemVer bumps, releases and changelogs for the user's project. A profile could add them. |
| More built-in profiles | Candidates: `go`, `static-web`. |
| Native Windows support | A PowerShell bootstrap. Until then, Windows users use WSL. |
| No `gh` dependency | Call the GitHub API directly from `crew`, reusing `gh`'s stored login when present. |
| Respecting the user's CI | Optionally wait for the user's own CI checks before merging, in addition to the gate. |
| Branch protection | Where the GitHub plan allows it, protect `main` against force pushes and deletion without blocking the DM's fast-forward merges. |
| Review depth by risk | The Reviewer scales the depth of its review to the diff, for example a quick check for documentation and a deep one for schema or security changes. |
| Investigator role | A read-only agent that answers "where is X" and "why does Y fail" for the DM during conversations. |
| Harness shortcuts | Optional conveniences such as `/pm` and `/dm` commands, for harnesses that support them. `crew` never depends on them. |
| Cost reporting | Model usage and cost per ticket, where the harness reports it. |
| Notifications | Tell the user when a question is waiting for them, outside an open session. |
