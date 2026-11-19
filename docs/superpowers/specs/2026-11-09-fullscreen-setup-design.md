# Fullscreen setup wizard

Status: draft, not yet approved for planning.

## Why

`wf setup` is the first thing a new person sees. Today it is a Bubble Tea
wizard capped at 76 columns (`panelWidth` in `internal/cli/setup_model.go`),
and it assumes Jira for tickets and GitHub for pull requests. Its step list
(`stepSite`, `stepEmail`, `stepJiraToken`, `stepAskGitHub`, ...) is written
around those two services.

We want setup to:

- feel like a calm, beautiful, fullscreen app, with one clear thing to do per
  screen;
- never assume a provider. The person picks the tools their team uses, and the
  rest of the flow follows from that choice;
- let the person review everything and change any answer before files are
  written.

## Goals

- Fullscreen layout that uses the whole terminal and follows its size.
- Providers are picked from a list built from `workfile.Kinds`. Adding a kind
  to that table makes it appear in setup with no setup changes.
- Six sections in a fixed order (below).
- A summary screen where the person can jump back to any section, change it,
  and return to the summary.
- A final report screen that says what was written and what to run next.

## Non-goals

- Editing an existing `.workfile/`. Setup still refuses a folder that already
  has one (`setupDirectory` in `internal/cli/setup.go`). See open questions.
- Changing what setup writes. It still writes `policy.yml`, `providers.yml` and
  `people.yml`, validates them in a staging folder, and saves credentials to
  the personal credentials file, as `saveSetup` does today.
- New providers. This work makes setup provider-neutral. It does not add Linear,
  Slack or others.

## Anti-goals

These would make it a failure even if it works:

- Any screen that says "Jira" or "GitHub" before the person has picked it.
- Any write to a provider. Setup reads only, like the rest of Workfile.
- Decoration that pushes out information on a small terminal.
- Hard-coded colours, box-drawing panel borders, or a flow that cannot be
  driven by key messages in tests.

## Layout

The whole screen is one Bubble Tea v2 model in the alt screen, so the terminal
is restored on exit.

```
 workfile                                             Providers · 2 of 6
 ●━━━━━━━━━━◉──────────○──────────○──────────○──────────○
 Welcome    Providers

                       (stage, vertically centred)

                         Heading for this screen

                 One line saying what to do on this screen.

                 Fields, list or results


 space select      enter continue      esc back                 ctrl+c quit
```

- **Top bar**: a full-width panel. Bold `workfile` on the left, the current
  section and `N of 6` on the right.
- **Progress line**: one dot per section. Done is green `●`, current is bold
  `◉`, future is faint `○`. Done and current sections are labelled. Future
  ones are dots only.
- **Stage**: fills the space between the bars, content vertically centred in a
  column of at most about 56 columns. One heading, one line of instruction,
  then the fields.
- **Footer**: a full-width panel with the keys that work on this screen, and
  `ctrl+c quit` on the right.
- Panels use the computed soft background from the theme, no borders, and are
  full width.

### Heading (welcome screen only)

The block wordmark, with the tagline "how work moves, written down":

```
        █   █ █▀█ █▀█ █ █ █▀▀ █ █   █▀▀
        █ █ █ █ █ ██▀ ██▄ █▀  █ █   █▀
        ▀▀▀▀▀ ▀▀▀ ▀ ▀ ▀ ▀ ▀   ▀ ▀▀▀ ▀▀▀

          how work moves, written down
```

- With kitty graphics, draw it as an image in the terminal's text colour
  (OSC 10), following the image rules in `AGENTS.md`.
- Otherwise draw the half-block text above.
- When the terminal is too narrow for it, show bold `workfile`.
- The wordmark reveals left to right, quick, stops on any key, and does not
  tick in plain output. It replaces the current page mascot in
  `setup_anim.go`.

## The six sections

### 1. Welcome and folder

One screen. The wordmark and tagline, the read-only promise ("Workfile only
reads. It never changes a ticket or a pull request."), and a text input for
the folder where `.workfile/` will be created, defaulting to the current one.
Validation is today's `setupDirectory`. An error shows under the field with `✗`
and what to do.

### 2. Providers

A multi-select list built from `workfile.Kinds`, ordered by label. Each row
shows the kind's `Label` and its roles, taken from `Tracker` and `CodeHost`
(`tickets`, `pull requests`). Nothing is pre-selected.

```
                [ ]  GitHub     tickets · pull requests
                [x]  Jira       tickets
```

- At least one picked kind must be able to hold tickets. If not, `enter` shows
  `✗ Pick at least one tool that holds tickets` in red.
- If more than one picked kind can hold tickets, a second screen in this
  section asks "Where do tickets live?" as a single choice. Workfile follows one
  tracker. Other picked kinds that can host code are used as code hosts.
- The progress line and section 3 are rebuilt from these picks.

### 3. Set up and test each provider

One sub-flow per picked provider, in the order picked. Each sub-flow:

1. Asks that kind's settings for its role, one screen at a time (for example
   Jira asks site and email, GitHub asks organisation).
2. Asks its credentials from `Kind.Auth`. Tokens are hidden as they are typed,
   and each screen says where to create the token.
3. Tests the connection with a spinner, then shows the result:
   `✓ Signed in as Alice`, `✓ Can read project APP`. On failure it shows `✗`
   with one short fix, and `enter` tries again.

The progress line keeps one dot for section 3. The top bar names the provider
being set up, such as `Jira · 3 of 6`.

**Required provider work:** the questions each kind asks must come from the
kind, not from setup. Today they are hard-coded steps in `setup_model.go`. The
builder decides the shape, but setup must not branch on a kind's name.

### 4. Projects, statuses, people

Three screens, using the tracker picked in section 2:

- **Projects**: a filterable multi-select (Bubbles `list`), as today.
- **Statuses**: map tracker statuses to pipeline states, as today.
- **People**: confirm the people imported for `people.yml`, as today.

**Required provider work:** `SetupProjects`, `SetupStatuses` and `SetupPeople`
in `internal/provider/setup.go` call Jira directly. They must move behind the
`tracker` interface (`internal/provider/tracker.go`) so any tracker can supply
them, with a GitHub tracker implementation. A tracker that cannot list one of
these must be handled with a clear screen, not a crash.

### 5. Summary

Every answer, grouped by section, one line each:

```
                   Folder     ~/code/example/.workfile
                   Jira       example.atlassian.net · Alice
                   GitHub     example-org · alice-example
                   Projects   APP, API
                   Pipeline   backlog → doing → review → done
                   People     3
```

- Rows are a list. `↑↓` moves, `enter` on a row jumps to that section.
  After the change, the person returns to the summary, not through the rest
  of the flow.
- Changing providers in section 2 may invalidate later answers (for example a
  new tracker means new projects and statuses). Those sections are marked
  `✗ needs an answer` on the summary and must be completed before writing.
- A separate action, `w write files`, writes. Its line names the three files.

### 6. Final report

Shown after files are written:

- `✓ Your workfile is ready` and the folder path.
- What was written: the three file names, and that credentials went to the
  personal credentials file, not into `.workfile/`.
- Next commands, each with the question it answers:
  `wf health` (how healthy is the board?), `wf status --me` (what should I do
  next?), `wf tickets` (browse your tickets).
- If saving failed, this screen shows `✗`, the reason, and leaves the summary
  one `esc` away so nothing is lost.

## Fitting the terminal

- Lay out from the latest `tea.WindowSizeMsg`. Nothing overflows.
- Narrow terminals drop, in order: the wordmark (to bold text), the animation,
  step labels on the progress line, then vertical spacing. Words, fields and
  keys always stay.
- Plain output (`NO_COLOR`, `TERM=dumb`, piped) has the same words with no
  escapes, no background and no animation.

## Tests

- 40 and 60 column `tea.WindowSizeMsg` tests that check every line width of
  `View()` on every section.
- A `NO_COLOR` test.
- Key-driven tests through `Update` for: picking providers, the tracker choice,
  a failed then passing connection test, jumping from the summary to a section
  and back, and an invalidated section blocking the write.
- A test of the wordmark's static frame.
- Tests use made-up responses and need no credentials.

## Left to the builder

- Package layout. Putting the new screen under `internal/tui/setup` (see the
  earlier discussion) is welcome but not required.
- How a kind declares its setup questions and test.
- Exact wording beyond what is written here, following the `AGENTS.md` rules.

## Open questions

- What should `wf setup` do when the folder already has a `.workfile/`? Today
  it refuses. Options: keep refusing, or load it into the summary screen to
  edit. This changes section 1 and section 5.
- Should section 3 allow two connections of the same kind (for example two
  GitHub organisations)? The provider guide allows it in `providers.yml`.
