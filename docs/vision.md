# Vision: every project has a workfile

New to Workfile? Read [Getting started](getting-started.md) first. This page
uses its words: provider, stage, move, gate and rule.

## The short version

Every project has a `.workfile/` folder. Step into the project's folder, and
its workfile tells you, and your agents, four things:

1. **What we use.** The providers: which Jira, which GitHub organisation, and
   later which chat, CI and deploy tools.
2. **Where everything is.** The board, the repositories, and the documents and
   templates the team relies on.
3. **How work flows.** The stages, the moves between them, and the gates each
   move must pass.
4. **How to ship.** What a piece of work needs, step by step, to get from a
   ticket to done in this project.

Projects differ, and that is fine. The workfile is where each project writes
down its own answers. People and agents read the same file, so they always
agree.

## The problem

Every project works a little differently. One uses Jira statuses called "In
Review", another calls it "Code Review". One needs a risk label on every pull
request (PR), another needs two approvals. One keeps its board on one Jira
site, a client project keeps it on another.

Today that knowledge lives in people's heads, in wiki pages that go out of
date, and, more and more, in the prompts and skills we give to AI agents. An
agent skill that says "move the ticket to In Review on example-team.atlassian.net"
works in one project and breaks in the next. Each new project means editing
every tool, or giving up and doing it by hand.

## The idea

**Facts about the project live in the project. Facts about you live with you.**

| Where | What it holds | Example |
| --- | --- | --- |
| The project's `.workfile/` | Providers and their scope, stages, moves, gates, people | "Review needs one approval and passing CI" |
| Your `~/.config/workfile/` | Your identity and settings | "I am this Jira account" |
| Your credentials file | Tokens, by name | `EXAMPLE_JIRA_TOKEN` |

Tools then stop carrying project details. A tool or an agent talks in
**stages**, such as "move this ticket to review", and asks Workfile what that
means here: which Jira status, which gates, and whether the move is allowed
now.

## Moving between projects

Alice works on two projects:

```text
~/work/example-web/.workfile/     Jira site A, stages todo → doing → review → done
~/work/example-shop/.workfile/    Jira site B, stages backlog → build → qa → released
```

She runs the same commands in each:

```sh
cd ~/work/example-web
wf status --me        # her work, checked against example-web's workflow

cd ~/work/example-shop
wf status --me        # her work, checked against example-shop's workflow
```

`wf` uses the nearest `.workfile/`, so the folder she is in decides the
project. Her agent does the same. Its instructions say "ship this ticket", not
"move it to In Review on site A", so the same agent works in both projects
without a change.

Nothing crosses between projects. Each workspace has its own policy, accounts,
credentials and snapshots.

## Agents

Agents can now do real work: write code, open PRs, move tickets. They need two
things that a token does not give them:

- **Context.** What does this project use, and where is everything? The
  workfile answers, so the agent does not guess or carry a stale copy.
- **Policy.** Is this move allowed now, under this team's rules? `wf check`
  answers before the agent acts. See [Workfile as the referee for
  agents](proposals/agent-referee.md).

Workfile stays the referee. It reads and decides. The agent does the work,
with its own access. Workfile never moves a ticket, adds a label or changes a
PR itself.

### An example: work picked up by an agent

1. Bob adds the label `agent-ok` to a ticket that passes the refinement gate,
   and assigns it to Alice.
2. Something Alice chose starts her agent on the ticket in its own branch. It
   might be a script she runs, or an event from the hosted service (see below).
3. Before each move, the agent asks `wf check`. It does the work and opens a PR.
4. The PR passes the code review gate. A person reviews it and merges it.

Every step uses the project's own workflow. Move to a project with different
stages and gates, and the same agent follows those instead.

## Teams

The policy is the team's contract, for people and for agents alike.

- **Your agent, your PR.** An agent runs with its person's access, so the PR
  is theirs and someone else must approve it.
- **Agent work can have its own gates.** For example, a PR labelled
  `agent-authored` needs two approvals.
- **The rules are checked.** `wf lint` runs in CI, so no change to the policy
  can open a path to done that skips code review.
- **Changes go through review.** The policy sits in the repository, so a rule
  change is a pull request.

## The hosted reconciler

The free CLI reads on demand. Teams that want the work reconciled all the time
need something that watches. That is **Workfile Cloud**, a hosted reconciler.

- Webhooks come in. Workfile reconciles them against `.workfile/`.
- It keeps an event log in policy terms: entered review, ready for done, drift.
- It **emits events** by webhook, GitHub `repository_dispatch` or a stream.
- It **never runs anything**. Your own systems turn an event into a run.

The CLI stays read-only and runs nothing, hosted or not. It is built to be
self-hosted: one Go binary, one database, no cloud-only parts.

## Principles

- **Workfile reads. It never writes** to Jira, GitHub or any other provider.
- **One source of truth.** The workfile points to things, such as a PR
  template or a team's `AGENTS.md`. It does not copy them.
- **Same commands everywhere.** `wf status`, `wf check` and `wf lint` work the
  same way in every project. Only the workfile changes.
- **Tools work without it.** A tool that reads a workfile should still work,
  more simply, in a project that does not have one.

## Built and next

Built today:

- Providers, stages, moves, gates and people in `.workfile/`.
- Five commands: `wf setup`, `wf status`, `wf check`, `wf lint` and `wf tracker`.
- `--json` for agents, and `wf status --since` for what changed.
- Explicit scope in `providers.yml`: the projects and repos that are the team's work.

Next:

- **Tools read the workfile.** `wf workspace --json`, so skills and scripts ask
  Workfile about the project instead of carrying its details. See [Tools read
  the workfile](proposals/tools-read-the-workfile.md).
- **Provider statuses in every move.** `wf status --json` names each move's
  destination stage. It should also give the provider status for that stage,
  from `statuses` in `providers.yml`, so an agent can make the move without
  reading YAML.
- **Where everything is.** Places in the workfile for repositories, PR
  templates, documents and channels, so a person or agent can find them from
  any project.
- **How to ship.** The project's shipping steps, such as commit format, PR
  title, required labels and risk rules, named in the workfile or pointed to
  from it.
- **Limits on work in flight.** Rules that count across tickets, such as "at
  most three agent PRs waiting for review", so agents do not outpace
  reviewers.
- **Review comment facts.** A fact for unresolved review comments, so an
  a check can catch it when a reviewer asks for a change.
- **The hosted reconciler.** Events in policy terms, emitted to your own systems.
- **A guard for agents.** `wf guard`, from the [referee
  proposal](proposals/agent-referee.md), so every move goes through a check.
