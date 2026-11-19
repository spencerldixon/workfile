# Phase 1b: policy as a structure you can see, lint and hand to an agent

**Status:** draft. Follows Phase 1 (five commands). Nothing here is built.

## Why

A thread by ThePrimeagen on agent-era control (state machine drawn out, prompt
one node, explicit exits, editor diagnostics tied to pipeline order) matches
Workfile's bet: workflow is explicit structure, and agents act inside it. It
points at five gaps:

1. `lint` only catches two mistakes. It does not look at the shape of the graph.
2. An agent cannot be handed one ticket's problem in one step.
3. The policy cannot be seen without a ticket. `status TICKET` draws the
   pipeline only around a ticket.
4. Exits are only partly explicit.
5. Editing `policy.yml` has no completion or diagnostics.
6. A name in `by` or `--user` cannot be turned into accounts without a bulk
   import or hand-written IDs.

All six keep the rule that Workfile never writes to Jira or GitHub. Each one
reads or prints.

## 1. Graph lint

Add rules to `Policy.Lint()` in `internal/workfile/lint.go`. Same `Finding`
shape (`rule`, `severity`, `message`). Messages say what to do.

| Rule | Severity | Fires when |
| --- | --- | --- |
| `unreachable-stage` | error | No chain of moves from the first stage reaches it. |
| `dead-end-stage` | error | A stage has no move out and is not marked `end: true`. |
| `no-end-stage` | warning | No stage is marked `end: true`. |
| `unreachable-end` | error | An end stage cannot be reached from the first stage. |
| `gate-never-satisfiable` | warning | A gate reads an attribute the stage's tracker or code host cannot supply. Needs `kinds.go`. |
| `duplicate-gate` | warning | Two gates hold identical rules. |
| `redundant-return` | warning | A return move repeats a forward move's destination. |

Not in scope: rule-level contradiction checks (`approvals >= 2` with
`approvals < 1`). That is the `when` and satisfiability work in
[agent-referee](../../proposals/agent-referee.md).

Done when each rule has a failing policy fixture and a passing one, `lint
--json` lists them, and `docs/policy.md` and `docs/usage.md` show the new
messages exactly as printed.

## 2. Agent handoff: `wf status TICKET --prompt`

Print a prompt for the person's own agent, scoped to one ticket. Output is plain
text to stdout, so it pipes (`wf status ABC-1 --prompt | pbcopy`).

It holds:

- The ticket key, title (through `clean`), current stage and target stage.
- The unmet gates, worded as actions, grouped as `groupedSteps` already groups
  them: the ticket first, then each PR once.
- The rule: change only what the steps name, then run `wf status ABC-1` again.
- The allowed moves from here, so the agent does not invent one.

It never includes provider response bodies or credentials. With no unmet gates
it says the ticket is ready and names the move.

Open question: one target (`--to review`) or all open destinations? Start with
the single next destination, plus `--to` to choose.

Done when output is the same under `NO_COLOR` and a pipe, with a golden test.

## 3. Policy view: horizontal or vertical

New: `wf policy` draws the configured stages and moves with no tickets. It
reads only the local `.workfile/`, so it needs no credentials. The open
question is orientation.

Today's rule in AGENTS.md: draw horizontally, shortcuts above, returns below,
each stage named once. That was written for ticket views with 4 to 7 stages.

| | Horizontal | Vertical |
| --- | --- | --- |
| Matches the workflow reading left to right | Yes | Reads like a list |
| Matches existing `pipeline.go` and one convention across commands | Yes | New code |
| 8+ stages on a 60 column terminal | Wraps into strips with numbered links | Fits, scrolls |
| Long stage names | Squeezed, truncated | Room for name, gates and counts |
| Showing gates on each move | Cramped | Natural, one line per gate |
| Shortcuts and returns | Arcs above and below | Side rails, left and right |
| Fits a report you scroll | Short, wide | Tall, narrow |

Likely answer: **both, for different jobs.**

- `wf policy` default is the horizontal chain, same as every other command,
  to stay consistent. It answers "what is the shape?".
- `wf policy --gates` (or a detail mode) is vertical, one stage per row, with
  the gates each move needs listed beneath. It answers "what must pass to get
  here?". Gates are text, and text lists vertically.
- Below a width threshold the default falls back to the vertical form instead
  of the numbered-strip form, which is hard to read.

Decide with a prototype before building: render the sample policy in
`docs/policy.md` and a 9-stage policy with 3 shortcuts and 2 returns, in both
forms, at 40, 60 and 100 columns. Pick on whether a person can name every move
without reading the YAML. Use the `prototype` skill and delete it after.

Constraints either way: stage order from `policy.states`, each stage named
once, only configured moves drawn, panel full width, ANSI 16 colours, plain
text under `NO_COLOR`, the 40 and 60 column width test, and a plain test.

## 4. Explicit exits

`end: true` already exists on a stage. Make it carry meaning:

- `lint` uses it for `dead-end-stage`, `no-end-stage` and `unreachable-end`.
- The policy view marks end stages (`·` done) at the right edge.
- Decide whether to allow named exits such as `abandoned: {end: true}`. A
  policy with only one end hides the "cancelled" path. Check how
  `transition.go` and `Routes` treat `end` before adding anything to the
  schema. Prefer no new fields if lint plus the view is enough.

## 5. Editor support

Publish a JSON Schema for `policy.yml`, `providers.yml` and `people.yml` next to
`docs/report.schema.json`. Generate it from the Go structs so it cannot drift,
and add a test that fails if it is stale.

- `wf lint --json` already gives diagnostics. Document wiring it into an editor
  task, with each finding carrying a line and column if the YAML loader can
  give them.
- No language server in this phase.

Done when `# yaml-language-server: $schema=...` gives completion and unknown
field errors in a common editor, and the README says how.

## 6. People: find a person in any tool

**Why.** Two moments need an identity: writing `by: [alice]`, and running
`status --user alice`. Both fail today unless `people.yml` already holds alice.
Setup tries to cover this by importing every assignable Jira user. That is a
bulk fetch, needs a permission that can be denied, and does nothing for GitHub.

**Approaches considered**

- A. `wf people` command only. Simple. Costs a second step after an error.
- B. Resolve live inside `--user` and `lint`. Hides a network call, and `by`
  rules still need the file, so two behaviours to explain.
- C. A shared resolver on each connection, used first by `wf people add`, later
  reusable by `--user`. **Chosen.** Same as A to start, no rewrite for B.

**Design**

- New optional interface in `internal/provider`, next to `tracker` and
  `codeHost`:

  ```go
  type personFinder interface {
      FindPeople(ctx context.Context, query string) ([]Person, error)
  }
  ```

  `Person` holds the connection name, the account ID in the form `people.yml`
  uses, a display name, and an email when the tool shows one. Jira and GitHub
  implement it. A connection that does not is skipped, and the command says so.
  Nothing above `provider` names a service.
- `wf people add QUERY` takes a name, display name, login or email. It makes one
  search per connection, shows candidates grouped by connection, and lets the
  person pick one per connection or skip it. It then writes the entry to
  `people.yml`. It is a Bubble Tea picker, with the standard width and plain
  tests.
- It never picks for the person. Two candidates, or none, always asks. A wrong
  account in a `by` rule is a policy hole, so a guess is not allowed.
- Email is the join key across tools where a tool exposes it, so one search can
  pre-select the same person in each tool. Otherwise the person picks per tool.
- Alias is the lowercase first word of the query or the chosen name, with a
  numbered suffix on clash, as setup does today. The person can edit it.
- `wf people` with no arguments lists who is in the file. `wf people add` for
  an existing alias adds the missing connections and changes nothing else.
- `lint` and `status --user` say `alice is not in people.yml. Run: wf people add
  alice`. A `by` rule naming a person with no account for that rule's
  connection gets the same pointer.
- Setup stops importing Jira users and no longer creates `people.yml` unless a
  person is added. Remove `SetupPeople` once nothing calls it.
- Writes only the local file. Nothing is written to Jira or GitHub.

**Limits to state in the docs**

- Jira shows email only when the profile makes it visible. GitHub shows only a
  public email, so a work address often finds nothing there. Name and login
  search still work.
- Provider text goes through `clean` before it is drawn.

**Done when:** tests use made-up responses for both providers, including no
match, several matches, and a connection with no finder. `docs/usage.md`,
`docs/policy.md` and `docs/providers/building-a-provider.md` describe the
command and the interface, and the setup steps no longer mention the import.

**Left to the builder.** The Jira and GitHub endpoints and their paging, and the
exact picker layout.

## Order

1. Graph lint (1) and exits (4) together. Small, no UI.
2. People (6). Independent of the rest, and it removes a bulk fetch from setup.
3. Policy view prototype, then decision, then build (3).
4. `--prompt` (2). Reuses `groupedSteps`.
5. Schema (5).

## Out of scope

- A language server or an interactive graph editor.
- Selecting a node in a full-screen UI. Revisit after the policy view ships.
- Any Jira or GitHub write.

## Docs and tests

Update `README.md`, `docs/usage.md` and `docs/policy.md` with sample output that
matches the code. Examples use Alice, Bob and Charlie, `example-*` accounts and
`example.com`. Run `go vet ./... && go test -race ./...`, then `make install`.
