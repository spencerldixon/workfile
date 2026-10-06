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
  lookback: 90d
  auth:
    token: ${EXAMPLE_GITHUB_TOKEN}
```

## GitHub.com

Use a GitHub personal access token that can read the repositories you care
about. A classic token with the `repo` and `read:org` scopes works. If your
organisation uses single sign-on, authorise the token for it. If you use a
fine-grained token, choose your organisation and repositories. Give it read
access to pull requests, issue labels, repository details, commit statuses,
checks, and the organisation data the search needs. `wf test github` checks the fields that Workfile reads.
These guides help:
[GitHub's GraphQL guide](https://docs.github.com/en/graphql/guides/forming-calls-with-graphql)
and [token setup](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens).

| Setting | What it means |
| --- | --- |
| `org` | Required. Your GitHub organisation name. |
| `lookback` | How many days back to look, such as `90d`. The default is `90d`. |
| `auth.token` | The name of the credential that holds your GitHub token. |

Workfile lists the repositories you can see that are not archived, and searches
their PRs. It works on up to six repositories at once. It includes open PRs and
merged PRs that were updated within the lookback. PRs closed without merging are
left out. Searches read titles, branch names and descriptions first. Workfile
then reads labels, reviews, draft status and current-commit check status only
for linked PRs, with up to six PRs read at once.

A PR is linked to a ticket when the PR **title or branch name** contains the
ticket key. Case does not matter, and the whole key must match, so `APP-12` does
not match `APP-123`. An explicit `https://example-team.atlassian.net/browse/APP-42` link in
the PR description also links the ticket, but only when the host is your
configured Jira site. Bare ticket mentions in descriptions do not link work.
Commit messages and Jira's development panel are not used. One PR can link to
several tickets, and repeated links do not duplicate it.

### PR attributes

| Attribute | What it holds |
| --- | --- |
| `github.prs` | How many PRs are linked to the ticket. |
| `github.labels` | The label names on each PR. |
| `github.approvals` | Reviewers whose latest opinion is an approval. Works with `by`. |
| `github.fresh_approvals` | Those approvers whose review commit matches the PR's current head commit. Works with `by`. |
| `github.draft` | Text `true` or `false`: whether the PR is a draft. |
| `github.checks` | Current commit's combined check and status result: `success`, `pending`, `failure`, or `none`. |
| `github.state` | `open` or `merged`. |
| `github.author` | The login of the person who wrote the PR. |
| `github.review_requests` | The logins of people asked by name to review. |

Approvals count each reviewer once. If a reviewer's latest opinion asks for
changes or was dismissed, it is not an approval. A comment-only review does not
cancel an earlier approval. With `by`, the names must be people whose account
for this connection is their GitHub login.

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

Repository lists, searches, labels, reviews and review requests all come in
pages, and Workfile reads every page. GitHub stops a search at 1,000 results. If
your search hits that limit, the command fails and asks you to shorten
`lookback`. It will never show a partial result as if it were healthy.

The lookback and what your token can see always limit a report. A PR older than
the lookback, or in an archived or hidden repository, is never linked. Set the
lookback long enough to cover the tickets you check. A passing token test cannot
tell you about repositories the token is not allowed to see.

## When something goes wrong

| What you see | What to try |
| --- | --- |
| Credential is not set | Make sure the `${NAME}` in `auth` matches an environment variable or a line in your credentials file, and that it is not empty. |
| Credentials permission error | On macOS or Linux, run `chmod 600` on the file it names. |
| HTTP 401 | Check the account, the token, and whether the token has expired. |
| HTTP 403 or GraphQL query rejected | Check scopes, repository access, organisation rules, single sign-on, and rate limits. |
| Jira query rejected | Try your JQL in Jira, and check the project and status names. |
| No Jira cloud ID | Check `site`. Only Jira Cloud is supported. |
| Unmapped Jira status | Add it to the right stage under `statuses`, or leave it out of your search on purpose. |
| A PR you expected is missing | Check its title or branch for the ticket key, its last update date, whether you can see the repository, and whether it is archived. |
| A ticket you expected is missing | Check your search, your `--me` or `--user` filter, the limit, and your account's access. |

Requests time out after 30 seconds. If a request is rate limited or the gateway
has a short failure, Workfile tries again up to two times, with short waits that
you can cancel. If the wait would be longer, it tells you, so you can try later.
Responses from the providers are never printed. That keeps error messages from
repeating credentials or private data.
