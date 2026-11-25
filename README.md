# workfile

**See where your work is, what it needs, and what can happen next.**

Workfile is a Go CLI for workflow as code. Define your team's stages and rules
in `.workfile/`. Workfile reads Jira and GitHub, checks the evidence, and shows
the next action. It never changes a ticket, label or pull request.

## Start here

```sh
wf setup                        # connect tools, choose scope, apply a starter policy
wf tracker                      # browse tickets and their next actions
wf tracker --view inbox         # live policy and PR alerts
wf status --me                  # your work against the policy
wf policy                       # inspect the workflow
wf policy --lint                # check the policy itself
```

Setup stores tokens privately, outside your repo. Its starter policy has stages
and moves but no gates. Add your team's requirements to `policy.yml`.
See [Getting started](docs/getting-started.md) for a small policy and the core ideas.

## Install

You need [Go](https://go.dev/doc/install) 1.26 or newer to build:

```sh
make install
```

This builds `bin/wf` and installs `~/.local/bin/wf`. Add that folder to your PATH
if needed:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

On Windows, run `go build -o bin/wf.exe ./cmd/wf` and put `wf.exe` on your PATH.
The finished binary runs without Go. You can also download an archive for your
platform from [GitHub Releases](https://github.com/spencerldixon/workfile/releases),
extract it, and put `wf` or `wf.exe` on your PATH.

## Commands

| Command | Use it to |
| --- | --- |
| `wf setup` | Connect a tracker and optional code host; preview and apply a starter policy. |
| `wf setup --test [NAME ...]` | Test all connections, or just the named ones. |
| `wf tracker [--view VIEW]` | Browse tickets in a list or configured board, with descriptions, comments, PRs and next actions. |
| `wf tracker --view inbox` | See your next actions and PR alerts. Press `s` to switch the Inbox scope. |
| `wf status [TICKET ...]` | Check the board, or inspect named tickets against the policy. |
| `wf check TICKET --to STAGE` | Ask whether a ticket may move now: exit `0` yes, `1` no, `2` cannot tell. |
| `wf policy` | Show workflow stages, moves, gates and provider mappings offline. |
| `wf policy --lint` | Validate policy and report warnings and possible gaps. |

```sh
wf status --me                  # assigned tickets and PRs you wrote or must review
wf status --since 1d            # compare with an earlier local snapshot
wf status --gate code_review    # tickets failing this gate
wf status APP-42                # one ticket's evidence and next moves
wf check APP-42 --to done --json # machine-readable answer for a script or agent
wf policy --json                # workflow and provider scope for an agent
wf --help                       # all commands and options
wf --version
```

`status`, `check` and `policy` support `--json`. Run from your project or any
subfolder; `--dir DIR` starts the search for `.workfile/` elsewhere.

These older names still work, with a notice: `health` is `status`,
`test` is `setup --test`, and `tickets` is `tracker`.

## Browse your work

`wf status APP-42` shows linked PRs, failing checks, and open review threads.
Use the ticket key in the PR title or link to the ticket in the description.
When you look up a ticket directly, Workfile also searches branch names that
start with its key. See [how Workfile links PRs](docs/providers/github.md#how-prs-are-found).

Tracker opens a list with a ticket preview. Enter opens the reader with the ticket key and title beneath the view tabs.
Scroll over either pane, or press `w` and use the arrow keys to read through the linked PRs. Use `/` to search, `s` to choose a project,
`p` to open linked PRs, and `r` to refresh. Configured board views group cards
by workflow stage and sync every two minutes while selected.
Ticket panes have a one-column inset on both sides. On wide screens, this gives
the vertical divider one column of space on each side.
A blank line separates the content from the divider above the command hints.

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

Action checks cover a sample of up to 12 active tickets, with at most two
background checks at once. Unchecked work stays unknown; press `n` to check
more. Rich descriptions, comments and images load on selection, not for the
whole board.

Tracker’s Inbox follows your assigned tickets and authored PRs, including PRs
without a ticket. It links PRs by the ticket key in the title or branch, or by a
ticket link in the description. Review requests and watched repos are opt-in.

Each alert shows its title and next action. Inbox starts in **Needs attention**.
Press `s` to choose **All signals**, `t` to read the ticket, or `o` to open the PR
or comment thread. Resolve an alert with `e` or snooze it with `z`. See
[Inbox configuration](docs/configuration.md#inbox) to set the default scope.

Piped output, `NO_COLOR` or `TERM=dumb` use noninteractive text. Images have text
fallbacks. A Nerd Font is recommended for interactive icons.
See [Using wf](docs/usage.md) for keys, refresh behaviour and output details.

## Configuration

```text
.workfile/
  policy.yml       # stages, moves and gates
  providers.yml    # connections, scope, status mappings and credential references
  people.yml       # optional names and provider accounts
  config.yml       # optional tracker and inbox preferences
```

For example, `.workfile/config.yml` can start tracker on your assigned tickets
and set the Inbox scope and polling interval:

```yaml
tracker:
  default_view: me
  columns: [key, title, status, action]
  preview:
    comments: true
    workflow: auto

inbox:
  default_scope: needs_attention
  poll: 2m
  refresh_every: 5
  full_every: 15
  snooze: 1h
```

No `config.yml` is required. Preferences inherit in this order, highest first:

1. Explicit command-line flags.
2. `~/.config/workfile/<workspace>/config.yml` - private workspace overrides.
3. `.workfile/config.yml` - shared repo preferences.
4. `~/.config/workfile/config.yml` - personal defaults.
5. Built-in defaults.

`<workspace>` is `name` in `policy.yml`. Credentials and identity overrides
stay private. An optional `inbox.agent` command must also be private: pressing
`a` previews the selected signal’s configured prompt and Workfile context. Enter
launches your agent. The agent can make changes with its own access. Workfile
reads provider data.

See the [configuration reference](docs/configuration.md) for every setting,
board views, identity, file locations and environment variables. A copyable
[config file](example/.workfile/config.yml) sits beside the example policy.

## Providers and boundaries

| Role | Providers |
| --- | --- |
| Tracker (tickets) | Jira, GitHub Issues |
| Code host (pull requests) | GitHub |

Provider scope comes from `providers.yml`. Views can narrow it, never expand it.
Reports cover only the work and evidence your tokens can read. Workfile does not
write to Jira or GitHub. It writes local preferences, snapshots and inbox state;
setup also writes the workspace files and private credentials you approve.

## Read next

- [Getting started](docs/getting-started.md) - setup and a small policy.
- [Using wf](docs/usage.md) - commands, keys and exit codes.
- [Configuration](docs/configuration.md) - shared and personal settings.
- [Writing a policy](docs/policy.md) - stages, moves, gates and rules.
- [Examples](example/) - policies and config you can copy.
- [Jira](docs/providers/jira.md) and [GitHub](docs/providers/github.md) - tokens, scope and facts.
- [Vision](docs/vision.md) - future direction, not a command reference.
