# Proposal: personal automations (MVP)

**Status:** built. The user guide is [Automations](../automations.md), and
`wf check` is in [Using wf](../usage.md#check-may-this-ticket-move-now). This
page keeps the design notes. Where they differ, the guide is right: `wf watch`
draws panels, not edge blocks, and a question with nobody at a terminal waits
up to a day for the next `wf watch` in a terminal.

New to Workfile? Read [Getting started](../getting-started.md) first. This page
uses its words: stage, move, gate and rule.

## The idea

Today Workfile tells you when work is ready to move, or what is stopping it.
You still have to notice and act. Automations let you say **"when this
happens to my work, do that"**. For example:

- When a PR on my ticket gets its first approval, ask my coding agent to move
  the ticket to "Ready" and write a summary comment.
- When one of my tickets can move to ship, run a script that posts in our
  release channel.

Each person writes their own automations for each workspace (one `.workfile/`
folder). You can keep them in two places, and use either or both:

- **Shared**, in the workspace's `.workfile/` folder, next to the policy. The
  whole team can see them, copy good ideas and review changes.
- **Private**, in your own config folder, under the workspace's name. Nobody
  else sees them. Good for experiments, habits that only suit you, or a
  workspace where you cannot commit.

Each person's computer only runs that person's own automations.

## What stays the same

Workfile's checks still only read. `wf health` and `wf status` do not change.
Workfile itself never calls a Jira or GitHub API that changes something.

Automations are a separate command, `wf watch`, that you choose to run. When
something happens, it starts a command or an agent that **you** configured, on
**your** computer, with **your** permission. If that command moves a ticket,
the command did it, with its own access. The README sentence "Workfile only
reads" becomes "Workfile's checks only read. Automations run commands you
choose."

## Files

| File | What it holds |
| --- | --- |
| `.workfile/automations/<person>.yml` | Shared: one person's automations for this workspace. `<person>` is their name in `people.yml`. Committed, so the team can see it. |
| `~/.config/workfile/<workspace>/automations.yml` | Private: your automations for one workspace. `<workspace>` is the workspace's `name:`. Never committed. |
| `~/.config/workfile/config.yml` | Personal settings: who you are, and which agent command to use. Not committed. |

Both automation files are optional, and they use the same format. A workspace
with neither works exactly as it does today. `wf health` and `wf status` check
the shared files for mistakes like every other file, but never run them. They
do not read private files, because they report on the team. `wf automations`
and `wf watch` check your private file.

If you have both, Workfile runs both. An `id` must be unique across the two
files; the same `id` in both is an error, not an override.

### Linking a private file to its workspace

A private file finds its workspace by name. The workspace sets the name once,
in `policy.yml`:

```yaml
# .workfile/policy.yml
name: example-web
```

Every clone of the repo, on every computer, then has the same name, and
Alice's private file is `~/.config/workfile/example-web/automations.yml`.
`XDG_CONFIG_HOME` replaces `~/.config`, as it does for the other personal files.

- **A name is required for private files.** Without `name:`, Workfile does not
  look for a private file, and `wf automations` says to add one.
- **Names are safe folder names:** lowercase letters, digits and dashes. The
  name comes from the repository, so anything else, such as `../`, is an error.
- **The private file says which repositories it belongs to.** Any repository
  can claim any name, so the name only finds the file. A `repos:` list at the
  top decides whether it runs:

  ```yaml
  # ~/.config/workfile/example-web/automations.yml
  repos:
    - github.com/example-team/web
  automations:
    - id: move-when-approved
      ...
  ```

  Workfile compares the list with the git remotes of the repository that holds
  `.workfile/`. Remotes are set when you clone and are never part of the
  repository's files, so a repository cannot fake them. `git@github.com:...`
  and `https://github.com/...` count as the same. If no remote matches,
  nothing in the private file runs, and `wf automations` says which remote it
  found and which line to add.

  Every clone and worktree of the same repository matches, with no extra
  step. A fork has its own remote, so it does not match unless you add it.
- **Private `run` commands start from your private folder**, not the
  repository. `./scripts/announce.sh` is
  `~/.config/workfile/example-web/scripts/announce.sh`, a script you wrote,
  never one the repository ships. Agents still start in the repository, so
  they can work on its code.
- **Private files need git.** A workspace that is not in a git repository
  cannot use a private file in the MVP.

`wf automations init` creates the folder and a file whose `repos:` lists the
current repository's remotes.

### An automation file

```yaml
# .workfile/automations/alice.yml — Alice's automations for this project
automations:
  - id: move-when-approved
    on:
      becomes: github.approvals >= 1
    stage: review
    agent: |
      A pull request for {ticket} "{title}" was just approved.
      Move {ticket} to "Ready to ship" in Jira, then comment with a
      one-line summary of each linked PR.
    mode: ask

  - id: announce-ready
    on:
      ready: ship
    run: [./scripts/announce.sh, "{ticket}", "{url}"]
    mode: auto
```

Each automation has:

- `id`: a short name, unique within the file. It is how Workfile remembers what
  already ran. Same character rules as rule IDs.
- `on`: the trigger. Exactly one of the triggers below.
- `stage`: optional. Only tickets in this stage. One stage or a list.
- `when`: optional. An extra condition, written like a policy rule's `when`.
  If it is false, the automation is skipped this time.
- `scope`: optional. `mine` (the default) means tickets assigned to you, or
  linked to a PR you opened or were asked to review. This is the same test as
  `wf status --me`. `all` means every ticket in the search.
- `run` **or** `agent`: the action. Exactly one.
- `mode`: `observe`, `ask` or `auto`. Default `observe`.

Mistakes are errors, not guesses, like the policy files: unknown fields, a
`ready` stage that no move leads to, an expression the policy could not use, a
file whose name is not a person in `people.yml`, or two automations with the
same `id`.

### Personal settings

```yaml
# ~/.config/workfile/config.yml
me:
  person: alice            # optional; see "Whose file runs" below
  jira: example-alice-account-id
  github: example-alice

agent: [claude, -p]        # the prompt is added as the last argument
```

`agent` is any command that takes a prompt as its last argument. Workfile does
not care which agent you use. Without `agent`, automations that use `agent:`
are listed as "needs an agent command" and never run.

## Triggers

Workfile reads today's facts, not history. So a trigger is a **change between
two checks**: something that was not true last time `wf watch` looked, and is
true now.

| Trigger | Fires when | Example |
| --- | --- | --- |
| `ready: <stage>` | A move to that stage becomes available: every gate for it now passes. | `ready: ship` |
| `enters: <stage>` | The ticket is now in that stage and was not last time. | `enters: review` |
| `becomes: <condition>` | A condition that was false is now true. Uses the policy's expression language. | `becomes: github.approvals >= 1` |

`ready` is the one to use for "move it on when it is allowed". It uses your
team's own gates, so an automation can never fire for a move the policy would
not allow at that moment.

A `becomes` condition about pull requests, such as `github.approvals`, is
checked for each linked PR, like a gate rule. It fires once for each PR that
changes, and `{record}` names that PR.

Some rules keep triggers predictable:

- **The first check is a starting point.** The first time `wf watch` runs in a
  project, or sees a ticket or automation for the first time, it only records
  facts. Nothing fires for things that were already true.
- **Each change fires once.** An automation fires again for the same ticket
  (and PR) only after the fact turns false and then true again.
- **Unknown is not a change.** If evidence cannot be read, for example GitHub is
  down, the fact counts as unchanged. Unknown never fires a trigger, and
  coming back from unknown to the same value does not fire one either.
- **Editing an automation resets it.** Changing `on`, `stage`, `when` or
  `scope` starts a new starting point for that automation, so a fix to a
  condition does not fire for every ticket at once.

## Actions

### `run`: a command

```yaml
run: [./scripts/announce.sh, "{ticket}", "{url}"]
```

- A list: the program, then its arguments. There is no shell, so ticket titles
  and other provider text can never become commands. Use
  `run: [sh, -c, "..."]` only if you really want a shell, and then pass values
  in environment variables, not in the script text.
- It runs from the folder that holds `.workfile/`. In a private file, it runs
  from your private folder instead.
- Placeholders: `{ticket}`, `{title}`, `{url}`, `{stage}`, `{to}` (for
  `ready`), `{record}` and `{record_url}` (for PR triggers), `{automation}`.
  Each is replaced inside one argument only.
- The same values are in environment variables: `WORKFILE_TICKET`,
  `WORKFILE_STAGE` and so on. `WORKFILE_EVENT` is the path of a JSON file
  holding the trigger and the ticket's assessment, in the same format as
  `wf status --json`.
- Workfile's own credentials are removed from the environment. The command gets
  your normal environment otherwise.
- Time limit: 10 minutes. Output is saved to a log file (see "Where things are
  kept").

### `agent`: a prompt

```yaml
agent: |
  A pull request for {ticket} "{title}" was just approved. ...
```

Workfile fills in the placeholders and runs your `agent` command with the
prompt as its last argument, from the project folder. It also sets
`WORKFILE_EVENT`, so the agent can read the full assessment.

Ticket titles and descriptions are written by other people. A prompt that
includes them could carry instructions you did not write. Workfile puts
provider text between clear markers in the prompt, and `ask` mode shows you the
filled-in prompt before it runs.

## Modes

| Mode | What happens when it fires |
| --- | --- |
| `observe` | `wf watch` prints what it would have done. Nothing runs. The default, so a new automation is safe to try. |
| `ask` | `wf watch` shows the ticket, the trigger and the exact command or filled-in prompt, and asks yes or no. If nobody answers, nothing runs. |
| `auto` | Runs straight away. |

`wf watch --dry-run` treats every automation as `observe`.

## Whose file runs

Your computer only runs your own automations. Your private file is yours by
where it lives, so it needs no lookup and no entry in `people.yml`. Workfile
finds your shared file like this:

1. `me.person` in your personal settings, if set.
2. Otherwise, the person in `people.yml` whose accounts match your `me`
   identity (the same identity `wf status --me` uses).
3. Otherwise, your shared file does not run, and `wf automations` says how to
   set `me.person`. Your private file still runs.

Other people's files are never run, even with `scope: all`. Shared automations
that run once for the whole team need a single shared runner, which is out of
scope for the MVP (see "Later").

## Trust

Your shared file is in the shared repository, so someone else could change it
in a pull request. Before running anything, `wf watch` compares your file's
actions with the last version you approved. If any `run`, `agent`, `mode` or
`scope` changed, it shows the difference and stops until you run:

```sh
wf automations trust
```

Approval is stored on your computer, not in the repository.

Your private file needs no approval. Only you can change it, and its `repos:`
list already says where it may run.

## Commands

| Command | What it does |
| --- | --- |
| `wf automations` | Lists your automations, shared and private: trigger, action, mode, which file, whether they are trusted, and when each last fired or failed. |
| `wf automations --all` | Everyone's shared automations in this workspace, by person. Read only, for sharing ideas. Private files never appear. |
| `wf automations init` | Creates your private file for this workspace, with `repos:` set to this repository's remotes. Needs `name:` in `policy.yml`. |
| `wf automations trust` | Approves the current actions in your shared file. |
| `wf watch` | Checks your work every 2 minutes and fires automations. Stops with Ctrl+C. |
| `wf watch --once` | One check, then exits. For cron or launchd. |
| `wf watch --interval 5m` | A different check interval. Minimum 1 minute. |
| `wf watch --dry-run` | Prints what would run. Runs nothing. |

`wf watch` output follows the other commands: a block per check with something
to report, the ticket key first, then the automation and what happened.

```text
  workfile / watch · every 2m · alice

  ▌  10:42 · 1 automation fired
  ▌
  ▌  APP-71  Explain notification preferences
  ▌    move-when-approved · example-team/web #207 approved
  ▌    ask: run agent? [y/N] y
  ▌    ✓ finished in 48s · log: wf automations --log move-when-approved
```

Exit codes: 0 when stopped normally, 2 when files are invalid or nothing can be
read.

## Where things are kept

Your private automations are in `~/.config/workfile/<workspace>/`, as above.
What Workfile records is kept per folder, in
`~/.local/state/workfile/<workspace>-<hash>/`, where `<hash>` is a short hash of
the `.workfile/` folder's path (just `<hash>` when the workspace has no name).
Two clones of the same repo keep separate
records, so each fires on its own starting point.

- `facts.json`: the facts each trigger needs from the last check.
- `fired.json`: which automation fired for which ticket and PR, and when.
- `trust.json`: the approved actions in your shared file.
- `logs/`: output from each run, kept for 14 days.

Deleting the folder resets automations to a fresh starting point. Nothing fires
for facts that are already true.

## Safety limits

- One action at a time per ticket. Others wait their turn.
- At most 10 actions per check. The rest wait for the next check, so a sudden
  flood of changes cannot start dozens of agents at once.
- A failed action is reported and not retried. It can fire again only after its
  trigger changes again.
- An action that changes a ticket, such as moving it, can cause a later
  `enters` trigger. Each change fires once, so loops stop after one round.

## Out of scope for the MVP

- **Moves by Workfile itself.** A built-in `move:` action needs Jira write
  access, which Workfile does not have. For now, an agent or script does the
  move.
- **Shared team automations.** These need one runner for the team, for example
  a CI job, so they do not run once per person.
- **Events Workfile does not read**, such as new comments, Slack messages or
  review requests. The MVP uses facts the policy can already see.
- **Work Hub.** Work Hub can later read the same files, show automations on its
  Workflow graph, and turn `ask` into an Inbox item with a Run button.

## Building it

Suggested order, each step with tests that use made-up responses:

1. **Load and check files.** Add `name:` to `policy.yml`, checked as a safe
   folder name. Parse `automations/<person>.yml` and
   `~/.config/workfile/<workspace>/automations.yml`. Report every mistake listed
   above, including an `id` used in both. Match the private file's `repos:`
   with the repository's remotes, in both URL forms, including from a
   worktree. `wf health` and `wf status` show
   shared-file mistakes but never run anything. Add `wf automations` (list
   only) and `wf automations init`.
2. **Facts and changes.** After an assessment, work out each trigger's facts per
   ticket and PR. Compare with the saved facts. Cover: the first check fires
   nothing, each change fires once, unknown is unchanged, editing an
   automation resets it.
3. **`wf watch --once --dry-run`.** Print what would fire.
4. **Actions.** `run` without a shell, placeholders inside single arguments,
   credentials removed, time limit, logs. Test that a title such as
   `$(touch hacked)` is passed as plain text, and that a private `run` starts
   from the private folder.
5. **Agent, modes and trust.** `agent` command from personal settings, `ask`
   prompts, `trust` hashes and diffs.
6. **Docs.** README, `docs/usage.md`, a new `docs/automations.md`, and an
   example `automations/alice.yml` and private file. Update "Workfile only
   reads".

## Done when

- Alice can write `.workfile/automations/alice.yml` with
  `on: { becomes: github.approvals >= 1 }` and an `agent` prompt. When a PR on
  her ticket gets its first approval, `wf watch` asks her once and runs her
  agent with the filled-in prompt.
- Alice can put the same automation in
  `~/.config/workfile/example-web/automations.yml` instead, with nothing
  committed, and it behaves the same in her clone of `example-web`.
- A different repo that sets `name: example-web` never runs Alice's private
  file, because its remote is not in her `repos:` list. A second clone or a
  worktree of `example-web` runs it with no extra step.
- Bob's computer never runs Alice's file, shared or private.
- Restarting `wf watch`, or a GitHub outage, never fires an automation for
  something that was already true.
- Changing someone's `run` line in a pull request stops their automations until
  they approve it with `wf automations trust`.
- `wf health` and `wf status` behave exactly as before for projects without
  automation files.

## Open questions

1. Should `scope: all` exist in the MVP, or should personal automations only
   ever see your own work?
2. Is `.workfile/automations/<person>.yml` the right place for shared files, or
   should they live under the person in `people.yml`?
3. Should `ask` also work without a terminal, for example as a desktop
   notification with Run and Skip buttons?
4. What should the shared team runner be: a CI job, Work Hub, or a small
   server?
