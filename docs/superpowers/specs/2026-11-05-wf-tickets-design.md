# `wf tickets` design

**Status:** approved and built. Works with any tracker adapter and any terminal with kitty graphics.

## Why

Reading a ticket today means leaving the terminal for the tracker, then running
`wf status APP-42` to learn what is blocking it. `wf tickets` puts both in one
place: find a ticket fast, read it as the tracker shows it, and see what Workfile
knows that the tracker does not.

## Goals

- List the tickets in the tracker's scope, in a scrollable list with fuzzy search.
- Show the selected ticket underneath: description, images and comments.
- Add what Workfile knows: where the ticket is in the pipeline, what blocks it,
  how to fix that, its PRs, and where it can move next.
- Show large titles and inline images with kitty graphics where the terminal
  supports them.
- Press `o` to open the ticket in the browser.

## Non-goals

- **No writes.** No assigning, status changes, comments or replies. Workfile
  stays read-only, as `AGENTS.md` requires. Every new request is a read.
- No policy icons in the list. They would need GitHub reads for every ticket in
  scope. Policy shows for the selected ticket only.
- No `--json` or ticket key arguments in the first version.
  `wf status` covers those needs.

## Anti-goals

It has failed, even if it works, if:

- moving down the list floods Jira or GitHub with requests
- a terminal without kitty graphics gets a broken or noisy screen
- text from a ticket can inject escape sequences into the terminal

## The screen

Panels are soft backgrounds in the real screen. The lines here are only to show
where one ends.

```text
  Tickets   / login█                                   3 of 84

  APP-42  Fix redirect loop on expired login     Review   Alice
▸ APP-57  Add single sign-on login               To do    Bob
  APP-61  Login page slow on first load          To do    -
  ──────────────────────────────────────────────────────────────
  APP-57 · Story · To do · Bob

  ▇▇▇ Add single sign-on login ▇▇▇         (large title, image)

  to do → in progress → review → qa → done

  ✗ Needs work
  1. Get at least 1 approval on example-team/api #31
     unblocks → qa · → release

  Why
  Customers on example.com want to log in with their own
  accounts, so support stops resetting passwords.

  [ login-mock.png, inline, scrolls with the text ]

  Comments (2)
  Alice · 2 days ago
  Should this cover the admin screens too?

  Pull requests
  example-team/api #31 · open · 0 approvals

  ↑↓ move · / search · tab scroll ticket · r refresh · o open · q quit
```

Order in the bottom half: identity, title, pipeline, what blocks it, Jira
description and images, comments, PRs, next moves. Blockers come before the
description so the next action is on the first screen. Workfile's panels show
only what is unmet, so they stay short on a healthy ticket.

The list takes the top half, or less when there are few tickets, so the spare
room goes to the ticket.

The screen fills the terminal edge to edge. From 110 columns the ticket and
Workfile's view sit side by side: the ticket's text at a comfortable length on
the left, and blockers on the right, which stay in view while the text scrolls.

The current row is a bright bar while the list has focus and a soft one while the
ticket has it, and keeps its `▸` marker without colour. A search underlines what
it matched. Each comment is a block, newest first, and a reply's block sits
indented under what it answers, in conversation order. `Client.Detail` sorts
comments for every tracker, and an adapter that cannot return them all returns the
newest. Jira's comment model has no replies, so Jira comments are one level
deep.

Tickets follow `policy.states`, keeping the tracker's order within a stage.
Unmapped tickets come last.

Tabs along the top run All, Assigned to me, then each state in workflow order,
starting on All. `←` and `→` move between them. They only filter the list, which is read once,
so switching is instant. States come from `policy.states`, so tabs work the same
on every tracker and need no setup. `--view` sets where the screen starts and what
piped output shows. `--scope` goes to the tracker in its own query language. The
list ignores `only_mine`, because it needs other people's and unassigned tickets
and has its own Assigned to me tab.

## Keys

| Key | Does |
|---|---|
| `↑` `↓` | Move through the list, or scroll the ticket when it has focus |
| `/` | Start searching. Typed letters go to the search, not to shortcuts |
| `enter` | Read the selected ticket, or keep a search being typed |
| `tab` | Move focus between the list and the ticket |
| `esc` | Clear the search, or leave the ticket and return to the list |
| `←` `→` | Move between views |
| `r` | Read the selected ticket again |
| `o` | Open the selected ticket in the browser |
| `q` | Quit |

## Reading data

Requests happen in three stages, so the screen is useful early and most tickets
are never read in full.

1. **On start, one slim search.** `Listing` reads only what a row shows. It must
   not use `Tickets`, which reads every description and can read history in bulk.
   Fuzzy search needs the whole scope, so this is the one bulk read, and it
   does not apply `only_mine`, unlike `Tickets`.
2. **On selection, after a 200ms pause.** Moving quickly past a ticket reads
   nothing. Then, for that ticket only: `Detail` for its text and comments, and
   `Tickets`, `Records` and `Assess` with its key for the Workfile panels. Each
   draws when it arrives.
3. **Images, last.** Download only the images in the selected ticket, only when
   the terminal can show them.

Everything is cached by ticket key for the session. Nothing is read twice unless
the person presses `r`. Cancel reads for a ticket the person has moved past.

A failed read shows in the pane in words, with the other part still shown if it
worked. One failed image never fails the ticket.

## Rendering

### Rich text

Each tracker turns its own format into a block model in `internal/workfile`, with
no terminal code: heading, paragraph, list, code,
quote, table, rule, image, each holding runs of text with bold, italic, code and
link. For Jira, `description()` stays as it is, because policy checks depend on
its output.

Unknown node types show their text and never fail.

### Graphics

- **Detection:** ask the terminal with a kitty graphics query, once per run,
  before Bubble Tea starts, alongside the existing background query and with the
  same short timeout, and never by the terminal's name. `WORKFILE_GRAPHICS=off` turns graphics off, matching
  `WORKFILE_BACKGROUND=off`.
- **Ids:** name images with the 256-colour form, ids 16 to 255, reused in turn.
  Bubble Tea rewrites 24-bit colour on terminals without true colour, which
  would corrupt the id.
- **Sending:** send each image once with `tea.Raw`, using the kitty writer in
  `github.com/charmbracelet/x/ansi/kitty`.
- **Placing:** draw with kitty Unicode placeholders (`kitty.Placeholder`). They
  are ordinary cells in `View()`, so images should scroll and clip with the
  viewport with no cursor tricks.
- **Cleaning up:** delete images on quit, and on leaving a ticket if memory
  needs it.

### Titles

Draw the title into a transparent PNG at twice the text height, using the Go
fonts from `golang.org/x/image`, which is a new dependency. The ink takes the
terminal's foreground colour, asked once with OSC 10. Long titles wrap, then fall
back to bold text if they would take more than 3 lines of image, or if the font
lacks a character, such as Japanese text or emoji.

### Without graphics

| Situation | Title | Image |
|---|---|---|
| No kitty graphics | Bold text | Named link, such as `login-mock.png` |
| Narrow terminal | Bold text | Named link |
| Piped, `NO_COLOR`, `TERM=dumb` | No screen. Print key, title, status and web address for each ticket | None |

## Safety

- All tracker text goes through `clean` before it is drawn: titles, comments, file
  names, link text.
- An adapter builds image addresses itself. Jira downloads only from
  `/rest/api/3/attachment/content/{id}`, built from the id, never from a URL in the
  response. Pass `redirect=false`: Jira otherwise answers `303` to its file
  store, and this client follows no redirects. The token reaches no other host.
- Cap each download at 16 MB, the limit in `request`. Read the size with
  `image.DecodeConfig` before decoding, and refuse huge images. Accept PNG, JPEG
  and GIF only. Scale down before sending.
- `o` opens only the ticket's own URL, which Workfile builds from the site and
  key. It never opens a URL found in ticket text.

## Code

| File | Holds |
|---|---|
| `internal/workfile/document.go` | Block model and ticket detail types |
| `internal/provider/tracker.go` | The `tracker` interface and which adapter to use |
| `internal/provider/jira_document.go` | Jira adapter: turn ADF into blocks, apart from `description()` |
| `internal/provider/jira_detail.go` | Jira adapter: slim listing, ticket detail, image download |
| `internal/cli/tickets.go` | `ticketsCommand`, the plain output, starting the screen |
| `internal/cli/tickets_model.go` | Bubble Tea model: keys, focus, reads, cache |
| `internal/cli/tickets_view.go` | Drawing the list and ticket |
| `internal/cli/graphics.go` | Detection, sending and placing images, drawing titles |

- `source` gains three read methods. The fake in tests gives them made-up data.
- Reuse the `ticketModel` panels (`track`, `attention`, `pullRequests`,
  `nextMoves`), so wording matches `wf status TICKET`.
- Use Bubbles `list`, `viewport`, `textinput`, `spinner` and `help`. The
  `sahilm/fuzzy` package becomes a direct dependency if `list`'s own filter does
  not fit.
- Move `openSetupBrowser` to a shared `openBrowser`.
- Add `tickets` to `commands` and `help`.

## Testing

- `WindowSizeMsg` at 40 and 60 columns, checking the width of every `View()`
  line, and a plain `NO_COLOR` test, as `AGENTS.md` requires.
- Drive `Update` with key messages: search, focus, `o`, `r`, quit.
- Debounce: moving past three tickets reads only the last.
- Cache: returning to a ticket reads nothing.
- ADF fixtures for each block type, plus an unknown node.
- Hostile fixtures: escape sequences in titles and comments, a huge image, a
  non-image attachment.
- No fake ever receives a write, and the provider adds no `PUT` or `DELETE`.

## Any tracker, any terminal

Nothing here is written for one tracker or one terminal.

- **Trackers:** the screen depends on `Listing`, `Detail` and `Image`, behind the
  `tracker` interface in `internal/provider/tracker.go`. Keys, statuses and
  addresses are the tracker's own text. `Type` and `Assignee` may be empty.
  Rich text arrives as `workfile.Block`s, and `Attachment` means something only
  to the tracker that wrote it. Jira is the first adapter. A new tracker is one
  adapter file and one line in `Client.tracker`.
- **Terminals:** graphics depend only on the terminal's answer to the kitty
  graphics query. The code never checks a terminal's name.

### Known limit

The kitty query says a terminal has graphics. It does not say the terminal
supports Unicode placeholders, and no query does. A terminal with graphics but
no placeholders would show odd characters where images belong.
`WORKFILE_GRAPHICS=off` is the way out.

## Changes to project rules

`AGENTS.md` now allows one more computed colour: title images use the terminal's
foreground colour, read from OSC 10. It also records `WORKFILE_GRAPHICS`, the
image rules and the tracker rule.

Update `README.md` and `docs/usage.md` with the command, keys and fallbacks.
