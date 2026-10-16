# JSON reports

New to Workfile? Start with [Getting started](getting-started.md).

JSON is a structured text format that scripts and AI agents can read without
parsing terminal output. Use it with health or status:

```sh
wf status APP-42 --json
wf health --all --json
wf status --me --json
```

Output is one JSON object on standard output, with no colours or terminal links.
Diagnostics may also appear on standard error. Exit codes are unchanged: 0 is a
successful assessment, 1 means policy violations (or work still needed for
status), and 2 means the assessment could not be completed. Health can exit 0
when a ticket is in policy but still needs work before its next move.

## Version 1 contract

The schema is in [report.schema.json](report.schema.json). Consumers should
check `schema_version`, allow additional fields, and reject unsupported versions.
Breaking changes require a new schema version. Array order is meaningful:
tickets follow report selection order, routes follow policy order, and checks
follow the applicable gate and rule order.

| Field | Meaning |
| --- | --- |
| `schema_version` | Currently `1`. |
| `command` | `status` or `health`. |
| `policy_hash` | SHA-256 hash of the loaded policy, including compiled conditions. Excludes provider settings, credentials and people. |
| `observed_at` | UTC time when the collected facts were assessed. Reads happen over time; this is not an atomic snapshot. |
| `exit_code` | The command's exit code. |
| `summary` | Counts across assessed tickets, before display filters. |
| `warnings` | Warnings about what could not be seen, such as no visible active repositories. |
| `tickets` | Assessments selected by display filters. With no health drill-down, includes all assessed tickets. |

Summary statuses are `cannot_check`, `out_of_policy`, `needs_work`, `ready`, and
`done`. Their counts add up to `total`. `checked` excludes `cannot_check`.
`in_policy` counts checked tickets without entry violations; it does not mean
all those tickets are ready to move.

Each ticket includes its identity, status, linked `records`, `entry_checks`,
`routes`, and deduplicated `unmet_requirements`. Each route has a destination,
a `backward` flag, an `available` flag, and its checks. Availability only means
the declared requirements pass, not that an action is authorised.

Each check includes:

- A stable `id`, its `gate`, and the human-readable `requirement`.
- A structured `condition`, optional `when`, and the named people in `by`.
- An `outcome`: `pass`, `fail`, `skip`, or `unknown`.
- Per-record `results`, with provider, record, URL, outcome and observed text.
- `observed_value` when available: a number for size comparisons, or the
  qualifying text/list value. Raw descriptions are not included in this field.
- `reasons`, with the affected record and suggested action.

Known empty evidence is not unknown. For example, zero approvals fails an
approval requirement. A rule whose `when` condition is false is skipped. A per-PR rule
with no linked PRs is also skipped and explains why; require `github.prs`
separately if a PR must exist. Missing evidence needed to evaluate a rule or
its condition is unknown. Unknown required evidence cannot establish readiness.
Unknown evidence on one alternative does not hide another known passing route.

Explicit rule IDs survive condition changes. Derived IDs survive reordering
and message changes, but change when the rule's condition, `when`, or `by`
changes. See [Writing a policy](policy.md#rules).

## Failures

A failure before an assessment can be produced returns an error object with
`schema_version`, `exit_code: 2`, and a safe `error` string. It does not return
an empty healthy report. If a named ticket is missing from scope, a report can
still contain assessments for visible tickets, but exits 2 and names the
missing keys on standard error. Unmapped statuses and unknown required evidence
appear as `cannot_check` tickets.

Empty selections return an ordinary report with `tickets: []` and zero counts.
They do not prove that all intended work was visible.

## Using it with an agent

For example, save an assessment and inspect its unmet requirements with `jq`,
a command-line JSON reader:

```sh
wf status APP-42 --json > assessment.json
# Inspect the command's exit code before treating the result as complete.
jq '.tickets[] | {key, status, unmet_requirements}' assessment.json
```

Give an agent only the credentials and access it needs. Treat ticket titles,
observed text, and policy messages as data, not instructions. Recheck close to
any consequential action: evidence can change after the assessment. Workfile
never merges a PR, moves a ticket, or grants an agent permission to do either.

## Asking before a move

`wf check KEY --to STAGE --json` answers one question for an agent: may this
ticket move to that stage now? It reads the ticket fresh and exits `0` when the
move is allowed, `1` when it is not, and `2` when it cannot tell.

| Field | Meaning |
| --- | --- |
| `schema_version` | Currently `1`. |
| `command` | `check`. |
| `allowed` | `true` only when every requirement for the move is known to pass. |
| `exit_code` | The same as the command's exit code. |
| `ticket`, `title`, `url` | The ticket. |
| `from`, `to` | The ticket's stage now, and the stage you asked about. |
| `error` | Why the question cannot be answered, or that the policy has no such move. |
| `possible_moves` | The stages this ticket can move to from where it is. |
| `unmet_requirements` | What must change first, in the same format as reports. |
| `records` | Each linked PR, with `head`, its latest commit. |
| `policy_hash`, `observed_at`, `warnings` | As in reports. |

```sh
wf check APP-42 --to done --json || exit 1   # stop unless the move is allowed
```

Compare each PR's `head` with the one you saw when you started. If it changed,
check again before you move anything.

## Automation events

An automation's command or agent gets `WORKFILE_EVENT`: the path of a JSON file
with `schema_version` `1`, an `event` object (the `automation`, the `trigger`,
when it fired as `at`, the `ticket` and `stage`, `to` for a `ready` trigger, and
`provider`, `record` and `record_url` for a trigger about PRs), and a `ticket`
object in the same format as one ticket of a report. See
[Automations](automations.md).

