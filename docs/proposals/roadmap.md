# Roadmap: workflow and automations as code

**Status update (2026-11-13):** automations moved to the hosted product. The
CLI no longer runs them. See the [vision](../vision.md). The text below is kept
as written.

**Status:** steps 1 to 5 are built, with the single-workspace parts of step 6
(one `wf watch` per workspace folder). Per-workspace `me` and `agent`
settings, `wf workspaces`, `wf watch --all`, `--json` events and step 7 are not
built yet. The user guide is Automations.

New to Workfile? Read [Getting started](../getting-started.md) first. This page
uses its words: stage, move, gate and rule. For where this is all heading, read
the [vision](../vision.md).

## The idea

Today a team writes its workflow down in `.workfile/` and Workfile checks
Jira and GitHub against it. Next, the same folder also holds **automations**:
"when this happens to my work, do that". Workflow and automations become code,
for your team, committed in the repo.

- **The policy is the rails.** Stages, allowed moves and gates. Shared by the
  whole team. Unchanged.
- **Automations are the trains.** Each person writes their own, and they run
  on their own computer. They can add steps but never skip a gate.
- **Workfile is the referee.** It notices changes and says what is allowed.
  Agents and scripts do the work, with their own access. Workfile's checks
  still only read.

Example: when a PR on my ticket passes every gate for "done", my agent checks
it again, merges it and moves the ticket.

## Workspaces

A workspace is one `.workfile/` folder. Workfile already works this way: it
finds the nearest `.workfile/` from where you run it.

- **One repo** has its own `.workfile/` at its root.
- **A group of repos** shares one `.workfile/`, kept in one of them or in a
  small repo of its own. `providers.yml` already lists the GitHub organisation
  and the Jira search that cover every repo in the group.
- **Different companies** are different workspaces. Each has its own policy,
  automations, accounts and credentials. Nothing crosses between them.

```text
example-web/.workfile/
  policy.yml               the workflow (exists today)
  providers.yml            where to read (exists today)
  people.yml               who is who (exists today)
  automations/
    alice.yml              Alice's shared automations (new)
    bob.yml                Bob's shared automations (new)
```

Your computer keeps a few things outside the repo, one set per workspace:

```text
~/.config/workfile/config.yml                    your identity and agent command, per workspace
~/.config/workfile/<workspace>/automations.yml   your private automations (new)
~/.local/state/workfile/<workspace>-<hash>/      what already fired, run logs, approvals
```

Identity is per workspace because you may be `example-alice` at one company and
`alice-example` at another.

Automations can be shared or private. Shared files are committed, so the team
can read and copy them. Private files stay on your computer and connect to a
workspace by its `name:` in `policy.yml`, for example
`~/.config/workfile/example-web/automations.yml`. Any repository can claim a
name, so a private file also lists the git remotes it may run in. You can use
both. Details
are in Where automations live.

## An automation file

This mock uses the stages from the example policy in
[Policy files](../policy.md): `todo`, `doing`, `review` and `done`.

```yaml
# .workfile/automations/alice.yml
# Alice's automations for this project. The team can read this file.
# Only Alice's computer runs it.

automations:
  # Start a plan as soon as I pick up a ticket.
  - id: plan-on-pickup
    on:
      enters: doing
    agent: |
      /plan {ticket}
      Write the plan as a comment on {ticket}. Do not write code yet.
    mode: auto

  # Tell me when CI breaks on one of my PRs, and have a first look.
  - id: look-at-failing-ci
    on:
      becomes: github.checks == failure
    stage: [doing, review]
    agent: |
      CI just failed on {record} ({record_url}) for {ticket}.
      Find the failing check and explain the cause. Do not push anything.
    mode: auto

  # First approval: tidy up the PR description before it goes on.
  - id: summary-on-approval
    on:
      becomes: github.approvals >= 1
    stage: review
    agent: |
      /pr-summary {record}
    mode: ask

  # Every gate for "done" passes: re-check, merge, move the ticket.
  - id: merge-when-ready
    on:
      ready: done
    when: jira.labels excludes do-not-merge
    agent: |
      /merge-and-close {ticket} {record}
    mode: ask

  # A plain script, no agent: post in the team channel when work ships.
  - id: announce-done
    on:
      enters: done
    run: [./scripts/announce.sh, "{ticket}", "{url}"]
    mode: observe
```

What each part means:

- `on` is the trigger. `enters` fires when a ticket arrives in a stage,
  `becomes` when a condition turns true, and `ready` when every gate for a move
  now passes. `ready: done` uses the team's own gates, so it can only fire when
  the policy would allow the move.
- `stage` and `when` narrow it down. Conditions use the same words as policy
  rules.
- `agent` is a prompt for your agent command, usually naming one of your
  skills. `run` is a command, run with no shell.
- `mode` is how much you trust it. `observe` only reports, `ask` waits for a
  yes, and `auto` just runs. New automations start in `observe`.
- Every automation looks only at your own work unless it says `scope: all`.

The same file works as a private file. Moved to
`~/.config/workfile/example-web/automations.yml`, it runs the same way, but
only Alice can see it.

The full file format, triggers, modes and safety limits are in
Automations.

## What to add to Workfile

Each step is useful on its own and ships with tests that use made-up responses.

### 1. `wf check`: the safety check for agents

```sh
wf check APP-71 --to done --json
```

Reads the ticket fresh and answers: is this move allowed right now, and if
not, what is unmet? It exits 0 when the move is allowed and 1 when it is not.
The JSON includes each linked PR's latest commit, so an agent can confirm
nothing changed since its trigger fired.

Every automation leans on this. It is also useful today, with no automations
at all: any agent or script can ask before it moves a ticket.

### 2. Workspace names and identity

- An optional `name:` in `policy.yml`, so a workspace keeps the same name
  across clones and computers. Without it, Workfile uses the folder path. Names
  are safe folder names (lowercase letters, digits and dashes), because private
  automations live in a folder with that name. Private automations need one.
- Per-workspace sections in `~/.config/workfile/config.yml` for `me` and
  `agent`, with a default for workspaces that do not set them.
- `wf workspaces` lists the workspaces this computer knows about and whether
  each one reads cleanly.

### 3. Automation files

Load and check `.workfile/automations/<person>.yml` and your private
`~/.config/workfile/<workspace>/automations.yml`. Mistakes are errors, the same
as policy mistakes. `wf automations` lists yours from both. `wf automations
--all` lists everyone's shared files, for sharing ideas. `wf automations init`
creates your private file.

### 4. Triggers

`wf watch --once --dry-run` checks your work, compares it with the last check,
and prints what would fire. The rules are in
Triggers: the first check fires nothing,
each change fires once, and unknown is never a change.

### 5. Actions and modes

`run` (a command, no shell) and `agent` (a prompt for your agent command), in
`observe`, `ask` or `auto` mode. A trust step stops your automations if someone
else changes your shared file in a pull request, until you approve it. A
private file runs only in repositories whose git remote is in its `repos:`
list, and its `run` commands start from your private folder.

### 6. Many workspaces, one runner at a time

- `wf watch --all` watches every workspace on this computer.
- A lock per workspace, so `wf watch` and Work Hub never both fire the same
  automation.
- `--json` output for each event, so another program can show it and run it.

### 7. Watching without hitting rate limits

`wf health` reads everything in the lookback window: one GitHub search per
repo, one Jira search, and the Jira history for each ticket. That is right for
a report, but too much to repeat every 2 minutes. A watch check reads only
what it needs, in three parts:

1. **What changed.** One Jira search for your work updated since the last
   check, and one GitHub search across the organisation
   (`org:example-org is:pr updated:>=<last check>`), not one per repo. Each
   overlaps the last check by a minute so nothing slips between them.
2. **What you are waiting on.** CI finishing does not mark a PR as updated, so
   the search above misses it. Workfile keeps a short list of your open PRs
   that are waiting on something, such as checks or an approval, and reads
   them again in one batched query.
3. **A full read now and then.** About every 30 minutes, read everything as
   `wf health` does, to catch anything the first two missed.

Limits to stay inside:

- **GitHub** allows 5,000 points an hour for each person's account. A small
  search costs about 1 point. Your agent's `gh` commands, Work Hub, `wf` and
  every workspace that uses the same account all share that budget.
- **Jira Cloud** does not publish a fixed limit for personal API tokens. It
  answers "too many requests" and says how long to wait. Workfile already
  waits and retries.

How often to check:

- Every 2 minutes by default. Never more than once a minute.
- Every GitHub query also asks how much of the hour's budget is left. Below
  20%, checks slow down until the budget resets.
- After a "too many requests" answer, wait as told, then check less often for
  a while.
- Check every 10 minutes when nothing of yours is open, or when the computer is
  idle.
- The lock from step 6 also means one budget: Work Hub and `wf watch` never
  both check the same workspace.

A rough cost, for 10 repos checked every 2 minutes: reading everything is
about 300 to 600 points an hour, and reading only changes is about 30 to 60.

Checking faster than once a minute needs webhooks, where GitHub and Jira send
changes as they happen. They need a public address, so they come later, with
the cloud runner.

## What to build outside Workfile

- **Agent skills.** The steps live in your agent, not in Workfile. A
  `merge-and-close` skill: run `wf check`, confirm the approval is on the
  latest commit and CI passed, merge the PR, move the ticket, comment with a
  summary. A `refine` skill for when a ticket is assigned to you.
- **Write access for the agent.** The GitHub CLI for merging and a Jira tool
  for moving tickets, set up per workspace with that company's accounts.
  Workfile's own credentials are never passed to actions.
- **Running in the background.** A launchd job that runs `wf watch --all`, for
  when Work Hub is closed. Later, a scheduled cloud job so automations run
  while your laptop sleeps.

## Where Work Hub fits

Work Hub is the screen and the runner. Workfile is the rules and the trigger
engine. There is one engine, written and tested once, in Go.

| Job | Owner |
| --- | --- |
| Stages, moves, gates | Workfile policy |
| Automations | Workfile automation files |
| Noticing a trigger has fired | Workfile (`wf watch --once --json`) |
| Running the action | Work Hub while open, `wf watch` when not |
| `ask` mode | Work Hub Inbox item with Run and Skip buttons |
| Run history and logs | Workfile state folder, shown in Work Hub |
| Comments, Slack mentions, review requests | Work Hub Inbox (not triggers yet) |

This matches Work Hub's rule that Workfile is the only place to configure
anything. Its planned Automations table reads the same files. Its own trigger
evaluation and Jira transition action are no longer needed. Moves happen
through agents and scripts, and they ask `wf check` first.

## Out of scope for now

- **Moves by Workfile itself.** A built-in `move:` action needs Jira write
  access, which Workfile does not have. An agent or script does the move.
- **Shared team automations.** These need one runner for the team, for example
  a CI job, so they do not run once per person.
- **Events Workfile does not read**, such as new comments, Slack messages or
  review requests. Automations use facts the policy can already see.

## Order

1. `wf check`. Smallest, safest, and every later step needs it.
2. Workspace names and per-workspace identity.
3. Automation files and `wf automations`.
4. `wf watch --once --dry-run`.
5. Actions, modes and trust. First real automation: merge when ready, in `ask`
   mode.
6. `--all`, locks and `--json` events.
7. Watching without hitting rate limits: read only changes, re-read PRs you
   are waiting on, read the GitHub budget and slow down.
8. Work Hub reads automations, shows `ask` in Inbox and runs actions.
9. Later: team automations in `.workfile/automations/team.yml` with one
   shared runner (a CI job), always-on cloud running, webhooks, and triggers
   from comments and Slack.

## Done when

- Alice commits `.workfile/automations/alice.yml`. When her PR passes every
  gate for "done", her agent asks her once, re-checks, merges and moves the
  ticket.
- Bob can read Alice's file, but his computer never runs it.
- Alice has two workspaces for two companies. Each uses its own identity,
  credentials and agent, and nothing crosses between them.
- With Work Hub open and `wf watch` running, each automation fires once.
- A day of watching 10 repos every 2 minutes uses less than 10% of the GitHub
  budget, and a PR waiting on CI fires within one check of CI finishing.
- `wf health` and `wf status` behave exactly as before.

## Open questions

1. Should `name:` live in `policy.yml` or `providers.yml`?
2. Does Work Hub run `wf` as a command each sync, or should Workfile offer a
   long-running mode it can talk to?
