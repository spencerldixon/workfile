# Proposal: Workfile as the referee for agents

**Status:** `wf prove` is built (see [Prove](../usage.md#prove-does-the-policy-say-what-you-mean)).
The rest is an idea. It builds on [the roadmap](roadmap.md) and
[Automations](../automations.md).

## The short version

A piece of work is spread across tools. Jira holds the ticket, GitHub holds
the branch, the PR, the reviews and CI, and later tools hold the deploy. Each
tool has its own small state machine, and no tool owns the whole thing.

Workfile is **the state machine for work across those tools**. `policy.yml`
names the stages, the moves between them, and the gates each move needs. The
gates read facts from every tool. That one machine is what an AI agent needs:

- **Where is this work?** One stage, worked out from every tool, not from a
  Jira status someone forgot to update.
- **What can happen next?** The moves from here, and which are allowed now.
- **What must I do first?** The unmet gates, as actions.
- **Am I allowed to do this?** A yes or no before the agent acts.

Agents now do real things to that shared state: they merge pull requests,
move tickets and push to branches. Every tool gives them *permission* (a
token). None gives them *policy* (whether this move is allowed now, under the
team's rules). A state machine that agents act on needs a referee.

Workfile already holds the policy and checks it with no side effects. The
technically interesting step is to make it a **reference monitor**: the one
place that decides whether a move is allowed. That is an old security idea
(Anderson, 1972). A reference monitor needs three properties:

| Property | What it means for Workfile | Today | Missing |
| --- | --- | --- | --- |
| **Always invoked** | Every merge or move goes through a check. Nothing goes around it. | `wf check` exists, but nothing makes anyone call it. | `wf guard`: a hook for agents, and a required check for everyone. |
| **Tamper-evident** | A decision can be shown later, and tied to exactly what it saw. | `policy_hash`, `observed_at` and each PR's `head` are in the JSON. | Signed receipts, and `wf verify` to evaluate them again. |
| **Verifiable** | The policy itself can be checked for holes before any ticket runs. | `wf prove`: what each stage guarantees, invariants, and the path that breaks one. | Rules inside gates (`when`), and gates that can never pass. |

Each feature is useful alone. Built together, they let Workfile say
something no workflow tool can say today: *this change reached production
only through moves the policy allows, and here is the proof.*

## Why this is hard, and why that is the interesting part

Workfile decides about state it does not own. Jira and GitHub hold the truth.
Workfile can only observe them, a little late, through APIs that can fail.
Three problems follow, and each has a clean answer.

### 1. Is the policy itself sound? (`wf prove`, built)

`Assess` in `internal/workfile/evaluate.go` accepts a ticket's stage if **at
least one** forward path to it passes its gates. That is correct, because
Workfile cannot see the path a ticket really took. But it means the policy is
only as strong as its weakest path.

So for any stage, the real requirement is an OR of paths, each an AND of
gates. A team that writes `code_review` on one path may think every change to
`done` is reviewed. It is only true if *every* path to `done` has it.

The question is simple to state as a graph problem:

> Gate `g` is guaranteed at stage `s` exactly when `s` cannot be reached from
> the first stage after removing every move that requires `g`.

If `s` can still be reached, the search returns the path, and that path is the
counterexample. Run it for every gate and stage, and you get the set of gates
each stage guarantees. (This is the same idea as "available expressions" in a
compiler: intersect over every incoming path.)

It is exact and cheap. A move is forward only when its target comes later in
`states` (`Policy.Routes` in `model.go`), so forward moves can never form a
loop. `Assess` already relies on this when it walks the stages once, in
order. `prove` reads the graph the same way, so what it proves is what
`wf status` enforces.

**Our own example has a hole.** In
[`example/branching`](../../example/branching/.workfile/policy.yml):

```text
intake → triage → discovery → ready → doing → review → security → release → done
                                   refinement   linked_pull_requests  threat_model  security_signoff  merged
                                                risk_assessment
```

`code_review` is never required on this path. A ticket that passes security
sign-off can reach `done` with no approval and failing CI, and `wf status`
reports it as in policy. That is probably not what the policy's author meant.
A person reading the YAML would struggle to see it. A machine finds it at once.

What `wf prove` does:

```yaml
# .workfile/policy.yml
invariants:
  done: [code_review, merged]
```

```text
  INVARIANTS
  ✗ done requires code review
    A ticket can reach done without it:
    intake → triage → discovery → ready → doing → review → security → release → done
    Require code review on review → security or security → release.
  ✓ done requires merged
```

It suggests only moves that lead around the gate. Release → done would also
close the hole, but reviewed paths use it too, so they would be asked twice.
It runs in CI on pull requests that change `.workfile/`, so a policy change is
checked like a code change. Stages no ticket can reach were already an error
when the policy loads.

Not built yet: invariants with OR (`release: code_review or release_waiver`),
and gates no move uses.

Later, the same analysis can look inside rules: a `when` condition that makes a
rule apply only to some tickets, or two rules that can never both pass.

Why it matters more with agents: a person follows the path they expect. An
agent follows any path that is allowed. Holes that never matter for people
will be found by agents.

### 2. Was the decision made on the facts that were acted on? (receipts)

Workfile reads facts over a few seconds, then decides. An agent acts after
that. In between, someone can push a commit, and the approval no longer
covers the code that gets merged. This is the classic time-of-check to
time-of-use problem.

The answer is to tie each decision to the versions it saw, then make the
action conditional on them:

- `wf check` already reports each PR's `head` commit.
- GitHub's merge API takes the expected head commit and refuses if it moved
  (`gh pr merge --match-head-commit <sha>`). That is a compare-and-swap. The
  agent merges only the code that was checked.
- Jira has no such condition on a transition. There, the agent checks, moves,
  then checks again, and reports a ticket that changed in between.

A **receipt** records the decision:

```json
{
  "ticket": "APP-71",
  "to": "done",
  "verdict": "allowed",
  "policy_hash": "sha256:…",
  "policy_commit": "4f2c1e9",
  "observed_at": "2026-10-27T10:14:03Z",
  "records": [{ "id": "web#207", "head": "a91b3c0" }],
  "facts": { "…": "the evidence each rule read" },
  "signature": "…"
}
```

`wf verify receipt.json` loads the policy at `policy_commit`, evaluates the
recorded facts again, and checks the signature and the hash. This works
because evaluation is already a pure function: `internal/workfile` has no
network and no terminal code. The same facts and the same policy always give
the same answer. That property is the foundation, and it must be kept.

Be honest about what a receipt proves. It shows that *this* Workfile, with
*this* policy, read *these* facts and decided *this*. It does not prove the
facts were true. Jira and GitHub cannot be asked about the past. A receipt
from a person's laptop is evidence. A receipt from a CI job or cloud runner
with its own key is much stronger evidence.

This is also what auditors ask for. SOC 2 and ISO 27001 change management
want proof that each production change was reviewed and tested. Most teams
collect that proof by hand.

### 3. Does every move go through the check? (`wf guard`)

Two places to check, for two audiences:

- **For agents, before they act.** `wf guard install claude` adds a hook that
  runs before tool calls. It catches `gh pr merge`, Jira transitions and pushes
  to protected branches, runs `wf check`, and stops the call with the unmet
  requirements if it fails. The agent gets a clear reason and can fix it.
  `wf mcp` exposes `check`, `status` and `explain` as tools, so agents can ask
  before they try.
- **For everyone, at the server.** A GitHub Action runs `wf check --to done`
  as a required status check. GitHub then refuses the merge, for people and
  agents alike. This is the only place that is really always invoked.

Neither breaks the rule that Workfile never writes to Jira or GitHub. Workfile
answers yes or no. The hook or the CI job does the blocking.

## What ties it together

The three features share one thing: a small, deterministic function from
`(policy, facts)` to a decision. `prove` reasons about it over all possible
facts. Receipts record one call to it. `guard` makes sure it is called.

That is why Workfile should not guess. Linking a PR to a ticket by text
similarity, or a gate judged by a language model, gives a different answer on
different days. A referee whose answer changes cannot be proved, replayed or
trusted. If inference is ever added, it can suggest links, but it must never
make a gate pass.

## What this is not

- Not another dashboard. Agents can build those from `--json`.
- Not an agent. Workfile decides. Agents and scripts act.
- Not a replacement for Jira or GitHub permissions. Tokens say who *can* act.
  Workfile says whether the move is *allowed now*.

## A two-minute demo

1. Open a policy pull request. `wf prove` comments with the path that skips
   `code_review`. Fix it.
2. An agent tries to merge a PR with no approval. The hook stops it:
   "Cannot move APP-71 to done yet: get at least 1 approval on web#207."
3. A reviewer approves. The agent runs `wf check`, merges with
   `--match-head-commit`, and saves the receipt.
4. Someone pushes to the branch just before merge. The merge is refused,
   because the checked commit is no longer the head.
5. `wf verify` on last month's receipts: every change to `done` passed
   `code_review` on the commit that shipped.

## Build order

1. **`wf guard` and `wf mcp`.** Small, and the demo is clear. Uses `wf check`
   as it is.
2. **`wf prove`.** Built: guarantees per stage, `invariants:` in
   `policy.yml`, the shortest bypass and the moves that close it, `--json`.
   Next: a CI example, and looking inside `when` conditions.
3. **Receipts and `wf verify`.** Record the facts each rule read, sign with
   the person's SSH key first (as `git` does), then a CI identity.
4. **Agent-aware facts.** Whether a PR's author or approver is a person or an
   agent, so a policy can say "an agent's PR needs a human approval".

## Open questions

- Should `invariants:` live in `policy.yml`, or should `wf prove` only report
  what each stage guarantees and let the team decide?
- Returns (backward moves) are left out of the analysis, as `Assess` leaves
  them out. Is that right for every team, or should `prove` also report
  returns that need no gate?
- How much evidence goes in a receipt? Descriptions can hold private text.
  Store a hash of them, not the text.
- Confirm that Jira's transition API has no precondition we can use.
- Which signing scheme: SSH signatures, Sigstore, or both?
