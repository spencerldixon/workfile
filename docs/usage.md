# Using wf

New to Workfile? Start with [Getting started](getting-started.md).

Run `wf` inside a project that has a `.workfile/` folder, or in any folder
below it. `wf` uses the nearest `.workfile/` it finds. Use `--dir` to choose
where it starts looking.

## How the output looks

`wf health` and `wf status` draw each section as a **panel**: a plain title and
a soft background, with no edge or border. Icons on the rows say how each item
is doing. Some older reports, such as `wf health --gate`, still draw **blocks**
with a coloured edge on the left: red when something in the block needs work
and green when everything is fine.

If your terminal answers when asked for its background colour, each block gets a
background a little lighter (on dark themes) or a little darker (on light
themes) than your own. Workfile asks once per run, and waits at most a fifth of a
second. If your terminal does not answer, you get the coloured edge only. Set
`WORKFILE_BACKGROUND=off` to turn the background off. Red and green are your
terminal theme's own. Piped output has no colours, terminal hyperlinks or background. Ticket and PR
lists show web addresses instead.

A report only describes current facts visible to your tokens, within the
configured Jira search. It cannot prove the actual path a
ticket took, or tell you about repositories hidden from your token.

Use `--evidence` to add one evidence block per selected ticket. It shows each
requirement once, its outcome, and the observed evidence for each ticket or PR.
Unknown checks are shown even without the flag. Missing required evidence is
"cannot check", not a pass or a policy violation. Known empty evidence, such as
zero approvals, can still fail a requirement. A known passing alternative is
not blocked merely because another route has unknown evidence.

Use `--json` for a machine-readable assessment with no terminal escapes. It
uses the same evaluator and exit codes. See [JSON reports](json.md) for the
versioned contract and an agent example.

## Setup: start from scratch

```sh
wf setup
wf setup --dir /path/to/project
```

Setup needs an interactive terminal. You do not need an existing `.workfile/`.
It runs as one screen at a time. A strip at the top shows where you are:
Connect, Discover, then Save. The keys are always shown at the bottom. `enter`
continues, `esc` goes back one screen, and Ctrl-C cancels. The welcome screen
looks like this:

```text
 workfile setup

  ╭────────────────╮
  │   ^   ^        │
  │      ◡         │
  │                │
  │  ▬▬▬▬▬▬▬▬ ✓    │
  │  ▬▬▬▬▬▬   ✓    │
  │  ▬▬▬▬▬▬▬  ✓    │
  ╰────────────────╯

   w o r k f i l e  ✦

  Welcome to workfile

  Let's connect your tickets and pull requests, then check
  them against a written workflow. Workfile only reads. It
  never changes a ticket, a label or a pull request.

  It takes about two minutes.

 enter begin · ctrl+c quit
```

The animation lasts about two seconds. Press any key to skip it. With
`NO_COLOR`, or on a very narrow terminal, you see only the finished picture.

1. **Folder.** Choose the parent folder for `.workfile/`. Setup refuses a folder
   that already has one.
2. **Jira.** Enter your Jira site and account email. Setup opens Atlassian's
   token page. Create an API token, copy it, and paste it. Each character shows
   as `*`, with a count such as "16 characters pasted", so you can see the paste
   worked. Press `ctrl+o` to open the page again. If a connection fails, you
   return to the site field with a message and your earlier answers kept.
3. **GitHub (optional).** Choose yes or no. For yes, enter your organisation and
   create a **classic** token. The link pre-selects the two scopes Workfile needs:
   `repo` and `read:org`. Workfile only reads, but classic tokens have no
   read-only choice for repositories. If your organisation uses single sign-on,
   choose Configure SSO next to the token and authorise it.
4. **Project.** Choose a visible Jira project. Type to filter a long list.
5. **Statuses.** Every status your project uses is ticked. Keep the ones tickets
   move through. Untick any your team rarely uses (`space`), and tickets in
   those statuses are left out of the Jira search and reports. Move a status
   with `shift+↑` or `shift+↓` to change the order. A live line shows the
   pipeline you are making. Keep at least one. Setup suggests the order from
   Jira's status categories, not from its configured workflow moves.
6. **Review.** Each stage moves to the next, and the last stage is terminal. Full
   Jira workflows, branches, restrictions and gates are **not** imported, and no
   approval or description rules are invented. Setup also imports active,
   visible users assignable to the project into `people.yml`, with Jira account
   IDs and display names. Aliases come from names, such as `alice`; duplicates get
   a numbered suffix. It does not guess GitHub identities. If this read is denied,
   you can still save with an empty people file.
7. **Save.** Setup checks the generated configuration, creates `policy.yml`,
   `providers.yml` and `people.yml`, and saves tokens in your personal
   credentials file with private permissions. Existing credential entries are
   kept. No secrets appear in the project files. `policy.yml` has blank lines
   between sections, in the order `tracker`, `states`, `transitions`, `gates`.
   `people.yml` has a blank line between people.

The last screen tells you to enter your folder and run `wf status`. Add checks
and any real branches to `.workfile/policy.yml` before treating readiness as a
team requirement. A starter with no gates cannot find unmet requirements.

Nothing is saved before you confirm on the review screen. `NO_COLOR` and
`TERM=dumb` disable colour. Setup does not
support report filters or `--json`. It never changes Jira or GitHub data.

## Health: how does the board compare to policy?

```sh
wf health
wf health --gate code_review
wf health --stage review
wf health APP-42
```

Plain `wf health` checks every ticket in your scope (your Jira search, or your GitHub project) and sums them up. It
starts with one sentence, such as "40 of 168 tickets are out of policy (24%)",
and a bar. Gates and stages always follow the order of your workflow.

In a terminal, `wf health` opens an interactive screen:

- On the left are your gates and stages, each with its failure rate. Move with
  ↑ and ↓.
- On the right, a panel explains the one you chose. For a gate, it shows the
  move the gate guards, every rule in it with how many tickets fail it, and
  which stages those tickets are in. For a stage, it shows which gates its
  tickets fail.
- Press enter to list the tickets. A gate's tickets are grouped by the rule
  they fail. Press esc to go back, and q to quit.
- In a terminal narrower than 60 columns, the list and the panel take turns:
  enter opens the panel and esc returns to the list.

When you pipe the output, set `NO_COLOR`, or pass `--evidence`, it prints the
same facts instead:

- **Gates**: each gate's failure rate, with the rules its tickets fail listed
  under it, one line each with the number of tickets.
- **Stages**: which stages hold tickets that break policy.
- **Next**: the command that lists the tickets for the gate failing most.

The percentages work like this:

- For a gate, it is the failing tickets divided by the tickets that must pass
  that gate. A ticket must pass a gate if it is in, or past, the stage the gate
  guards. A ticket in `todo` does not count against `code_review`.
- For a stage, it is the failing tickets divided by all tickets in that stage.

Red means failing and green means passing. These are your terminal theme's own
red and green. If you pass `--limit`, only a sample is checked, and the summary
says so. Tickets with an unmapped Jira status or unavailable required evidence
cannot be checked. The summary counts them separately, and they are left out of
the percentages. `wf status --failing` shows why.

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

Status starts with one sentence, such as "8 tickets can move on, 9 need work
and 4 are out of policy", and a count for each group with its icon. Then it
lists the tickets by what to do with them:

- **Ready to move**: tickets that can move on now, and where to.
- **Needs work**, grouped by fix: each fix once, such as "Link at least 1 PR",
  with the tickets that need it under it. A fix on a pull request names the PR.
- **Out of policy**: tickets that fail a gate they already passed, with
  everything they need.
- **Cannot check**: tickets whose evidence is missing, and why.

Done tickets are counted but not listed. `--failing` leaves out the tickets
that are ready to move.

In a terminal, `wf status` opens an interactive screen with the same groups:

- On the left are the tickets, under the same headings. A ticket that needs
  more than one fix is listed under its first. Move with ↑ and ↓.
- On the right is the chosen ticket, drawn as `wf status APP-42` draws it:
  where it is, what needs attention, its pull requests and its next moves.
  Press page down and page up when it is longer than the screen.
- Press m to show only your own work, and m again for everyone's. Press q to
  quit.
- In a terminal narrower than 70 columns, the list and the ticket take turns:
  enter opens the ticket and esc returns to the list.

When you pipe the output, set `NO_COLOR`, or pass `--evidence`, it prints the
groups instead, with web addresses written out.

The groups mean:

1. **Cannot check**: its Jira status is not in your workflow, or required evidence is unavailable.
2. **Out of policy**: a gate from an earlier stage fails.
3. **Needs work**: it is fine so far, but every move forward is blocked.
4. **Ready to move**: a move forward is open, or a valid return exists.
5. **Done**: it is finished and passes every earlier gate.

### Your own work

```sh
wf status --me
wf status --user bob
```

This is the same screen and the same groups, with only the tickets that
involve you: tickets assigned to you, and tickets with a pull request you wrote
or were asked to review. `--user bob` shows Bob's. Every ticket in your Jira
search is checked, not only the first 25, so none of your work is missed.

Without `--me`, the interactive screen's m key does the same for the tickets
already checked.

### One ticket

```sh
wf status APP-42
```

Name a ticket to see the full picture. Here is a ticket in review, piped to a
file at 72 columns:

```text
  APP-42
  https://example-team.atlassian.net/browse/APP-42
  A clear account settings page
  Bug · Unassigned

  WHERE IT IS
  ●━━━━━━━●━━━━━━━━◉╌╌╌╌╌╌╌╌╌○
  todo    doing    review    done
  Out of policy: 1 earlier requirement is not met.

  ✗ NEEDS ATTENTION
  APP-42
    1. Expand the description to at least 100 characters —
       refinement.
       needed to move to done

  example-team/api #18
    2. Get at least 1 approval — code review.
       needed to move to done
    3. Add a label matching "*-risk" — code review.
       needed to move to done

  PULL REQUESTS
  example-team/api #18  Needs review  → merge
  https://github.com/example-team/api/pull/18

  NEXT MOVES
  ✗ → done  3 requirements not met
  ✓ ↩ todo  ready now
```

- The ticket key links to Jira. The title, type and assignee follow it.
- **Where it is** shows one forward path through the workflow, with each stage
  once. `◉` is the current stage, and its name is bold and underlined when
  colour is on. `●` is a stage behind it and `○` a stage ahead. The link out of the current stage is
  solid and green when the next move is ready, and dashed and red when it is
  blocked. Earlier stages are not ticked: Workfile cannot prove the path a
  ticket took. The path goes through the current stage. Where the workflow
  branches, such as qa or security after review, it shows the nearest branch
  unless the ticket is in another one. Detours such as blocked and extra end
  stages such as cancelled are left out; if the ticket is in one, the panel says
  so. One sentence under the path says what to do about the ticket.
- Narrow terminals shrink the gaps first. If the path still does not fit, it
  wraps onto more lines, each picking up the link where the last one stopped.
- **Needs attention** lists each fix once, under the ticket or pull request it
  applies to, numbered through the whole panel. Under each fix, a dimmed line
  says which destinations it is needed for.
- Each pull request shows its repository and number, which link to the pull
  request. A short status tells you if the policy gates pass, if a review is
  missing, or if it has been merged. Queued PRs show **Queued for merge** with
  **Wait for merge**. If a queued PR has failing gates or unreadable evidence,
  those problems still show. Queue membership does not count as merged or
  satisfy other gates. If a PR was not checked, it does not claim to pass.
  GitHub's own branch rules and checks still apply on top of your policy.
- **Next moves** lists every place the ticket can go from its current stage,
  including returns, marked with ↩. A green `✓` means ready now. A red `✗`
  says how many requirements are not met.

For example, suppose QA needs a code review, and release needs the review plus a
waiver. The review appears once, and says it unblocks QA and release. The waiver
appears once, and says it unblocks release only. Workfile never does the work
for you. The lists are for you to carry out in Jira or GitHub.

If a gate requires `github.checks == success`, CI (continuous integration:
automated checks for a change) must pass on every linked PR's current commit.
The shared next-step wording is:

- Pending: "Wait for CI to finish successfully."
- Failed: "Fix the failing CI checks, then rerun them."
- No checks reported: "Run CI for this PR's current commit."

Unreadable CI evidence cannot establish readiness. The CI requirement is an
explicit policy rule; see [the GitHub guide](providers/github.md#ci-in-the-code-review-gate).

Finished tickets show any unmet requirements, or say that no next step is needed.
On narrow or wide terminals you still see the same information. Long text wraps
inside the panels.

Set `NO_COLOR` to turn off colours and keep clickable terminal hyperlinks.
When you pipe the output, or use `TERM=dumb`, there are no terminal escapes.
Every ticket and PR list shows web addresses instead. Long addresses wrap on
narrow screens. The stage markers, solid and dashed links, ticks and crosses
remain visible without colours.

## Tickets: find a ticket and read it

```sh
wf tickets
wf tickets --view me
wf tickets --view review
wf tickets --scope 'project = EXAMPLE'
```

`wf tickets` shows the tickets in your scope, with the selected ticket
underneath. It only reads: it never changes a ticket.

The screen fills your terminal. The bottom half shows the ticket's description
and comments from your tracker, and what Workfile knows: where the ticket is in
your pipeline, what is blocking it and how to fix that, its PRs, and where it can
move next. On a wide terminal these sit side by side, so what is blocking the
ticket stays in view while you read. Each comment is its own block, newest
first. On trackers that have replies, a reply's block sits indented under the
comment it answers, and replies read in the order they were written.

Tickets follow your pipeline, in the order of `states` in `policy.yml`. Within a
stage they keep your tracker's order. Tickets whose status is not mapped come
last.

The screen starts on everything. Tabs along the top run All, Assigned to me, then
each state in your workflow, in the order of `states` in `policy.yml`. `←` and `→` move between
them, and each shows how many tickets it holds. Switching is instant, because the
list is read once.

| Option | What it does |
| --- | --- |
| `--view NAME` | Start on `all`, `me` or a state in your policy. Piped output shows that view. |
| `--scope QUERY` | Use this search instead of the configured scope, written in your tracker's own query language. |

Assigned to me looks only at the assignee, not at PRs, so it needs no GitHub reads.
Tickets whose status is not mapped show only in All.

`wf tickets` reads everyone's tickets in your scope, including unassigned ones,
even with `only_mine` set. If your scope has no limit on finished work, that can
mean a lot of old tickets and a slow start. Limit it in `providers.yml`, for example
with `AND (statusCategory != Done OR resolved >= -30d)`.

The current row is highlighted. The highlight softens while you read the ticket,
and a search underlines what it matched.

| Key | What it does |
| --- | --- |
| `↑` `↓` | Move through the list, or scroll the ticket when it has focus |
| `/` | Search. Letters go to the search until you press `enter` or `esc` |
| `enter` | Read the selected ticket, or keep a search you are typing |
| `tab` | Move between the list and the ticket |
| `esc` | Clear the search, or go back to the list from the ticket |
| `←` `→` | Move between views |
| `r` | Read the selected ticket again, and retry any image that failed |
| `o` | Open the selected ticket in your browser |
| `q` | Quit |

If your terminal supports the kitty graphics protocol, titles are drawn large
and images appear inside the ticket. Workfile asks the terminal, so this works
in any terminal that has it. Otherwise titles are bold and images are shown by
name. Set `WORKFILE_GRAPHICS=off` to always use text, for example if a terminal
says it can draw images and then shows odd characters instead.

To stay kind to your providers, `wf tickets` reads the list once, with only the
fields it shows. It reads a ticket only when you stop on it, and remembers it
until you quit or press `r`.

Piped to a file, it prints a plain list:

```text

  workfile / tickets

  APP-1 Fix redirect loop on expired login
    In Review · Alice
    https://example.atlassian.net/browse/APP-1

  APP-2 Add single sign-on login
    To Do · Bob
    https://example.atlassian.net/browse/APP-2
```

## Check: may this ticket move now?

```sh
wf check APP-42 --to done
wf check 42 --to done --json
```

`wf check` reads one ticket fresh and asks your policy one question: may it move
to that stage right now? It exits `0` when the move is allowed, `1` when it is
not, and `2` when it cannot tell, for example because evidence is unavailable.
Agents and scripts should run it before they move a ticket, so they never move
work the team's gates do not allow.

```text
  APP-42
  https://example-team.atlassian.net/browse/APP-42
  Explain notification preferences
  Story · Assigned to Alice

  ✗ CANNOT MOVE TO DONE YET
  web #207
    1. Get at least 1 approval — code review.
```

When the move is allowed, the panel says `✓ CAN MOVE TO DONE`. A stage the
ticket has no move to says so, and lists the moves it does have.

With `--json` the answer is one object: `allowed`, `exit_code`, `from`, `to`,
`possible_moves`, `unmet_requirements` (the same fields as in
[JSON reports](json.md)), and `records`, each linked PR with its latest commit
in `head`. An agent can compare `head` before and after its work to make sure
nothing changed in between.

## Prove: does the policy say what you mean?

```sh
wf prove
wf prove --json
```

`wf prove` checks the policy itself, not tickets. It reads no provider, so it
needs no credentials and is quick enough for every CI run.

Workfile accepts a ticket's stage when **at least one** forward path to it
passes its gates (see [Current facts and branching paths](policy.md#current-facts-and-branching-paths)).
So a gate on one path says nothing about the others. `wf prove` answers two
questions:

- **What each stage guarantees:** the gates every forward path to the stage
  passes. A ticket in that stage is sure to have passed these, and only these.
- **Whether your invariants hold:** for each gate you list under `invariants:`
  in `policy.yml`, whether any path reaches the stage without it. If one does,
  it shows the shortest such path and, where one move would close every such
  path, which moves to add the gate to.

On the [branching example](../example/branching/.workfile/) with
`invariants: {done: [code_review, merged]}`:

```text

  WORKFILE PROVE
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

Review → security and security → release are the only moves that just lead
around code review. Requiring it on either one closes the hole. Moves that a
reviewed path also uses, such as release → done, are not suggested: that would
ask for review twice on those paths. When no single move closes every path,
`wf prove` says to require the gate on a move on the path shown, then run it
again.

With no invariants, `wf prove` still lists what each stage guarantees, and exits
`0`. Returns (moves back to an earlier stage) are left out, as they are when
Workfile checks a ticket.

With `--json` the answer is one object: `invariants`, each with `stage`, `gate`,
`holds`, and for a broken one `bypass` and `fixes` as lists of moves (`from`,
`to`, `requires`); and `guarantees`, one entry per stage in pipeline order. See
[JSON reports](json.md#prove).

## Automations: when this happens, do that

```sh
wf automations             # your automations, and anything stopping them
wf automations init        # create your private file for this workspace
wf automations trust       # approve the actions in your shared file
wf automations --all       # everyone's shared automations, read only
wf automations --log ID    # the latest run log for one automation
wf watch                   # check every 2 minutes and fire
wf watch --once            # one check, for cron or launchd
wf watch --dry-run         # show what would run, run nothing
```

Automations are written per person, either shared in
`.workfile/automations/<you>.yml` or private in
`~/.config/workfile/<workspace>/automations.yml`. A trigger fires on a change
between two checks: when a move becomes allowed (`ready`), when a ticket enters
a stage (`enters`), or when a condition turns true (`becomes`). The action is a
command with no shell, or a prompt for your agent, in `observe`, `ask` or
`auto` mode. Read [Automations](automations.md) for the file format, the safety
rules and sample output.

`wf automations` exits `1` when something stops your automations, such as a
shared file you have not approved. `wf watch` stops with `q` or Ctrl-C.

## Test: do the connections work?

```sh
wf test
wf test jira
wf test github another_github
```

Names are the connection names in `providers.yml`. With no names, every
connection is tested. If one fails, the rest still run.

For Jira, the test checks who the token belongs to and reads one ticket from your
search. It also reads label history if your gates need it.

For a GitHub Issues tracker, the test checks the project and the view.

For a GitHub code host, it checks who you are, lists the repositories you can
see, and runs a sample PR search. If your Jira search, GitHub project or GitHub
organisation is empty, the test tells you.

A pass means those reads worked. It does not prove you can see every ticket or
repository you meant to include.

## Options

| Option | What it does |
| --- | --- |
| `--limit N` | Check up to N tickets. `status` checks 25 unless you change it. `health` checks all of them unless you set this. N must be 1 or more. |
| `--all` | Check every ticket in your search, with no limit. With `automations`, list everyone's shared automations. |
| `--me` | Only tickets assigned to you, or linked to a PR you opened or were asked to review. |
| `--user NAME` | The same filter for a person in `people.yml`. |
| `--gate NAME` | Health only. List the tickets failing this gate. |
| `--stage NAME` | Health only. List the tickets in this stage that break policy. |
| `--failing` | Status only. Hide tickets that are ready or done. |
| `--json` | Health, status, check and prove. Emit a versioned JSON report for scripts and agents. |
| `--evidence` | Health and status only. Show observed evidence and pass, fail, skip or unknown outcomes. |
| `--dir DIR` | Start looking for `.workfile/` in DIR. Works with every command. |
| `--to STAGE` | Check only, and required. The stage the ticket would move to. |
| `--log ID` | Automations only. Show the latest run log for one automation. |
| `--once` | Watch only. Check once, then exit. Questions wait for a terminal. |
| `--interval 5m` | Watch only. The time between checks. The default is 2 minutes, and the least is 1. |
| `--dry-run` | Watch only. Show what would run, and run nothing. |

You can put options before or after ticket keys. Use `--me` or `--user`, not
both. `--gate` and `--stage` must name a gate or stage from `policy.yml`. `wf
test` takes `--dir` and help, but not the other filters.

`wf status` fetches up to the limit and then applies `--failing`. So `wf status
--failing --limit 25` checks 25 tickets. It does not promise 25 failures. To
find every failure, use `--all --failing`.

`--me` and `--user` look through your whole search and all linked PRs first, and
only then apply the limit. This takes more work, but you will not miss a ticket
just because it was not in the first 25. When you name tickets, the limit is
ignored, but the tickets must still be in your scope.

## Who is "me"?

By default, `--me` asks Jira and GitHub who owns your token. If you use a shared
service account, set your identity yourself in `~/.config/workfile/config.yml`:

```yaml
me:
  jira: your-atlassian-account-id
  github: your-github-login
```

The same file holds two settings for automations. `me.person` is your name in
`people.yml`, which picks your shared automations file when Workfile cannot tell
from your accounts. `agent` is the command that receives an automation's prompt
as its last argument:

```yaml
me:
  person: alice
agent: [claude, -p]
```

A setting for a connection name beats a setting for a provider type. For
example, `github_client` beats `github` for a connection called `github_client`.
You count as involved if you wrote a PR or were asked by name to review it. Past
reviewers do not count, and neither do members of a team that was asked.

## Only your tickets

To see only tickets assigned to you in a workspace, add `name:` to its
`policy.yml`, then create `~/.config/workfile/<name>/config.yml`:

```yaml
only_mine: true
```

Every listing then adds `AND assignee = currentUser()` to the Jira search, or
your `me.jira` account when you set one. Fewer tickets means fewer requests.
Tickets you name, such as `wf status APP-42`, are still shown. Board health and
other listings cover only your tickets too. The exception is `wf tickets`, which
reads everyone's and has its own Assigned to me tab.

## Personal files and settings

| Setting | Default |
| --- | --- |
| Credentials | `~/.config/workfile/credentials` |
| Identity settings | `~/.config/workfile/config.yml` |
| Private automations | `~/.config/workfile/<workspace>/automations.yml` |
| Workspace settings, such as `only_mine` | `~/.config/workfile/<workspace>/config.yml` |
| What automations saw, approvals and run logs | `~/.local/state/workfile/<workspace>-<hash>/` |
| `XDG_CONFIG_HOME` | Replaces `~/.config` for these files. |
| `XDG_STATE_HOME` | Replaces `~/.local/state`. |
| `WORKFILE_CREDENTIALS_FILE` | The full path to the credentials file. |
| `WORKFILE_CONFIG_FILE` | The full path to the identity settings file. |
| `NO_COLOR` | If set, turns off colours. Terminal hyperlinks stay on. |
| `WORKFILE_BACKGROUND` | Set to `off` to stop `wf status --me` asking the terminal for its background colour. |
| `WORKFILE_GRAPHICS` | Set to `off` to stop `wf tickets` asking the terminal about graphics, and always show titles and images as text. |
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
| `0` | Health found nothing wrong. Status found only ready or done tickets. Every connection test passed. Check found the move allowed. Prove found every invariant holds. Watch stopped normally. |
| `1` | Health found tickets out of policy. Status found tickets out of policy or needing work. Check found the move not allowed. Prove found a path that skips an invariant's gate. Automations found something stopping them. |
| `2` | Bad options or settings, a provider read failed, required evidence is unavailable, a Jira status is not in your workflow, or a ticket you named is not in your search. |

If no tickets match, the report says so and exits with `0`. Reports only cover
the tickets that were checked. Tickets outside your search, your limit, or your
permissions are not included. Press Ctrl-C to cancel
requests that are still running.

GitHub searches look for the checked tickets' keys in PR titles and
descriptions first, six tickets per search. Full labels, reviews and readiness evidence are fetched only for
PRs linked to the tickets being checked, with up to six PRs read at once. Reports still use fresh data; no results are cached between runs.
