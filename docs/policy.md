# Writing a policy

New to Workfile? Start with [Getting started](getting-started.md), which explains
words like stage, gate and rule.

A policy lists the stages of your workflow. It also lists the **gates** a ticket
must pass before it can move to another stage. A gate holds one or more rules.
Every rule that applies must pass.

## Your files

| File | What it holds |
| --- | --- |
| `.workfile/policy.yml` | Stages, moves between stages, and gates. |
| `.workfile/providers.yml` | Your Jira and GitHub connections, your Jira search, status names, and where credentials come from. |
| `.workfile/people.yml` | Optional. Names and accounts for people, used by `by` and `--user`. |

Each file is one YAML document. Mistakes are errors, not guesses. That covers
unknown fields, unknown gates or attributes, repeated keys, and names that point
to something that does not exist. Every command checks your files before it
connects to anything.

## A small, complete workflow

```yaml
tracker: jira
states: [todo, doing, review, done]

transitions:
  todo:
    to: [doing]
    requires: [refinement]
  doing:
    to: [review]
    requires: [pull_request]
  review:
    to: [done, todo]
    requires: [code_review]
  done:
    end: true

gates:
  refinement:
    - if: jira.description.length >= 100
    - if: jira.labels excludes needs-more-information
  pull_request:
    - if: github.prs >= 1
  code_review:
    - if: github.approvals >= 1
    - if: github.checks == success
```

- `tracker` is the name of the Jira connection that supplies tickets.
- `states` puts the stages in order. The order decides which moves go forward
  and which are returns. It also sets the order of `wf health` and `wf status`.
- `to` lists where a ticket can go next. A `requires` list shares the same gates
  across forward destinations. A `requires` map assigns gates to individual
  destinations.
- A move back to an earlier stage, written with `to`, adds no gates of its own.
  The earlier stage's own entry gates still apply.
- A stage marked `end: true` has no moves out.

The code review gate needs both an approval and passing CI (continuous
integration: automated checks for a change) on every linked PR's current
commit. Pending, failed and absent checks do not satisfy it. Unavailable
required evidence is unknown, not a pass. This is an explicit policy rule,
not a built-in requirement attached to the name `code_review`.

## Different gates for different places

Keep `to` and use a destination-specific `requires` map when only some moves
need a gate:

```yaml
todo:
  to: [ready, done]
  requires:
    ready: [refinement]
    done: []
```

Refinement applies to `todo → ready`, not `todo → done`. A destination omitted
from the map also adds no gates. Its existing entry requirements still apply.
The shared form, `requires: [refinement]`, continues to apply refinement to
both forward moves. It does not change existing policies.

Names in a `requires` map must be destinations listed in `to`. Each value must
be a gate list; use `[]` for no additional gates. Explicit destination gates
also apply to returns. A shared list retains the existing shorthand behaviour:
it adds gates to forward moves, not returns.

You can also use the existing `routes` shorthand:

```yaml
review:
  routes:
    qa: [code_review]
    security: [threat_model]
    release: [code_review, release_waiver]
    doing: [rework_note]
```

These are alternatives:

- QA needs a code review.
- Going straight to release needs a code review **and** a release waiver.
- Security has its own gate.
- Going back to doing needs a rework note, plus whatever doing needs to be
  entered.

An empty list, such as `doing: []`, means that move adds no gates.

You can add `requires` next to `routes`. Those gates are added to **every**
destination, returns too. Leave it out if returning should skip the forward
gates.

Some rules to remember:

- A stage uses either `to` or `routes`, never both.
- A destination-specific `requires` map is used with `to`, not with `routes`.
  With `routes`, requirements are already written against each destination.
- An `end: true` stage cannot have moves out.
- A stage cannot move to itself, and a destination cannot be listed twice.
- A stage may have only moves back, such as a blocked stage that returns to doing.
- Every stage must be reachable going forward from the first stage.
- Returns can loop, but they must not add new entry gates to earlier stages.

The [destination-requires example](../example/destination-requires/.workfile/policy.yml)
shows refinement on one branch and an ungated shortcut to done.

The [twelve-stage example](../example/branching/.workfile/policy.yml) has
different review paths, a release shortcut, cancelling, and return loops.

## Current facts and branching paths

Workfile does not read a ticket's history. It asks one question: do the ticket's
facts today pass the gates along **at least one forward path** to its stage?

This matters on a board with branches. A ticket that took the security path is
not marked wrong for missing a QA sign-off. If no path passes, Workfile explains
the path with the fewest failed rules. Remember that this is a check of today's
facts. It does not prove how the ticket got where it is in Jira.

A forward move needs the entry gates for that stage plus its own route gates. A
return needs the entry gates of the stage it goes back to, plus the return's own
gates. For example, a ticket that reached review without a required PR can go
back to doing without anyone making that PR first. It still needs doing's own
entry gates and any gate named on the return.

`wf health` lists gates in the order they first appear along your workflow,
starting from the first stage.

## Rules

```yaml
gates:
  code_review:
    - if: github.approvals >= 2
      when: github.labels contains high-risk
      by: [alice, charlie]
      message: "{record} needs two approvals from the named reviewers"
```

- `id` is an optional, unique rule identifier for scripts and agents. Use a short,
  stable name, such as `current-commit-reviewed`. IDs start with an ASCII letter
  or digit and use letters, digits, `.`, `_`, `:`, `/` or `-`, up to 128 characters.
  Without it, Workfile derives
  an ID from the gate, condition, `when` and `by`. Reordering rules or changing
  their messages does not change derived IDs; changing conditions does.
- `if` is the condition that must be true.
- `when` makes the rule optional. If it is false, the rule is skipped.
- `by` counts only evidence from the people you name. It works on Jira labels
  and GitHub approvals (including `fresh_approvals`), because both remember who did them.
- `message` replaces the built-in explanation. `{record}` is the PR ID, or the
  ticket key for a ticket rule. `{ticket}` is always the ticket key.

The example above needs two approvals from Alice and Charlie together. For "at
least one of these people", use `>= 1`. To need each person on their own, write
one rule for each person.

`when` looks at all the facts, before `by` narrows them. So a high-risk label
turns the rule on even if someone else added the label.

## Expressions

```text
connection.attribute[.length or .count] operator value
```

The connection is a name from `providers.yml`, such as `jira` or
`github_client`. A value is a whole number, a plain word, text in double quotes,
or a list of text.

Text length counts characters. List size counts items. If you compare with a
number, the size is used. So `github.approvals >= 1` means the same as
`github.approvals.count >= 1`. A known empty value counts as zero. Evidence that was not returned is unknown,
not an empty value. Workfile does not claim a move is ready when its required
evidence is unknown.

| Operator | What it means | Example |
| --- | --- | --- |
| `>` `>=` `<` `<=` | Compare a number or a size. | `jira.description.length >= 100` |
| `==` `!=` | Exactly equal, or not. | `jira.type == Story` |
| `contains` | The list has this item. | `jira.labels contains approved` |
| `contains_any` | The list has at least one of these items. | `jira.labels contains_any [bug, feature]` |
| `excludes` | The list does not have this item. | `jira.labels excludes blocked` |
| `matches` | Text, or a list item, fits a pattern. | `github.labels matches "*-risk"` |

Patterns ignore upper and lower case. `*` matches any text, `?` matches one
character, and `[abc]` matches one character from the set. Other text
comparisons care about case. Put text with spaces or pattern symbols in quotes,
like `"Spike: *"`.

Use several rules to mean "and". Use `contains_any` for "or" within one list.
You cannot write free-form true/false logic, compare one attribute with another,
or run code from a policy.

## Rules about pull requests

Ticket attributes are checked once for the ticket. PR attributes, such as
`github.labels` and `github.approvals`, are checked for **every** linked PR. If
one PR fails, the gate fails and names that PR.

If a ticket has no linked PRs, the per-PR rules are skipped. To insist on at
least one PR, add a separate rule: `github.prs >= 1`. That one counts PRs for
the whole ticket.

The condition can use Jira while the requirement applies to each PR:

```yaml
gates:
  security_review:
    - if: github.approvals >= 1
      by: [alice]
      when: jira.labels contains security
```

One rule can use PR attributes from one GitHub connection only. Mixing PR
attributes from two GitHub connections in one rule is an error, because there is
no way to pair their PRs. Write two rules instead.

## People and connections

```yaml
# people.yml — replace these made-up people with your own
alice:
  name: Alice
  jira: example-alice-account-id
  github: example-alice
```

`by: [alice]` uses Alice's account for whichever connection the `if` uses. Every
person you name in a rule needs an account for that connection. `name` is
optional and is only used for display.

You can connect more than one GitHub organisation:

```yaml
# providers.yml — in addition to your Jira connection
github_client:
  provider: github
  org: example-client
  lookback: 90d
  auth:
    token: ${CLIENT_GITHUB_TOKEN}
```

Then write `github_client.approvals` in rules. People named in `by` rules for
that connection need a `github_client` account in `people.yml`. Only one Jira
connection is supported.

Want something to copy? See [the examples](../example/). Want every attribute
you can use? See the [Jira provider](providers/jira.md) and [GitHub provider](providers/github.md) docs.
