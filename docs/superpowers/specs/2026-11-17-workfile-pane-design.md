# Workfile ticket pane

## Chosen design

Use prototype A's action-first layout with prototype B's **Where it is** diagram
at the top. The pane belongs to one ticket and is shared by tracker board and
list views.

1. Where it is: the configured horizontal workflow, with the current stage marked.
2. Next action: a short summary and evidence freshness.
3. Needs you: numbered unresolved fixes to work through.
4. Unclear ownership or evidence, when needed.
5. Waiting: work with no current personal action.
6. Other linked PRs, without repeating targets already named in the action list.

Group by ticket or PR, naming each target once. Deduplicate the same fix across
gates and destinations using the existing NextActions domain result. The display
must preserve distinct required reviewer groups even when their wording is the
same. Keep numbering continuous. A PR that both needs a fix
and awaits review stays in Needs you, with its waiting state beneath the fix.

This is not a dismissible inbox: an unmet requirement disappears only when new
evidence confirms it is resolved. Workfile does not change tickets or PRs. Keep
policy permission to move distinct from operational PR problems.

Use terminal-derived panel backgrounds. Preserve padded, nested comment panels
in the ticket column. On narrow terminals, put the actionable pane before the
description. A long action list must remain scrollable even when the ticket
text is short.

## Implementation seam

`internal/cli/workfile_pane.go` owns workflow and action presentation. It accepts
an assessment and computed next actions, without fetching or mutating evidence.
The tracker adapter supplies the selected ticket, time, theme and width.
`ticket_preview.go` composes that pane with the ticket's description and comments.

## Primary-source prototypes

The original three HTML alternatives and the selected hybrid are captured on
branch `prototype/workfile-pane-2026-11-17`. They are intentionally outside the
implementation branch. Retrieve the self-contained HTML with:

```sh
git show prototype/workfile-pane-2026-11-17:internal/cli/workfile-panel.prototype.html > /tmp/workfile-panel.prototype.html
```
