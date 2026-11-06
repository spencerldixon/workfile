# Provider standard design

**Status:** draft, not built. The contract a provider must meet is in
[Building a provider](../../providers/building-a-provider.md). This file says why,
and how the code gets there.

## Why

Workfile should read work from any tracker. Today only reading tickets is
generic. Adding a provider means finding about eight places that branch on
`jira` or `github`, and the compiler flags none of them. The comment in
`tracker.go` says an adapter and one line are enough, but `config.go` rejects
the new kind first.

The root problem: the code treats **kind** as **role**. `jira` means tracker and
`github` means code host. Those are two separate ideas, and GitHub can be both.

## Goals

- A new tracker or code host is one adapter file, one row in a table, and one test
  fixture.
- Forget a required method and the code does not compile. Break a rule and the
  shared test fails.
- GitHub Issues works as a tracker, and proves the standard.
- Every `providers.yml` and `policy.yml` that loads today still loads and behaves
  the same.

## Non-goals

- **No writes.** Adapters only read, as `AGENTS.md` requires.
- No plugins. Providers are compiled in.
- No second tracker in one workspace.
- No new code host. The `codeHost` standard is complete, but GitHub stays the only
  one for now.
- No third provider type. Slack and others are left out for now. Automations can
  already notify through commands a person chooses.
- `wf setup` stays Jira only. Other trackers are written by hand in
  `providers.yml`, until a later piece of work. (My call.)
- `Provider` stays one flat struct, because the YAML is strict
  (`KnownFields(true)`) and existing files must load. (My call.)

## Anti-goals

It has failed, even if it works, if:

- a Jira workspace makes more requests than it does today
- a new provider still needs a change in `internal/cli`
- the standard is so abstract that the adapter is harder to read than `jira.go`

## Concepts

- **Connection:** one entry in `providers.yml`.
- **Kind:** the service it talks to (`jira`, `github`).
- **Role:** the job it does. The connection named by `policy.tracker` is the
  **tracker**. Every other connection is a **code host**.

A role comes from the policy, never from the kind. This keeps the routing in
`evaluate.go` and `evidence.go`, which already test
`e.Provider == w.Policy.Tracker`.

A team using GitHub for both jobs has two connections:

```yaml
issues:
  provider: github
  org: example-team
  project: 7
  statuses: { todo: Backlog, doing: In progress, done: Done }
  auth: { token: "${EXAMPLE_GITHUB_TOKEN}" }
github:
  org: example-team
  auth: { token: "${EXAMPLE_GITHUB_TOKEN}" }
```

Two connections are the simple choice: `issues.labels` and `github.labels` stay
distinct, and `github.approvals` means what it means today. (My call.)

## Design

### Kind table, in `internal/workfile/kinds.go`

It lives in `workfile` because `provider` imports `workfile`, and config checks
and expression parsing need it with no network. The shape is in the guide.

It replaces:

| Today | Becomes |
|---|---|
| `config.go:223-269` switch on kind | loop over `Role.Settings`, `Kind.Auth`, `Role.Check` |
| `config.go:283` tracker must be Jira | the kind must have a `Tracker` role |
| `config.go:335` `by` rule | `ByFacts` of the connection's role |
| `expression.go:129-131` fact lists | `Facts` of the connection's role |
| `expression.go:139` `kind == "github"` | role is code host |
| "Jira settings on a GitHub instance" errors | one generic error naming the setting |

`Expression.kind` becomes a role. A connection whose kind lacks the role it was
given is rejected, which keeps today's rule that a Jira connection must be the
tracker. The shared tracker check (`statuses` is present
and sound) runs for any tracker. `Role.Check` holds the rest, such as Jira needing
`scope` and GitHub needing `project`.

### Adapters, in `internal/provider`

`connection`, `tracker`, `codeHost` and the facts each must supply are defined in
the guide. Registration is
two maps, `trackers` and `codeHosts`, from kind to constructor.

- `Client.Identity` and `Client.Test` look up the connection's adapter and call
  it. Both `if p.Kind == "jira"` branches go. An unknown kind is an error, not a
  fall through to GitHub.
- `Client.Records` loops over code hosts through the interface, and passes them
  the tracker's `Keys`, so a code host never reads tracker settings.
- `githubRecords` becomes `githubCodeHost.Records`. It stops reading the tracker's
  `Site` (`github.go:298`) and asks `Keys` instead.
- Adapters carry their connection name, and stop reading `Policy.Tracker`.
- `Image` shares a new `Client.download`, which holds the 16 MB limit and error
  wording now inside `jiraTracker.Image`.
- `MyJiraAssignee` moves out of `workfile` into the Jira adapter, because it
  returns JQL. `only_mine` is the adapter's job.

### Ticket keys

Keys stop being assumed to look like `APP-42`.

- `Keys` (in the guide) is per kind, and pure.
- `linkedKeys`, `explicitKeys` and the regular expressions in `cli/keys.go` and
  `github.go:242` move into `workfile` as a shared `dashKeys`. Jira uses it, and
  so can any tracker with `APP-42` keys.
- `ticketArgs` runs before the workspace loads, so it cannot know the tracker.
  Key checks move to the `completeKeys` step in `cli.go:186`, which runs after
  load. A bad key now reports after a bad workspace, not before.
- `Client.Records` (`client.go:217`) and `githubRecords` (`github.go:270`) stop
  uppercasing keys. `Keys.Parse` decides the one true form.
- `default_project` moves from a shared pattern to `Role.Check`.
- `Ticket` gains `LinkedPRs`, for trackers that know their PRs. It is empty for
  Jira, so Jira keeps searching by key.

### Wording

`Kind.Label` feeds text that names a tracker: `assess.go:57`, `check.go:45`,
`status_view.go:230`, `details.go:110`, `ticket_view.go:325`. The note in
`automation/run.go:91` becomes generic: "comes from your tracker or code host".
Comments and `tracker: must name a configured ...` wording change to match.

## GitHub tracker

Built last, using only the standard. If it needs a change in `internal/cli`, the
standard is wrong: fix the standard, not the adapter.

### What a real board taught us

We read a real team's GitHub Project through the API: 768 items, 519 of them
archived. It shaped these decisions.

- Workflow state lived in the Project's `Status` field, a single choice with nine
  options. An older repository at the same team used labels as statuses, then stopped.
  Labels stayed for exceptions: `blocked`, `failed-qa`, `changes-requested`.
- **The issue's open or closed state does not follow the board.** 33 live items
  were in `Done` but still open. 99 archived items were closed but still in
  `On Staging, Ready for QA`.
- Finished work leaves the board by archiving. All 249 live items were open issues.
- Every status had an emoji (`🕰 Backlog`).
- Saved views are one board with different filters, such as `label:blocked` and
  `sprint:@current`.
- People wrote `Resolves #42` in a PR, and `#42` when talking about an issue.
- GitHub's closing link was exact where text would not be. In three samples each
  issue had one merged closing PR, and two also had other PRs that did not count.
  One of those was closed without merging.
- Closing links include open PRs, drafts too: all 4 open PRs with a link showed up
  from the issue's side. Closed PRs that never merged are left out unless
  `includeClosedPrs` is set, which is what we want.

### Decisions

- **Status** comes from the Project's `Status` field, and never from the issue's
  open or closed state. The field name is fixed. (My call.)
- **Settings:** `org`, `project` (a number), optional `view` (a number), `statuses`,
  `default_project`. There is no `scope` setting. Only organisation projects are
  supported. `Provider` gains `project` and `view`.
- **Scope:** the live items of the project, narrowed by the view's filter if `view`
  is set. A view with an empty or null filter means the whole project. A `Listing`
  scope is filter text. One paged read of `projectV2.items(query:)` gives status,
  title, assignees and labels for every ticket. `only_mine` adds `assignee:@me`,
  so it needs no later filtering. (Whole project by default is my call.)
- **Matching statuses:** compare without case and without leading symbols, so
  `done: Done` matches `☠️ Done`. Emoji text is hard to type exactly, as some emoji
  hold invisible characters. Filter text does not get this help: GitHub needs
  `status:"🕰 Backlog"`, and `status:Backlog` finds nothing. (My call.)
- **Tickets by key:** read `repository.issue(number).projectItems`, and pick this
  project's item. Batch several issues with aliases. Archived items are returned
  here, so an archived ticket keeps its status. Set `OutOfScope` when the item is
  archived, or when `view` is set and a filtered read does not include it.
- **Issue not on the project:** returned with `Error`, as an unmapped Jira status is.
- **Items with no issue:** drafts and PRs on the board are skipped. The real board
  had 7, all archived.
- **Key:** `app#42`, repository then number, in lower case. With
  `default_project: app`, `42` and `#42` both work.
- **Facts:** `summary` title, `description` and `headings` from the Markdown body,
  `labels`, `assignee` first assignee, `project` repository, `type` the issue type
  or unavailable. Label `by` rules are not in the first version. Priority, Size and
  other Project fields are not facts in the first version. (My call.)
- **Rich text:** a small Markdown to `Block` converter in `github_document.go`.
- **Images:** image addresses in the body go in `Attachment`. `Image` must accept
  only GitHub's own hosts, so issue text cannot make Workfile send a token elsewhere.
- **Linking PRs:** `closedByPullRequestsReferences`, with its default of leaving
  out closed PRs that never merged, read in the same query as the ticket, fills `Ticket.LinkedPRs`. The code host reads those PRs directly, with no
  search. Text linking stays as a fallback, for the full `app#42` key in a PR title
  or branch, or an issue address in the body. A bare `#42` is not searched, because
  it matches too loosely. A PR that only mentions an issue does not link. (My call.)
- **Token:** classic token with `repo`, `read:org` and `read:project`. Without
  `read:project` GitHub refuses the read, so `wf test` must say which scope is
  missing.

### Not yet proven

- `sprint:@current` was accepted, but found nothing on an idle board. Test it on a
  project with a running sprint.
- The archived item rule rests on three issues. `includeArchived` made no
  difference to them. Test it in the adapter's first real run.
- Why 3 of those 4 open PRs linked with no closing word in their body. They may
  be linked by hand in GitHub, or use wording our check missed. Either way they
  appear, so this changes nothing in the adapter.

## Code host facts

GitHub's ten PR facts become two groups, so a second code host knows what it owes:

- **Standard:** `state`, `draft`, `checks`, `approvals`, `author`,
  `review_requests`, `labels`. Their values are fixed by the guide. They match what
  `github.go` writes today, so no policy changes.
- **Optional:** `fresh_approvals`, `merge_queue`. A kind lists them in `Role.Facts`
  only if it can supply them. A policy using one on a kind without it fails at load,
  not at run time.
- **Worked out by Workfile:** `prs`, a count of records. `evaluate.go` already does
  this.

`internal/cli/details.go` reads `merge_queue` directly. A missing value already
means "not queued" there, so it needs no change. (My call to keep it.)

`Record.ID` stays `repo#42`, as `details.go:237` splits it at `#`.

## Build order

Each step leaves all tests passing. Until step 5, the only changes people see are
error wording and the order of the bad key error.

1. Kind table, and move config and expression code onto it.
2. `connection` and `codeHost` interfaces. Move `Identity`, `Test`, `Records`.
3. `Keys`. Move key checks after load.
4. Wording, `Client.download`, `conformTracker` on Jira, `conformCodeHost` on
   GitHub, `Ticket.LinkedPRs` in the code host.
5. GitHub tracker, its docs in `docs/providers/github.md`, and README and
   `docs/policy.md` updates (`tracker` no longer means Jira).

## How we know it worked

- `go vet ./... && go test -race ./...` pass at every step.
- `rg 'Kind == "(jira|github)"' internal` finds nothing outside the kind table
  and registration.
- `internal/provider/github_speed_test.go` and Jira request counts do not rise.
- `conformTracker` passes for `jira` and `github`, and `conformCodeHost` for
  `github`.
- The GitHub tracker changes no file in `internal/cli`.

## Open questions

None. The calls marked above are open to change.
