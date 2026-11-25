# Using wf

New to Workfile? Start with [Getting started](getting-started.md).
For config files, defaults and precedence, see [Configuration](configuration.md).

Run `wf` in a project with `.workfile/`, or any subfolder. It uses the nearest
workspace. `--dir DIR` changes where the search starts. Run `wf` or `wf --help`
for help.

## The commands

| Command | Use it to |
| --- | --- |
| [`wf setup`](#setup-connect-your-tools) | Connect tools and apply a starter policy. `--test` checks connections. |
| [`wf tracker`](#tracker-browse-the-teams-tickets) | Browse and read team tickets, PRs and next actions. |
| [`wf tracker --view inbox`](#inbox-what-wants-you-now) | Follow your activity and work that needs you. |
| [`wf status`](#status-where-is-the-work) | Check where work is and what it needs. |
| [`wf check`](#check-may-this-ticket-move-now) | Ask whether one ticket may move now. |
| [`wf policy`](#policy-how-does-this-project-ship) | Inspect the workflow offline, or validate it with `--lint`. |

These older names still work and print a notice on stderr:
`health` → `status`, `test` → `setup --test`, `tickets` → `tracker`.

## How the output looks

Interactive screens use your terminal's colours and width. Panels group related
work; icons and words preserve meaning without colour:
`✓` ready, `✗` needs work, `‼` out of policy, `!` cannot check, `·` done.

Piped output, `NO_COLOR` or `TERM=dumb` use noninteractive text. Piped output
writes web addresses out. `NO_COLOR` disables colour but keeps terminal
hyperlinks where supported. A Nerd Font is recommended for interactive icons.
Images use kitty graphics when available, with text fallbacks.
`WORKFILE_BACKGROUND=off` disables panel tint; `WORKFILE_GRAPHICS=off` disables images.

Reports describe current evidence within your configured scope and token access.
Policy checks do not prove which route a ticket took through the workflow.

## Setup: connect your tools

```sh
wf setup
wf setup --dir /path/to/project
wf setup --test                 # all connections
wf setup --test jira github     # only these connection names
```

Setup needs an interactive terminal. Enter continues, Esc goes back, and Ctrl+C
cancels before applying. Any key skips the opening animation.

1. **Providers.** Choose Jira or GitHub Issues as tracker, then an optional
   GitHub code host.
2. **Folder.** Choose where to create `.workfile/`. It must not already contain
   a workspace.
3. **Connections.** Enter provider details. Ctrl+O opens the token page. Tokens
   are masked and each connection is tested.
4. **Scope.** Choose projects and repositories. `/` filters; Space selects rows
   in multi-select lists. Jira accepts multiple projects. GitHub Issues uses
   one organisation project and its issue repositories. Code-host repositories
   have their own scope.
5. **Policy.** Keep the statuses you use; Shift+Up/Down reorders them. The last
   stage marks completed work.
6. **Review and apply.** Press `p` to preview `policy.yml`, then Enter to apply.

Setup writes `policy.yml`, `providers.yml` and `people.yml`, and saves tokens in
your private credentials file. It does not create `config.yml`. Provider scope
includes skipped statuses and `keep_done_for: 14d`. The starter links each stage
to the next with no gates. Add requirements to `policy.yml` yourself.

For token requirements, see [Jira](providers/jira.md) or
[GitHub](providers/github.md). Authorise organisation SSO when required.

### Test connections

`wf setup --test` tests every configured connection, continuing after a failure:

- **Jira:** token identity and one ticket in scope.
- **GitHub Issues:** the project and configured scope.
- **GitHub code host:** identity, visible repositories and a sample PR search.
  Missing named repositories or an unreadable reported PR fail the test.

With `repos: all`, GitHub may omit private repositories your token cannot see.
List repository names explicitly when you need access to each one verified.

## Status: where is the work?

```sh
wf status                    # board rollup
wf status --me               # your work
wf status --user bob          # a person from people.yml
wf status APP-42 APP-57       # named tickets
wf status --gate code_review # tickets failing this gate
wf status --stage review     # out-of-policy tickets in this stage
wf status --since 1d         # compare with an earlier local run
```

| Request | Result |
| --- | --- |
| No filter | Gates and stages, with failing counts and percentages. |
| `--gate` or `--stage` | Tickets behind that gate or stage. |
| `--me` or `--user` | One person's work, grouped by next action. Use one, not both. |
| Ticket keys | Each ticket's requirements, linked PRs and possible moves. |

### The rollup

Plain `wf status` checks every ticket in scope. This is its noninteractive output:

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

Gates and stages follow workflow order. A gate's denominator is tickets that
must pass it: tickets in or past the stage it guards. A stage's denominator is
all tickets in that stage. A ticket can fail more than one rule or gate.

In an interactive terminal, gates and stages appear on the left and the selected
item on the right. Enter lists its tickets, Esc goes back, and `q` quits.
`--evidence` uses printed output instead.

### Your own work

`--me` includes assigned tickets and tickets with PRs you authored or were asked
by name to review. `--user bob` uses Bob's accounts from `people.yml`.
Past reviewers and members of a requested team do not count as named reviewers.
See [identity settings](configuration.md#identity).

Work is grouped by what to do. Each fix appears once, with the tickets it applies
to. Finished tickets are counted rather than listed.

| Group | Meaning |
| --- | --- |
| Cannot check | A status is unmapped or required evidence is unavailable. |
| Out of policy | Requirements for the current stage fail. |
| Needs work | No forward move is open. |
| Ready to move | At least one forward move is open. |
| Done | The ticket is in a terminal stage. |

`--failing` hides ready and done tickets. In an interactive terminal, select a
ticket to read its details beside the list. `m` switches to your work; `q` quits.

`--me` and `--user` look through your scope first, then show up to 25 matching
tickets. Use `--limit N` or `--all` to change the limit. The limit does not avoid
the initial search through scope.

### One ticket

`wf status APP-57` shows:

- **Where it is:** the configured workflow, with the current stage highlighted
  or in brackets in plain text. Branches and returns match `wf policy`.
- **Needs attention:** unmet requirements, open review threads, and failing
  checks, grouped by ticket and PR and numbered across the report. Policy fixes
  name the destinations they unblock.
- **Pull requests:** linked PRs and current evidence. Queued PRs say
  **Queued for merge** and **Wait for merge**; queue membership is not a pass
  or a merge.
- **Next moves:** configured destinations, with returns marked `↩`.

For `github.checks == success`, checks must pass on every linked PR's current
commit. Next steps distinguish waiting, failing checks and CI that has not run.
Missing required evidence is **cannot check**, not an empty result or a pass.

Named-ticket lookups can find PRs whose branch starts with the ticket key.
Put the key in the title or include the ticket URL in the PR description to
link PRs in bulk board reads as well. See [how PRs are found](providers/github.md#how-prs-are-found).

### Since: what changed?

```sh
wf status --since last
wf status --since 12h
wf status --me --since 1d
```

`--since` accepts `last`, `1h` to `168h`, or `1d` to `7d`. It adds a changes
panel before the normal report, comparing this run with a saved local snapshot.
It adds no provider requests and does not change the exit code.

Each status run saves a snapshot; snapshots older than eight days are removed.
If the requested snapshot is unavailable, Workfile uses the oldest and says so.
With no previous run it says **Nothing to compare yet**. This compares observed
states, not provider history.

### Evidence

`--evidence` shows the outcome and observed facts for each requirement. A known
empty result, such as zero approvals, can fail; an unreadable result cannot be
assumed empty. See [JSON reports](json.md) for machine-readable evidence.

## Check: may this ticket move now?

```sh
wf check APP-42 --to done
wf check 42 --to done --json
```

Check reads one ticket fresh and evaluates the requested move. Exit `0` means
allowed, `1` means blocked, and `2` means it could not tell. If no such move is
configured, it explains that and lists the possible moves. It never moves the ticket.

Bare ticket numbers work when the tracker can resolve them unambiguously.
Named tickets must still be in provider scope.

For scripts and agents, `--json` includes `allowed`, `exit_code`, `from`, `to`,
`possible_moves`, `unmet_requirements` and linked `records`. Each PR's `head`
identifies the commit checked. See [JSON reports](json.md#asking-before-a-move).

## Policy: how does this project ship?

```sh
wf policy
wf policy --json
wf policy --lint
wf policy --lint --json
```

`wf policy` shows the workflow diagram, gate rules, provider scope and status
mappings. It also lists `AGENTS.md` and a conventional GitHub PR template when
they exist in the project. Both modes read local files and need no credentials.
See [JSON reports](json.md#policy).

Use `--lint` to validate the policy and see possible gaps:

| Section | Meaning |
| --- | --- |
| Verdict | `✓ Pass`, `! Pass` with possible gaps, or `✗ Fail` with problems. |
| Problems | Errors that stop policy loading. Exit `1`. |
| Gates that can be skipped | A gate required on one path into a stage but not another. |
| Warnings | Valid but suspicious policy, such as an unused gate (`unused-gate`). |
| Flow | Configured stages and moves, with gate names on the moves they guard. |

Empty sections are omitted. Gaps and warnings do not fail lint. Branches may
intentionally have different gates. To require a gate on every move into a
stage, use [`any`](policy.md#any-a-gate-on-every-move-into-a-stage).

The diagram uses terminal graphics when available, otherwise text. It shows
only configured moves; narrow views can use numbered stage strips. Gate shapes
can share compatible moves. `WORKFILE_GRAPHICS=off` always uses text.

```text
  Flow
   Todo-[🔒 Ready To Start]->Doing-[🔒 Sign Off]->Done
```

JSON includes `problems`, `warnings`, `gaps` and `guarantees`. See
[JSON lint reports](json.md#policy-lint).

## Tracker: browse the team's tickets

```sh
wf tracker
wf tracker --view me
wf tracker --view review
```

Tracker uses the ticket scope from `providers.yml`; it cannot expand that scope.
By default it opens a list with a ticket preview. Tabs are All, Assigned to me,
and each stage in workflow order. Configured `views` replace these tabs.
`--view` selects a built-in tab or configured view ID.
Ticket panes have a one-column inset on both sides. On wide screens, this gives
the vertical divider one column of space on each side.
A blank line separates the content from the divider above the command hints.

Default columns are key, title, status, assignee and next action. Narrow screens
hide assignee, then status, then action to keep key and title readable.
The top of a 100-column screen with `columns: [key, title, status, assignee]`,
without colour:

```text
▸ All (4)   Assigned to me   Todo (1)   Doing (1)   Review (1)   Done (1)          Synced 11:30:00
────────────────────────────────────────────────────────────────────────────────────────────────────

  Key    Title                                                       Status       Assignee
▸ APP-1  Fix redirect loop on expired login                        ‼ To Do        Alice
  APP-2  Add single sign-on login                                  → In Progress  Bob
  APP-3  Clear account settings page                               → In Review    Unassigned
  APP-4  Tidy the billing screen                                   · Done         Charlie
────────────────────────────────────────────────────────────────────────────────────────────────────
```

### Navigation

| Key | Action |
| --- | --- |
| Up / Down | Select a ticket, or scroll the focused reader pane. |
| Left / Right | Switch list views; on a board, move between nonempty stages. |
| `[` / `]` | Switch views, including from a board. |
| `/` | Search tickets. |
| `s` | Choose a project for the current board or list. |
| Enter | Open the full-height ticket reader. |
| `w` | Switch reader focus between ticket and Workfile. |
| Tab | Switch between the list and reader. |
| Esc | Clear search or return to the list or board. |
| `r` | Sync a board; in a list or reader, refresh the selected ticket. |
| `o` | Open the ticket in your browser. |
| `p` | Open a linked PR, or choose from several, including **Open all N PRs**. |
| `c` | Open the relevant check results when available. |
| `1` to `9` | Open a numbered fix when the Workfile pane is visible. |
| `n` | Check another batch of up to 12 active tickets. |
| `q` | Quit. |

The project picker shows counts from the cached listing. Arrows select, `/`
searches, Enter applies, and Esc cancels. **All projects** restores the view's
configured scope. The choice lasts for the session, separately per tab, and
combines with view filters and search. It does not fetch another listing.

### Ticket details and next actions

The reader shows the ticket heading directly below the view tabs. It stays in
view as you scroll the description and comments.
Comments show newest first unless threaded; replies stay under their parent.
On wide terminals, a thin divider separates the ticket and Workfile panes.
Scroll over either pane with the mouse or trackpad. In the reader, Down and
Page Down continue into Workfile when you reach the end of the ticket. Press
`w` to switch panes directly at any width. The Workfile pane shows the
workflow, ticket fixes and one panel per linked PR. Passing requirements are
omitted. Press a fix's number to open its ticket, PR or failing checks.
Refresh with `r` to recheck; fixes disappear only when fresh evidence clears them.

The action column combines policy requirements with operational PR problems,
such as failed CI, requested changes or merge conflicts. A policy-permitted
move does not hide outstanding PR work.

| Action state | Meaning |
| --- | --- |
| Needs you | A fix on your assigned ticket or authored PR, or a review requested from you. |
| Waiting | Work is waiting for CI, a requested review or re-review. |
| Ready to move | A forward move passes without known actions, waits or unknown evidence. |
| Unknown | Evidence or action ownership is unavailable; unchecked work is not marked ready. |

Tracker resolves code-host and tracker identities separately. It does not assume
the ticket assignee owns every linked PR. Missing approval without a known
request says **Request a review** when you own the PR.

### Board views

A [configured view](configuration.md#tracker) with `layout: board` groups cards
by `policy.states`. Unmapped tickets have a separate final column. Board views
default to your assigned tickets; set `filter.assignee: all` for everyone.

Arrows navigate cards; Up from the top card focuses the tabs. Enter opens the
reader, Esc returns, and `[` / `]` switches views. Narrow screens follow the
selected stage rather than squeezing all columns together.

Cards show next actions and linked PR statuses, with PRs needing you first.
`+N` indicates more PRs. Unchecked cards say **PRs not checked**; they are not
assumed to have no PRs. Enter opens the full details.

While a board tab is selected, it syncs every two minutes; `r` syncs immediately,
even on an empty board. Sync refreshes the listing and bounded evidence sample,
keeping selection and completed previews where possible. Failed syncs keep the
previous data, show an error and retry. List tabs do not poll.

### Reads, sampling and plain output

The default action column, any attention filter, or a board layout enables
background policy checks. Tracker starts with up to 12 active tickets in the
current view, prioritising the visible page, with at most two background checks
at once. `n` extends that sample. Selecting a ticket can start its own check.

Background checks read policy facts, which can include description text, but
not rich previews, comments or images. Those load only when needed for a
selected ticket. Evidence stays cached for the session until refreshed; the
preview shows its check time. List `r` refreshes the selected ticket, or the
bounded sample when the view is empty.

Attention-filtered views match checked tickets only. A `+` on a tab means
unchecked work remains. An empty sample does not mean all work is clear.

Noninteractive list output includes ticket URLs without rich previews. A plain
stage-only list does not run policy checks, even with the default columns.
Attention-filtered lists check up to 12 active tickets and print a sample
caption. Plain boards list stages vertically with PR URLs, actions and a checked
count. These preferences never add provider writes.

### Tracker preferences

See [Configuration: tracker](configuration.md#tracker) for columns, list and
board views, filters, preview settings and merge rules. No config file is required.

## Inbox: what wants you now?

```sh
wf tracker --view inbox
```

Inbox follows unfinished tickets assigned to you and PRs you authored. Opt in
to review requests or watch named repositories in `config.yml`. A PR can appear
without a ticket.

Each row shows the ticket or PR number, title and next action or update.
Alerts appear newest first. New comments on the same PR share one row. Comments
and failing checks on a PR can be resolved or snoozed independently. When Inbox
sees a required approval or CI check pass, a ready alert explains what changed.

Inbox starts with **Needs attention**. Press `s` to choose **All signals** and
include updates such as passing checks. Set the starting scope and alert kinds
for Needs attention in `config.yml`.

Inbox uses the same status symbols and colours as Tracker, policy checks and
status reports. Red marks failures and blockers, amber marks requests, pending
work and unavailable evidence, and green marks passing checks or a ready move.
Plain output uses standard symbols and words.

| Key | Action |
| --- | --- |
| `j` / `k`, Down / Up | Select a row. |
| `t` | Read the linked ticket, workflow and numbered fixes. |
| Esc | Return to the queue. |
| `e` | Resolve the alert locally, hiding it until new activity or evidence changes it. |
| `s` | Choose the Inbox scope. |
| `z` | Snooze locally, for one hour by default or until a new blocker appears. |
| `a` | Preview the configured agent prompt and Workfile context. Press Enter to launch. |
| `o` | Open the PR. Open the comment thread when the alert has a thread link. |
| `r` | Refresh the Inbox now. |
| `[` / `]` | Switch Tracker views. |
| `q` | Quit. |

Following a link or launching an agent leaves the alert in Inbox. Press `e` to
resolve it. In the ticket reader, arrows scroll and a fix's number opens its
link. Esc returns to the same Inbox row.

Resolve, snooze and activity cursors are local.
An agent launched with `a` is your own command and may make changes with its own
access. See [agent configuration](configuration.md#agent-launcher).

### Inbox settings

Defaults are `poll: 30s`, `refresh_every: 5`, `full_every: 15` and `snooze: 1h`.
See [Configuration: inbox](configuration.md#inbox) for all settings and prompts.

### How often it reads

Inbox polls in the background while interactive Tracker is open, including
while List or Board is selected:

- Each poll checks which tracked PRs changed, in batches of up to 100. Only
  changed PRs need activity reads. Searches discover open authored PRs and,
  when configured, requested reviews and PRs in watched repositories.
- Tickets are reread when their PRs change or new comments arrive.
- Every `refresh_every` polls, known tickets are reread for changes such as moves.
- Every `full_every` polls, a board read discovers newly assigned tickets. If
  both are due, the board read covers both. `r` performs this read immediately.
- The first run reads the last 24 hours. Failed reads keep previous data and
  show a `!` row for the connection; the next poll retries.

Noninteractive inbox reads once and prints newest alerts first, with symbols and web
addresses. It does not write inbox state, so scripts cannot clear new activity.
The number after `Inbox` counts alerts in the configured default scope. This
example enables review requests:

```text
Inbox 2

‼ APP-1  Fix login  Out of policy: Add a release note  now
  https://example-team.atlassian.net/browse/APP-1
… #7  Update the lint runner  Review #7  1d
  https://github.example.com/example-team/api/pull/7
```

## Options

| Option | Applies to | Meaning |
| --- | --- | --- |
| `--me` | Status | Assigned tickets and PRs you authored or must review. |
| `--user NAME` | Status | The same for a person in `people.yml`. |
| `--since last\|12h\|1d` | Status | Compare with an earlier local snapshot. |
| `--gate NAME` | Status | Tickets failing this gate. |
| `--stage NAME` | Status | Out-of-policy tickets in this stage. |
| `--failing` | Status | Hide ready and done tickets. |
| `--limit N` | Status | Check up to N tickets; N must be positive. |
| `--all` | Status | Check every ticket in scope. |
| `--evidence` | Status | Print observed evidence for every requirement. |
| `--to STAGE` | Check | Required destination. |
| `--view VIEW` | Tracker | Select an available tab ID. |
| `--test [NAME ...]` | Setup | Test connections instead of setting up. |
| `--json` | Status, check, lint | Versioned machine-readable output. |
| `--dir DIR` | All | Start the workspace search from DIR. |
| `--version` | Global | Print the installed version. |
| `-h`, `--help` | Global | Show help. |

Options can go before or after ticket keys. Gate and stage names come from
`policy.yml`. Rollup, gate and stage status views check the whole scope unless
limited. Named tickets ignore the limit but must be in scope.

## Who is "me"?

By default, the token owner. To override identities, see
[Configuration: identity](configuration.md#identity).

## Only your tickets

The private workspace setting `only_mine: true` narrows status scope reads to
assigned tickets, not named-ticket lookups or tracker browsing. See
[Configuration: only your tickets](configuration.md#only-your-tickets).

## Personal files and settings

See [Configuration](configuration.md) for file paths, credentials, environment
variables and local state. Preferences do not change policy or provider scope.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Status found only ready or done tickets; check allowed the move; policy loaded successfully; connection tests passed. |
| `1` | Status found out-of-policy or blocked tickets; check blocked the move; policy validation found problems. |
| `2` | Bad options or settings, unavailable required evidence, failed provider reads, an unmapped status, or a named ticket outside scope. |

No matching tickets exits `0`. Reports cover only checked tickets. Tracker and
inbox are browsing commands, not CI gates: inbox shows read failures as rows
rather than returning a policy-failure exit code. Press Ctrl+C to cancel requests.

Status and check fetch current evidence each run. Interactive tracker caches
reads during its session; inbox persists activity cursors and local clear/snooze
state. Status snapshots persist for `--since`. None of these is provider history.
