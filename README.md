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
3. Asks you which profile the project uses, then runs `crew init` with your answers. `crew init` takes flags only and never prompts; the agent has the conversation with you.
4. Runs `crew doctor`. Anything that needs a credential, such as GitHub authentication, is reported as a command for you to run in your own terminal. Agents never handle secrets.

The bootstrap block never fetches anything from a moving branch, and it shows the exact commands it runs. `crew upgrade` moves the pin to a newer release.

### What `crew init` does

```
crew init --profile <name> [--harness <name>] [--repo <owner>/<name>] [--public] [--json]
```

1. **Repository:** uses the existing git repository and its `origin`. Without a git repository it runs `git init -b main`. Without an `origin` it creates the GitHub repository, private unless `--public` is given, and adds it as `origin`. `--repo` names the new repository; by default it is the directory's name under your `gh` login. `origin` uses SSH when `gh` is configured for SSH, and HTTPS otherwise.
2. **Labels:** creates the `stage:`, `priority:`, `kind:`, `tier:` and `testing:` labels and `blocked`. Running it again is safe.
3. **Crew files:** writes `.crew/crew.toml`, with default models for the detected harness (see [Models](#models)), seeds `.crew/docs/` from the profile, and adds `.crew/worktrees/`, `.crew/logs/`, `.crew/lead.lock*` and `.crew/events.*` to `.gitignore`.
4. **Application files:** for a new project only, writes the profile's `Containerfile`, `compose.yaml` and starter scaffold. A project is new when its directory holds nothing besides `.git`, `AGENTS.md` and the files `crew init` writes.
5. **Commit:** commits only the files it created and pushes them to `main`, with the message `Set up Skeleton Crew`. Your own uncommitted changes are left alone.
6. **Check:** runs `crew doctor`, including one gate run.

Running `crew init` again is safe. It never overwrites a file, adds only what is missing, and makes no commit when nothing is missing. If an earlier run committed but could not push to a new repository, it pushes.

It changes nothing and reports why when `origin` is not on github.com, when the checkout is not on `main`, when `main` has diverged from `origin/main`, when `.gitignore` has uncommitted changes, or when an existing `.crew/crew.toml` names a different profile.

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

`crew ticket new "<title>"` writes this skeleton, with `priority:medium` and `kind:feat` unless `--priority` and `--kind` say otherwise. The PM fills it in with `crew ticket edit` once the ticket has its number, which the criterion IDs need. `crew ticket ready` checks that the goal has text and that the acceptance criteria hold at least one line of the form `- [#<n>-AC<k>] <text>`, where `<n>` is the ticket's own number:

```markdown
## Goal
## Acceptance criteria
## Out of scope
## Assumptions
## Non-functional
## Risks
```

### Parent tickets

The PM splits a large requirement into a parent ticket and smaller child tickets with `crew ticket new --parent <n>`. Parents use GitHub's own sub-issues. A parent has no stage, is never worked on directly, and is closed by `crew status` once all its children are done or cancelled. `crew ticket new --parent <n>` makes the new ticket a sub-issue of `n` and takes `n`'s stage label off. It refuses a parent that is closed or past `2:drafting`.

### Editing a ticket

```
crew ticket edit <n> [--body-file <file>] [--priority <p>] [--kind <k>] [--testing <level>] [--add-dependency <m>] [--remove-dependency <m>]
```

- `--body-file` replaces the body with the file's contents; `--body-file /dev/stdin` reads it from standard input.
- `--testing` takes `minimal`, `standard` or `thorough`, or `project`, which removes the ticket's override.
- `--add-dependency` and `--remove-dependency` may be repeated. Each names a ticket that `n` is blocked by.
- It checks every change before it makes any, and refuses a dependency that does not exist, is already there when added, or is not there when removed.
- It prints the ticket's fields, then one line `revised <criterion>` for each criterion it marked as revised; see [Obsolete tests are maintained](#obsolete-tests-are-maintained).

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
- **Merge conflicts** at `6:pending-merge` stay at stage 6. The DM resolves trivial conflicts such as lockfiles and version lines itself.
- **Anything else at stage 6 goes forward through a new ticket:** a conflict that is not trivial, or a gate that fails after the rebase. No Dev works at stage 6, because stages never go back to 4 and Devs never rebase. The DM cancels the ticket and opens a new one with `crew ticket new`, at `2:drafting`. Its `Assumptions` name the cancelled ticket and its branch, `origin/<branch>`, which the new ticket's Dev may read to reuse the work. The PM copies the goal and writes the criteria under the new number. Tests that name the cancelled ticket's criteria are flagged by the gate as `cancelled`, and the new ticket's Dev renames them to the new criteria, with failing tests first as usual.
- **Deployment failures:** if the pipeline or environment failed, the DM triages it and asks you only for credentials. If the code caused it, the original ticket goes to `9:done` and the DM opens a linked `fix` ticket at `high` with `crew ticket fix`. Fixes always go forward.

### Comments and terse views

Agents never call the GitHub API or `gh` directly. They go through `crew`, which returns compact views. Agents may read with `git`, such as `git diff` and `git log`, but `crew` makes every commit, push and branch. The DM alone may change things with `git` and `gh` directly: to handle an exception, to resolve a trivial merge conflict, or to open a knowledge-only PR. It drops node IDs, URLs, label colours, reactions and other API structure. Labels become fields such as `stage=4:in-progress`. Times are absolute UTC timestamps, never relative ("2h ago" goes stale as soon as it is read). Ticket bodies and comments are never altered.

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

Routine progress is recorded as label changes, which GitHub keeps in the issue timeline, not as comments. The timeline lists when each stage was reached, when `blocked` was set or cleared, when the issue was closed, and the first line of each `log` comment.

Agents post comments only through `crew ticket comment <n> --kind <kind> --as <role> "<text>"`, where the role is `pm`, `dm`, `dev` or `reviewer`. Commands that post comments themselves, such as `crew ticket ask`, set the kind and role. The comment starts with an invisible marker line, such as `<!-- crew:decision dm -->`, followed by the text. Any comment without a marker is a human comment, shown under its author's GitHub login:

| Kind | Shown by `crew ticket show` |
| --- | --- |
| `decision`, `question`, `answer`, `review` | In full |
| `log` | Collapsed into the timeline |
| Human comments | In full |

`--all` shows the full thread, with `log` comments in full.

`crew ticket list` prints one line per open ticket, in number order: the label fields, then the title.

```
$ crew ticket list
#12 stage=4:in-progress priority=high kind=feat tier=2 blocked · Login with email
#13 stage=2:drafting priority=medium kind=fix · Expired links crash
```

## Status

`crew status [--as <role>]` shows each role what needs its attention. The role is `pm` or `dm`; without `--as`, it shows only what concerns everyone. The PM runs `crew status --as pm` and the DM `crew status --as dm` at the start of every session. The lead DM adds `--lead <token>`.

- **Everyone:** questions waiting for you. Each `blocked` ticket is listed with the time it was blocked and the first line of its latest `question` comment that is not a Dev's. A ticket that reaches the escalation limit is blocked with such a question.
- **PM:** new `1:intake` tickets, tickets in `2:drafting`, and DM decisions the PM has not acknowledged. `crew ticket ack <n>` posts a `log` comment `ack` as `pm`; a `decision` comment by `dm` is acknowledged once such a comment follows it on its ticket.
- **Lead DM:** unanswered Dev questions, the board, stale handoffs and recovery proposals. A Dev question is unanswered while no `answer` comment follows it. The board is every open ticket past `2:drafting`. Recovery proposals are described in [Session recovery](#session-recovery). With `--lead <token>`, it also advances `7:merged` through `8:deploying` to `9:done` from the deployment workflow's status, and removes the worktrees and compose projects that tickets left behind; without the token it changes neither.
- **Stale handoffs:** each role sees the stale tickets at the stages it owns, the PM `1:intake` and `2:drafting` and the DM the rest. A ticket is stale when it has been at its stage longer than the time below, counted from the last time it reached the stage. A `blocked` ticket is never stale, because it waits for you.

| Stage | Stale after | Flag |
| --- | --- | --- |
| `1:intake` | 1 day | `not triaged` |
| `2:drafting` | 7 days | `not ready` |
| `3:eligible` | 1 day | `not started` |
| `4:in-progress` | 4 hours | `not submitted` |
| `4:waiting-review` | 1 hour | `no reviewer` |
| `4:review-rejected` | 1 hour | `no new attempt` |
| `5:review-approved` | 1 hour | `not merged` |
| `6:pending-merge` | 1 hour | `not merged` |
| `7:merged` | 1 hour | `no deployment` |
| `8:deploying` | 1 hour | `deployment not finished` |

`crew status` reads the comments of the last 30 days, in one listing for the whole repository, so a question or decision older than that no longer shows.

Every run of `crew status`, whatever the role, also keeps the tickets in order, and reports what it changed:

- Open issues without a stage label, such as issues you create on GitHub, are labelled `1:intake` the first time `crew status` sees them, with `priority:medium` and `kind:feat` when they have no priority or kind. An issue with sub-issues is a parent and keeps no stage.
- A parent whose sub-issues are all closed is closed, as not planned when every sub-issue was, and as completed otherwise.

The output has one section per part, each opened by a line `=== <name>`, and leaves out the empty ones. With nothing to show, it prints `nothing needs attention`.

```
$ crew status --as dm
=== changes
#21 labelled stage=1:intake priority=medium kind=feat
#7 closed: all sub-issues closed
=== waiting for the user
#14 blocked since 2026-10-15T10:00Z: Which payment provider should the checkout use?
=== dev questions
#12 2026-10-15T08:52Z: Should expired links show an error page or redirect?
=== board
#12 stage=4:in-progress priority=high kind=feat tier=2 · Login with email
#13 stage=4:waiting-review priority=medium kind=fix tier=1 · Expired links crash
#14 stage=3:eligible priority=high kind=feat blocked · Checkout
=== stale
#13 4:waiting-review since 2026-10-15T09:12Z: no reviewer
=== recovery
#12 4:in-progress: answer the Dev question, then start a Dev at tier 2
#13 4:waiting-review: start a Reviewer
```

The PM's sections are `changes`, `waiting for the user`, `intake`, `drafting`, `decisions` and `stale`. A decision line is like a question line: the ticket, the comment's time and its first line.

## Selecting work

`crew ticket next` lists the eligible tickets that are not blocked and whose dependencies are done, one line each as `crew ticket list` prints them. Dependencies use GitHub's own issue dependencies ("blocked by"), which `crew ticket edit` sets. A dependency is done when its issue is closed as completed; a cancelled dependency keeps the ticket out of the list until the dependency is removed. They are sorted by priority, from `critical` to `low`, then by age, oldest first.

- By default the DM takes the first one.
- The DM may skip down the list when the top ticket would collide with work in flight, for example two tickets that both change the same module. It records the reason as a `log` comment.
- If every candidate would collide, the slot stays idle until a merge lands.

## Parallelism

Several Devs can work at once, up to a cap in `.crew/crew.toml`:

```toml
[parallel]
devs = 3
```

Each ticket in flight has its own:

- git branch and worktree; see [Conventions](#conventions),
- compose project (`crew-<ticket>`) with automatically assigned ports. A profile's `compose.yaml` publishes container ports only, so the container engine picks free host ports and two tickets never clash. The project is created when the Dev first runs `crew env up` or `crew run`.

Every ticket at a stage 4 holds a slot: `4:in-progress`, `4:waiting-review` and `4:review-rejected`, which keeps its slot for the next attempt. At the cap, `crew ticket start` is refused with one line such as `at-cap: 3 of 3 slots busy: #4 #5 #6`.

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
top_attempts = 2   # attempts at max_tier before the ticket comes to you
```

- Ranks run from 1 upwards with no gaps. You may define as many as you like; `crew doctor` checks the list.
- `name` is optional. It helps the DM and you tell ranks apart.
- Each failed attempt, such as a review rejection or a Dev reporting it is stuck, moves the next attempt up one rank automatically.
- A Dev that needs a decision runs `crew ticket ask`. That is not a failed attempt. The DM answers with an `answer` comment, or takes a requirements question to the PM or to you, and then starts a Dev at the same rank.
- When a ticket fails at `max_tier`, the DM decides what to do: rethink the approach and retry, split the ticket, or ask you.
- Attempts at `max_tier` are limited by `[escalation] top_attempts` (default 2). `crew ticket retry` counts them from the ticket's label history: each time the ticket reached `4:in-progress` at the `max_tier` rank since `blocked` was last cleared. When a retry would start one attempt more, `crew` refuses it, sets `blocked` and posts a `question` comment as `dm` that lists the attempts and their failure reasons, so the ticket comes to you. Once you have answered, clearing `blocked` gives the ticket a new budget at `max_tier`. A blocked ticket is never retried.
- Splitting does not use the retry budget, because it changes the plan. The new tickets start at `2:drafting` with the split ticket's priority and kind, so the PM writes their acceptance criteria, and their first attempt uses rank 1. Each gets a `log` comment `split from #<n>`, and the split ticket one naming them. If the split ticket is a sub-issue, the new tickets become sub-issues of the same parent. A ticket created by a split cannot be split again; if it reaches the limit, it comes to you.
- You set the model and effort of the PM and DM yourself, in your harness session. `crew` sets them for Devs and Reviewers.
- `crew init` fills in default models for your harness. It detects the harness from the environment (`AI_AGENT`, `CLAUDECODE`, `OPENCODE`), and `--harness <name>` overrides the detection. For Claude Code the ranks get the model aliases `haiku`, `sonnet` and `opus`, which follow new model releases. For any other harness, `model` is left empty, which means the session's model. You can also tell the DM in conversation, for example "use cheaper models" or "use rank 3 for #12".
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
- At `crew ticket submit`, the gate checks that every criterion of the ticket has a test that failed before it passed: a red `log` comment on the ticket names the criterion, a test file at the gated commit still names it, and the full suite passes. A branch with no red, green or refactor commit since it left `main` holds only changes with no behaviour, so the gate skips this check for it, and the Reviewer judges whether that is true.
- `crew` records each red result as a `log` comment on the ticket, with the test names and a failure excerpt. The record is keyed by test, not by commit, so it survives rebases. The Reviewer checks that the test failed for the right reason: an assertion about the behaviour, not a typo or a missing import.
- Changes with no behaviour, such as documentation, configuration and knowledge files, use a plain `crew commit`. The Reviewer judges whether a change really has no behaviour.
- Red and green steps run the profile's fast `test-changed` command. The full suite runs at the gate.
- Exit codes 125 to 127 mean the container engine or the shell failed, so no test ran. `crew` refuses the step with `the tests could not run`, whatever the phase.
- `crew commit` commits every change in the ticket's worktree; there is no staging step. A red step runs `test-changed` on the test files the commit changes. Green and refactor steps run it on every test file the branch has changed since it left `main`.
- The red `log` comment starts with a line such as `red: #12-AC1 #12-AC2`, naming the criteria in the lines the commit adds to test files, followed by the test files and the last 20 lines of the test output.
- A refactor step must not add a test file, and its added test lines must not name a criterion that its removed lines do not. Moving a test between existing test files is fine.

### Obsolete tests are maintained

- Tests name their criteria, so `crew` finds tests whose criterion was removed, revised or belongs to a cancelled ticket. The gate flags them, and the ticket that changed the requirement updates or deletes them in the same PR.
  - The gate reads every test file at the gated commit and each ticket a criterion ID in it points to. A criterion is **removed** when its ticket's acceptance criteria no longer list it, or the ticket does not exist, and **cancelled** when its ticket is at `0:cancelled` or closed as not planned.
  - When `crew ticket edit` changes the text of an existing criterion, it marks the line with the time, such as `- [#12-AC1] (revised 2026-10-16T09:12Z) <text>`. A test file that names the criterion is **revised** when its last commit is older than that mark. `crew` keeps the marks itself: a criterion whose text is unchanged keeps its earlier mark, and a new criterion has none.
  - A flag is one line in the gate's output, such as `flag: tests/test_login.py names #12-AC1: removed`. Flags do not fail the gate.
- The gate refuses skipped or disabled tests. A profile lists the markers that skip or disable a test in its stack, such as `@pytest.mark.skip`, and the gate refuses any line that the branch adds to a test file and that contains one of them. Tests skipped before the branch left `main` do not block it.
- A commit that changes or deletes an existing test must give the reason, such as the criterion revision. The Reviewer rejects any test weakened just to make it pass.

## Conventions

| Item | Format | Example |
| --- | --- | --- |
| Branch | `<kind>/<ticket>/<slug>` | `feat/12/login-with-email` |
| Worktree | `.crew/worktrees/<kind>/<ticket>/<slug>` | `.crew/worktrees/feat/12/login-with-email` |
| Commit | `[<kind>:#<ticket>] <phase>: <message>` | `[feat:#12] red: login rejects an unknown email` |
| Pull request | Title `[<kind>:#<ticket>] <ticket title>`, body `Ticket #<ticket>` | `[feat:#12] Login with email` |

- `<kind>` is `feat`, `fix` or `chore`.
- `<slug>` is the ticket's title in lower case, with every run of other characters than letters and digits turned into `-`, cut to at most 40 characters at a `-`.
- `<phase>` is `red`, `green` or `refactor`, and is left out for changes with no behaviour.
- `crew` creates branches and worktrees and writes the commit prefix, so agents never type it. `#<ticket>` makes GitHub link the commit to the issue.
- `crew ticket start` creates the branch from `origin/main` and its worktree. GitHub refuses a pull request with no commits, so the first `crew commit` on the branch opens the PR. It is a normal PR, not a draft, because draft PRs are not available on every plan; the stage label says whether it is ready for review. The body has no closing keyword, so merging does not close the ticket; `crew` closes it at `9:done`.

## Review and merge

- Every ticket gets a Reviewer. It starts with fresh context: the ticket, the acceptance criteria and the diff, not the Dev's reasoning. It reads the diff with `git diff origin/main...origin/<branch>`, and the red `log` comments with `crew ticket show <n> --all`.
- The Reviewer posts its verdict with `crew review`. The verdict is binding: an approval goes to merge, and a rejection means a new attempt.
- Every agent uses your `gh` login, so every PR is your own, and GitHub refuses an approval or a change request on your own PR. The verdict is therefore a comment review on the PR's head commit, whose body is a marker line, `<!-- crew:review reviewer -->`, followed by `approved` or `rejected: <reason>`. The stage label carries the binding verdict. `crew review` also posts the verdict as a `review` comment on the ticket, so `crew ticket show` shows it to the DM and to a second Reviewer.
- A rejection the DM believes is wrong is an exception for the DM to judge, but the DM cannot overrule the Reviewer alone. It runs `crew ticket appeal <n> "<reason>"`, which moves the ticket from `4:review-rejected` back to `4:waiting-review` for a second Reviewer. That Reviewer starts fresh and sees the first rejection and the DM's reason. If it approves, the ticket goes to `5:review-approved`. If it rejects, the rejection stands.
- An appeal is not a failed attempt and does not change the rank. Each attempt allows one appeal.
- The gate decides whether code can merge. See [Gates](#gates).
- The DM merges by pushing the gated commit to `main` as a fast-forward. GitHub then marks the PR as merged. There are no merge commits, and GitHub's own rebase merge is not used because it rewrites commits, so `main` would hold commits the gate never tested.
- `crew merge` moves the ticket to `6:pending-merge`, then fetches. If `main` has moved, it rebases the branch onto `origin/main` in a detached worktree at `.crew/worktrees/merge/<ticket>` and pushes the result to the branch. It runs the gate on the branch's commit unless that commit already has a successful `crew/gate` status, then pushes it to `main` without force, so only a fast-forward is accepted.
- A rebase conflict changes nothing: `crew merge` aborts the rebase, leaves the ticket at `6:pending-merge` and lists the conflicting files, such as `merge-conflict: #12 stays at 6:pending-merge: src/app/login.py`. A failed gate also leaves the ticket at stage 6. `crew merge` accepts a ticket at `6:pending-merge`, so it runs again once the DM has resolved a trivial conflict. Any other case goes forward through a new ticket; see [Ticket lifecycle](#ticket-lifecycle).
- After the merge, the ticket moves to `7:merged`, and on to `9:done` with its issue closed when there is no deployment workflow. `crew merge` then removes the ticket's worktree and stops its compose project with `down --volumes`.
- If a step after the push to `main` fails, such as a stage move or the cleanup, `crew merge` stops and reports it after the line `merged <commit> into main`. The DM finishes the remaining steps by hand.

## Gates

There is no CI gate. `crew` runs the gate locally, and the result does not depend on any agent's word.

- When a Dev submits its work with `crew ticket submit`, `crew` checks out the exact pushed commit into a clean temporary worktree and runs, through compose:
  1. `build`
  2. `lint`: formatting, static analysis and architecture rules
  3. `test`: the full suite
  4. the ticket's compose stack, waiting until `health` succeeds
  5. `e2e` against that running stack
- A profile leaves out the steps that do not apply to its stack.
- `crew ticket submit` refuses, before running anything, when the ticket is not at `4:in-progress`, when the worktree has uncommitted changes, when the branch has no commits, or when its last commit is not pushed. The gate runs on the commit at `origin/<branch>`.
- The checks that need no containers run first, so they fail in seconds: [TDD](#devs-write-failing-tests-first) and [skipped tests](#obsolete-tests-are-maintained).
- The gate checks the commit out, detached, at `.crew/worktrees/gate/<ticket>` and runs in its own compose project, `crew-<ticket>-gate`, so the Dev's own environment is left alone. It starts with `down --volumes`, so every gate run starts from a fresh database, and builds the images from the gated commit. `build`, `lint` and `test` each run in a new `app` container. The stack starts with `up -d`; `crew` runs `health` in the running `app` container every 2 seconds, for up to 2 minutes, and then runs `e2e` there. The gate always ends with `down --volumes` and removes its worktree.
- On success the ticket moves to `4:waiting-review`. On failure it stays at `4:in-progress` and the Dev gets the last 20 lines of the failing step's output, followed by one line such as `gate failed: test: exit 1; full output: .crew/logs/20261016T091244Z-gate-12.log`. The log holds the output of every step. `crew` keeps the newest 50 logs in `.crew/logs/` and deletes older ones.
- `crew` posts the result as a commit status (`crew/gate`) on that commit, so it shows on the PR: `pending` while the gate runs, then `success` or `failure`, with the failing step in the description. Commit statuses work on every GitHub plan and need no Actions.
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

- A DM session starts with `crew dm claim`. If no one holds the lead, it gets it, along with a token, and prints one line such as `lead 3f9a0c1e5b7d42a8961e0f3c2b4d6a81 claimed 2026-10-16T09:12Z`. Every lead-only command carries the token as `--lead <token>`. While another session holds the lead, a claim is refused with `lead held since 2026-10-16T09:12Z; crew dm claim --takeover takes it once the user confirms`.
- The lead is recorded in `.crew/lead.lock` in the main checkout, a local file that is not committed. All DM sessions of a project run in the same checkout, because worktrees, compose projects and gate runs are local too. The file holds the token and the claim time:

  ```toml
  token = "3f9a0c1e5b7d42a8961e0f3c2b4d6a81"
  claimed = "2026-10-16T09:12:44Z"
  ```

  A claim writes a temporary file and links it into place, which fails if a lock already exists, so of two claims at the same moment only one wins, and no one ever reads a half-written lock. A takeover renames its file over the old lock. Only one claim at a time replaces an unreadable lock: it holds `.crew/lead.lock.repair` while it reads the lock again and replaces it, and any other claim meanwhile is refused and can simply run again. A takeover removes a guard that a dead claim left behind.
- Only the lead starts Devs and Reviewers, merges, cancels, runs recovery, reads events and records harness wiring. `crew` refuses these from any other session with one line that starts with `not-lead`, such as `not-lead: the lead was claimed at 2026-10-16T09:12Z by another session` or `not-lead: no lead; run crew dm claim`.
- Every other DM is **conversational**. It discusses implementation with you, records decisions, creates tickets from your requests and opens knowledge-only PRs. It never starts or merges work.
- `crew dm release --lead <token>` gives up the lead when the lead session ends normally. Only the lead can release it.

### The lead can never lock the system

No internal mechanism may stop the crew from making progress. For the lead this means:

- **Takeover always works.** Any DM can take the lead with `crew dm claim --takeover` once you confirm. The old token stops working immediately. A dead session or a lost token costs at most one confirmation from you.
- A lead that has lost its own token takes over from itself the same way.
- If the old lead was still alive, its next lead-only command gets `not-lead` and it continues as a conversational DM. No work is lost, because all state is on GitHub.
- Every state change is a check-and-set on the stage label, such as "move #12 from 5 to 6 only if it is still at 5". If two DMs race during a takeover, one succeeds and the other gets `stage-changed`. Nothing happens twice.
- GitHub has no conditional label write, so the removal of the old stage label is the check: of two moves that race, only one removes it. A move reads the stage, adds the new stage label, then removes the old one. If the old one is already gone, another writer moved the ticket first: the move takes its own new label off again, unless the other writer made the same move, and is refused. Because the new label goes on first, a ticket is never without a stage, even for a moment.
- A move from a stage the ticket is not at is refused with one line that starts with `stage-changed`, such as `stage-changed: #12 is at 6:pending-merge, not 5:review-approved`, whether the stage changed before the read or during the move.
- A missing or unreadable lock file counts as "no lead", so the next claim succeeds. An unreadable one also prints a warning, such as `warning: .crew/lead.lock unreadable: invalid token; counted as no lead`.
- `crew` never decides on its own that a session is dead. Nothing moves without an open session, and an open session means you are there to confirm.

## Keeping the lead DM running

An agent session only acts when it has a turn. Without help, the lead DM stops when it runs out of work and waits for a prompt, even after new work becomes eligible. Keeping it in a long polling loop is not acceptable either, because every idle check would grow its context.

`crew` ships no harness adapters. It provides one command to wait on, and it defines what any wiring must achieve. The lead DM checks its harness every time it starts and uses the lightest wiring that works.

### Waiting for events

`crew events --lead <token>` reports what changed since the previous call. It is the single signal the wiring triggers on. It prints one line per event, oldest first, or `no events`:

```
$ crew events --lead 3f9a0c1e5b7d42a8961e0f3c2b4d6a81
2026-10-16T09:12Z #12 reached 4:waiting-review
2026-10-16T09:20Z #13 dev question: Should expired links show an error page or redirect?
2026-10-16T09:30Z #14 unblocked
2026-10-16T09:31Z #14 comment by the-user: Use Stripe.
2026-10-16T10:12Z #12 stale at 4:waiting-review: no reviewer
```

- **Events** are the ones the lead DM must act on, on open tickets: a ticket reaching `3:eligible`; a Dev or Reviewer finishing, which is a ticket reaching `4:waiting-review`, `4:review-rejected` or `5:review-approved`, or a Dev `question`; a question being answered, which is `blocked` being cleared, an `answer` comment that is not the DM's, or a human comment; and a handoff going stale at a DM stage, at the time the [stale table](#status) gives. The lead DM's own work, such as starting a Dev or merging, is no event.
- **Read state** is local: `.crew/events.read` in the main checkout records the time of the last call, and the next call reports what happened from then on. Event times are GitHub's timestamps, to the second, compared with the local clock, which must be in sync. An event in the same second as a call is reported again by the next call, and wakes the next wait at once; none is missed. The first call in a checkout reports nothing and starts the record. A missing or unreadable record counts as none, with a warning.
- `crew events` is lead-only, so a session that has lost the lead cannot mark events read that the new lead has not seen.

`crew events --wait` blocks until there is something unread, then exits without printing anything or marking anything read. Some harnesses can run a command in the background and wake the agent when it exits. In such a harness this is the whole wiring, and nothing is installed:

1. The lead DM starts `crew events --wait` in the background and ends its turn.
2. When something happens, the command exits and the harness wakes the lead DM.
3. The lead DM runs `crew events`, does the work, and starts `crew events --wait` again.

Waiting costs the lead DM's context nothing, because the waiting happens in the shell.

Some harnesses run background commands in a long-lived service of their own, so a wait can outlive the session that started it. Two rules make a leftover wait harmless:

- `--wait` never marks events read, so a leftover wait cannot swallow an event meant for the next lead DM.
- Each `--wait` replaces the previous one. A wait that has been replaced exits quietly, so at most one wait runs, and a leftover stops as soon as a new lead DM starts waiting.

A wait writes a random name to `.crew/events.wait` when it starts, replacing whatever is there. Every 30 seconds it reads the file, exits with status 0 and no output if it holds another name, and otherwise looks for unread events. No lock is involved: the last wait to start wins, and the others exit at their next look. With no read record, a wait exits at once, because everything counts as unread until the first `crew events`. The lead DM starts a wait only after its previous one has exited, so a replaced wait never wakes it into starting another.

### Checking the harness

Every time a lead DM session starts, it checks its wiring:

1. It identifies the harness it is running in. The second line of `crew brief dm` names it, as `crew init` detects it, with its record, such as `harness=claude-code wiring=wait`, `wiring=own` or `wiring=none`, or says `harness=unknown`.
2. It looks the harness up in `.crew/crew.toml`:
   - **Recorded as supporting `crew events --wait`:** it uses it.
   - **Recorded with its own wiring:** it uses that wiring.
   - **Not recorded:** it runs the self-test.
3. **Self-test.** It tells you: "If I have not continued within a minute, send me any message." It starts `crew events --wait --test` in the background, which exits after a few seconds as if an event had arrived, and ends its turn.
   - **Woken by the harness:** it records that the harness supports `crew events --wait`.
   - **Not woken:** when you send a message, it sees that the test event fired without waking it. It writes its own wiring with the harness's features, shows it to you and installs it only when you confirm. Then it records that wiring.

The lead DM records the result with `crew dm wiring <harness> --wait`, or for its own wiring with `crew dm wiring <harness> --own "<how to start it>" --file <path>...`, naming every file of the wiring. The record is a table in `.crew/crew.toml`:

```toml
[harness.claude-code]
wiring = "wait"

[harness.opencode]
wiring = "own"
start = "Load the crew-wake plugin at the start of the session."
files = [".opencode/plugins/crew-wake.ts"]
```

`crew dm wiring` replaces the harness's table, commits it with the named files straight to `main` and pushes, as `crew init` does. Both are crew configuration, not application code, so they do not go through the gate; the user has confirmed any wiring of the lead DM's own. It refuses when the checkout is not on `main` or `.crew/crew.toml` has uncommitted changes, and a failure, such as a refused push, changes nothing.

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

Devs and Reviewers run inside the lead DM's harness session and stop when it ends. Every commit is pushed immediately and every stage is a label, so GitHub always holds the full state. `crew status --as dm` proposes one action per ticket that is not `blocked`. `crew` cannot see which agents are running, so the lead DM acts on a proposal only for a ticket that no Dev or Reviewer of its own session is working on, which at the start of a session is every ticket:

| Found at | Recovery |
| --- | --- |
| `4:in-progress` | If a Dev question is unanswered, answer it first. Then start a new Dev at the same rank. An interrupted session is not a failed attempt. The Dev continues from the branch, the ticket comments and the existing worktree, so uncommitted edits survive. |
| `4:waiting-review` | Start a Reviewer. |
| `4:review-rejected` | Start a Dev one rank up, as normal escalation. |
| `5:review-approved` | Queue the merge. |
| `6:pending-merge` | Run the gate again if `main` has moved, then merge. |
| `7:merged`, `8:deploying` | Read the deployment workflow's status and advance. |

`crew status --as dm --lead <token>` does the deterministic part itself:

- **Deployments.** It reads the newest run of the deployment workflow on the ticket's merged commit, which is the head commit of its merged PR. Once a run exists, the ticket moves to `8:deploying`. When the run succeeds, the ticket moves to `9:done` and its issue is closed, through `8:deploying` if status had not yet seen the run. Until then, the proposal is `wait for the deployment`. A run that finished without success leaves the ticket at `8:deploying` with the proposal `triage the deployment: <conclusion>`, such as `failure` or `cancelled`. The DM triages it: a pipeline or environment problem is fixed and the run started again, and status advances the ticket once it succeeds; a problem in the code goes to `crew ticket fix <n> "<title>"`, which opens a `fix` ticket at `high` and `2:drafting`, posts a `log` comment on each ticket naming the other, and moves the original ticket to `9:done` and closes it. `crew ticket fix` refuses a ticket whose newest deployment run has not finished without success.
- **Leftovers.** Worktrees and compose projects of tickets no longer in stages 4 to 6 are removed. `crew` finds them through the worktrees under `.crew/worktrees/`: a Dev's worktree runs compose project `crew-<n>`, a gate worktree `crew-<n>-gate`, and a merge worktree none. Each project goes down with `down --volumes` before its worktree is removed, so a project is never left without the worktree that finds it. A removal that fails stops the removals; status still shows everything else and then reports the failure. Each ticket cleaned up is a line in `changes`, such as `#12 removed .crew/worktrees/feat/12/login-with-email crew-12`.

Without `--lead`, status changes neither, and proposes `advance from the deployment status` for tickets at `7:merged` and `8:deploying`.

## Profiles and containers

The core of Skeleton Crew knows nothing about languages or frameworks. A **profile** describes a stack:

- commands: `setup`, `build`, `lint`, `test`, `test-changed` (only the tests affected by a change, for fast TDD cycles), `run`, `health`, `e2e`
- globs that identify test files
- a `Containerfile` and a `compose.yaml` for the app and its services
- an optional deployment workflow (GitHub Actions)
- default conventions and architecture decisions
- a starter scaffold
- database migration rules where the stack has a database: naming, forward-only changes, and renumbering on rebase if the naming needs it. Applied migrations are never edited; fixes go forward. Every gate run starts from a fresh database.

A profile is a directory with a `profile.toml`, a `docs/` folder that seeds `.crew/docs/`, and a `scaffold/` folder holding the `Containerfile`, the `compose.yaml` and the starter files:

```toml
[commands]
setup        = "pip install --no-cache-dir -e '.[dev]'"
build        = "python -m compileall -q src"
lint         = "ruff check . && ruff format --check ."
test         = "pytest -q"
test-changed = "pytest -q"

[tests]
globs = ["tests/**/test_*.py", "tests/**/*_test.py"]
skip  = ["@pytest.mark.skip", "pytest.skip(", "@unittest.skip"]
```

A command that is left out is a step that does not apply. `crew` runs `test-changed` with the changed test files appended as arguments. `skip` lists the text that marks a skipped or disabled test, which the gate refuses in added test lines; it is optional.

Every profile's `compose.yaml` has a service called `app`, which runs the profile's commands: `crew` runs `<engine> compose -p crew-<ticket> run --rm -T app sh -c '<command>'` in the ticket's worktree. The engine is the first of Podman and Docker that has compose, as `crew doctor` reports.

Built-in profiles: **python**, **node-ts**, **java-spring**. At bootstrap you can choose one of them or describe your own stack, and the DM writes a custom profile for it.

The application always runs in containers, never directly on the host. Agents run on the host through your harness, but everything the code needs runs through compose: tests, builds, the app and its database. Your machine needs no language toolchains.

## Deployment

Skeleton Crew never deploys anything itself. A profile may include a GitHub Actions deployment workflow that runs on every push to `main`. It is `.github/workflows/deploy.yml` or `.github/workflows/deploy.yaml`, and `crew` looks for it in the merged commit. The crew only reads that workflow's status to move tickets through `8:deploying` to `9:done`, as described in [Session recovery](#session-recovery). Without a deployment workflow, merging finishes the ticket.

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
  lead.lock               which DM session holds the lead; not committed
  events.read             when crew events last read; not committed
  events.wait             which crew events --wait is the current one; not committed
  logs/                   full gate output; not committed, the newest 50 kept
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
- The PM owns `product.md`. The DM curates the rest. The PM's changes to `product.md` reach `main` in a knowledge-only PR that the DM opens.
- Knowledge is curated, not an append-only log. `product.md`, `architecture.md` and `conventions.md` together stay within a size budget. `crew doctor` warns when they exceed it, and the DM condenses them, moving detail into ADRs.
- To make the crew use your own documentation, such as an existing `docs/` folder, ask the DM. It records an ADR that tells agents where those documents are and when to read them.

### Briefings

`crew brief <role> [<ticket>]` assembles everything an agent needs. The role is `pm`, `dm`, `dev` or `reviewer`.

- **In full:** the role rules, the ticket when one is given, `product.md`, `architecture.md` and `conventions.md`.
- **As an index:** the ADRs, one line each with number, title and summary. The agent opens the ones relevant to its work. An ADR is `.crew/docs/adr/<number>-<slug>.md`; its title is its first heading, without the number, and its summary is the text after `Summary:`.
- **Model and effort:** for a Dev, those of the ticket's `tier:` rank; for a Reviewer, those of `models.reviewer`. A Dev briefing with a ticket that has no `tier:` label is refused, because `crew ticket start` sets it first. When the rank's `model` is empty, the briefing names no model, and the agent uses the session's model.
- **Testing level:** the project's level, or the ticket's `testing:` label when it has one.
- The knowledge files and ADRs are read from `origin/main` after a fetch, so every agent gets the same agreed knowledge, whatever its checkout holds.

The briefing starts with a header, then one section per part, each opened by a line `=== <name>`:

```
$ crew brief dev 12
brief dev #12
tier=2 model=sonnet effort=medium testing=standard
=== rules
<the role rules>
=== ticket
<the terse view, as crew ticket show prints it>
=== .crew/docs/product.md
<product.md, verbatim>
=== .crew/docs/architecture.md
<architecture.md, verbatim>
=== .crew/docs/conventions.md
<conventions.md, verbatim>
=== .crew/docs/adr
0001 Python stack: Python 3.13 in containers, packaged with pyproject.toml, tested with pytest and checked with ruff.
```

A knowledge file that `main` does not hold shows as a line such as `=== .crew/docs/product.md: missing`.
- **Code search:** if the harness provides a code index or code search tool, agents prefer it for finding code and fall back to plain search. `crew` does not detect such tools; subagents normally inherit them from the harness. Results must point into the agent's own worktree. An index of the main checkout shows `main`, not the ticket's branch, so the agent re-indexes its worktree or reads the files directly.

## The `crew` CLI

`crew` is a single Go binary with no runtime dependencies. Built-in profiles, templates and role rules are embedded in it, so no framework code is copied into your project. Commands print terse, stable output for agents, and structured output with `--json`. Times are absolute UTC timestamps.

`.crew/crew.toml` and `profile.toml` use a strict subset of TOML: comments, `[table]` and `[a.b]` headers, bare keys, and values that are basic strings, decimal integers, booleans or one-line arrays of strings. Every file `crew` accepts is valid TOML. Anything outside the subset, and any unknown key, is an error that names the file and line, such as `.crew/crew.toml:7: unsupported value`. The file also records the profile, as `profile = "python"`.

Builds are reproducible. `crew` is built with Go 1.27.2 and the standard library only, with `CGO_ENABLED=0`, `-trimpath` and no VCS stamping, and the version is embedded at build time. `scripts/self-build.sh` builds it inside the official Go container image, pinned by digest, and its output matches the release binary byte for byte.

`crew` reaches GitHub only through `gh api`, with your own `gh` login. It never reads, prints or stores the token.

An unknown command or flag exits non-zero with one line that names it, such as `unknown flag: --bogus`.

`crew` does the deterministic work: the normal path and the invariants, such as stage order, the gate before merge, TDD checks, staying in sync with GitHub and keeping secrets out. When it meets something outside the normal path, such as an unusual git state or an unexpected GitHub response, it changes nothing, stops and reports what it found in one line, such as `unexpected github response: POST repos/<owner>/<repo>/labels: HTTP 422 Validation Failed (name: already_exists)`. The DM then handles the case with judgement. It may use `git` and `gh` directly, unlike Devs and Reviewers, but it must still respect the invariants, and it records what it did as a `decision` comment.

There is no generic "set stage" command. Each stage change has exactly one command, and that command checks that the move is legal. This is how `crew` keeps stage numbers from going backwards.

### Setup

| Command | Does |
| --- | --- |
| `crew --version` | Prints the version, such as `v0.1.0` |
| `crew init` | Sets up the project; see [What `crew init` does](#what-crew-init-does) |
| `crew doctor` | Checks tools, authentication, labels, configuration, tier ranks, knowledge size and harness wiring; runs one gate. Prints one line per check, such as `ok git 2.47.1` or `fail gh-auth: run in your own terminal: gh auth login`, and exits non-zero if any check fails |
| `crew upgrade` | Moves the pinned version to a newer release, using the install method you chose |
| `crew brief <role> [<ticket>]` | Prints an agent's briefing, with the model and effort to use |

### Project Manager

| Command | Stage change |
| --- | --- |
| `crew ticket new "<title>" [--priority <p>] [--kind <k>] [--intake] [--parent <n>]` | Creates a ticket at `2:drafting`, or at `1:intake` with `--intake`; with `--parent`, as a child of ticket `n` |
| `crew ticket triage <n>` | `1:intake` to `2:drafting` |
| `crew ticket edit <n> [<flags>]` | Changes body, priority, kind, testing level or dependencies; no stage change. Marks a criterion whose text it changes as `(revised <time>)`; see [Editing a ticket](#editing-a-ticket) |
| `crew ticket ack <n>` | Marks the DM decisions on a ticket as seen by the PM, with a `log` comment `ack` |
| `crew ticket ready <n>` | `2:drafting` to `3:eligible`; refused without numbered acceptance criteria |

### Lead DM

`crew ticket start`, `retry`, `appeal`, `split`, `cancel` and `fix`, `crew merge`, `crew events`, `crew dm wiring` and `crew dm release` are lead-only, as is `crew status` with `--lead`. Each takes `--lead <token>` and is refused with `not-lead` without the lead's token.

| Command | Stage change |
| --- | --- |
| `crew dm claim [--takeover]` | Takes the lead and prints its token |
| `crew dm release --lead <token>` | Gives up the lead |
| `crew events [--wait [--test]] --lead <token>` | Reports what changed since the previous call; the signal for harness wiring. `--wait` blocks until something is unread, without marking it read; `--test` exits after a few seconds as if an event had arrived |
| `crew dm wiring <harness> --wait \| --own "<how to start it>" [--file <path>]... --lead <token>` | Records the harness's wiring in `.crew/crew.toml` and commits it, with the wiring's files, to `main` |
| `crew ticket next` | Lists eligible tickets in order |
| `crew ticket start <n> --tier <rank>` | `3:eligible` to `4:in-progress` and sets `tier:<rank>`; creates the branch and worktree, and names the compose project `crew-<n>`; refused at the parallel cap |
| `crew ticket appeal <n> "<reason>"` | `4:review-rejected` to `4:waiting-review` for a second, independent Reviewer, and posts the reason as a `review` comment by `dm`; one appeal per attempt, refused when the attempt ended with `crew ticket stuck` |
| `crew ticket retry <n>` | `4:review-rejected` to `4:in-progress`, one rank up to at most `max_tier`; enforces `top_attempts` |
| `crew ticket split <n> "<title>" "<title>"...` | Creates one child ticket per title, at least two, at `2:drafting`, for the PM to complete, and cancels the parent |
| `crew merge <n>` | `5:review-approved` to `6:pending-merge` to `7:merged`, and to `9:done` without a deployment workflow; rebases and gates again if `main` moved, then fast-forwards `main`. Also accepts a ticket at `6:pending-merge` |
| `crew ticket cancel <n>` | Any open stage to `0:cancelled`, and closes the issue as not planned |
| `crew ticket fix <n> "<title>"` | For a deployment that failed because of the code: opens a `fix` ticket at `high` and `2:drafting`, and moves `n` from `7:merged` or `8:deploying` to `9:done` and closes it |
| `crew status --as dm --lead <token>` | The DM's status; also advances deployments and removes leftover worktrees and compose projects; see [Session recovery](#session-recovery) |

### Dev

Run inside the ticket's worktree.

| Command | Does |
| --- | --- |
| `crew commit --red\|--green\|--refactor "<message>"` | Checks the TDD phase, commits and pushes immediately; see [Test-driven development](#test-driven-development) |
| `crew commit "<message>"` | Commits a change with no behaviour, such as documentation or configuration, and pushes immediately |
| `crew env up`, `crew env down`, `crew env logs` | Starts the ticket's compose project in the background, stops it and removes its volumes, or prints its logs |
| `crew run <command> [<args>]` | Runs a profile command, such as `test` or `build`, in the `app` container, with any arguments appended |
| `crew ticket submit` | Runs the gate on the exact pushed commit; `4:in-progress` to `4:waiting-review` on success |
| `crew ticket ask "<question>"` | Posts a `question` for the DM and ends the Dev's run; no stage change, and not a failed attempt |
| `crew ticket stuck "<reason>"` | `4:in-progress` to `4:review-rejected`; counts as a failed attempt. Posts the reason as a `log` comment starting `stuck: ` |

### Reviewer

| Command | Stage change |
| --- | --- |
| `crew review <n> --approve` | Posts the verdict as a PR comment review and a ticket comment; `4:waiting-review` to `5:review-approved` |
| `crew review <n> --reject "<reason>"` | Posts the verdict as a PR comment review and a ticket comment; `4:waiting-review` to `4:review-rejected` |

### Anyone

| Command | Does |
| --- | --- |
| `crew status [--as <role>]` | Shows what needs the caller's attention; see [Status](#status) |
| `crew ticket list` | One line per ticket |
| `crew ticket show <n> [--all]` | The terse ticket view |
| `crew ticket comment <n> --kind <kind> --as <role> "<text>"` | Posts a tagged comment |
| `crew ticket block <n> --as <role> "<reason>"` | Sets the `blocked` flag and posts the reason as a `question` comment by the role, `pm` or `dm`, so `crew status` shows it to you |
| `crew ticket unblock <n>` | Clears the `blocked` flag |

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
