# Building a provider

This is the standard from the
[provider standard design](../superpowers/specs/2026-11-06-provider-standard-design.md).
Jira and GitHub follow it.

A provider connects Workfile to a service. New to Workfile? Start with
[Getting started](../getting-started.md).

Every provider has one of two types:

| Type | Question it answers | Examples |
|---|---|---|
| **Tracker** | What work exists, and what stage is it in? | Jira, GitHub Issues |
| **Code host** | What code changes belong to this ticket? | GitHub |

A workspace has one tracker: the connection named by `tracker` in `policy.yml`.
Every other connection in `providers.yml` is a code host. One service can do both
jobs, through two connections.

## The steps

| Step | Where |
|---|---|
| 1. Describe the kind | a row in `internal/workfile/kinds.go` |
| 2. Write the adapter | `internal/provider/<kind>.go`, or `<kind>_tracker.go` and `<kind>_host.go` as GitHub does |
| 3. Register it | one line in `trackers` (`tracker.go`) or `codeHosts` (`connection.go`) |
| 4. Pass the shared test | `conformTracker` or `conformCodeHost` |
| 5. Document it | `docs/providers/<kind>.md`, linked from `docs/getting-started.md` |

You should not need to change `internal/cli`. If you do, the standard has a gap.
Fix the standard, so the next provider does not hit it.

## 1. Describe the kind

```go
type Kind struct {
	Label    string              // the name people read: "Example"
	Auth     []string            // required credentials, each a ${NAME}
	Keys     func(Provider) Keys // needed if it can be a tracker
	Tracker  *Role               // nil if it cannot be a tracker
	CodeHost *Role               // nil if it cannot be a code host
}

type Role struct {
	Settings []string // allowed providers.yml settings in this role
	Check    func(name string, p Provider, add func(string, ...any)) // may be nil
	Facts    []string // TrackerFacts or CodeHostFacts, plus any of its own
	ByFacts  []string // facts that say who supplied them, for `by` rules
}
```

Workfile rejects settings not listed, and a connection given a role its kind
does not have. A new setting also needs a field on `workfile.Provider` and a line in
`settings()` in `kinds.go`. Without that line, other kinds silently accept it.

## 2. Write the adapter

The adapter is a struct holding the `Client` and the connection's name. Both types
are also a `connection`:

```go
type connection interface {
	// Identity is who the token belongs to, in the form people.yml uses.
	Identity(ctx context.Context) (string, error)
	// Test proves the token can read what Workfile needs, in one short line.
	// If a permission is missing, it names it.
	Test(ctx context.Context) (string, error)
}
```

Then follow [Tracker](#tracker) or [Code host](#code-host).

## Tracker

```go
type tracker interface {
	connection
	Tickets(ctx context.Context, keys []string, limit int) ([]workfile.Ticket, error)
	Listing(ctx context.Context, scope string) ([]workfile.Listing, error)
	Detail(ctx context.Context, key string) (workfile.Detail, error)
	Image(ctx context.Context, attachment string) ([]byte, error)
}
```

| Method | Reads | Rules |
|---|---|---|
| `Tickets` | Full tickets, for policy checks | With `keys`, read those even outside the scope, and set `OutOfScope` on any it hides. Leave out a key that does not exist. With no `keys`, read the scope up to `limit`; 0 means all. `only_mine` narrows a scope read only |
| `Listing` | The little a list shows, for the whole scope | Ask for only those fields. A non-empty `scope` replaces the configured one. Ignore `only_mine`. `State` is empty for an unmapped status |
| `Detail` | One ticket's text and comments | Return `workfile.Block`s, never the tracker's format. If comments fail, still return the ticket. If you cannot return every comment, return the newest and set `CommentTotal` |
| `Image` | An image a block named in `Attachment` | Check `attachment` first: it comes from ticket text. Never send a token to a host a ticket supplied. Use `c.download` |

Fields on `Ticket`:

| Field | Rule |
|---|---|
| `Key` | The form `Keys.Parse` returns |
| `State` | The policy state, from `statuses`. Use the field that drives the team's board: a tracker's open or closed flag may not follow it |
| `Error` | Set for an unmapped status, and still return the ticket |
| `Assignee` | The identity, not the display name. Empty if unassigned |
| `Summary` | One short line: `In Progress · assigned to Alice` |
| `URL` | The page a person would open |
| `LinkedPRs` | PRs the tracker knows are linked, as `org/repo#42`. Read them in the same query. Leave empty if it does not know |

Standard facts, in `TrackerFacts`:

| Fact | Type | Meaning |
|---|---|---|
| `summary` | `string` | The title |
| `description` | `string` | The body as plain text |
| `headings` | `[]string` | Headings in the body |
| `labels` | `[]workfile.Actor` | `Value` is the label. `ID` and `Name` say who added it, if known |
| `assignee` | `string` | Display name, empty if unassigned |
| `type` | `string` | The kind of ticket |
| `project` | `string` | The project or repository |

### Keys

```go
type Keys interface {
	Parse(input, project string) (string, error) // what a person typed, to the one true form
	Search(key string) string                    // text a code host searches for; may match loosely
	Mentions(text string) []string               // keys in a PR title or branch
	Links(body string) []string                  // keys in explicit links to this tracker
}
```

Keys that look like `APP-42` can use `dashKeys`. Other shapes need their own
file in `internal/workfile`, as `github_keys.go` shows. `Keys` is pure: no network.

## Code host

```go
type codeHost interface {
	connection
	// Records returns the code changes linked to each ticket, by Ticket.Key.
	Records(ctx context.Context, tickets []workfile.Ticket, keys workfile.Keys) (map[string][]workfile.Record, error)
}
```

`keys` is the tracker's, so a code host works with any tracker.

**Finding work.** For each ticket:

1. Read every PR in `Ticket.LinkedPRs` directly.
2. For the rest, search for `keys.Search(key)`, a few tickets per request.
3. Search matches loosely, so keep a result only if `keys.Mentions` finds the key
   in its title or branch, or `keys.Links` finds it in its body.

A ticket with no linked work gets no entry. If a read fails, return the error.
Never return part of an answer as if it were all of it.

A code change that belongs to two tickets appears under both.

Fields on `Record`:

| Field | Rule |
|---|---|
| `ID` | `repo#42`. The screens split it at `#` |
| `Repository` | `org/repo` |
| `URL`, `Title` | The change's page and title |
| `Summary` | One short line: `open · by alice · no approvals` |
| `Head` | The latest commit |
| `People` | The author and requested reviewers, as identities |

Standard facts, in `CodeHostFacts`. Values must match exactly, so a policy means the
same on any code host:

| Fact | Type | Meaning |
|---|---|---|
| `state` | `string` | `open` or `merged` |
| `draft` | `string` | `true` or `false` |
| `checks` | `string` | The current commit: `success`, `pending`, `failure` or `none` |
| `approvals` | `[]workfile.Actor` | Reviewers whose latest opinion is an approval, each once |
| `author` | `string` | The identity of who wrote it |
| `review_requests` | `[]string` | Identities asked by name to review |
| `labels` | `[]string` | Labels on the change |

`prs` is a count, and Workfile works it out from your records.

Optional facts: list these in `Role.Facts` only if your host can supply them. A
policy using a fact the kind lacks fails when it loads.

| Fact | Type | Meaning |
|---|---|---|
| `fresh_approvals` | `[]workfile.Actor` | Approvals on the current commit |
| `merge_queue` | `string` | `queued` or `none` |

## Standards

These apply to every provider.

**Every fact is known or explained.** Each listed fact is in `Facts`, or in
`Unavailable` with a reason. Empty and unknown differ: no description is `""`, but
a field the service did not send is `Unavailable`, and Workfile reports "cannot
check" and not a failed rule. Never guess a value.

**Read only.** Never create, change or delete anything. POST is for a search or a
GraphQL query only. No mutations. Jira's changelog bulk fetch is a POST that only
reads.

**Be sparing.** Cost follows the tickets asked for, not the size of the service.
Fetch many tickets per search, name the fields you want, and read expensive facts
only when the policy needs them. Use `parallel` for reads you cannot batch.

**Go through `Client`.** Use `c.request`, `c.graph` and `c.download`: they set
headers, retry, refuse redirects, and keep response bodies out of errors. Read
secrets with `c.Credentials.Get`. Keep the base address in a `Client` field, so
tests can replace it.

**Errors.** Start with the connection name. Never include a response body or an
authenticated address. Say what to do: `check the site setting`, not `bad request`.

**Text.** Return provider text as it came. It is untrusted, and the CLI cleans it
before printing. Do not build terminal output here.

**Examples.** Use Alice, Bob and Charlie, `example-*` accounts and `example.com`.

## 3. Register it

```go
var trackers = map[string]func(*Client, string) tracker{
	"example": func(c *Client, name string) tracker { return exampleTracker{c, name} },
}
```

Code hosts go in `codeHosts`, the same way.

## 4. Pass the shared test

Give it made-up responses. It needs no credentials.

```go
func TestExampleTracker(t *testing.T) {
	conformTracker(t, trackerFixture{
		Provider: workfile.Provider{Kind: "example", Site: "example.com"},
		Handle:   exampleResponses,
		InScope: "APP-1", OutOfScope: "APP-2", Unmapped: "APP-3", Missing: "APP-404",
	})
}
```

Both tests check that:

- no request writes
- a failed request's error holds no response body and no token
- every standard fact is in `Facts` or `Unavailable`, for every fixture
- fact values are in the allowed set

`conformTracker` also checks `OutOfScope`, missing keys, unmapped statuses,
`only_mine`, that `Key` survives `Keys.Parse`, and that a bad `attachment` is
refused before any request.

`conformCodeHost` also checks `LinkedPRs` are read with no search, that loose search
results are dropped, that a change linked to two tickets appears under both, and
that a failed read returns an error.

## 5. Document it

Write `docs/providers/<kind>.md` like [the Jira guide](jira.md): a sample
`providers.yml`, the smallest token that works, and any fact that behaves
differently or is never available.

## Before you open a pull request

- [ ] `go vet ./... && go test -race ./...` pass
- [ ] the shared test runs for your kind
- [ ] nothing in `internal/cli` changed
- [ ] `wf test` and `wf status` work against a real account
- [ ] your docs are linked from `docs/getting-started.md`
