# Provider Standard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make tracker and code host providers standard and extensible, then prove it by adding GitHub Issues as a tracker.

**Architecture:** A kind table in `internal/workfile` replaces every branch on `jira` or `github`. Role comes from the policy: the connection named by `policy.tracker` is the tracker, all others are code hosts. Adapters in `internal/provider` sit behind `connection`, `tracker` and `codeHost`, while `Client`'s public methods keep their signatures, so `internal/cli` and its fakes barely change.

**Tech Stack:** Go, `go test -race`, GitHub GraphQL, no new dependencies.

**Spec:** `docs/superpowers/specs/2026-11-06-provider-standard-design.md`. The contract is `docs/providers/building-a-provider.md`. Read both.

## Global Constraints

- Never add a Jira or GitHub write. POST is for a search or GraphQL query only. No mutations.
- `Client.Tickets`, `Listing`, `Detail`, `Image`, `Identity(ctx, name)`, `Test(ctx, name)` and `Records(ctx, tickets) ([]Ticket, []string, error)` keep their signatures. The `source` interface in `internal/cli/cli.go` does not change.
- Every `providers.yml` and `policy.yml` that loads today still loads and behaves the same.
- A Jira workspace makes no more requests than today. `internal/provider/github_speed_test.go` stays green unchanged.
- Tests use made-up responses, with Alice, Bob, Charlie, `example-*` and `example.com`. No real names.
- Never use em dashes. Never co-author a commit. Never edit a CHANGELOG.
- Run before each commit: `eval "$(mise env -s bash)" && go vet ./... && go test -race ./...`

## Review Focus

- An issue on two projects, or on this project with no `Status`: read this project's item only, and return `Error` when there is no status. Test in Task 8.
- More than 100 project items: all pages read, and a repeated cursor stops with an error and does not loop. Test in Task 8.
- A token without `read:project`: `wf test` names the missing scope, with no raw GitHub text. Test in Task 8.
- An image address in issue text on a lookalike host (`https://github.com.example.com/a.png`): refused before any request, so no token leaves. Test in Task 9.
- An old `providers.yml` with `lookback` on GitHub, and connections with no `provider:` line: still load. Test in Task 1.

---

## Before Task 1

Two things must happen first. Both are the owner's call, so stop and ask if they are not done.

1. `main` has about 55 uncommitted changes, and this plan depends on them (`internal/provider/tracker.go` is untracked). Commit them, so a branch holds the baseline.
2. `wf-watch-json` has 14 commits touching `client.go`, `github.go`, `jira.go` and `evidence.go`. Merge it before starting, or expect conflicts in Tasks 4 and 5.

### Task 1: Kind table and config checks

**Files:**
- Create: `internal/workfile/kinds.go`, `internal/workfile/kinds_test.go`
- Modify: `internal/workfile/config.go:219-285`, `internal/workfile/model.go:82-94`

**Interfaces:**
- Produces:
  - `type Kind struct { Label string; Auth []string; Keys func(Provider) Keys; Tracker, CodeHost *Role }`
  - `type Role struct { Settings []string; Check func(name string, p Provider, add func(string, ...any)); Facts, ByFacts []string }`
  - `type Keys interface` as in the guide, with `Mentions(text string) []string`. Task 3 fills `Keys` in the table.
  - `var Kinds map[string]Kind` with rows `jira` and `github`. GitHub has `CodeHost` only for now.
  - `var TrackerFacts, CodeHostFacts []string`, values from the guide.
  - `func (w *Workspace) RoleOf(name string) *Role`: the connection's role, nil if its kind lacks it.
  - `func (w *Workspace) IsTracker(name string) bool`
  - `func (w *Workspace) TrackerLabel() string`
  - `Provider` gains `Project int yaml:"project"` and `View int yaml:"view"`.

- [ ] **Step 1: Write failing tests** in `kinds_test.go`, using `example(t)` from `workfile_test.go`:
  - `TestUnknownKindIsRejected`: `provider: example` gives `must use a known provider`.
  - `TestSettingOutsideTheRoleIsRejected`: `site` on the GitHub connection gives an error naming `github` and `site`.
  - `TestJiraMustBeTheTracker`: a second Jira connection is rejected, as today.
  - `TestKindMustBeAbleToBeTheTracker`: `tracker:` naming a connection whose kind has no `Tracker` is rejected.
  - `TestOldProvidersFilesStillLoad`: `lookback` on GitHub and no `provider:` line both load.
  - `TestEveryRoleListsTheStandardFacts`: each row's `Facts` contains all of `TrackerFacts` or `CodeHostFacts`.
- [ ] **Step 2: Run** `go test ./internal/workfile -run 'Kind|Setting|Jira|OldProviders|EveryRole'`. Expected: FAIL, undefined `Kinds`.
- [ ] **Step 3: Implement.** Replace the `switch provider.Kind` with: look up the kind, get the role, reject settings not in `Role.Settings`, run `Role.Check`, loop `Kind.Auth`. `func (p Provider) settings() []string` lists the names of non-empty settings. GitHub's code host `Settings` keeps `org` and `lookback`. Move the Jira `site`, `scope`, `default_project` and `statuses` body into `checkJira`, and `org` into `checkGitHub`. The check that every policy state has a status runs for any tracker. Keep `w.Providers[name] = provider`.
- [ ] **Step 4: Run** the full check. Expected: PASS.
- [ ] **Step 5: Commit** `feat(workfile): add kind table and role based config checks`

### Task 2: Facts by role

**Files:**
- Modify: `internal/workfile/expression.go:119-139`, `internal/workfile/config.go:333-338`
- Test: `internal/workfile/kinds_test.go`

**Interfaces:**
- Consumes: `RoleOf`, `IsTracker`.
- Produces: `Expression.Record()` unchanged in name and meaning. Private field `kind` becomes `record bool`, set when the connection is a code host and the fact is not `prs`.

- [ ] **Step 1: Write failing tests:**
  - `TestFactsFollowTheRole`: with a test row whose kind has both roles, `issues.summary` parses on the tracker connection, and `issues.approvals` fails with `has no attribute`.
  - `TestByFollowsTheRole`: `by` on `github.approvals` and `jira.labels` loads, and on `github.checks` it fails.
  - `TestOptionalFactNeedsTheKind`: a test code host without `merge_queue` in `Facts` fails at load.
- [ ] **Step 2: Run** them. Expected: FAIL.
- [ ] **Step 3: Implement.** `prs` is valid on any code host. `by` checks `RoleOf(expr.Provider).ByFacts`, and its error becomes `by is not supported for %s.%s`. Jira `ByFacts` is `labels`. GitHub `ByFacts` is `approvals`, `fresh_approvals`.
- [ ] **Step 4: Run** the full check. Expected: PASS.
- [ ] **Step 5: Commit** `feat(workfile): choose facts by role`

### Task 3: Ticket keys

**Files:**
- Create: `internal/workfile/keys.go`, `internal/workfile/keys_test.go`
- Modify: `internal/cli/keys.go`, `internal/cli/keys_test.go`, `internal/cli/cli.go:186-190`, `internal/workfile/kinds.go`
- Delete: `internal/provider/links.go`, and `linkedKeys` and `ticketKey` from `github.go`. Move their tests from `readiness_test.go`.

**Interfaces:**
- Produces:
  - `type dashKeys struct{ site, path string }` implementing `Keys`. Jira's row uses `path: "/browse/"`.
  - `func (w *Workspace) TrackerKeys() Keys`
  - `func completeKeys(input []string, keys workfile.Keys, project string) ([]string, error)`

- [ ] **Step 1: Write failing tests:**
  - `TestDashKeysParse`: `app-5` gives `APP-5`, `02713` with project `WEB` gives `WEB-2713`, `0` gives `WEB-0`. With no project, the error holds `set default_project in providers.yml`. `app 5` gives `is not a ticket key`.
  - `TestDashKeysMentions`: the existing `linkedKeys` cases, including no match for `MYAPP-12` when seeking `APP-12`.
  - `TestDashKeysLinks`: the existing `explicitKeys` cases.
  - `TestDashKeysSearch`: `APP-1` gives `APP-1`.
  - Update `keys_test.go` to the new `completeKeys`, repeats still dropped.
- [ ] **Step 2: Run** them. Expected: FAIL.
- [ ] **Step 3: Implement.** Keep today's error wording exactly. `completeKeys` calls `Keys.Parse` and drops repeats. Remove `ticketArgs` and its `args:` uses. Commands with `ticketKeys: true` now check keys in `cli.go:186`, after load.
- [ ] **Step 4: Run** the full check. A test expecting a bad key error with no workspace must use a loaded workspace now. Change only those. Expected: PASS.
- [ ] **Step 5: Commit** `refactor: move ticket key rules behind Keys`

### Task 4: Connection interface and registry

**Files:**
- Modify: `internal/provider/tracker.go`, `client.go:134-199`, `jira.go`, `jira_detail.go`, `setup.go`, `github.go`
- Modify: `internal/workfile/credentials.go:57-71`, `workspace_settings_test.go:45-60`
- Create: `internal/provider/connection.go`, `internal/provider/github_host.go`

**Interfaces:**
- Produces:
  - `type connection interface { Identity(ctx) (string, error); Test(ctx) (string, error) }`
  - `tracker` embeds `connection`.
  - `type codeHost interface { connection; Records(ctx context.Context, tickets []workfile.Ticket, keys workfile.Keys) (map[string][]workfile.Record, error) }`
  - `var trackers map[string]func(*Client, string) tracker`, `var codeHosts map[string]func(*Client, string) codeHost`
  - `func (c *Client) connection(name string) (connection, error)`: tracker or code host by role. An unknown kind or role is an error.
  - `type jiraTracker struct{ *Client; name string }`, `type githubHost struct{ *Client; name string }`
  - `func (c *Client) download(ctx context.Context, address, auth string) ([]byte, error)`, with the 16 MB limit and error text now in `jiraTracker.Image`.
  - `func (c jiraTracker) assignee() (string, error)`, replacing `Workspace.MyJiraAssignee`.

- [ ] **Step 1: Write failing tests** in `provider_test.go`:
  - `TestUnknownKindDoesNotFallThroughToGitHub`: `c.Identity(ctx, "missing")` returns an error, and the handler sees no request.
  - `TestDownloadRefusesLargeImages`: 16 MB + 1 byte gives `image exceeds 16 MB`.
  - Move `TestMyJiraAssigneePrefersTheConfiguredAccount` to the provider package, against `assignee()`.
- [ ] **Step 2: Run** them. Expected: FAIL.
- [ ] **Step 3: Implement.** Move the Jira branch of `Identity` and `Test` onto `jiraTracker`, and the GitHub branch onto `githubHost`. `Client.Identity` and `Client.Test` become lookups. Adapters use their `name`, never `Policy.Tracker`. `githubRecords` becomes `githubHost.Records`, and `Client.Records` calls it through `codeHost`, passing `c.Workspace.TrackerKeys()`. `Client.Records` uses `tickets[i].Key` as given, with no upper-casing.
- [ ] **Step 4: Run** the full check, and `rg 'Kind == "(jira|github)"' internal`. Expected: PASS, and no matches outside `kinds.go`.
- [ ] **Step 5: Commit** `refactor(provider): put adapters behind connection and codeHost`

### Task 5: Code host finds work through Keys and LinkedPRs

**Files:**
- Modify: `internal/provider/github_host.go`, `internal/workfile/model.go:116-125`
- Test: `internal/provider/provider_test.go`

**Interfaces:**
- Consumes: `workfile.Keys`, `githubHost.Records`.
- Produces: `Ticket.LinkedPRs []string`, each `org/repo#42`. `func (c githubHost) readRef(ctx, token, ref string) (pullRequest, error)`.

- [ ] **Step 1: Write failing tests:**
  - `TestLinkedPRsAreReadWithNoSearch`: a ticket with `LinkedPRs: {"example-team/app#7"}` gives one record, and no request contains `search(`.
  - `TestTicketsWithoutLinksStillSearch`: one ticket of each kind gives one search, naming only the unlinked key.
  - `TestLinkFromAnotherOrgIsIgnored`: `other-org/app#7` makes no request.
  - `TestChangeLinkedToTwoTicketsAppearsUnderBoth`.
- [ ] **Step 2: Run** them. Expected: FAIL.
- [ ] **Step 3: Implement** the three steps under "Finding work" in the guide. `keySearches` takes `keys.Search(key)` results. Filter with `keys.Mentions(title + " " + branch)` and `keys.Links(body)`. `readRef` reuses `readPR`'s fields, with `repository(owner, name).pullRequest(number)`. Stop `strings.ToUpper` at `github.go:270`, and compare keys as returned.
- [ ] **Step 4: Run** the full check. `github_speed_test.go` must pass unchanged. Expected: PASS.
- [ ] **Step 5: Commit** `feat(provider): link code changes through Keys and LinkedPRs`

### Task 6: Wording

**Files:**
- Modify: `internal/cli/assess.go:57`, `check.go:45`, `status_view.go:230`, `details.go:110`, `ticket_view.go:325`, `internal/automation/run.go:91`

**Interfaces:**
- Consumes: `Workspace.TrackerLabel()`.

- [ ] **Step 1: Write a failing test** `TestWordsNameTheTracker` in `internal/cli`: with a test kind labelled `Example`, the not-visible error says `your Example token`, and no Jira word appears. Run it. Expected: FAIL.
- [ ] **Step 2: Implement.** Replace `Jira` in those lines with `TrackerLabel()`. Change the note in `run.go` to `comes from your tracker or code host`. Leave setup screens alone: they are Jira only.
- [ ] **Step 3: Run** the full check. Update tests that pin the old note. Expected: PASS.
- [ ] **Step 4: Commit** `fix: name the configured tracker in messages`

### Task 7: Shared conformance tests

**Files:**
- Create: `internal/provider/conformance_test.go`, `internal/provider/jira_conformance_test.go`, `internal/provider/github_conformance_test.go`

**Interfaces:**
- Produces:
  - `type trackerFixture struct { Provider workfile.Provider; Handle func(*http.Request) (int, string); InScope, OutOfScope, Unmapped, Missing string }`
  - `func conformTracker(t *testing.T, f trackerFixture)`
  - `type hostFixture struct { Provider workfile.Provider; Handle func(*http.Request) (int, string); Tickets []workfile.Ticket }`
  - `func conformCodeHost(t *testing.T, f hostFixture)`
  - `func readOnly(t *testing.T, r *http.Request)`: fails on anything but GET, a POST whose path ends in `/search/jql` or `/graphql`, or a GraphQL body starting `mutation`.

- [ ] **Step 1: Write** both functions, with one `t.Run` per check listed in step 4 of the guide. Allowed values: `state` in `open`, `merged`; `draft` in `true`, `false`; `checks` in `success`, `pending`, `failure`, `none`; `merge_queue` in `queued`, `none`.
- [ ] **Step 2: Write** `TestJiraConforms` and `TestGitHubHostConforms`, with fixtures adapted from `provider_test.go`.
- [ ] **Step 3: Run** `go test -race ./internal/provider -run Conforms`. Expected: PASS. A failure is a real gap in an adapter: fix the adapter, and report it in the commit message.
- [ ] **Step 4: Commit** `test(provider): add shared conformance tests`

### Task 8: GitHub tracker reads tickets

**Files:**
- Create: `internal/provider/github_tracker.go`, `internal/provider/github_tracker_test.go`, `internal/workfile/github_keys.go`, `internal/workfile/github_keys_test.go`
- Modify: `internal/workfile/kinds.go`, `internal/provider/tracker.go`, `internal/provider/jira.go` (`jiraState`)

**Interfaces:**
- Consumes: `conformTracker`, `nextCursor`, `c.graph`, `parallel`.
- Produces:
  - `type githubKeys struct{ org string }`
  - `type githubTracker struct{ *Client; name string }`
  - `func stateFor(p workfile.Provider, status string) string` in `tracker.go`, replacing `jiraState`, comparing without case and leading symbols.
  - GitHub's row gains `Tracker` with `Settings` `org`, `project`, `view`, `statuses`, `default_project`, and `Facts: TrackerFacts`. `checkGitHubTracker` requires `project` above 0, and `default_project` matching `^[a-z0-9._-]+$`.

- [ ] **Step 1: Write failing tests:**
  - `TestGitHubKeys`: `App#42` gives `app#42`. `42` and `#42` with project `app` give `app#42`. `42` with no project errors. `Mentions("see app#42 and app#42x")` gives only `app#42`. `Links` finds `https://github.com/example-team/app/issues/42`, and ignores another org.
  - `TestStateIgnoresCaseAndSymbols`: `done: Done` matches `☠️ Done` and `🕰 Backlog` matches `backlog`, with and without the invisible emoji character. Jira `In Review` still matches `in review`.
  - `TestStateComesFromTheProjectNotTheIssue`: an open issue in `Done` has state `done`.
  - `TestGitHubListingReadsEveryPage`: 150 items over two pages gives 150 listings in 2 requests.
  - `TestRepeatedCursorStops`: a server repeating its cursor gives an error, and the handler is called at most twice.
  - `TestDraftsAndPRsOnTheBoardAreSkipped`.
  - `TestViewFilterIsTheScope`: `view: 3` with filter `label:blocked` sends `query: label:blocked`. Empty and null filters send no query. The view is read once per run.
  - `TestOnlyMineAddsAssignee`: `assignee:@me` on `Tickets`, not `Listing`. With `me.issues: alice`, `assignee:alice`.
  - `TestTicketsByKeyPicksThisProject`: an issue on projects 3 and 7, with `project: 7`, uses 7's status.
  - `TestIssueWithNoStatusHasAnError`, and the same for an issue not on the project.
  - `TestArchivedItemIsOutOfScope`.
  - `TestLinkedPRsComeWithTheTicket`: `closedByPullRequestsReferences` fills `LinkedPRs`, in the same request.
  - `TestMissingScopeNamesReadProject`: an `INSUFFICIENT_SCOPES` response makes `Test` return an error holding `read:project`, with no GitHub message text.
  - `TestGitHubTrackerConforms`.
- [ ] **Step 2: Run** them. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - Scope reads use `organization(login).projectV2(number).items(first: 100, after:, query:)`. Page with `nextCursor`. Read `type`, `isArchived`, `fieldValueByName(name: "Status")`, and the issue's number, title, body, repository name, assignees, labels, `issueType`, `closedByPullRequestsReferences(first: 20)`. Leave `includeClosedPrs` at its default.
  - `Listing` asks for no body and no PR links.
  - Key reads batch issues with aliases on `repository(name).issue(number).projectItems`. Do not set `includeArchived`.
  - Facts follow the "Decisions" section of the spec. `headings` are `#` lines in the body, outside code fences. No `issueType` goes in `Unavailable`.
  - `Identity` reads `viewer.login`. `Test` reads the project's title and item count, as `alice · Example board · 12 items`.
  - `only_mine` uses `me[name]`, then `me["github"]`, then `@me`.
  - `limit` stops paging once reached.
  - `Detail` and `Image` return `errors.New("not built")` until Task 9, with those `conformTracker` checks skipped.
- [ ] **Step 4: Run** the full check, and `git diff --stat -- internal/cli`. Expected: PASS, and no change in `internal/cli`.
- [ ] **Step 5: Commit** `feat(provider): read tickets from GitHub Projects`

### Task 9: GitHub issue text and images

**Files:**
- Create: `internal/provider/github_document.go`, `internal/provider/github_document_test.go`
- Modify: `internal/provider/github_tracker.go`

**Interfaces:**
- Consumes: `c.download`.
- Produces: `func markdownBlocks(text string) []workfile.Block`, handwritten, as `go.mod` has no Markdown library. Do not add one.

- [ ] **Step 1: Write failing tests:**
  - `TestMarkdownBlocks`: one table case each for heading levels, paragraph, bold, italic, code span, link, fenced code, bullet and numbered lists with `Level`, quote with `Quoted`, rule, table with `Header`, and image with `Name` and `Attachment`. Unknown syntax stays as plain text.
  - `TestGitHubDetail`: body and comments as blocks, `Created` parsed, `CommentTotal` from `totalCount`, newest 100 read with `comments(last: 100)`. A failed comment read still returns the body.
  - `TestImageOnlyGoesToGitHub`: `https://github.com/user-attachments/assets/a.png` is fetched. `https://github.com.example.com/a.png`, `http://github.com/a.png` and `https://example.com/a.png` are refused, and the handler sees no request.
- [ ] **Step 2: Run** them. Expected: FAIL.
- [ ] **Step 3: Implement.** Allowed hosts are exactly `github.com`, `user-images.githubusercontent.com` and `private-user-images.githubusercontent.com`, https only. `Client` refuses redirects, so an image that redirects fails with the normal HTTP error. That is accepted: the block still names the image. Remove the skips in `conformTracker`.
- [ ] **Step 4: Run** the full check. Expected: PASS.
- [ ] **Step 5: Commit** `feat(provider): show GitHub issue text and images`

### Task 10: Docs and final checks

**Files:**
- Modify: `docs/providers/github.md`, `docs/providers/building-a-provider.md`, `docs/policy.md:53`, `docs/getting-started.md:183`, `docs/usage.md`, `README.md`, `AGENTS.md` (Packages), `example/`

- [ ] **Step 1: Write docs.** `github.md` gains "GitHub Issues as a tracker": the sample two connections from the spec, `read:project`, status matching, filter text needing the exact emoji, and which PRs link. Say `wf status 42` or `wf status app#42`, and that an unquoted `#42` is a comment in most shells. `policy.md` says `tracker` names any tracker connection. `getting-started.md` links the guide. Remove the guide's Draft note. `AGENTS.md` Packages names `tracker`, `codeHost` and `kinds.go`. Update the `tracker.go` comment.
- [ ] **Step 2: Run** `eval "$(mise env -s bash)" && go vet ./... && go test -race ./...`. Expected: PASS.
- [ ] **Step 3: Run** the spec's checks: `rg 'Kind == "(jira|github)"' internal` finds nothing outside `kinds.go`, and `git diff --stat <task 7 commit> -- internal/cli` is empty.
- [ ] **Step 4: Run** `make install`, then ask the owner to try `wf test` and `wf tickets` against a real board. This also settles the spec's "Not yet proven" list. Needs `read:project`.
- [ ] **Step 5: Commit** `docs: describe the provider standard and GitHub tracker`
