# Design: `wf inbox`

**Status:** draft for approval. Follows `2026-11-16-command-center-handoff.md`.
**Mocks:** https://claude.ai/artifact/MAYxS8GaT1WBEGeDnEWqu7 (mock A is the base)

## Why

Attention is pulled across GitHub and Jira. A typical day has several PRs in
flight at different readiness. `wf inbox` is one screen that answers "what
happened to my work, and what wants me right now?", lets the person act on
each item, and can be cleared like an email inbox.

## Goals

- One list of all activity on the person's work, with what needs them marked.
- Every row worded as an event or an action, with its age.
- Clearable: done, snooze, hand to an agent, open.
- Provider agnostic: the screen never knows which service an event came from.
- Configurable like `tracker`, including the poll interval.
- Sparing with API requests.

## Non-goals (v1)

- Flow and Pick up tabs. Multiple workspaces in one inbox. Slack.
- Notifications. "New activity" may later be an automation trigger.
- Agent liveness. A launched agent is a local note only.
- Any write to Jira or GitHub. Workfile only reads. Clearing is local.

## Anti-goals

Boring, noisy, too much information. A list that cannot reach zero.

## Approaches considered

1. **Grow `wf tracker`.** Add inbox modes to `trackerModel`. Rejected: tracker
   rows are tickets, inbox rows are events; the model would split two ways.
2. **New screen on a shared core (picked).** Pure inbox logic in
   `internal/workfile`, a neutral `Activity` supplied through the provider
   interfaces, a new Bubble Tea model in `internal/cli`, a small command runner
   package. Reuses tracker preview panels and `NextActions`.
3. **Background poller writing a snapshot.** Instant start and triggers with
   the screen closed, but adds a long-running process. Deferred; it can sit on
   the core from approach 2.

The person picked 2, as `wf inbox`, lean scope.

## Scope

- Current workspace only (found the usual way, `--dir` works). The core takes
  a list of workspaces so merging projects later is a loader change.
- Only the Inbox screen. Flow and Pick up come later.

## The screen

Mock A layout: list on the left, preview on the right, footer keys.

```
 workfile   Inbox 3                                   alice · synced 1m ago

 ✗ ABC-12  Bob requested changes on #84       2m │ ABC-12  Fix login redirect   review
 ● ABC-21  Alice asked you to review #97     25m │ New on #84            2 new
 ✓ ABC-11  Ready to move → release            3h │ Bob · auth/redirect.go:41   2m
 ○ ABC-15  Charlie approved #91               5m │ This drops return_to ...
 ○ ABC-16  Checks passed on #93              18m │ Next
 ○ ABC-10  Alice commented on ABC-10         40m │ 1. Answer Bob's 2 comments on #84

 j/k move · e done · s snooze · a agent · o open #84 · r refresh · q quit
```

### Rows

- A row is a ticket. Its PRs sit under it. A PR with no ticket is its own row
  (shown by PR number).
- One row per ticket in the list, showing its most important unhandled event.
  Other events on that ticket appear in the preview. Repeat events fold
  ("3 new comments").
- The person's own actions never show.

### State rows and event rows

- Action rows (`✗ ‼ ● ✓ !`) reflect **current state**: changes requested and
  not since re-reviewed, checks failing now, review requested now, out of
  policy now, ready now. A failure that is now fixed does not show.
- Information rows (`○`) are **events**: approvals, checks passing, pushes,
  ticket comments, comments on PRs you only review.
- **Comments on your own PR** are the one event-driven action row: `●`
  "Bob left 2 comments on #84". They count in `Inbox N` until read. Reading
  means `e` (labelled "read" on these rows), `o` or `a`. Selecting the row
  alone does not clear it, so moving through the list never loses one.
- An action row's fingerprint is its kind plus the identity of the state: the
  PR head commit for checks and reviews, the review request, or the
  `NextActions` summary for policy rows. `e` hides it until the fingerprint
  changes or a new event arrives on that ticket. So a row you clear without
  fixing stays cleared until something happens (as the handoff decided).
- "Oldest waiting first" uses the provider's time for the state where it has
  one (review submitted, review requested, check finished, status changed).
  Otherwise the first time this inbox saw the state, kept locally. *(Builder
  check; my call.)*

### Symbols and colours

| Symbol | Colour (ANSI) | Meaning | Needs action |
|---|---|---|---|
| `✗` | red (1) | needs work: changes requested, failing checks | yes |
| `‼` | red (1), bold | out of policy | yes |
| `●` | cyan (6) | someone asked you: review request, or new comments from others on your own PR | yes |
| `✓` | green (2) | ready to move | yes |
| `!` | yellow (3) | workfile could not check this | yes |
| `○` | faint | for your information | no |

Action rows: coloured symbol, normal text. Information rows: faint symbol and
text. With colour off, symbols carry the meaning. No emoji.

### Order and count

- Action rows first, oldest waiting first. New activity on a row does not move
  it; it gets an "N new" marker in the preview.
- Information rows below, newest first.
- The `Inbox N` count is action rows only. At zero, the inbox-zero moment
  (mock D, without the Flow summary): a short skippable animation, then
  "Nothing needs you". Information rows may still show below.

### Clearing

- `e` done: hides the row until a new event arrives for it.
- `s` snooze: hides it until the snooze time (config, default 1h) or a new
  event, whichever is first.
- Information rows clear themselves once seen: selected or opened in this
  session, they are gone next launch. `e` clears one now.
- `o` opens the PR (or ticket) in the browser and marks its events seen.
- All clearing state is local.

### Keys

`j/k` move, `e` done, `s` snooze, `a` agent (action rows only), `o` open,
`r` refresh now, `q` quit. The footer shows only keys that apply to the
selected row.

### Preview

Reuses the tracker preview panels: ticket head, "New on #N" with the new
comments and reviews quoted (through `clean`), "Next" from `NextActions`,
"Where it is". Shows "agent on it · 40m" when an agent note exists.

## Activity

A new neutral type in `internal/workfile`:

```go
type Activity struct {
    Ticket, PR  string    // either may be empty, not both
    Kind        string    // comment, review, changes_requested, review_requested, approved, checks_passed, checks_failed, pushed
    Who         string    // display name; compared through identities, never across providers
    WhoID       string
    At          time.Time
    Text        string    // short excerpt, untrusted
    Where       string    // e.g. file:line, optional
    URL         string
}
```

- Supplied through the `tracker` and `codeHost` interfaces (a new
  `Activity(ctx, since, refs)` method). Which kinds a provider supplies is
  recorded in `kinds.go`. v1: GitHub supplies PR kinds; Jira supplies
  `comment` on tickets in scope.
- Policy-derived rows (`‼` out of policy, `✓` ready to move, `!` cannot check)
  come from existing assessments and `NextActions`, not from `Activity`.
- A pure function builds rows: `Inbox(assessments, activity, local state, who)
  → []Row`. No terminal or network code. Each row has a stable fingerprint
  (ticket or PR plus newest event time) so done and seen survive restarts.

## What is "my work"

- Tickets in scope assigned to me, not in an end state.
- PRs linked to those tickets.
- PRs I authored, and PRs where my review is requested, even with no ticket,
  **limited to the repos in this workspace's scope**. Other orgs and
  open-source work are left out. This needs a new search method on
  `codeHost`. *(Builder check; my call.)*
- A review request on a PR linked to someone else's ticket is keyed to that
  ticket (rows are tickets), but the ticket is not assessed: no policy rows,
  and "Next" is just "Review #97". *(Builder check; my call.)*

## Fetching and polling

- Poll every `inbox.poll` (default 2m, minimum 30s) while the screen is open.
  `r` polls now. No polling when closed. Each poll runs as `tea.Cmd`s and never
  blocks `Update`.
- Only work in flight is polled. End-state tickets and closed PRs are never
  re-read.
- **Two-phase poll.** First a cheap change check, then full reads only for
  what changed. GitHub reads use GraphQL today, which has no free conditional
  requests; so the change check is one small query for `updatedAt` on the
  in-flight PRs (plus one search for review requests). Only PRs whose
  `updatedAt` moved get their timeline read, limited to events after the last
  cursor. Jira: one search for tickets updated since the last poll.
- Builder may instead use GitHub REST with `If-None-Match` per repo as the
  change check (304s do not count against the rate limit) if it is measurably
  cheaper. Either way, the screen does not change.
- First launch reads activity from the last 24h only.

## Agents

- `a` picks the prompt for the row's kind, falls back to `default`, fills
  placeholders, then runs `inbox.agent`.
- Placeholders: `{key} {pr} {url} {stage} {to} {who} {count} {fix} {prompt}`.
- **Safety:** `agent` is split into words once (shell-style quoting allowed in
  the config value), then placeholders are filled inside those words. It runs
  with `exec`, never through a shell. Provider text cannot inject a command.
  Missing placeholders (no PR on the row) are an empty string, and a prompt
  that needs one is shown as an error toast instead of running.
- The runner starts the command as a background `tea.Cmd` and waits for it
  to exit. It never takes over the terminal (no `tea.ExecProcess`): the agent
  command is expected to open its own window or tab, as `rex tab` does, and
  docs say so. A non-zero exit or start failure shows a red toast with the
  exit code (never its output) and leaves the row in place.
  *(Builder check; my call.)*
- After a successful launch: the row clears like `e`, a toast shows what ran, and a local
  note records ticket, PR, kind, prompt, time.
- The runner lives in a new small package (proposed `internal/run`), no
  terminal code, so automations can reuse it.

## Configuration

Same three layers as `tracker` (personal `~/.config/workfile/config.yml`,
repo `.workfile/config.yml`, personal per-workspace), validated the same way.

```yaml
inbox:
  poll: 2m
  snooze: 1h
  agent: rex tab -- claude {prompt}
  prompts:
    default:           "Get {pr} past {stage} for {key}"
    changes_requested: "Address {who}'s review on {pr} for {key}"
    comments:          "Answer the {count} new comments on {pr} for {key}"
    review_requested:  "Review {pr} for {who}"
    checks_failed:     "Fix the failing checks on {pr} for {key}"
    out_of_policy:     "Bring {key} back into policy: {fix}"
    ready:             "Move {key} to {to}"
```

With no `agent` set, `a` is hidden. Prompt kinds are exactly the action row
kinds: `changes_requested`, `comments`, `review_requested`, `checks_failed`,
`out_of_policy`, `ready`, plus `default`. `!` rows and `○` rows take no
prompt. Unknown prompt kinds and placeholders are config errors.

## Local state

`$XDG_STATE_HOME/workfile/<key>/inbox.json` (next to the `status --since`
snapshots, same key rule): per-provider cursors, done and snooze by
fingerprint, seen events, agent notes. Written atomically. Corrupt or missing
state starts fresh, never fails the command.

## Plain output

Piped, `NO_COLOR` or `TERM=dumb`: one poll, then a plain list with the same
symbols, action rows first, URLs written out. No animation. Plain output
reads `inbox.json` but never writes it: no cursors moved, nothing marked
seen, so a script cannot eat "new" markers. *(Builder check; my call.)*

## Tests

- Pure `Inbox` function: ordering, folding, own actions hidden, done/snooze/
  seen, fingerprints.
- Provider activity parsing from made-up responses.
- Runner: placeholder filling, no shell, injection attempt stays one argument.
- Screen: `WindowSizeMsg` 40 and 60 columns with line widths checked, plain
  test, key-driven `Update`, inbox-zero static frame.
- Config: layering and validation.

## Docs

`README.md` and `docs/usage.md` get `wf inbox` with sample output matching
the code. Provider docs note which activity kinds each supplies.

## Left to the builder

- Exact GraphQL query shapes and cursors; REST vs GraphQL change check.
- Narrow layout: below about 80 columns, drop the preview and show the list
  only, with `enter` opening the preview full screen.
- Package and file names, beyond `internal/workfile` for logic and
  `internal/cli` for the screen.
- Animation details for inbox zero, within the AGENTS.md rules.
