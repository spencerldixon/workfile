# Getting started

Your team probably has rules for how work should move. For example:

- "A ticket needs a proper description before anyone starts it."
- "A ticket needs a pull request before it goes to review."
- "A pull request needs an approval and passing automated checks before the ticket is done."

These rules often live in people's heads. Workfile lets you write them down in a
few small files. Then it checks your real Jira tickets and GitHub pull requests
against them, and tells you what each one needs next.

Workfile only reads. It never changes a ticket, a label or a pull request.

## Do this first

```sh
wf setup            # connect your tools, pick projects and repos
wf status --me      # see your work
wf lint             # check the policy itself
```

Then change `.workfile/policy.yml` until it matches how your team really works.

## The words you will see

| Word | Meaning |
| --- | --- |
| **Provider** | A tool workfile reads from. Jira holds tickets. GitHub holds pull requests (PRs). |
| **Connection** | One configured provider, such as `jira` or `github`. Listed in `providers.yml`. |
| **Scope** | The slice of work workfile reads: your Jira projects, your GitHub repos, and how long finished work stays. |
| **Stage** | A step in your workflow, such as `todo`, `doing`, `review`, `done`. Listed in order in `policy.yml` under `states`. |
| **Move** | A step from one stage to another. A move backwards is a **return**. |
| **Gate** | A named checklist a ticket must pass to make a move, such as `code_review`. |
| **Rule** | One condition in a gate, such as `github.approvals >= 1`. |
| **Fact** | Something workfile reads, such as a description length or an approval count. |
| **People** | An optional list of names with their Jira and GitHub accounts. |
| **Policy** | All of the above together. The written-down way your team works. |

### How tickets and PRs link

A PR belongs to a ticket when the PR title contains the ticket key, like
`APP-42`. A link to the ticket on your Jira site in the PR description also
works.

### Scope is explicit

Workfile reads only what `providers.yml` lists. Nothing is taken from a tool's
own screen settings. For Jira you list `projects`. For GitHub you list `repos`
(or write `repos: all`). Either way you say how long finished work stays with
`keep_done_for`.

```yaml
jira:
  site: example-team.atlassian.net
  projects: [APP]
  keep_done_for: 14d
```

This keeps requests few and the file honest. Read it, and you know what workfile
sees. See the [Jira](providers/jira.md#scope) and [GitHub](providers/github.md#scope) guides.

## A small policy

```yaml
states: [todo, doing, review, done]

transitions:
  todo:
    to: [doing]
    requires: [refinement]      # to start work, pass the refinement gate
  doing:
    to: [review]
    requires: [pull_request]    # to go to review, pass the pull_request gate
  review:
    to: [done]
    requires: [code_review]     # to finish, pass the code_review gate
  done:
    end: true

gates:
  refinement:
    - if: jira.description.length >= 100
  pull_request:
    - if: github.prs >= 1
  code_review:
    - if: github.approvals >= 1
    - if: github.checks == success
```

Read it like a story:

1. A ticket starts in `todo`.
2. To move to `doing`, it needs a description of at least 100 characters.
3. To move to `review`, it needs a linked PR.
4. To move to `done`, its PR needs an approval and passing CI (automated checks) on the current commit.

## Status: the question you will ask most

`wf status` answers: *where is the work, and what does it need?*

Each ticket lands in one group:

| Group | Meaning |
| --- | --- |
| `✓` Ready | It can move on now. |
| `✗` Needs work | It is fine so far, but every move forward is blocked. |
| `‼` Out of policy | It is past a gate it does not pass. |
| `!` Cannot check | Evidence is missing, so workfile will not guess. |
| `·` Done | Finished. |

Example: a ticket in `review` with no linked PR is **out of policy**. It should
not have got there without one. A ticket in `doing` with no PR is only **needs
work**. It just cannot move on yet.

Workfile judges tickets by the facts today. It does not read history.

Some workflows branch, such as a QA path or a security path. A ticket only has
to pass the gates on **one** valid path to its stage.

## What changed?

```sh
wf status --since 1d
```

Shows what moved, got blocked or became ready since an earlier run on your
computer. It reads saved snapshots, so it adds no requests.

## What you will do

1. **Install** `wf`. See the [README](../README.md#install).
2. **Run `wf setup`.** Pick a folder, connect Jira and optional GitHub, paste
   your tokens (they show as `*`), pick your projects and repos, tick the
   statuses you want, and confirm. Setup writes `.workfile/` and saves tokens
   privately. See [setup details](usage.md#setup-connect-your-tools).
3. **Prefer to do it by hand?** Copy an example from [`example/`](../example/)
   and edit it. Credentials never go in the project files.
4. **Check the connections** with `wf setup --test`.
5. **Look at your work** with `wf status --me`, then `wf status`.
6. **Add your team's gates** to `policy.yml`. The starter has none, so every
   ticket reads as ready.
7. **Run `wf lint`.** It catches plain mistakes and proves no path skips a gate.
   Run it in CI.
8. **Let agents ask first.** `wf check APP-42 --to done` says whether a move is
   allowed. Exit code `0` means yes.

## Where to read next

- [Using wf](usage.md): every command and option.
- [Writing a policy](policy.md): stages, moves, gates and rules in detail.
- [Jira provider](providers/jira.md) and [GitHub provider](providers/github.md): tokens, scope and facts.
- [Building a provider](providers/building-a-provider.md): add a new tracker or code host.
- [Vision](vision.md): where this is going.
- [Examples](../example/): policies you can copy.
