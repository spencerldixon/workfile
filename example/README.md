# Example policies

Everything here is made up: the organisation, the projects, the people and the
account IDs. The files hold the names of credentials, never the credentials
themselves.

- [`.workfile/`](.workfile/): Jira tickets with GitHub PRs. Work needs a good
  description, a PR, passing CI (continuous integration: automated checks for a
  change), and reviews. High-risk PRs also need a named reviewer's
  approval.
- [`branching/.workfile/`](branching/.workfile/): twelve stages with QA and
  security branches, a shortcut to release, different gates for each
  destination, cancelling, and valid returns. Code review also needs passing CI.
- [`destination-requires/.workfile/`](destination-requires/.workfile/): a small
  Jira workflow where `todo → ready` needs refinement, but `todo → done` does not.
- [`jira-only/.workfile/`](jira-only/.workfile/): a smaller workflow that uses
  only Jira labels to sort and sign off work.

To use one, copy it into your project from the workfile folder:

```sh
cp -R example/.workfile /path/to/your-project/.workfile
# Or:
cp -R example/jira-only/.workfile /path/to/your-project/.workfile
```

Then change the site, search, statuses, organisation and people to match your
team. Make your personal credentials file as described in the
[README](../README.md), and run `wf test --dir /path/to/your-project`.

The GitHub example needs at least one PR before a ticket reaches review. Rules
about reviews do not need a PR to exist: they are skipped when no PR is linked.
Keep the `github.prs >= 1` rule if your workflow needs a PR.
