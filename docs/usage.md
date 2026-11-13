# Using wf

New to Workfile? Start with [Getting started](getting-started.md).

Run `wf` inside a project that has a `.workfile/` folder, or in any folder below
it. `wf` uses the nearest `.workfile/` it finds. Use `--dir` to choose where it
starts looking.

Run `wf` with no command for help.

## The five commands

| Command | Use it to |
| --- | --- |
| [`wf setup`](#setup-connect-your-tools) | Connect your tools and write `.workfile/`. `--test` checks connections. |
| [`wf status`](#status-where-is-the-work) | See where work is, and what it needs. |
| [`wf check`](#check-may-this-ticket-move-now) | Ask if one ticket may move now. |
| [`wf lint`](#lint-is-the-policy-sound) | Check the policy itself. |
| [`wf tracker`](#tracker-browse-the-teams-tickets) | Browse and read the team's tickets. |

### Renamed commands

| Old name | Now | |
| --- | --- | --- |
| `wf health` | `wf status` | Board rollup is plain `wf status`. |
| `wf test` | `wf setup --test` | |
| `wf prove` | `wf lint` | Lint also finds plain mistakes. |
| `wf tickets` | `wf tracker` | Scope comes from `providers.yml`, with no override. |

The old names still work for one release. They print a notice on stderr.

## How the output looks

- Each section is a **panel**: a plain title on a soft background.
- Icons carry the meaning: `✓` ready, `✗` needs work, `‼` out of policy, `!` cannot check, `·` done.
- Red and green are your terminal theme's own.
- Output fits your terminal width.
- Piped output, `NO_COLOR` or `TERM=dumb` give plain text. Web addresses are written out.
- `WORKFILE_BACKGROUND=off` turns the panel tint off.

A report only describes what is true now, within your scope, and visible to your
tokens. It cannot show the path a ticket took.

## Setup: connect your tools

```sh
wf setup
wf setup --dir /path/to/project
wf setup --test                 # test every connection
wf setup --test jira github     # test only these
```

Setup needs an interactive terminal. It runs one screen at a time. `enter`
continues, `esc` goes back, `ctrl+c` cancels. A short animation plays at the
start. Press any key to skip it.

The steps:

1. **Folder.** Where `.workfile/` goes. Setup refuses a folder that already has one.
2. **Jira.** Site and email. Setup opens Atlassian's token page. Paste the token. It shows as `*`.
3. **GitHub (optional).** Organisation and a **classic** token with `repo` and `read:org`. Authorise SSO if your organisation uses it.
4. **Projects.** Tick the Jira projects your team works in. Type `/` to filter.
5. **Repositories.** Tick the repos your team ships from, or pick the first row for all.
6. **Statuses.** Untick any status your team rarely uses. Those tickets are left out. `shift+↑` and `shift+↓` change the order.
7. **Review and save.** Nothing is saved before you confirm.

Setup writes `policy.yml`, `providers.yml` and `people.yml`. It writes every scope
setting, so the file documents your team: `projects`, `repos`, `keep_done_for: 14d`
and `skip_statuses`. Tokens go in your private credentials file, never the project.

It does not import Jira's workflow or invent gates. Each stage links to the next.
Add your team's checks to `policy.yml`. Without gates, "ready" means nothing yet.

### Test connections

`wf setup --test` checks each connection in `providers.yml`. If one fails, the
rest still run.

- Jira: who the token belongs to, and one ticket from your scope.
- GitHub Issues: the project and your scope.
- GitHub code host: who you are, the repos you can see, and a sample PR search.

A pass means those reads worked. It does not prove you can see every ticket or
repo you meant to include.

## Status: where is the work?

```sh
wf status                    # the whole board
wf status --me               # your work
wf status APP-42             # one ticket
wf status --gate code_review # tickets failing one gate
wf status --since 1d         # what changed
```

What you get depends on what you ask for:

| You run | You see |
| --- | --- |
| `wf status` | A rollup of the board: which gates and stages fail, and how many tickets. |
| `wf status --gate NAME` or `--stage NAME` | The tickets behind one gate or stage. |
| `wf status --me` or `--user NAME` | One person's work, grouped by what to do. |
| `wf status APP-42 APP-57` | The full picture for each named ticket. |

Add `--since` to any of them to see what changed first.

Exit codes: `0` nothing wrong, `1` a ticket is out of policy or needs work, `2` it
could not finish. See [Exit codes](#exit-codes).

### The rollup

Plain `wf status` checks every ticket in your scope. Here is a small board:

```text
  workfile status                                          4 tickets
  Jira · APP · repos app, api

  1 of 4 tickets are out of policy (25%).
  ████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░

  Gates                            failing of tickets that must pass

  refinement    ██████░░░░░░  1 / 2   50%
    1  Description: at least 100 characters

  pull request  ████████████  1 / 1  100%
    1  At least 1 linked PR

  Under each gate: tickets failing each rule. A ticket can fail more
  than one.

  Stages                           out of policy of tickets in stage

  todo    ░░░░░░░░░░░░  0 / 2    0%
  doing   ░░░░░░░░░░░░  0 / 1    0%
  review  ░░░░░░░░░░░░  0 / 0    0%
  done    ████████████  1 / 1  100%

  Next  wf status --gate pull_request
```

- The line under the title is your scope: what workfile reads.
- Gates and stages follow your workflow order, never count or alphabet.
- A gate's percentage is failing tickets over the tickets that must pass it. A ticket must pass a gate if it is in, or past, the stage the gate guards.
- A stage's percentage is out-of-policy tickets over all tickets in the stage.
- **Next** names the command that lists the tickets behind the worst gate.

In a terminal this opens an interactive screen: gates and stages on the left, the
chosen one on the right, `enter` to list its tickets, `esc` to go back, `q` to quit.
Piped, or with `NO_COLOR` or `--evidence`, it prints the text above.

`--gate` and `--stage` list the tickets behind the numbers. A ticket that fails two
gates appears under each.

### Your own work

```sh
wf status --me
wf status --user bob
```

This lists tickets assigned to you, and tickets with a PR you wrote or were asked
to review. `--user bob` shows Bob's. Use one, not both.

```text
  workfile status                                          4 tickets
  Jira · APP · repos app, api

  1 ticket can move on, 2 need work and 1 is out of policy.
  ‼ 1 out of policy   ✗ 2 need work   ✓ 1 ready   · 0 done
  Only tickets involving Bob.

  Ready to move                                             1 ticket

  ✓ APP-61  Show stock levels on product pages          todo → doing
      https://example-team.atlassian.net/browse/APP-61

  Needs work                              2 tickets · grouped by fix

  Expand the description to at least 100 characters                1
  refinement
    ✗ APP-52  Fix the search box                                todo
        https://example-team.atlassian.net/browse/APP-52

  Link at least 1 PR                                               1
  pull request
    ✗ APP-57  Add a delivery estimate                          doing
        https://example-team.atlassian.net/browse/APP-57

  Out of policy                                past a gate they fail

  ‼ APP-70  Rename loyalty tiers                                done
      https://example-team.atlassian.net/browse/APP-70
      Expand the description to at least 100 characters · refinement
      Link at least 1 PR; include APP-70 in the PR title
      pull request
```

The groups, most urgent first:

1. **Cannot check** - its status is not in your workflow, or evidence is missing.
2. **Out of policy** - a gate from an earlier stage fails.
3. **Needs work** - fine so far, but every move forward is blocked. Grouped by fix, each fix once.
4. **Ready to move** - a move forward is open.
5. **Done** - counted, not listed.

`--failing` hides tickets that are ready or done.

In a terminal, tickets are on the left and the chosen ticket is on the right.
Press `m` to show only your work. Press `q` to quit.

`--me` and `--user` look through your whole scope first, then show up to 25 of
your tickets. Use `--limit N` or `--all` to change that.

### One ticket

```sh
wf status APP-57
```

```text
  APP-57
  https://example-team.atlassian.net/browse/APP-57
  Add a delivery estimate
  Bug · Assigned to Bob

  WHERE IT IS
  ●━━━━━━━◉╌╌╌╌╌╌╌╌○─────────○
  todo    doing    review    done
  Blocked: 1 thing needs attention before it can move on.

  ✗ NEEDS ATTENTION
  APP-57
    1. Link at least 1 PR; include APP-57 in the PR title — pull request.
       needed to move to review

  PULL REQUESTS
  No linked PRs. A PR links when its title names the ticket, or its description links to it.

  NEXT MOVES
  ✗ → review  1 requirement not met
```

- **Where it is** draws one forward path, each stage once. `◉` is the current stage, `●` is behind it, `○` is ahead. The link out of the current stage is solid when the move is ready, dashed when blocked.
- Earlier stages are not ticked. Workfile cannot prove the path a ticket took.
- Branches show the nearest one. Detours such as `blocked` and extra end stages such as `cancelled` are left out. If the ticket is in one, the panel says so.
- **Needs attention** lists each fix once, under the ticket or PR it applies to, numbered through the panel.
- **Pull requests** shows each PR and its state. Queued PRs show **Queued for merge** and **Wait for merge**. Queue membership never counts as merged or as a passing gate.
- **Next moves** lists every place the ticket can go, returns marked `↩`.

If a gate needs `github.checks == success`, CI must pass on every linked PR's
current commit. The next step says: wait for CI, fix the failing checks, or run
CI for this commit.

### Since: what changed?

```sh
wf status --since last    # since your last run
wf status --since 12h     # since about 12 hours ago
wf status --since 1d      # since about a day ago
wf status --me --since 1d
```

`--since` takes `last`, `1h` to `168h`, or `1d` to `7d`.

It shows a panel above the usual output. Here a ticket moved, one became
blocked, and one became ready:

```text
  Since your last run, just now

  APP-2  ✗ blocked: refinement
         https://example-team.atlassian.net/browse/APP-2
  APP-3  ✓ ready for doing · · back on track
         https://example-team.atlassian.net/browse/APP-3
  APP-1  todo → doing · ✗ blocked: pull request
         https://example-team.atlassian.net/browse/APP-1
```

How it works:

- Every `wf status` run saves a small snapshot on your computer, in `~/.local/state/workfile/<workspace>/snapshots`. Snapshots older than 8 days are deleted.
- `--since` compares this run with an earlier snapshot. It adds no requests.
- If there is no snapshot that old, it uses the oldest one and says so.
- With no earlier run it says "Nothing to compare yet". Run it again later.
- It shows what changed since you last looked. It is not a history.
- It never changes the exit code.

### Evidence

`--evidence` shows, for each requirement, the outcome and what workfile observed.
Missing required evidence is **cannot check**, never a pass or a violation. Known
empty evidence, such as zero approvals, can still fail a rule.

## Check: may this ticket move now?

```sh
wf check APP-42 --to done
wf check 42 --to done --json
```

`wf check` reads one ticket fresh and asks your policy: may it move to that stage
right now?

| Exit | Meaning |
| --- | --- |
| `0` | Yes. |
| `1` | No. |
| `2` | Cannot tell, for example evidence is unavailable. |

```text
  APP-57
  https://example-team.atlassian.net/browse/APP-57
  Add a delivery estimate
  Bug · Unassigned

  ✗ CANNOT MOVE TO REVIEW YET
  APP-57
    1. Link at least 1 PR; include APP-57 in the PR title — pull request.
```

When the move is allowed, the panel says `✓ CAN MOVE TO REVIEW`. A stage the
ticket has no move to says so, and lists the moves it does have.

Agents and scripts should run it before they move a ticket. With `--json` you get
`allowed`, `exit_code`, `from`, `to`, `possible_moves`, `unmet_requirements` and
`records`, each PR with its latest commit in `head`. Compare `head` before and
after your work to be sure nothing changed. See [JSON reports](json.md).

## Lint: is the policy sound?

```sh
wf lint
wf lint --json
```

`wf lint` checks the policy itself, not tickets. It reads no provider, so it
needs no credentials and is quick enough for every CI run. It exits `1` on a
problem or a broken invariant, and `0` otherwise.

The output has these sections. Empty ones are left out:

| Section | What it shows |
| --- | --- |
| **Problems** | Mistakes that stop the policy loading, such as an unknown gate. Exit `1`. |
| **Warnings** | Plain mistakes in a valid policy. Exit `0`. |
| **Invariants** | For each gate you list under `invariants:`, whether any path skips it. |
| **What each stage guarantees** | The gates every forward path to the stage passes. |

A typo in a gate name is a problem:

```text
  WORKFILE LINT
  1 problem in .workfile/. Fix them, then run wf lint again.

  PROBLEMS
  ✗ policy.yml: ready requires unknown gate refinment
```

Warnings today:

| Rule | When |
| --- | --- |
| `unused-gate` | A gate is defined but no move requires it. |
| `no-invariants` | `invariants:` is empty. |

Workfile accepts a ticket's stage when **any one** forward path to it passes. So a
gate on one path says nothing about the others. Invariants close that gap. Write
down what must always be true:

```yaml
# .workfile/policy.yml
invariants:
  done: [code_review, merged]
```

On the [branching example](../example/branching/.workfile/), with that invariant
added, `wf lint` finds a hole:

```text

  WORKFILE LINT
  1 of 2 invariants can be skipped.

  INVARIANTS
  ✗ done requires code review
    A ticket can reach done without it:
    intake → triage → discovery → ready → doing → review → security → release → done
    Require code review on review → security or security → release.
  ✓ done requires merged

  WHAT EACH STAGE GUARANTEES
  Every path to the stage passes these gates.
  intake     nothing
  triage     nothing
  discovery  nothing
  ready      nothing
  doing      refinement
  blocked    refinement
  review     refinement · linked pull requests · risk assessment
  qa         refinement · linked pull requests · risk assessment · code review
  security   refinement · linked pull requests · risk assessment · threat model
  release    refinement · linked pull requests · risk assessment
  done       refinement · linked pull requests · risk assessment · merged
  cancelled  nothing
```

Review → security and security → release are the only moves that lead around code
review. Requiring it on either closes the hole. If no single move closes every
path, `wf lint` says to require the gate on a move on the path shown, then run it
again. Returns (moves back) are left out.

With no invariants, `wf lint` still lists what each stage guarantees. Here is the
plain example policy:

```text

  WORKFILE LINT
  No invariants to check.

  WARNINGS
  ! No invariants. Add one, such as done: [code_review], to prove no path skips a gate.

  WHAT EACH STAGE GUARANTEES
  Every path to the stage passes these gates.
  todo    nothing
  doing   refinement
  review  refinement · pull request
  done    refinement · pull request · code review
```

With `--json` you get `problems`, `warnings`, `invariants` and `guarantees`. See
[JSON reports](json.md#lint).

## Tracker: browse the team's tickets

```sh
wf tracker
wf tracker --view me
wf tracker --view review
```

`wf tracker` shows your tracker, scoped to your team, in one standard screen for
any tracker. The scope comes from `providers.yml`: the same slice `wf status`
reads. There is no override. It only reads.

The screen shows the list, with the selected ticket underneath: its description
and comments, where it is in your pipeline, what blocks it, its PRs, and where it
can move next.

Tabs run All, Assigned to me, then each stage in `states` order. Use `←` and `→`
to switch. `--view` picks the first tab: `all`, `me`, or a stage.

| Key | What it does |
| --- | --- |
| `↑` `↓` | Move through the list, or scroll the ticket |
| `/` | Search |
| `enter` | Read the selected ticket |
| `tab` | Move between the list and the ticket |
| `esc` | Clear the search, or go back to the list |
| `←` `→` | Move between views |
| `r` | Read the ticket again |
| `o` | Open the ticket in your browser |
| `q` | Quit |

To stay kind to your providers, the list is read once with only the fields it
shows. A ticket is read when you stop on it, and remembered until you quit.

If your terminal supports the kitty graphics protocol, titles and images are drawn
large. Otherwise they are text. Set `WORKFILE_GRAPHICS=off` to always use text.

Piped, it prints a plain list:

```text

  workfile / jira tickets

  Jira · APP · repos app, api

  APP-61 Show stock levels on product pages
    To Do · Bob
    https://example-team.atlassian.net/browse/APP-61

  APP-57 Add a delivery estimate
    In Progress · Bob
    https://example-team.atlassian.net/browse/APP-57
```

## Options

| Option | What it does |
| --- | --- |
| `--me` | Status. Your work. |
| `--user NAME` | Status. The same for a person in `people.yml`. |
| `--since last\|12h\|1d` | Status. What changed since an earlier run. |
| `--gate NAME` | Status. Tickets failing this gate. |
| `--stage NAME` | Status. Tickets in this stage that are out of policy. |
| `--failing` | Status. Hide tickets that are ready or done. |
| `--limit N` | Status. Check up to N tickets. N must be 1 or more. |
| `--all` | Status. Check every ticket in scope. |
| `--evidence` | Status. Show the evidence for each requirement. |
| `--to STAGE` | Check. Required. The stage the ticket would move to. |
| `--view all\|me\|STAGE` | Tracker. The tab to start on. |
| `--test [NAME ...]` | Setup. Test connections instead of setting up. |
| `--json` | Status, check and lint. Versioned JSON for scripts and agents. |
| `--dir DIR` | Start looking for `.workfile/` in DIR. Works everywhere. |
| `--version` | Print the version. |

Options go before or after ticket keys. `--gate` and `--stage` must name a gate or
stage from `policy.yml`.

Plain `wf status`, `--gate` and `--stage` check every ticket in your scope. Name
tickets and the limit is ignored, but they must still be in your scope.

## Who is "me"?

By default, `--me` asks Jira and GitHub who owns your token. If you use a shared
service account, set your identity in `~/.config/workfile/config.yml`:

```yaml
me:
  jira: your-atlassian-account-id
  github: your-github-login
```

A setting for a connection name beats a setting for a provider type. You count as
involved if you wrote a PR or were asked by name to review it. Past reviewers do
not count, and neither do members of a team that was asked.

## Only your tickets

To see only tickets assigned to you in a workspace, add `name:` to its
`policy.yml`, then create `~/.config/workfile/<name>/config.yml`:

```yaml
only_mine: true
```

Every status listing then adds your assignee to the Jira search or GitHub filter.
Fewer tickets means fewer requests. Tickets you name, such as `wf status APP-42`,
are still shown. `wf tracker` still reads everyone's.

## Personal files and settings

| Setting | Default |
| --- | --- |
| Credentials | `~/.config/workfile/credentials` |
| Identity settings | `~/.config/workfile/config.yml` |
| Workspace settings, such as `only_mine` | `~/.config/workfile/<workspace>/config.yml` |
| Snapshots for `--since` | `~/.local/state/workfile/<workspace>/snapshots` |
| `XDG_CONFIG_HOME` | Replaces `~/.config`. |
| `XDG_STATE_HOME` | Replaces `~/.local/state`. |
| `WORKFILE_CREDENTIALS_FILE` | The full path to the credentials file. |
| `WORKFILE_CONFIG_FILE` | The full path to the identity settings file. |
| `NO_COLOR` | Turns off colours. Terminal hyperlinks stay on. |
| `WORKFILE_BACKGROUND` | `off` stops asking the terminal for its background colour. |
| `WORKFILE_GRAPHICS` | `off` stops asking the terminal about graphics. |
| `COLUMNS` | Sets the display width. Handy for previews. |

The credentials file has one `NAME=value` per line. Blank lines, comments, a
leading `export` and quotes around values are fine. Nothing is expanded and no
commands run. An environment variable beats the file. On macOS and Linux run
`chmod 600` on it. On Windows, limit access in the file's security settings.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Status found only ready or done tickets. Check found the move allowed. Lint found nothing wrong. Setup tests passed. |
| `1` | Status found tickets out of policy or needing work. Check found the move not allowed. Lint found a problem or an invariant that can be skipped. |
| `2` | Bad options or settings, a provider read failed, evidence is unavailable, a status is not in your workflow, or a named ticket is not in your scope. |

If no tickets match, the report says so and exits `0`. Reports only cover the
tickets that were checked. Press `ctrl+c` to cancel running requests.

GitHub searches look for the checked tickets' keys in PR titles and descriptions,
six tickets per search. Labels, reviews and readiness evidence are fetched only for
linked PRs, up to six at once. Reports use fresh data. Nothing is cached between
runs, except the snapshots for `--since`.
