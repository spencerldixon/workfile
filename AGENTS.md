# Working on workfile

Workfile is a small Go CLI. It reads Jira tickets and GitHub pull requests,
checks them against a written workflow policy, and reports what needs attention.
It only reads. It never changes a ticket, a label or a pull request.

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
- `internal/cli`: commands and everything drawn on the terminal.

## Designing terminal output

Every report should answer a question a person actually has, and make the next
action obvious. These rules came from designing `health`, `status`, `status --me`
and `status TICKET`. Follow them for any new output, and keep the commands
consistent with each other.

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
- Draw forward moves only. Returns are implied; list the ones that apply in text.
  Show each stage once. If a branching workflow does not fit as a tree, fall back
  to one line per stage naming its forward destinations.
- One convention across commands. `status TICKET`, `status --me` and
  `health --gate` should word and group the same fix the same way. Reuse the
  shared helpers (`groupedSteps`, `nextSteps`, `ticketHead`, `stageBoxes`,
  `rateRows`, `block`) rather than writing a new layout.

### Blocks, not boxes

- Draw each section as a **block**: a title, a coloured edge (`▌`) on the left, and
  a soft background panel. Do not draw box-drawing borders.
- Use `view.block` (or `view.lane` in tests). It tints the panel from the
  terminal's own background and falls back to the edge alone if the terminal does
  not answer.
- Edge colour carries meaning: red when something in the block needs work, green
  when it is all fine, muted for neutral information such as a pipeline or a PR
  list.
- Keep blocks short and scannable: lead with the key, then one short line each.
  Truncate titles to one line. Put tags (`→ review`, `OUT OF POLICY`, a role)
  at the right edge with `leftRight`.

### Terminal colours

- Use the 16-colour ANSI palette for meaning so the person's theme decides the
  shade: `31` red for failing or blocked, `32` green for passing or ready, `2`
  dim for secondary text, `1` bold for keys and titles. Never hard-code a
  256-colour or RGB value for red, green or text.
- The only computed colour is the panel background, derived from the terminal's
  reported background (OSC 11), asked once per run, in true colour or the nearest
  256 colour. `WORKFILE_BACKGROUND=off` turns it off.
- Never rely on colour alone. Pair it with an icon or word: `✓` ready, `✗` needs
  work, `‼` out of policy, `!` cannot check, `·` done.
- Output piped to a file, `NO_COLOR`, or `TERM=dumb` must be plain text with the
  same information: no escapes, no background, and web addresses written out
  where a hyperlink would have been.
- Ticket keys and PR numbers are hyperlinks (`view.link`) when colour is on.

### Fit the terminal

- Lay out from `view.innerWidth()`. Wrap text; never let a line overflow.
- Narrow terminals drop decoration before information: bars first, then counts,
  but keep the percentage and the words.
- Every new view needs a test that renders at 40 and 60 columns and checks the
  line width, and a plain (`NO_COLOR`) test.

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
