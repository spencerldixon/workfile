# Proposal: tools read the workfile

**Status:** an idea. Nothing on this page is built yet. It is the first step
towards the [vision](../vision.md).

New to Workfile? Read [Getting started](../getting-started.md) first. This page
uses its words: provider, stage, move, gate and rule.

## The short version

Agent skills and scripts carry project details today: a Jira site, status
names, a project key. So they work in one project and break in the next.

Instead, a tool asks `wf` about the project it is in, and talks only in stages.
"Move this ticket to review" means whatever review means in this folder's
workfile. Change folder, and the same tool works in another project.

Three small additions to Workfile make this possible:

1. `wf workspace --json`: describe the project.
2. The provider statuses for each move, in `wf status --json`.
3. The assignee of each ticket, in `wf status --json`.

## The problem

Here are four agent skills that a person might use every day:

| Skill | Project details it carries today |
| --- | --- |
| Find work | The Jira site, the names of the "to do" statuses, and its own search |
| Ship it | The Jira site, and the status name for review |
| Create ticket | The Jira project key and board |
| Watch a PR | The GitHub organisation |

Every one of those details is already in the project's `.workfile/`. The
skills copy them, and the copies go out of date or are wrong in the next
project.

## Ask `wf`, do not read the YAML

A tool could read `providers.yml` and `policy.yml` itself. It should not:

- **`wf` already finds the workspace.** It uses the nearest `.workfile/`,
  checks it, and stops with a clear error when it is wrong. Every tool would
  have to do the same again.
- **`wf` knows what the config means.** `review: [In Review, Review]` maps one
  stage to two Jira statuses. A tool that reads the YAML has to guess which one
  to use.
- **The JSON is a contract.** It has a `schema_version`. The config files can
  change shape without breaking tools.

So there is one reader of the workfile, and every tool asks it.

## What a tool can ask today

- `wf status --all --json`: each ticket's stage, provider status, linked PRs,
  the moves allowed now, and what is unmet. See [JSON reports](../json.md).
- `wf check KEY --to STAGE`: may this ticket move to this stage now? Exit 0 for
  yes, 1 for no.

That answers "is this ticket ready?" and "may I move it?". It does not yet
answer "where is this project's work?" or "which Jira status is review?".

## 1. `wf workspace --json`

A new command that describes the workspace the current folder belongs to:

```json
{
  "schema_version": 1,
  "name": "example-web",
  "root": "/home/alice/work/example-web",
  "providers": {
    "jira": {
      "provider": "jira",
      "site": "example-team.atlassian.net",
      "scope": "project = APP AND (statusCategory != Done OR resolved >= -14d)",
      "statuses": {
        "todo": ["To Do"],
        "doing": ["In Progress"],
        "review": ["In Review", "Review"],
        "done": ["Done"]
      }
    },
    "github": {
      "provider": "github",
      "org": "example-team"
    }
  },
  "stages": ["todo", "doing", "review", "done"]
}
```

- **No secrets.** It never prints a credential value. It may name the
  credential, such as `EXAMPLE_JIRA_TOKEN`, so a tool can say what is missing.
- **No network.** It reads only the workfile, so it is instant and works
  offline.
- **It grows with the vision.** Repositories, PR templates, documents and
  shipping steps join this output when the workfile learns them.

Without `--json`, it prints the same facts as a short panel, so a person who
steps into a project can see what it uses.

## 2. Provider statuses for each move

Each route in `wf status --json` names its destination stage. Add the provider
statuses for that stage, from `statuses` in `providers.yml`:

```json
{
  "to": "review",
  "available": true,
  "provider_statuses": ["In Review", "Review"]
}
```

A tool that moves the ticket takes the first status that one of the ticket's
Jira transitions leads to. It never guesses a transition. Workfile still only
reads: the tool makes the move, with its own access.

## 3. The assignee of each ticket

The ticket JSON has no assignee today. A "find work" tool needs unassigned
tickets, and an automation that dispatches agents needs to know whose ticket
it is. Add the assignee as the person's name from `people.yml` when it is
known, and as the provider's display name otherwise. Leave it empty for no
assignee.

## How a tool uses it

Write this once, in a shared reference that every skill links to:

1. Run `wf workspace --json`. When it works, take the site, scope, statuses and
   organisation from it.
2. When `wf` is not installed, or the folder has no workspace, use the
   project's `AGENTS.md`. When that says nothing, ask the person.
3. Before moving a ticket, run `wf check KEY --to STAGE`. When it says no, stop
   and report what is unmet.
4. Move the ticket with a transition that leads to one of the route's
   `provider_statuses`.

Then the four skills change like this:

| Skill | With the workfile |
| --- | --- |
| Find work | `wf status --all --json`: tickets at the first stages, with no assignee, that can move to the working stage now. The refinement gate replaces most of its own judgement of whether a ticket is clear. |
| Ship it | Move to `review`: `wf check`, then a transition to one of the route's statuses. |
| Create ticket | The site and project from `wf workspace --json`. |
| Watch a PR | The organisation from `wf workspace --json`. |

One change in behaviour: find work searches only the current project's tickets,
not every project the person can see. That is the point. The folder decides
the project.

## Order

1. `wf workspace --json`. It is the smallest change, and it unblocks every
   tool.
2. The shared reference for skills, then the skills with the most project
   details in them: ship it and find work.
3. Provider statuses in routes, and the assignee on each ticket.

## Open questions

- **The name.** The [roadmap](roadmap.md) plans `wf workspaces`, which lists
  every workspace on the computer. `wf workspace` beside it could confuse.
  `wf describe` is one alternative.
- **Who "me" is.** Per-workspace identity is not built yet (see the roadmap).
  Should `wf workspace --json` include the person's own account names once it
  is?
- **The scope search.** Printing the Jira search lets tools reuse it. Is any
  team's search sensitive enough that it should be left out?
