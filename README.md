# workfile

**Your tools disagree about your work. Workfile shows what is actually true, and what happens next.**

Workflow as code. One binary. One folder in your repo. Read-only tokens.

## The problem

Your team has rules for how work should move:

- "A ticket needs a proper description before anyone starts it."
- "A high risk PR needs a review from a senior."

Those rules live in people's heads. Jira says one thing. GitHub says another.
Tickets slip through the cracks.

## What workfile does

You write the rules down in a few small files next to your code. Workfile reads
Jira and GitHub, works out where each ticket really is, and tells you what it
needs next.

It is a state machine for how work moves across your tools. It also serves as
documentation for your ways of working.

## Try it

```sh
wf setup                        # connect Jira and GitHub, pick your projects and repos
wf status --me                  # your work, and what each ticket needs
wf lint                         # is your policy sound?
```

Here is `wf status --user bob`. `--me` prints the same for you:

```text
  workfile status                                          4 tickets
  Jira · APP · repos app, api

  1 ticket can move on, 2 need work and 1 is out of policy.
  ‼ 1 out of policy   ✗ 2 need work   ✓ 1 ready   · 0 done
  Only tickets involving Bob.

  Ready to move                                             1 ticket

  ✓ APP-61  Show stock levels on product pages          todo → doing
      https://example-team.atlassian.net/browse/APP-61

  Needs work                              2 tickets · grouped by fix

  Expand the description to at least 100 characters                1
  refinement
    ✗ APP-52  Fix the search box                                todo
        https://example-team.atlassian.net/browse/APP-52

  Link at least 1 PR                                               1
  pull request
    ✗ APP-57  Add a delivery estimate                          doing
        https://example-team.atlassian.net/browse/APP-57

  Out of policy                                past a gate they fail

  ‼ APP-70  Rename loyalty tiers                                done
      https://example-team.atlassian.net/browse/APP-70
      Expand the description to at least 100 characters · refinement
      Link at least 1 PR; include APP-70 in the PR title
      pull request
```

Scripts, CI jobs and agents can ask before they move work:

```sh
wf check APP-57 --to review     # exit 0 yes, 1 no, 2 cannot tell
```

## Install

### Build it yourself

You need [Go](https://go.dev/doc/install) 1.26 or newer. In this folder, run:

```sh
make install
```

This builds `bin/wf` and copies it to `~/.local/bin/wf`. If `~/.local/bin` is
not on your PATH yet, add this line to your shell settings:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

On Windows, run `go build -o bin/wf.exe ./cmd/wf` and put `wf.exe` in a folder on
your PATH. You do not need Go to run the finished program.

### Use a release archive

If your team gives you a release archive, pick the one for your computer.
`darwin` means macOS. `arm64` means Apple Silicon. `amd64` means Intel or AMD.
On an Apple Silicon Mac:

```sh
tar -xzf workfile_v0.1.0_darwin_arm64.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 755 workfile_v0.1.0_darwin_arm64/wf "$HOME/.local/bin/wf"
wf --version
```

Use the version number you were given instead of `v0.1.0`.

## The five commands

Run `wf` with no command for help.

| Command | What it tells you |
| --- | --- |
| `wf setup` | Connects your tools and writes a starter `.workfile/` folder. `wf setup --test` checks the connections. |
| `wf status` | Where work is, and what it needs. `--me` for your work, `--since` for what changed. |
| `wf check TICKET --to STAGE` | May this ticket move now? Exit code `0` yes, `1` no, `2` cannot tell. |
| `wf lint` | Is the policy sound? Passes or fails, lists possible gaps, and draws your policy. |
| `wf tracker` | Your tracker, scoped to the team. Browse and read tickets. |

Renamed commands:

| Old name | Now |
| --- | --- |
| `wf health` | `wf status` |
| `wf test` | `wf setup --test` |
| `wf prove` | `wf lint` |
| `wf tickets` | `wf tracker` |

The old names still work for one release. They print a notice.

## Everyday use

```sh
wf status                       # the whole board, by gate and stage
wf status --me                  # your work
wf status --since 1d            # what changed since yesterday
wf status APP-42                # one ticket in full
wf status --gate code_review    # tickets failing one gate
wf check APP-42 --to done       # may it move? exit 0 yes, 1 no
wf lint                         # run this in CI when .workfile/ changes
wf setup --test                 # do my connections work?
```

All options are in [Using wf](docs/usage.md).

## What workfile never does

- It never writes to Jira or GitHub.
- It never moves a ticket, adds a label or changes a pull request.
- It never runs your commands or agents.

Workfile only reads. Tokens can be read-only. Nothing leaves your machine.

## The words you will see

- **Provider** - a tool workfile reads from, such as Jira or GitHub.
- **Stage** - a step in your workflow, such as todo, doing, review, done.
- **Move** - a valid step from one stage to another.
- **Gate** - rules that must pass before a move, such as refinement or code review.
- **Fact** - a piece of data workfile reads, such as the number of approvals.
- **Policy** - your `policy.yml`. It describes your workflow.

## Supported providers

| Role | Providers |
| --- | --- |
| Tracker (holds tickets) | Jira, GitHub Issues |
| Code host (holds PRs) | GitHub |

## Read next

- [Getting started](docs/getting-started.md): the ideas, in plain English.
- [Using wf](docs/usage.md): every command and option.
- [Writing a policy](docs/policy.md): stages, moves, gates and rules.
- [Examples](example/): policies you can copy.
- [Vision](docs/vision.md): where this is going.
