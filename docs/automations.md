# Automations

New to Workfile? Read [Getting started](getting-started.md) first. This page
uses its words: stage, move, gate and rule.

Your `.workfile/` folder is the source of truth for how work moves: the stages,
the moves between them, and the gates each move must pass. Workfile checks Jira
and GitHub against it. Automations put the same source of truth to work. They
let you say **"when this happens to my work, do that"**:

- When a pull request (PR) on my ticket gets its first approval, ask my coding
  agent to write a summary.
- When my ticket can move to done, because every gate for done passes, run a
  script that posts in our release channel.

Three ideas keep this safe:

- **The policy is the rails.** Stages, moves and gates. Shared by the whole
  team. Automations cannot change it.
- **Automations are the trains.** Each person writes their own, and they run on
  their own computer. They can add steps but never skip a gate: the `ready`
  trigger fires only when the policy allows the move, and an agent can ask
  `wf check` before it moves anything.
- **Workfile is the referee.** It notices changes and says what is allowed.
  Your scripts and agents do the work, with their own access. Workfile's checks
  still only read: `wf health` and `wf status` never change anything.

## Where automations live

You can keep automations in two places, and use either or both:

| File | Who can see it |
| --- | --- |
| `.workfile/automations/<you>.yml` | **Shared.** Committed with the policy, so your team can read it, copy good ideas and review changes. `<you>` is your name in `people.yml`. |
| `~/.config/workfile/<workspace>/automations.yml` | **Private.** Only on your computer. Good for experiments, habits that only suit you, or a repository where you cannot commit. |

Each computer only runs its own person's automations. Bob's computer never runs
Alice's file.

A **workspace** is one `.workfile/` folder. A private file finds its workspace by
name, which the workspace sets once in `policy.yml`:

```yaml
# .workfile/policy.yml
name: example-web
```

Every clone of the repository then has the same name, so Alice's private file is
`~/.config/workfile/example-web/automations.yml`. Names use lowercase letters,
digits and single dashes. `XDG_CONFIG_HOME` replaces `~/.config`.

To start a private file, run this inside the repository:

```sh
wf automations init
```

It creates the file with one automation that only reports. Then edit it.

## An automation file

Shared and private files use the same format. A private file also lists the
repositories it may run in (see [Safety](#safety)).

```yaml
# ~/.config/workfile/example-web/automations.yml
repos:
  - github.com/example-team/web

automations:
  # First approval: ask my agent to tidy up the PR description.
  - id: summary-on-approval
    on:
      becomes: github.approvals >= 1
    stage: review
    agent: |
      {record} on {ticket} was just approved. Rewrite its description as a
      short summary of the change. Do not push code.
    mode: ask

  # Every gate for done passes: post in the release channel.
  - id: announce-done
    on:
      ready: done
    when: github.labels excludes do-not-merge
    run: [./scripts/announce.sh, "{ticket}", "{url}"]
    mode: auto
```

Each automation has:

| Field | Meaning |
| --- | --- |
| `id` | A short name. Unique across your shared and private files. |
| `on` | The trigger: exactly one of `ready`, `enters` or `becomes`. |
| `stage` | Optional. Only tickets in this stage, or a list of stages. |
| `when` | Optional. An extra condition, written like a policy rule. If it is false when the trigger fires, the automation is skipped that time. A `when` about PRs must hold for every linked PR, or for the PR that fired. |
| `scope` | Optional. `mine` (the default) means tickets assigned to you, or linked to a PR you opened or were asked to review, the same as `wf status --me`. The work only has to be yours when the change is seen, so assigning a ticket to yourself and moving it between two checks still fires. `all` means every ticket in the search. |
| `run` or `agent` | The action: a command, or a prompt for your agent. Exactly one. |
| `mode` | `observe` (the default), `ask` or `auto`. |

Mistakes are errors, the same as in the policy: unknown fields, a stage the
policy does not have, an expression the policy could not use, a shared file
whose name is not a person in `people.yml`, or an `id` used twice. `wf health`
and `wf status` check shared files and stop with an error if one is wrong, but
they never run anything.

## Triggers

Workfile reads today's facts, not history. So a trigger is a **change between
two checks**: something that was false last time and is true now.

| Trigger | Fires when | Example |
| --- | --- | --- |
| `ready: <stage>` | A move to that stage becomes allowed: every gate for it now passes. | `ready: done` |
| `enters: <stage>` | The ticket is now in that stage and was not last time. | `enters: review` |
| `becomes: <condition>` | A condition that was false is now true. It uses the [policy's expressions](policy.md#expressions). | `becomes: github.approvals >= 1` |

`ready` is the one to use for "move it on when it is allowed". It uses your
team's own gates, so it can never fire for a move the policy would not allow.

A `becomes` condition about PRs, such as `github.approvals`, is checked for each
linked PR. It fires once for each PR that changes.

These rules keep triggers predictable:

- **The first check is a starting point.** The first time Workfile sees an
  automation or a ticket, it only records the facts. Nothing fires for something
  that was already true.
- **Each change fires once.** It fires again only after the fact turns false
  and then true again.
- **Unknown is not a change.** If evidence cannot be read, for example while
  GitHub is down, nothing fires, and coming back to the same value does not
  fire either.
- **Editing resets.** Changing `on`, `stage`, `when` or `scope` gives that
  automation a new starting point, so a fixed condition does not fire for every
  ticket at once.

## Actions

### `run`: a command

```yaml
run: [./scripts/announce.sh, "{ticket}", "{url}"]
```

- A list: the program, then its arguments. There is **no shell**, so ticket
  titles and other text from Jira or GitHub can never become commands. If you
  really want a shell, use `run: [sh, -c, "..."]` and read values from the
  environment variables below, not from placeholders inside the script.
- A shared file's command starts in the folder that holds `.workfile/`. A
  private file's command starts in your private folder, so
  `./scripts/announce.sh` is `~/.config/workfile/example-web/scripts/announce.sh`.
  A private file never runs a script that a repository ships.
- It may run for up to 10 minutes. Its output goes to a log.

### `agent`: a prompt

```yaml
agent: |
  {record} on {ticket} was just approved. Rewrite its description.
```

Set your agent command once, in `~/.config/workfile/config.yml`. Any command that
takes a prompt as its last argument works:

```yaml
agent: [claude, -p]
```

The agent starts in the repository, so it can work on the code. Without an
agent command, automations that use `agent` are listed as "needs an agent
command" and do not run.

Ticket titles are written by other people, so a prompt that includes one could
carry instructions you did not write. Workfile puts `{title}` between
`<untrusted>` markers and tells the agent to treat that text as information,
not instructions. In `ask` mode you see the whole prompt before it runs.

### Placeholders and environment

| Placeholder | Environment variable | Value |
| --- | --- | --- |
| `{ticket}` | `WORKFILE_TICKET` | The ticket key, such as `APP-71`. |
| `{title}` | `WORKFILE_TITLE` | The ticket title. |
| `{url}` | `WORKFILE_URL` | The ticket's web address. |
| `{stage}` | `WORKFILE_STAGE` | The ticket's stage now. |
| `{to}` | `WORKFILE_TO` | For `ready`: the stage it can move to. |
| `{record}` | `WORKFILE_RECORD` | For a trigger about PRs: the PR, such as `web#207`. |
| `{record_url}` | `WORKFILE_RECORD_URL` | For a trigger about PRs: the PR's web address. |
| `{automation}` | `WORKFILE_AUTOMATION` | The automation's `id`. |

Each placeholder is replaced inside one argument only. Using one the trigger
does not have, such as `{record}` with `enters`, is an error.

`WORKFILE_TRIGGER` describes the trigger, `WORKFILE_PROVIDER` names the PR's
connection, and `WORKFILE_ROOT` is the folder that holds `.workfile/`.
`WORKFILE_EVENT` is the path of a JSON file with the trigger and the ticket's
assessment, in the same format as one ticket of `wf status --json` (see
[JSON reports](json.md)). Your command gets your normal environment, without the
variables that hold Workfile's own tokens.

### Ask the policy before you move

An automation fired a little while ago. Before an agent or script moves a
ticket, it can ask whether the move is still allowed:

```sh
wf check APP-71 --to done --json
```

It reads the ticket fresh. It exits `0` when the move is allowed, `1` when it is
not, and `2` when it cannot tell. The JSON lists what is unmet and each linked
PR's latest commit (`head`), so the agent can confirm nothing changed. A good
agent prompt ends with "run `wf check {ticket} --to done` first, and stop if it
fails".

## Modes

| Mode | When it fires |
| --- | --- |
| `observe` | `wf watch` says what it would have done. Nothing runs. The default, so a new automation is safe to try. |
| `ask` | `wf watch` shows the ticket, the trigger and the exact command or prompt, and waits for `y` or `n`. |
| `auto` | It runs straight away. |

`wf watch --dry-run` treats every automation as `observe`.

A question needs someone at a terminal. `wf watch --once`, or output piped to a
file, never asks: the question waits for the next `wf watch` in a terminal, for
up to a day. After that it is skipped.

## Safety

`wf watch` runs nothing you have not approved.

**Your shared file** is in the shared repository, so someone else could change it
in a pull request. Workfile compares its actions (`run`, `agent`, `mode` and
`scope`) with the last version you approved on this computer. It also compares
the contents of any repository script that `run` names by a relative path, such
as `./scripts/announce.sh`. If any changed, or you have never approved it, the
file does not run, and `wf automations` shows what changed. A script that
changes while `wf watch` is running, such as after a `git pull`, does not run
either. Programs on your PATH, and files a command reads for itself, such as a
`Makefile`, are not compared. Approve it with:

```sh
wf automations trust
```

**Your private file** needs no approval: only you can change it. But any
repository can claim any workspace name, so a private file also lists the
repositories it may run in:

```yaml
repos:
  - github.com/example-team/web
```

Workfile compares the list with the git remotes of the repository that holds
`.workfile/`. Remotes are set when you clone, and are never part of a
repository's files, so a repository cannot fake them. `git@github.com:...` and
`https://github.com/...` count as the same. Every clone and worktree of the
repository matches. A fork has its own remote, so it does not match unless you
add it. If nothing matches, nothing in the private file runs, and
`wf automations` says which line to add. Private files need a git repository.

Other limits:

- At most 10 actions start per check. The rest wait for the next check, so a
  flood of changes cannot start dozens of agents at once.
- Actions run one at a time.
- A failed action is reported and not retried. It can fire again only after
  its trigger changes again.
- Only one `wf watch` runs per workspace folder at a time.

## Commands

| Command | What it does |
| --- | --- |
| `wf automations` | Lists your automations, shared and private, and anything stopping them, with what to do. Exits `1` when something needs attention. |
| `wf automations --all` | Everyone's shared automations in this workspace, by person. Read only, for sharing ideas. Private files never appear. |
| `wf automations init` | Creates your private file for this workspace. Needs `name:` in `policy.yml` and a git remote. |
| `wf automations trust` | Approves the actions in your shared file. |
| `wf automations --log ID` | Shows the output of the automation's latest run. |
| `wf watch` | Checks your work every 2 minutes and fires automations. Press `q` to stop. |
| `wf watch --once` | One check, then exit. For cron or launchd. |
| `wf watch --interval 5m` | A different time between checks. At least 1 minute. |
| `wf watch --dry-run` | Shows what would run. Runs nothing. |
| `wf check KEY --to STAGE` | Says whether the ticket may move to that stage now. |

`wf automations` looks like this before Alice approves her shared file:

```text
  Automations · example-web
  for Alice · 3 automations · 2 can run

  ✗ NEEDS ATTENTION
  1. Shared file .workfile/automations/alice.yml: You have not
     approved its actions on this computer yet. Check them, then run
     wf automations trust.

  ✗ SHARED WITH THE TEAM
  .workfile/automations/alice.yml
  Not running here: see Needs attention.

  look-at-failing-ci                                         observe
    when github.checks == failure turns true · in doing or review
    asks your agent: CI just failed on {record} ({record_url}) for
    {ticket}. …

  ✓ PRIVATE
  ~/.config/workfile/example-web/automations.yml

  summary-on-approval                                            ask
    when github.approvals >= 1 turns true · in review
    asks your agent: {record} on {ticket} was just approved. …

  announce-done                                                 auto
    when it can move to done · if github.labels excludes
    do-not-merge
    runs ./scripts/announce.sh {ticket} {url}
```

Once an automation has run, a last line says what happened, such as
`last finished · APP-71 · 10:42`.

`wf watch` prints a block for each check with something to report, and one for
each action that finishes. Under them, one line says what it is doing now:

```text
  Watching example-web · every 2m · for Alice
  3 automations · 3 can run

  ✓ 10:42 · 1 automation fired

  APP-71  Explain notification preferences
    announce-done · can move to done
    · running

  ✓ 10:42 · announce-done finished
  APP-71  Explain notification preferences
    finished in 3s · log: wf automations --log announce-done

  Next check in 1m 58s · q to stop
```

When an `ask` automation fires, that line becomes a question with the exact
command or prompt:

```text
  ? Run summary-on-approval?
  APP-71  Explain notification preferences
    github.approvals >= 1 turns true · web#207

  Your agent gets this prompt:
    web#207 on APP-71 was just approved. Rewrite its description as
    a short summary of the change. Do not push code.
  with: claude -p · in ~/code/example-web

  y run  ·  n skip  ·  q stop watching
```

`wf watch` checks every ticket in your Jira search, and the PRs linked to them,
each time. A long interval uses less of your Jira and GitHub request limits.

## Where things are kept

| Path | What it holds |
| --- | --- |
| `~/.config/workfile/config.yml` | Your identity (`me`), `me.person` (your name in `people.yml`, if Workfile cannot tell from your accounts), and `agent`. |
| `~/.config/workfile/<workspace>/` | Your private automations, and any scripts they run. |
| `~/.local/state/workfile/<workspace>-<hash>/` | What each check saw, what fired, approvals and run logs, for one workspace folder. `XDG_STATE_HOME` replaces `~/.local/state`. |

Run logs are kept for 14 days. Deleting the state folder gives every automation
a fresh starting point: nothing fires for facts that are already true. You will
need to approve your shared file again.

Workfile finds your name in `people.yml` from `me.person`, or else from the
person whose Jira or GitHub account matches yours:

```yaml
# ~/.config/workfile/config.yml
me:
  person: alice
agent: [claude, -p]
```
