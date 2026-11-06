# workfile

**Clear expectations for how work moves.**

Workfile reads your Jira tickets and their GitHub pull requests. It checks them
against your team's workflow and shows you what needs attention.
GitHub Issues can also be your tickets: see
[GitHub Issues as a tracker](docs/providers/github.md#github-issues-as-a-tracker).
`wf setup` only sets up Jira, so write `providers.yml` by hand for a GitHub tracker.

You write your workflow in a `.workfile/` folder. Then you run `wf` from any
folder inside your project. That folder becomes the source of truth for how
work moves, and you can put it to work: **automations** say "when this happens
to my work, do that", and only fire when your policy allows.

Workfile's checks only read. They never move a ticket, add a label, or change a
pull request. Automations run commands and agents you choose, on your own
computer, with your permission.

**New here?** Read [Getting started](docs/getting-started.md) first. It explains
the main ideas (providers, stages, transitions, gates and policies) in plain
English before you install anything.

## The commands

| Command | What it tells you |
| --- | --- |
| `wf setup` | Connect Jira and optional GitHub, and create a starter `.workfile/` folder. |
| `wf health` | How your board compares to your policy. Which gates and stages are failing, and how many tickets are affected. |
| `wf status` | Where each ticket is, and what it needs before it can move on. |
| `wf tickets` | A searchable list of your tickets. Read each one as your tracker shows it, with what Workfile knows about it. |
| `wf check` | Whether one ticket may move to a stage right now. Agents ask it before they move work. |
| `wf prove` | Whether your policy has a path that skips a gate. It reads no tickets, so it runs in CI. |
| `wf automations` | Your automations, and anything stopping them. |
| `wf watch` | Checks your work every 2 minutes and fires your automations. |
| `wf test` | Whether your tracker and code host connections work. |

Start from scratch with `wf setup`. It is a short, guided screen-by-screen setup
with a small welcome animation. It opens token pages in your browser, shows `*`
as you paste a token so you can see it landed, tests each connection, and lets you
choose a folder and a Jira project. It imports visible project people, then shows
the project's Jira statuses as a checklist: all are ticked, and you untick any your
team rarely uses. Those are left out of the Jira search. You confirm a simple
starter pipeline before saving. Existing `.workfile/` folders are never
overwritten, and tokens stay in your private credentials file. A wrong entry shows
a message on the same screen, and `esc` goes back.

The welcome screen looks like this:

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

Setup does not import Jira's full workflow graph or invent gates. Its confirmed
starter moves connect each stage to the next. Edit `policy.yml` to add the team's
checks and branches; without gates, ready does not mean your team approved the
work. See [setup details](docs/usage.md#setup-start-from-scratch).

Here is `wf health`. In a terminal it opens an interactive screen: gates and
stages on the left, and a panel on the right with the chosen gate's rules and
how many tickets fail each one. Press enter to list the tickets, grouped by the
rule they fail. When you pipe it, or set `NO_COLOR`, it prints this instead:

```text
  workfile health                                                168 tickets

  40 of 168 tickets are out of policy (24%).
  ███████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░

  Gates                                    failing of tickets that must pass

  refinement    ██░░░░░░░░░░  20 / 145   14%
    20  Description: at least 100 characters

  pull request  █░░░░░░░░░░░   7 / 101    7%
     7  At least 1 linked PR

  code review   ██░░░░░░░░░░   13 / 66   20%
     9  At least 1 approval
     4  Passing CI on the current commit

  Under each gate: tickets failing each rule. A ticket can fail more than
  one.

  Stages                                   out of policy of tickets in stage

  todo    ░░░░░░░░░░░░   0 / 23    0%
  doing   ██░░░░░░░░░░   9 / 44   20%
  review  ███░░░░░░░░░   8 / 31   26%
  done    ████░░░░░░░░  23 / 70   33%

  Next  wf health --gate code_review
```

And here is `wf status`. In a terminal it opens an interactive screen: the
tickets on the left, grouped by what to do, and the chosen ticket on the right.
Press m to see only your work. Piped, it prints the same groups:

```text
  workfile status                                                  4 tickets

  1 ticket can move on, 2 need work and 1 is out of policy.
  ‼ 1 out of policy   ✗ 2 need work   ✓ 1 ready   · 0 done

  Ready to move                                                     1 ticket

  ✓ APP-61  Show stock levels on product pages                  todo → doing

  Needs work                                      2 tickets · grouped by fix

  Link at least 1 PR                                                       1
  pull request
    ✗ APP-57  Add a delivery estimate                                  doing

  Get at least 1 approval                                                  1
  code review
    ✗ APP-42  Improve account settings         example-team/api #31 · review

  Add a label matching "*-risk"                                            1
  code review
    ✗ APP-42  Improve account settings         example-team/api #31 · review

  Out of policy                                        past a gate they fail

  ‼ APP-70  Rename loyalty tiers                                        done
      Expand the description to at least 100 characters · refinement
      Link at least 1 PR; include APP-70 in the PR title · pull request
```

Where your terminal cannot show links, such as in a pipe, each ticket's and
PR's web address is written out under it.

`wf status TICKET` also reads GitHub merge queue membership. Queued PRs show
**Queued for merge** and **Wait for merge**, rather than asking you to merge
again. Queue membership does not hide failing gates or count as merged. See the
[queued PR example](docs/usage.md#one-ticket) and
[merge queue facts](docs/providers/github.md#merge-queues).

`wf status --me` is the same, with only your work: tickets assigned to you, and
tickets with a pull request you wrote or were asked to review.

Output is drawn in panels with a soft background tint, and uses your terminal's own red and green, so it matches your theme. Ticket
keys, ticket titles, repository names and PR numbers are clickable links in most terminals. The output fits your
terminal width. When you pipe it to a file or another program, it is plain
text, with web addresses shown in every ticket and PR list. Set `NO_COLOR=1`
to turn colour off without turning terminal hyperlinks off.

Missing required evidence is **cannot check**,
not a pass or a policy violation. Use `--evidence` to see observed values and why
rules passed, failed, or were skipped. Unknown checks are shown automatically.
Use `--json` for scripts and AI agents. It includes schema version 1, a policy
hash, an observation time, stable rule IDs, evidence, available moves,
and deduplicated unmet requirements. See [JSON reports](docs/json.md).
A readiness assessment does not grant permission to merge or move work.

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

To install somewhere else, run `make install PREFIX=/usr/local`. You need
permission to write there. To build without installing:

```sh
go build -o bin/wf ./cmd/wf
./bin/wf --help
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

Use the version number you were given instead of `v0.1.0`. Nothing is published
for you. [Distribution](#distribution) explains how to make archives.

## Set up a project

1. Copy [an example policy](example/) into your project:

   ```sh
   cp -R example/.workfile /path/to/your-project/.workfile
   ```

2. Open `providers.yml` and fill in your Jira site, your Jira search, your
   status names, and your GitHub organisation. Open `people.yml` and add the
   account IDs and logins of the people your gates name. Every person in the
   examples is made up.

3. Make a private file for your credentials:

   ```sh
   mkdir -p "$HOME/.config/workfile"
   touch "$HOME/.config/workfile/credentials"
   chmod 600 "$HOME/.config/workfile/credentials"
   ```

   Open it in an editor and add these lines, using your own values:

   ```dotenv
   EXAMPLE_JIRA_EMAIL=you@example.com
   EXAMPLE_JIRA_TOKEN=your-jira-api-token
   EXAMPLE_GITHUB_TOKEN=your-github-token
   ```

   The `auth` settings in `providers.yml` use these names, written as
   `${NAME}`. An environment variable with the same name wins over the file.
   The [Jira provider guide](docs/providers/jira.md) and [GitHub provider guide](docs/providers/github.md)
   explain how to get tokens. On Windows, use `%USERPROFILE%\.config\workfile`
   and limit the file to your own account.

4. Check the connections, then look at your work:

   ```sh
   cd /path/to/your-project
   wf test
   wf health
   wf status --me
   wf status APP-42
   ```

Every command checks your policy files before it connects to anything. One
`.workfile/` folder can sit above several repositories, so they all share the
same workflow.

## Everyday use

```sh
wf health                       # the whole board, summed up by gate and stage
wf health --gate code_review    # tickets failing one gate
wf health --stage review        # tickets in one stage that break policy
wf health --user bob            # only tickets involving Bob
wf status                       # tickets at every stage, including done
wf status --me                  # your work, with every problem explained
wf status APP-42 APP-57         # the full picture for certain tickets
wf status 42                    # the same, by number (needs default_project)
wf status APP-42 --evidence     # observed evidence for every requirement
wf status APP-42 --json         # the same assessment for an agent or script
wf check APP-42 --to done       # may it move to done now? exit 0 yes, 1 no
wf test github                  # test just the connection named github
```

`wf health` checks every ticket in your search. `wf status` checks the 25 most
recently updated tickets unless you change it with `--limit` or `--all`. Read
[Using wf](docs/usage.md) for all the options.

A gate is a set of conditions that must be true before work moves forward:

```yaml
gates:
  code_review:
    - if: github.approvals >= 1
    - if: github.checks == success
    - if: github.approvals >= 1
      by: [alice]
      when: github.labels contains high-risk
```

This says every linked PR needs an approval and passing CI (continuous
integration: automated checks run for a change) on its current commit. Pending,
failed or absent checks do not pass. Unavailable CI evidence is "cannot check",
not a pass. A high-risk PR also needs an approval from `alice`. Her GitHub login
lives in `people.yml`. Both GitHub example policies include this CI requirement;
add `github.checks == success` to existing policies to enable it there.

Gates can also require a non-draft PR (`github.draft == false`) or approvals of
the current commit (`github.fresh_approvals >= 1`). Those rules remain opt-in.
The [policy guide](docs/policy.md) has more examples.

### Different requirements for each destination

A shared `requires` list applies to every forward destination. Use a map when
only one move needs a gate:

```yaml
todo:
  to: [ready, done]
  requires:
    ready: [refinement]
    done: []
```

Here, refinement blocks `todo → ready`, not `todo → done`. See the
[complete example](example/destination-requires/.workfile/).

The named-ticket view shows one forward path through the workflow, left to
right, with each stage once. With the ticket in todo, `todo → done` is ready but
`todo → ready` still needs refinement:

```text
  WHERE IT IS
  ◉╌╌╌╌╌╌╌○────────○
  todo    ready    done
  Ready to move to done.
```

`◉` is the current stage, `●` a stage behind it and `○` a stage ahead. The link
out of the current stage is solid and green when the next move is ready, and
dashed and red when it is blocked. Earlier stages are not ticked: Workfile
cannot prove the path a ticket took. The path goes through the current stage.
Where the workflow branches, such as qa or security after review, it shows the
nearest branch unless the ticket is in another one. Detours such as blocked and
extra end stages such as cancelled are left out; if the ticket is in one, the
screen says so. **Next moves** lists every place the ticket can go from here. On
a narrow terminal the gaps get smaller, then the path wraps onto more lines.

## Prove your policy

Workfile accepts a ticket's stage when **any one** forward path to it passes its
gates. So a policy is only as strong as its weakest path. Write down what must
always be true, and `wf prove` checks every path:

```yaml
# .workfile/policy.yml
invariants:
  done: [code_review, merged]
```

Here it is on the [branching example](example/branching/.workfile/), which has a
hole: the security path reaches done without code review.

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

It exits 1 when an invariant can be skipped, so run it in CI on any pull request
that changes `.workfile/`. It needs no credentials. `--json` gives agents the
same answer. See [Prove](docs/usage.md#prove-does-the-policy-say-what-you-mean).

## Automations

Automations put your policy to work. Each person writes their own, shared with
the team in `.workfile/automations/<you>.yml` or private in
`~/.config/workfile/<workspace>/automations.yml`:

```yaml
automations:
  - id: announce-done
    on:
      ready: done          # every gate for done now passes
    run: [./scripts/announce.sh, "{ticket}", "{url}"]
    mode: auto             # observe (the default), ask, or auto
```

```sh
wf automations          # what will run for you here, and what stops it
wf automations init     # start a private file for this workspace
wf watch --dry-run      # see what would fire, run nothing
wf watch                # check every 2 minutes and fire
```

A trigger is a change between two checks, so nothing fires for what was already
true. Commands run without a shell, a shared file runs only after you approve
it, and a private file runs only in the repositories it lists. Read
[Automations](docs/automations.md).

## Distribution

To build release archives from a checkout you trust:

```sh
make check
make dist VERSION=v0.1.0
```

This makes `dist/v0.1.0/`. It holds one archive each for macOS, Linux and
Windows, for both ARM64 and AMD64, plus a `SHA256SUMS` file. Each archive has the
program, this README, the docs, the examples, and the third-party notices. The
builds do not need a C runtime.

The packaging script needs a POSIX shell, `tar` and `shasum`. macOS has them.
On Windows, use WSL.

Share an archive with your team, or upload the archives and `SHA256SUMS` to your
repository's release page. To check a download, run this in the folder that has
`SHA256SUMS`:

```sh
shasum -a 256 --ignore-missing -c SHA256SUMS
```

The builds are not signed. If your team needs signing, do it in your own release
process. Nothing is published for you. Never put personal credentials in a
release archive.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
make build
```

The code has three small packages. `workfile` loads and checks policies.
`provider` reads trackers (Jira, GitHub Issues) and code hosts (GitHub). `cli` runs the commands and draws the output.
GitHub searches look for the checked tickets' keys in PR titles and
descriptions, six tickets per search, then fetch labels, reviews and readiness
evidence only for linked PRs. Up to six linked PRs are read at once.
Besides the Go standard library, it uses a YAML reader, terminal detection and
text-width libraries. It has no server, database, plugins or cache.

The tests use made-up responses, so they need no credentials. CI runs the Go
checks on macOS, Linux and Windows. Run `wf test` yourself to make sure your own
accounts and settings work.
