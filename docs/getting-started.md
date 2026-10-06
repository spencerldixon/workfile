# Getting started

This page explains the main ideas in Workfile, in plain English. Read it before
you install anything. It takes about five minutes. After it, the other pages
will make sense.

## The big idea

Your team probably has rules for how work should move. For example:

- "A ticket needs a proper description before anyone starts it."
- "A ticket needs a pull request before it goes to review."
- "A pull request needs an approval and passing automated checks before the ticket is done."

These rules often live in people's heads. Workfile lets you write them down in a
few small files. Then it checks your real Jira tickets and GitHub pull requests
against them and tells you which ones are off track.

Workfile only reads your data. It never changes a ticket, a label, or a pull
request.

## The words you will see

### Provider

A **provider** is a tool Workfile reads from. Today there are two:

- **Jira** holds your tickets.
- **GitHub** holds your pull requests (PRs).

Workfile links the two. A PR belongs to a ticket when the PR's title or branch
name contains the ticket's key, like `APP-42`. An explicit link to that ticket
on your configured Jira site in the PR description also links them.

### Connection

A **connection** is one configured use of a provider. It has a name, such as
`jira` or `github`, and holds things like your Jira site and your GitHub
organisation. You list your connections in `providers.yml`. The settings files
call these "instances". If you work with two GitHub organisations, you have two
GitHub connections. You use a connection's name in rules, like `github.approvals`.

### Search (also called scope)

Your **search** says which tickets Workfile should look at. It is a Jira search
(JQL), such as "everything in project APP that is not done". Tickets outside
your search are invisible to Workfile.

### State

A **state** is a step in your workflow, such as `todo`, `doing`, `review` and
`done`. You list them in order in `policy.yml`, where they are called `states`.
Each stage matches one or more Jira statuses. For example, the stage `review`
might match the statuses "In Review" and "Code Review". That match lives in
`providers.yml` under `statuses`.

### Transition (also called a move)

A **transition** is a move from one stage to another. A move forward goes toward
the end of the workflow. A move back, such as sending a ticket from review to
doing, is called a **return**. A stage can have several moves out, which makes a
branch. Different moves can need different gates. A `requires` list shares
requirements across forward moves; a `requires` map assigns them to individual
destinations. A stage with `end: true` is the finish line and has no moves out.

### Gate

A **gate** is a named checklist a ticket must pass to make a move. For example,
a gate called `code_review` might say "at least one approval". You attach gates
to moves. The ticket may only make the move when its gate is open.

### Rule

A **rule** is one condition inside a gate, such as `github.approvals >= 1`. A
gate can have many rules. A gate passes only when every rule that applies to the
ticket passes. A rule can say who counts, using `by`, and when it applies, using
`when`.

### Fact

A **fact** is something Workfile reads from Jira or GitHub, such as the length of
a description, a ticket's labels, or the number of approvals on a PR. Rules are
made of facts. Workfile does not read history. It only knows what is true right
now.

### People

Your **people** file is optional. It lists names with their Jira and GitHub
accounts. You need it when a rule says that a particular person must do
something, such as "approved by Alice". It also powers `--user`.

### Policy

Your **policy** is everything above, taken together: your stages, the moves
between them, and the gates on those moves. It is the written-down version of how
your team works. Its main file is `policy.yml`.

## How it fits together

Here is a small policy, in the plain words of the files:

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

Read it like a story. A ticket starts in `todo`. To move to `doing` it needs a
description of at least 100 characters. To move to `review` it needs a linked PR.
To move to `done` its PR needs an approval and passing CI (continuous
integration: automated checks run for a change) on the current commit.

## Health and status

Workfile has two main questions it can answer.

**Status** looks forward. For each ticket it asks: *what does this ticket need
before it can move on?* A ticket is ready to move, needs work, or is out of
policy.

**Health** looks at the whole board. It asks: *how much of our work follows our
policy, and where is it going wrong?*

A ticket is **out of policy** when it is already past a gate but does not pass it.
Imagine a ticket in `review` that has no linked PR. It should not have got there
without one. A ticket in `doing` with no PR yet is not out of policy. It simply
cannot move on until it has one. `wf status` shows that as "needs work".

Workfile judges tickets by the facts today. If a ticket's description was long
enough when it moved, but someone shortened it later, Workfile will notice.

Some workflows have branches, with a different path for QA, security, or a quick
release. A ticket only has to pass the gates on **one** valid path to its stage.
It does not need the gates from every branch.

## What you will do

1. **Install** `wf`. See the [README](../README.md#install).
2. **Run `wf setup`**. Choose a folder, connect Jira and optional GitHub in
   your browser, and paste your tokens into hidden prompts. Choose a Jira project
   and confirm a starter stage order. Setup writes `.workfile/` and saves tokens
   privately. See [setup details](usage.md#setup-start-from-scratch).
3. **Review the starter policy**. Its simple stage-to-stage moves are suggestions,
   not imported Jira workflow moves. Add your team's gates and branches.
   `people.yml` contains visible project users; add GitHub identities if needed.
4. **Prefer manual setup?** Copy an example from [`example/`](../example/), edit
   the providers and people, and add credentials to your private credentials file.
   Credentials never go in the project files.
5. **Run `wf test`** to check the connections work.
6. **Run `wf health` and `wf status`** to see how your work compares with your
   policy.
7. **Change `policy.yml`** until it describes the way your team really works.

## Where to read next

- [Using wf](usage.md): every command and option.
- [Writing a policy](policy.md): stages, moves, gates and rules in detail.
- [Jira provider](providers/jira.md) and [GitHub provider](providers/github.md): tokens, settings, and what each
  attribute means.
- [Examples](../example/): policies you can copy.
