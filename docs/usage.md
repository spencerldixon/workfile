# Using wf

New to Workfile? Start with [Getting started](getting-started.md).

Run `wf` inside a project that has a `.workfile/` folder, or in any folder
below it. `wf` uses the nearest `.workfile/` it finds. Use `--dir` to choose
where it starts looking.

## How the output looks

Every report is drawn in **blocks**. Each block has a title, a coloured edge on
the left, and a soft background. The edge is red when something in the block
needs work and green when everything is fine.

If your terminal answers when asked for its background colour, each block gets a
background a little lighter (on dark themes) or a little darker (on light
themes) than your own. Workfile asks once per run, and waits at most a fifth of a
second. If your terminal does not answer, you get the coloured edge only. Set
`WORKFILE_BACKGROUND=off` to turn the background off. Red and green are your
terminal theme's own. Piped output has no colours, links or background.

## Health: how does the board compare to policy?

```sh
wf health
wf health --gate code_review
wf health --stage review
wf health APP-42
```

Plain `wf health` checks every ticket in your Jira search and sums them up in
four blocks. They always follow the order of your workflow.

- **Summary**: how many tickets are in policy and how many are out.
- **Gates**: which gates are failing, and for how many tickets.
- **Stages**: which stages hold tickets that break policy.
- **Most common reasons**: the requirement that fails most often for each failing
  gate.

The percentages work like this:

- For a gate, it is the failing tickets divided by the tickets that must pass
  that gate. A ticket must pass a gate if it is in, or past, the stage the gate
  guards. A ticket in `todo` does not count against `code_review`.
- For a stage, it is the failing tickets divided by all tickets in that stage.

Red means failing and green means passing. These are your terminal theme's own
red and green. If you pass `--limit`, only a sample is checked, and the summary
says so. Tickets with a Jira status that is not in `providers.yml` are shown as
"cannot check" and are left out of the percentages.

Want to see the tickets behind the numbers? Use `--gate` or `--stage`, or both
together. You can also name tickets, as in `wf health APP-42`.

`wf health --gate refinement` looks closely at one gate:

- The first block says how many of the tickets that must pass the gate fail it, as
  a number, a bar and a percentage. Below that is one bar for each rule in the
  gate, so you can see which rule fails most. A ticket can fail more than one
  rule.
- **Where they are** shows the failing tickets by stage, in workflow order.
- Then there is one block per stage listing the failing tickets, with only this
  gate's problems. A ticket that fails two gates appears under each.

`--stage` lists the failing tickets in one stage in the same way, with every
unmet requirement.

Health asks one question: do the ticket's facts today match at least one valid
path to its stage? A ticket in review that cannot match any path into review is
out of policy. A ticket in todo with a short description is fine if the
description is only needed to leave todo. On a board with branches, a ticket
needs one valid path. It does not need the gates from every branch. Done tickets
are checked too.

Workfile cannot see history. It checks what is true now, not what was true when
a ticket moved. See [branches and current facts](policy.md#current-facts-and-branching-paths).

## Status: what should happen next?

```sh
wf status
wf status --me
wf status APP-42
```

The overview has three parts, always in the order of your workflow:

- **Summary**: how many tickets are out of policy (‼), need work (✗), are ready
  to move (✓), or are done (·), with percentages and bars. Tickets whose Jira
  status is not in your workflow show as "cannot check".
- **Pipeline**: one row per stage with its ready and failing counts, and a bar
  that shows how much of the stage is failing.
- **One block per stage**: the tickets in that stage. Out of policy tickets come
  first, then tickets that need work, then ready ones. Failing tickets list
  every unmet requirement. Ready tickets take one line and show where they can
  go. Done tickets are counted in the pipeline, and listed only if they are out
  of policy.

The groups mean:

1. **Cannot check**: its Jira status is not in your workflow.
2. **Out of policy**: a gate from an earlier stage fails.
3. **Needs work**: it is fine so far, but every move forward is blocked.
4. **Ready to move**: a move forward is open, or a valid return exists.
5. **Done**: it is finished and passes every earlier gate.

### Your own work

```sh
wf status --me
wf status --user bob
```

This is a kanban board of the work assigned to you that is not finished.
Tickets in an end state, such as done, are left out.

- A short strip at the top counts your tickets, how they are doing, and which
  stages they are in.
- Each stage with a ticket gets a **lane**, in workflow order. Inside a lane,
  each ticket shows where it can go next, then the fixes it still needs, grouped
  exactly as `wf status APP-42` groups them: the ticket's own fixes first, then
  each pull request once. A dimmed line under a fix says which destinations it
  unblocks. The ticket with the most to fix comes first. A ticket with nothing to
  fix takes one line.
- **Also involves you** is a smaller lane for pull requests you wrote or were
  asked to review. It lists the fixes for each PR, grouped the same way, and why it is yours.
- If nothing needs you, it says so.

See [How the output looks](#how-the-output-looks) for the lane colours.

### One ticket

```sh
wf status APP-42
```

Name a ticket to see the full picture:

- The ticket key links to Jira. Its title and assignee sit above the workflow.
- Arrows show the forward moves in your workflow. Every stage appears once. The
  current stage is underlined. The places it can go next are green if open and
  red if blocked. Other stages are dimmed. Moving back is not drawn. The
  Transitions block below lists the returns that apply. If a branching workflow is
  too wide to draw as a tree, you get one line per stage instead, naming where it
  can go.
- Each pull request shows its repository and links to GitHub. A short status
  tells you if the policy gates pass, if a review is missing, or if it has been
  merged. If a PR was not checked, it does not claim to pass. GitHub's own
  branch rules and checks still apply on top of your policy.
- The **Transitions** block compares each place the ticket can go with each gate.
  A green tick means pass. A red cross means fail. A dash means the gate does not
  apply there.
- **Next steps** lists each fix once, under the ticket or pull request it
  applies to. Under each fix, a dimmed line says which destinations it unblocks,
  for example `unblocks → qa · → release`. A fix that several destinations need
  is not repeated. **Ready now** lists the moves that are open. Returns are marked
  with ↩.

For example, suppose QA needs a code review, and release needs the review plus a
waiver. The review appears once, and says it unblocks QA and release. The waiver
appears once, and says it unblocks release only. Workfile never does the work
for you. The lists are for you to carry out in Jira or GitHub.

Finished tickets show any unmet requirements, or say that no next step is needed.
On narrow or wide terminals you still see the same information. Wide tables
become one list per destination, and long text wraps inside the blocks.

When you pipe the output, or set `NO_COLOR`, there are no colours or links. You
get a `(current)` marker, the same ticks and crosses, and the full ticket and PR
web addresses.

## Test: do the connections work?

```sh
wf test
wf test jira
wf test github another_github
```

Names are the connection names in `providers.yml`. With no names, every
connection is tested. If one fails, the rest still run.

For Jira, the test checks who the token belongs to and reads one ticket from your
search. It also reads label history if your gates need it. For GitHub, it checks
who you are, lists the repositories you can see, and runs a sample PR search. If
your Jira search or GitHub organisation is empty, the test tells you.

A pass means those reads worked. It does not prove you can see every ticket or
repository you meant to include.

## Options

| Option | What it does |
| --- | --- |
| `--limit N` | Check up to N tickets. `status` checks 25 unless you change it. `health` checks all of them unless you set this. N must be 1 or more. |
| `--all` | Check every ticket in your search, with no limit. |
| `--me` | Only tickets assigned to you, or linked to a PR you opened or were asked to review. |
| `--user NAME` | The same filter for a person in `people.yml`. |
| `--gate NAME` | Health only. List the tickets failing this gate. |
| `--stage NAME` | Health only. List the tickets in this stage that break policy. |
| `--failing` | Status only. Hide tickets that are ready or done. |
| `--dir DIR` | Start looking for `.workfile/` in DIR. Also works with `test`. |

You can put options before or after ticket keys. Use `--me` or `--user`, not
both. `--gate` and `--stage` must name a gate or stage from `policy.yml`. `wf
test` takes `--dir` and help, but not the other filters.

`wf status` fetches up to the limit and then applies `--failing`. So `wf status
--failing --limit 25` checks 25 tickets. It does not promise 25 failures. To
find every failure, use `--all --failing`.

`--me` and `--user` look through your whole search and all linked PRs first, and
only then apply the limit. This takes more work, but you will not miss a ticket
just because it was not in the first 25. When you name tickets, the limit is
ignored, but the tickets must still be in your Jira search.

## Who is "me"?

By default, `--me` asks Jira and GitHub who owns your token. If you use a shared
service account, set your identity yourself in `~/.config/workfile/config.yml`:

```yaml
me:
  jira: your-atlassian-account-id
  github: your-github-login
```

A setting for a connection name beats a setting for a provider type. For
example, `github_client` beats `github` for a connection called `github_client`.
You count as involved if you wrote a PR or were asked by name to review it. Past
reviewers do not count, and neither do members of a team that was asked.

## Personal files and settings

| Setting | Default |
| --- | --- |
| Credentials | `~/.config/workfile/credentials` |
| Identity settings | `~/.config/workfile/config.yml` |
| `XDG_CONFIG_HOME` | Replaces `~/.config` for both files. |
| `WORKFILE_CREDENTIALS_FILE` | The full path to the credentials file. |
| `WORKFILE_CONFIG_FILE` | The full path to the identity settings file. |
| `NO_COLOR` | If set, turns off colours and links. |
| `WORKFILE_BACKGROUND` | Set to `off` to stop `wf status --me` asking the terminal for its background colour. |
| `COLUMNS` | Sets the display width. Handy for previews. |

The credentials file has one `NAME=value` per line. Blank lines, comments, a
leading `export`, and single or double quotes around values are all fine. Values
are read exactly as written, so nothing is expanded and no commands run. An
environment variable beats the file. On macOS and Linux, other users must not be
able to read the file, so run `chmod 600` on it. On Windows, limit access in the
file's security settings.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Health found nothing wrong. Status found only ready or done tickets. Every connection test passed. |
| `1` | Health found tickets out of policy. Status found tickets out of policy or needing work. |
| `2` | Bad options or settings, a provider read failed, a Jira status is not in your workflow, or a ticket you named is not in your search. |

If no tickets match, the report says so and exits with `0`. Reports only cover
the tickets that were checked. Tickets outside your search, your limit, your
permissions, or the GitHub lookback are not included. Press Ctrl-C to cancel
requests that are still running.
