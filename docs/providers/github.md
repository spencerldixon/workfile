# GitHub provider

New to Workfile? Start with [Getting started](../getting-started.md).

Each entry in `providers.yml` is one connection. Its name is the start of every
expression that uses it, and it is the name you give to `wf test`.

The `provider` setting says which provider to use. If you leave it out, the
connection's name is used. Keep your real secrets in your environment or your
personal credentials file. Workfile refuses secrets written straight into
`auth`. A token can only read what its account is allowed to see.

```yaml
github:
  org: example-team
  auth:
    token: ${EXAMPLE_GITHUB_TOKEN}
```

## GitHub.com

Use a classic GitHub personal access token with the `repo` and `read:org`
scopes. `wf setup` opens GitHub's new-token page with both already selected.
Workfile only reads, but classic tokens have no read-only choice for
repositories, so `repo` is the smallest scope that works. If your organisation
uses single sign-on, choose Configure SSO next to the token and authorise it.
Fine-grained tokens are not recommended. `wf test github` checks the fields that Workfile reads.
These guides help:
[GitHub's GraphQL guide](https://docs.github.com/en/graphql/guides/forming-calls-with-graphql)
and [token setup](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens).

| Setting | What it means |
| --- | --- |
| `org` | Required. Your GitHub organisation name. |
| `auth.token` | The name of the credential that holds your GitHub token. |

Workfile searches your organisation for PRs that mention the tickets being
checked, six tickets per search, with up to six searches at once. The number of
searches follows the number of tickets, not the number of repositories, so
`wf status APP-42` makes one search. PRs of any age are found. It includes open
and merged PRs in repositories that are not archived. PRs closed without
merging are left out. Workfile then reads labels, reviews, draft status, merge
queue membership and current-commit check status only for linked PRs, with up
to six PRs read at once.

A PR is linked to a ticket when the PR **title** contains the ticket key. Case
does not matter, and the whole key must match, so `APP-12` does not match
`APP-123`. An explicit `https://example-team.atlassian.net/browse/APP-42` link
in the PR description also links the ticket, but only when the host is your
configured Jira site. Bare ticket mentions in descriptions do not link work.
GitHub search does not read branch names, so a PR that names the ticket only in
its branch is not found: put the key in the title. Commit messages and Jira's
development panel are not used. One PR can link to several tickets, and
repeated links do not duplicate it.

### PR attributes

| Attribute | What it holds |
| --- | --- |
| `github.prs` | How many PRs are linked to the ticket. |
| `github.labels` | The label names on each PR. |
| `github.approvals` | Reviewers whose latest opinion is an approval. Works with `by`. |
| `github.fresh_approvals` | Those approvers whose review commit matches the PR's current head commit. Works with `by`. |
| `github.draft` | Text `true` or `false`: whether the PR is a draft. |
| `github.checks` | Current commit's combined check and status result: `success`, `pending`, `failure`, or `none`. |
| `github.state` | `open` or `merged`. A queued PR is still `open`. |
| `github.merge_queue` | `queued` when the PR has a merge queue entry; otherwise `none`. Missing membership is unknown. |
| `github.author` | The login of the person who wrote the PR. |
| `github.review_requests` | The logins of people asked by name to review. |

Approvals count each reviewer once. If a reviewer's latest opinion asks for
changes or was dismissed, it is not an approval. A comment-only review does not
cancel an earlier approval. With `by`, the names must be people whose account
for this connection is their GitHub login.

### Merge queues

A merge queue holds approved changes while GitHub checks whether they can merge
safely together. Workfile reads the linked PR's current queue membership. Queued
PRs show **Queued for merge** and **Wait for merge**, instead of an instruction to
merge them again. Failing gates or unreadable evidence remain visible, even when
GitHub has queued the PR.

Queue membership never counts as merged, an approval, or a passing check. Existing
`github.state == merged` rules still fail until GitHub reports the PR as merged.
You can explicitly require membership with `github.merge_queue == queued`.
Workfile does not enqueue, dequeue or merge anything. It reads membership, not
queue position or the checks on GitHub's temporary merge-group commit; `checks`
still describes the PR's current commit.

### CI in the code review gate

CI means continuous integration: automated checks run for a change. Require
GitHub's combined check/status result to pass on each PR's current commit:

```yaml
gates:
  code_review:
    - if: github.approvals >= 1
    - if: github.checks == success
```

Both conditions must pass on **every** linked PR. Keep a separate
`github.prs >= 1` rule before review if a PR must exist; per-PR rules are skipped
when there are none. GitHub's example policies now include the CI rule, but
Workfile does not silently add it to existing policies.

When CI is pending, Workfile says to wait for it to finish successfully. When
it fails, it says to fix and rerun the failing checks. With no reported checks,
it says to run CI for the current commit. Unavailable evidence remains unknown.

### Readiness rules

```yaml
gates:
  release_ready:
    - id: linked-pr
      if: github.prs >= 1
    - id: ready-for-review
      if: github.draft == false
    - id: automated-checks
      if: github.checks == success
    - id: current-commit-reviewed
      if: github.fresh_approvals >= 1
```

`checks` uses GitHub's combined result for the latest commit, including check
runs and commit statuses. It is not limited to GitHub Actions. It does not check
which jobs your team expected to run, or reproduce branch protection. `none`
means GitHub returned no check result. It does not pass a `success` rule.
Pending checks are known evidence and fail a `success` rule until they finish.
Missing or unrecognised check data is unknown, not `none` or a pass.

Fresh approvals compare commit IDs, not dates. A new commit makes earlier
approvals stale even if they still count under `approvals`. If GitHub does not
provide the commits needed for the comparison, freshness is unknown. Draft
status is unknown when it was not returned. Unknown evidence on a required path
can make the ticket "cannot check" and exit with code 2. A known passing
alternative remains available. A provider request failure stops the report;
Workfile never treats a failed read as an empty list.

These rules are opt-in. GitHub's own merge restrictions still apply. Workfile
never merges a PR or moves a ticket.

Searches, labels, reviews and review requests all come in pages, and Workfile
reads every page. GitHub stops a search at 1,000 results. If one search hits
that limit, the command fails rather than show a partial result as if it were
healthy.

What your token can see always limits a report. A PR in an archived or hidden
repository is never linked. A passing token test cannot tell you about
repositories the token is not allowed to see.

Older `providers.yml` files may still have a `lookback` setting. It is no longer
used, and you can delete it.

## GitHub Issues as a tracker

GitHub can do two jobs: hold your tickets (a **tracker**), and hold the PRs that
change code (a **code host**). A team that uses it for both has two connections.
`issues` is the tracker. `github` is the code host.

```yaml
issues:
  provider: github
  org: example-team
  project: 7
  view: 3
  default_project: app
  statuses:
    todo: Backlog
    doing: In progress
    done: Done
  auth:
    token: ${EXAMPLE_GITHUB_TOKEN}
github:
  org: example-team
  auth:
    token: ${EXAMPLE_GITHUB_TOKEN}
```

In `policy.yml`, set `tracker: issues`. The keys under `statuses` must be stage
names from `policy.yml`. Two connections keep `issues.labels`
(labels on the issue) apart from `github.labels` (labels on the PR).

| Setting | What it means |
| --- | --- |
| `org` | Required. Your GitHub organisation name. |
| `project` | Required. The number of your organisation's GitHub Project, from its address. |
| `view` | Optional. The number of a saved view of that project. Only tickets the view shows are in your scope. |
| `statuses` | Required. Maps each stage in your policy to one or more `Status` names. |
| `default_project` | Optional. A repository name in lower case, such as `app`. It lets you name an issue by number alone. |

### Token

Use a classic token with `repo`, `read:org` and `read:project`. Without
`read:project` GitHub refuses the read, and `wf test` names the missing scope.

### Status comes from the project

A ticket's stage comes from the project's **`Status`** field, not from whether
the issue is open or closed. Boards often drift from the issue state, so the
board wins. The field name is fixed: it must be called `Status`. Only
organisation projects work. User projects do not.

Status names match without case and without leading symbols, so `done: Done`
matches a status called `☠️ Done`. If an exact match exists, it wins. If two
stages would match, the status is left unmapped and reported as an error.

Your scope is the live items of the project, or of the view if you set `view`.
Archived items are left out of lists, but you can still look one up by key. An
issue that is not on the project is reported with an error.

### Naming a ticket

A ticket key is the repository, then the number, such as `app#42`. Keys are in
lower case. With `default_project: app`, both of these work:

```sh
wf status 42
wf status app#42
```

Do not write `wf status #42` without quotes. In most shells an unquoted `#42` is
a comment, so the command runs with no ticket. Write `'#42'` or leave off the `#`.

### Filter text

A scope given to a list command is GitHub's own filter text. It is exact: the
status must be written with its emoji, as in `status:"🕰 Backlog"`. Plain
`status:Backlog` finds nothing. This differs from `statuses` in providers.yml,
which ignores leading symbols.

### What it reads

- `assignee` is the first person assigned to the issue. Only the first counts.
  `status --me` and `tickets --me` see that person only. A second assignee does
  not see the ticket in those views, and their automations do not fire, unless
  a PR involves them.
- With `view` set, Workfile reads the filtered view once and remembers it for 5
  minutes. One run of a command pays for one paged read of that view.
- At most 20 closing PRs per issue are read.
- `project` is the repository, and `type` is the issue type. Priority, size and
  other project fields are not facts. Label `by` rules are not supported.

### Which PRs link

A PR links to an issue when GitHub itself links them: for example `Resolves
#42` in the PR description, or a link made by hand in GitHub. Workfile reads
these links in the same query as the issue. PRs closed without merging are left
out. Open PRs and drafts are included.

Workfile also links a PR when its title or branch has the full key, such as
`app#42`, or when its description has the issue's address. A bare `#42` in a
title does not link, because it matches too loosely. A PR that only mentions an
issue does not link.

### Issue text and images

`wf status` shows the issue's text and comments. Headings, paragraphs, emphasis,
code, links, lists, quotes, rules and tables are shown. Anything else stays as
plain text.

Only `![alt](address)` images on GitHub's own hosts are shown. `<img>` tags stay
as text. An image address that redirects does not display. Workfile never sends
your token to another host.

## When something goes wrong

| What you see | What to try |
| --- | --- |
| Credential is not set | Make sure the `${NAME}` in `auth` matches an environment variable or a line in your credentials file, and that it is not empty. |
| GitHub project read refused | Add the `read:project` scope to your token, and check that the project belongs to your organisation. |
| Credentials permission error | On macOS or Linux, run `chmod 600` on the file it names. |
| HTTP 401 | Check the account, the token, and whether the token has expired. |
| HTTP 403 or GraphQL query rejected | Check scopes, repository access, organisation rules, single sign-on, and rate limits. |
| Jira query rejected | Try your JQL in Jira, and check the project and status names. |
| No Jira cloud ID | Check `site`. Only Jira Cloud is supported. |
| Unmapped GitHub status | Add the name to the right stage under `statuses`. |
| Unmapped Jira status | Add it to the right stage under `statuses`, or leave it out of your search on purpose. |
| A PR you expected is missing | Check its title or branch for the ticket key, its last update date, whether you can see the repository, and whether it is archived. |
| A ticket you expected is missing | Check your search, your `--me` or `--user` filter, the limit, and your account's access. |

Requests time out after 30 seconds. If a request is rate limited or the gateway
has a short failure, Workfile tries again up to two times, with short waits that
you can cancel. If the wait would be longer, it tells you, so you can try later.
Responses from the providers are never printed. That keeps error messages from
repeating credentials or private data.
