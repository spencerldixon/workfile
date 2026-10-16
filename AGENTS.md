# Working on workfile

Workfile is a small Go CLI.

It's goal is to be "workflow as code" for your engineering team.

You create a .workfile in your repo, with a policy.yml that describes how work moves through your team across providers like Jira, Github and Slack.

Since workfile reconciles state across these services, you can build automations on top of this: "when this happens to my work, do that".

Workfile's checks only read. 
Workfile itself never changes a ticket, a label or a pull request. 

Automations run commands and agents the person chose, on their own computer; those do the changing, with their own access. 

Never add a Jira or GitHub write to Workfile.

## Build and test

```sh
eval "$(mise env -s bash)"   # puts Go on PATH if it is installed with mise
go vet ./... && go test -race ./...
make install                 # builds and copies wf to ~/.local/bin
```

Tests use made-up responses and need no credentials. Run `make install` after
output changes so the person you are working with can try the real thing.

## Packages

- `internal/workfile`: loads policies and checks tickets. No terminal code, no network.
- `internal/provider`: reads Jira and GitHub.
- `internal/automation`: finds a person's automations, remembers what each
  check saw, and runs their commands. No terminal code, no network.
- `internal/cli`: commands and everything drawn on the terminal.


## Philosophy

- Everything returned should be useful.
- Every UI should be beauitful and easy to understand
- Performance should be fast, and not overload providers with unneccesary requests.
- We should be sparing and targeted with requesting data, no bulk fetches unless absolutely necessary.

## Bubble Tea owns the UI

All terminal UI is built with Bubble Tea, Bubbles and Lip Gloss, **version 2**
(`charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`).
This is the direction for every command, starting with `setup`.

- Never import the v1 packages (`github.com/charmbracelet/bubbletea` and
  friends). Bubble Tea v1 asks the terminal for its colours the moment it is
  imported, which hangs five seconds on terminals that do not answer, for every
  command, not just the TUI.
- Lip Gloss v2 always writes colour. Go through the `theme` helpers, which write
  plain text when colour is off.

- New screens are Bubble Tea models. Use Bubbles components (`textinput`,
  `list`, `spinner`, `viewport`, `help`) before writing your own. Do not add
  hand-rolled prompts, `bufio` wizards or `term.ReadPassword` flows.
- Keep models small and testable. Logic lives in plain functions. `Update`
  takes messages and returns a model. `View` only draws. Network calls run as
  `tea.Cmd`s and never block `Update`.
- Our priority is a **beautiful, simple design**. Fewer elements, generous
  spacing, one clear thing to do per screen. If a flourish does not help the
  person, leave it out.
- Delight is welcome where it fits, such as the animation on `setup`. It must
  be skippable, quick, and absent in plain output.
- Old output code (`view.block`, `view.lane`, `leftRight` and friends) stays
  until its command is converted. Do not use it in new screens, and do not
  extend it. Delete it once nothing calls it.

## Designing terminal output

Every report should answer a question a person actually has, and make the next
action obvious. These rules came from designing `health`, `status`, `status --me`
and `status TICKET`. Follow them for any new output, and keep the commands
consistent with each other. They apply to Bubble Tea screens too, except where
a section below says it is for old output only.

### Be useful and actionable

- Start from the question: *how healthy is the board? what do I do next? what is
  blocking this ticket?* Design the output to answer it, then check it does.
- Summary first, detail on request. Show a short rollup by default and let flags
  such as `--gate` and `--stage` drill into tickets.
- Say what to do, not only what is wrong. Prefer "Get at least 1 approval" to a
  bare failing check, and name the ticket or PR it applies to.
- Show only what is unmet. Passing requirements are noise in an action list.
- Every number needs a defined denominator, and the title or caption says what it
  is ("failing of tickets that must pass"). Show percentages next to counts.
- Never blame or rank people. Show gates, stages, tickets and PRs.
- Leave out what does not matter to the question. A to-do view ignores finished
  tickets, even ones that are out of policy.
- If a report is a sample, say so.

### Order and group the way the work happens

- Order by the workflow pipeline (`policy.states`), never by count or alphabet.
  Gates follow the first stage that requires them.
- Group fixes by the thing they apply to: the ticket first, then each PR, **each
  named once**. Number fixes through the whole view, and under each fix say which
  destinations it unblocks (`unblocks → qa · → release`) when there is more than
  one blocked destination. Never repeat a fix or a PR under every destination.
- Draw the pipeline horizontally, left to right, with each stage named once.
  Connect shortcuts and joins above the chain and returns below it. Draw only
  configured moves. Narrow views shorten arrows first, then use horizontal
  strips with numbered links; never repeat state names to show a destination.
- One convention across commands. `status TICKET`, `status --me` and
  `health --gate` should word and group the same fix the same way. Reuse the
  shared helpers (`groupedSteps`, `nextSteps`, `ticketHead`, `stageBoxes`,
  `rateRows`) rather than writing a new layout.

### Panels, not boxes

- Draw each section as a **panel**: a title and a soft background. No edge bar
  (`▌`) and no box-drawing borders. Space and the background do the grouping.
- Build panels with Lip Gloss. Take the background from the terminal's own
  background, so it follows the person's theme. If the terminal does not
  answer, draw the panel with no background at all.
- Panel titles are plain bold words with no icon. Meaning lives in the rows:
  the icons, counts and red or green on the items themselves. Older screens
  that still put `✗` or `✓` on titles change when they are converted.
- Keep panels short and scannable: lead with the key, then one short line each.
  Truncate titles to one line. Put tags (`→ review`, `OUT OF POLICY`, a role)
  at the right edge.
- The old `view.block` still draws the edge until its commands are converted.
  Leave it alone, and do not copy it.
- make panels full width.


### Terminal colours

- The terminal's theme decides the colours. In Lip Gloss use only the 16 ANSI
  colours (`lipgloss.ANSIColor`): `1` red for failing or blocked, `2` green for
  passing or ready, faint for secondary text, bold for keys and titles. Never
  hard-code a 256-colour, RGB or hex value for red, green or text, and do not
  use adaptive or custom palettes.
- The only computed colour is the panel background, derived from the terminal's
  reported background (OSC 11), asked once per run, in true colour or the nearest
  256 colour. `WORKFILE_BACKGROUND=off` turns it off.
- Icons stay: `✓` ready, `✗` needs work, `‼` out of policy, `!` cannot check,
  `·` done.
- Never rely on colour alone. Pair it with an icon or word: `✓` ready, `✗` needs
  work, `‼` out of policy, `!` cannot check, `·` done.
- Output piped to a file, `NO_COLOR`, or `TERM=dumb` must be plain text with the
  same information: no escapes, no background, and web addresses written out
  where a hyperlink would have been.
- Ticket keys and PR numbers are hyperlinks (`view.link`) when colour is on.

### Fit the terminal

- Lay out from the width in the latest `tea.WindowSizeMsg`. Wrap text; never
  let a line overflow.
- Narrow terminals drop decoration before information: animation and bars
  first, then counts, but keep the percentage and the words.
- Every new screen needs a test that sends a `tea.WindowSizeMsg` of 40 and 60
  columns and checks the width of every line of `View()`, and a plain
  (`NO_COLOR`) test. Drive `Update` with key messages; do not rely on a real
  terminal.
- Animations stop on any key, do not tick in plain output, and are covered by a
  test of their static frame.

### Safe output

- Run provider text through `clean` before printing it. Titles and messages are
  untrusted and must not inject escape sequences.
- Do not print provider response bodies.

## Writing docs and examples

- Plain, friendly English. Short sentences. Explain a term the first time it
  appears, and point new readers at `docs/getting-started.md`.
- Update `README.md` and `docs/usage.md` whenever output changes, including the
  sample output. Samples must match what the code prints.
- Examples and tests are anonymous. Use Alice, Bob and Charlie, `example-*`
  accounts, `example.com` addresses, and placeholder credentials such as
  `${EXAMPLE_JIRA_TOKEN}`. Never use real names, company names, account IDs,
  addresses, hostnames or tokens, in files or in commit messages.
