# Handoff: command center TUI

**Status:** brainstorming, mid-conversation. No design doc approved, nothing built.
**Next session:** answer the open question below, lay out build approaches, write the design doc.
**Mocks:** https://claude.ai/artifact/MAYxS8GaT1WBEGeDnEWqu7 (A split inbox, B flow board, C one at a time, D inbox zero)

## Why

Attention is pulled across GitHub, Jira and Slack. Workfile already folds
work into one state machine. The command center is the one screen on top of
that: what wants me right now, and where is everything I have in flight.
Typical day: 3 PRs started yesterday, 5 shipped and moving, all at different
readiness. Need to see that at a glance, act on it, hand pieces to agents, and
pick up new work.

## Decided

- **Builds on `wf tracker`** (uncommitted on `lint-redesign`): list + preview,
  views, `next_actions.go` attention states, `p`/`c` keys.
- **A row is a ticket.** PRs sit under it. A PR with no ticket is its own row.
- **Mock A is the base.** An inbox you work through, with age on every item.
- **Two modes, one screen:**
  - **Inbox** (default): only what wants me, one line per event, worded as an
    action, with age. `e` done (hidden until something new happens), `s` snooze,
    `a` agent, `o` open. Repeat events fold ("3 new comments"). My own actions
    never show. Clearing state is local only.
  - **Flow** (`tab`): everything in flight, grouped by who acts next: needs me,
    agent on it, waiting, ready to move. Plus "N shipped today".
  - **Pick up**: ready-to-start tickets, `u` runs my own pick-up command.
- **From B:** one-line pipeline strip on Flow (`doing 1 → review 4 → qa 3`),
  empty stages folded, select a stage to filter. Full lanes maybe later.
- **PR activity is the heartbeat.** New comments, reviews, changes requested,
  re-review requests. Show "new since you last looked"; opening marks seen.
- **Provider agnostic.** New neutral `Activity` concept (comment, review,
  changes requested, review request) supplied through `tracker` / `codeHost`
  interfaces and recorded in `kinds.go`. Screen never knows the service.
- **Shortcuts in config.** Keys run the person's own commands with `{key}`,
  `{pr}`, `{url}`. Agent launch and pick up are just default shortcuts, e.g.
  `rex tab -- claude "/pick-up {key}"`. Workfile never writes to Jira/GitHub.
- **Agent tracking v1:** launching writes a local note (ticket, PR, prompt,
  time, tab). Row shows "agent on it · 40m". No liveness check yet.
- **Freshness:** poll only in-flight items every ~2 min, plus `r`. No
  notification code: "new activity" is an automation trigger the person wires
  to whatever they like.
- **Slack:** out of v1.
- **Inbox zero:** short skippable delight, then "Nothing needs you" and a quiet
  summary of what is moving without me.

## Anti-goals

Boring, noisy, too much info. Must feel like an inbox you can clear, plus a
browsable overview.

## Open

1. **Multiple projects (next question).** A `.workfile` is per repo. When
   juggling work across several workspaces, one merged inbox or one per project
   with switching? Merged means loading several workspaces at once (new).
2. Inbox sort within a kind: oldest waiting first or newest first.
3. Then: lay out 2-3 build approaches, pick one, write the design doc, builder
   check, then `writing-plans`.
