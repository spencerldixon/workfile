# workfile

**Clear expectations for how work moves.**

Workfile reads your Jira tickets and their GitHub pull requests. It checks them
against your team's workflow and shows you what needs attention.

You write your workflow in a `.workfile/` folder. Then you run `wf` from any
folder inside your project.

Workfile only reads. It never moves a ticket, adds a label, or changes a pull
request.

**New here?** Read [Getting started](docs/getting-started.md) first. It explains
the main ideas (providers, stages, transitions, gates and policies) in plain
English before you install anything.

## The three commands

| Command | What it tells you |
| --- | --- |
| `wf health` | How your board compares to your policy. Which gates and stages are failing, and how many tickets are affected. |
| `wf status` | Where each ticket is, and what it needs before it can move on. |
| `wf test` | Whether your connections to Jira and GitHub work. |

Here is `wf health`:

```text
  workfile / health

  ▌  SUMMARY
  ▌
  ▌  168 tickets checked
  ▌  ✓ in policy       121   72%  █████████████████░░░░░░░
  ▌  ✗ out of policy    47   28%  ███████░░░░░░░░░░░░░░░░░

  ▌  GATES · failing of tickets that must pass
  ▌
  ▌  1. refinement      ██░░░░░░░░░░   24 / 168   14%
  ▌  2. pull request    █░░░░░░░░░░░    7 / 145    5%
  ▌  3. code review     ██░░░░░░░░░░   13 / 101   13%
  ▌  4. release waiver  █░░░░░░░░░░░    3 / 70     4%

  ▌  STAGES · failing of tickets in stage
  ▌
  ▌  1. todo            ██░░░░░░░░░░    4 / 23    17%
  ▌  2. doing           ████░░░░░░░░   13 / 44    30%
  ▌  3. review          ███████░░░░░   18 / 31    58%
  ▌  4. done            ██░░░░░░░░░░   12 / 70    17%

  ▌  MOST COMMON REASONS
  ▌
  ▌  1. refinement      24×  Description: at least 100 characters
  ▌  2. pull request     7×  At least 1 linked PR
  ▌  3. code review     13×  At least 1 approval
  ▌  4. release waiver   3×  Labels include "release-waiver"

  Drill down: wf health --gate refinement · wf health --stage todo
```

And here is `wf status`. It shows the board stage by stage, in workflow order:

```text
  workfile / status

  ▌  SUMMARY
  ▌
  ▌  25 tickets checked · limit 25
  ▌  ‼ out of policy   4   16%  ███░░░░░░░░░░░░░░░░░
  ▌  ✗ needs work      9   36%  ███████░░░░░░░░░░░░░
  ▌  ✓ ready to move   8   32%  ██████░░░░░░░░░░░░░░
  ▌  · done            4   16%  ███░░░░░░░░░░░░░░░░░

  ▌  PIPELINE
  ▌
  ▌  1. todo    ✓ 3  ✗ 1  ███░░░░░░░░░   4 tickets
  ▌  2. doing   ✓ 2  ✗ 6  █████████░░░  10 tickets
  ▌  3. review  ✓ 3  ✗ 4  ███████░░░░░   7 tickets
  ▌  4. done    ✓ 4  ✗ 0  ░░░░░░░░░░░░   4 tickets

  ▌  DOING · 10 tickets
  ▌
  ▌  ✗ APP-57  Add a delivery estimate
  ▌      pull request: needs at least 1 linked PR; has 0
  ▌
  ▌  ✓ APP-71  Explain notification preferences              → review

  ▌  REVIEW · 7 tickets
  ▌
  ▌  ‼ APP-42  Improve account settings               OUT OF POLICY
  ▌      refinement: description is 38 characters; needs at least 100
  ▌      pull request: needs at least 1 linked PR; has 0
```

`wf status --me` is a kanban board of your unfinished work. Each stage gets a
lane. Under each ticket assigned to you, it lists the requirements you still
have to meet to move it forward. Pull requests you wrote or were asked to review
get a smaller lane of their own.

Output is drawn in blocks with a coloured edge and a soft background tint, and uses your terminal's own red and green, so it matches your theme. Ticket
and PR names are clickable links in most terminals. The output fits your
terminal width. When you pipe it to a file or another program, it is plain
text. Set `NO_COLOR=1` to turn colour off. There is no JSON output.

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
    - if: github.approvals >= 1
      by: [alice]
      when: github.labels contains high-risk
```

This says every linked PR needs an approval. A high-risk PR also needs an
approval from `alice`. Her GitHub login lives in `people.yml`. The
[policy guide](docs/policy.md) has more examples.

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
`provider` reads Jira and GitHub. `cli` runs the commands and draws the output.
Besides the Go standard library, it uses a YAML reader, terminal detection and
text-width libraries. It has no server, database, plugins or cache.

The tests use made-up responses, so they need no credentials. CI runs the Go
checks on macOS, Linux and Windows. Run `wf test` yourself to make sure your own
accounts and settings work.
