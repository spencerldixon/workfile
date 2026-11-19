# workfile

**See where your work is, what it needs, and what can happen next.**

Workfile is a Go CLI for workflow as code. Define your team's stages and rules
in `.workfile/`. Workfile reads Jira and GitHub, checks the evidence, and shows
the next action. It never changes a ticket, label or pull request.

## Start here

```sh
wf setup                        # connect tools, choose scope, apply a starter policy
wf tracker                      # browse tickets and their next actions
wf inbox                        # activity and work that needs you
wf status --me                  # your work against the policy
wf lint                         # check the policy itself
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
The finished binary does not need Go. If your team supplies a release archive,
extract it and put its `wf` binary on your PATH instead.

## Commands

| Command | Use it to |
| --- | --- |
| `wf setup` | Connect a tracker and optional code host; preview and apply a starter policy. |
| `wf setup --test [NAME ...]` | Test all connections, or just the named ones. |
| `wf tracker [--view VIEW]` | Browse tickets in a list or configured board, with descriptions, comments, PRs and next actions. |
| `wf inbox` | Follow your ticket and PR activity; clear or snooze rows locally. |
| `wf status [TICKET ...]` | Check the board, or inspect named tickets against the policy. |
| `wf check TICKET --to STAGE` | Ask whether a ticket may move now: exit `0` yes, `1` no, `2` cannot tell. |
| `wf lint` | Validate policy and show gates, possible gaps and workflow moves. No provider reads. |

```sh
wf status --me                  # assigned tickets and PRs you wrote or must review
wf status --since 1d            # compare with an earlier local snapshot
wf status --gate code_review    # tickets failing this gate
wf status APP-42                # one ticket's evidence and next moves
wf check APP-42 --to done --json # machine-readable answer for a script or agent
wf --help                       # all commands and options
wf --version
```

`status`, `check` and `lint` support `--json`. Run from your project or any
subfolder; `--dir DIR` starts the search for `.workfile/` elsewhere.

Old names still work for one release, with a notice: `health` is `status`,
`test` is `setup --test`, `prove` is `lint`, and `tickets` is `tracker`.

## Browse your work

Tracker opens a list with a ticket preview. Enter opens the reader; `w` switches
to Workfile's checks and PR actions. Use `/` to search, `s` to choose a project,
`p` to open linked PRs, and `r` to refresh. Configured board views group cards
by workflow stage and sync every two minutes while selected.

The top of a 100-column screen with `columns: [key, title, status, assignee]`,
without colour:

```text
  workfile / tracker                                                   Jira · APP · repos app, api

  [All 4]  Assigned to me   Todo 1   Doing 1   Review 1   Done 1                   Synced 11:30:00

  Key    Title                                                       Status       Assignee
▸ APP-1  Fix redirect loop on expired login                        ‼ To Do        Alice
  APP-2  Add single sign-on login                                  → In Progress  Bob
  APP-3  Clear account settings page                               → In Review    Unassigned
  APP-4  Tidy the billing screen                                   · Done         Charlie

  Ticket preview                                                                     enter to read
```

Action checks cover a sample of up to 12 active tickets, with at most two
background checks at once. Unchecked work stays unknown; press `n` to check
more. Rich descriptions, comments and images load on selection, not for the
whole board.

Inbox follows unfinished tickets assigned to you and PRs you authored or were
asked to review. Enter shows activity, `t` reads the linked ticket, `e` marks a
row done, and `s` snoozes it. These changes stay on your computer.

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
and set the inbox polling interval:

```yaml
tracker:
  default_view: me
  columns: [key, title, status, action]
  preview:
    comments: true
    workflow: auto

inbox:
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
`a` in the inbox launches the command you chose. That command or agent can
make changes with its own access; Workfile's provider checks only read.

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
