# Configuration

New to Workfile? Start with [Getting started](getting-started.md).
Commands and keys are in [Using wf](usage.md).

Policy and provider files describe how your team works. Optional `config.yml`
files change presentation and personal preferences, not policy requirements.

## Files and precedence

| File | Contents | Commit it? |
| --- | --- | --- |
| `.workfile/policy.yml` | Stages, moves, gates and workspace name. | Yes |
| `.workfile/providers.yml` | Connections, scope, status mappings and credential references. | Yes, without secrets |
| `.workfile/people.yml` | Optional names and provider accounts for `by` and `--user`. | Yes |
| `.workfile/config.yml` | Optional shared `tracker` and `inbox` preferences. | Yes |
| `~/.config/workfile/config.yml` | Personal `me`, `tracker` and `inbox` defaults. | No |
| `~/.config/workfile/<workspace>/config.yml` | Private `only_mine`, `tracker` and `inbox` overrides. | No |
| `~/.config/workfile/credentials` | Secret values referenced by `${NAME}` in provider files. | No |

`<workspace>` is `name` in `policy.yml`, not the repository folder name. Without
a name, shared and global preferences still work, but private workspace settings
are not loaded. A name is at most 64 lowercase letters, digits and single dashes,
such as `example-web`.

Tracker and inbox settings inherit, highest precedence first:

1. Explicit CLI flags, such as `wf tracker --view flow`.
2. Private workspace config.
3. Shared repo config.
4. Personal global config.
5. Built-in defaults.

Set only what you want to change. Each file must contain one YAML document.
Unknown fields, wrong types and invalid settings are errors, not ignored values.
Tracker and inbox validate their own preferences before reading tickets;
`wf policy --lint` checks policy files, not these presentation settings.

Setup creates policy, providers and people files, plus private credentials.
It does not create `config.yml`. Add one yourself or copy the
[example config](../example/.workfile/config.yml).

## Shared config example

This can be committed as `.workfile/config.yml`. No setting is required:

```yaml
tracker:
  default_view: me
  columns: [key, title, status, action]
  preview:
    comments: true
    workflow: auto

inbox:
  poll: 2m
  refresh_every: 5
  full_every: 15
  snooze: 1h
```

Use the same `tracker:` and `inbox:` sections in personal and private workspace
files. Identity overrides belong only in the personal global file; `only_mine`
belongs only in the private workspace file. `inbox.agent` can be in either
private file, never the repo config.

## Tracker

| Setting | Default | Allowed values / behaviour |
| --- | --- | --- |
| `default_view` | `all`, or the first configured view | An available tab ID, including `inbox`. `--view` overrides it. |
| `columns` | `[key, title, status, assignee, action]` | A unique ordered list from those names; must include `key` and `title`. |
| `preview.comments` | `true` | `false` hides comments and skips their image downloads. |
| `preview.workflow` | `auto` | `auto` uses graphics when available; `text` uses text; `off` hides the map but keeps the stage and action summary. |
| `views` | All, Assigned to me, then each policy stage | A nonempty list of custom views replaces the ticket tabs. Inbox is always available. |

### List and board views

```yaml
tracker:
  default_view: flow
  views:
    - id: flow
      title: My board
      layout: board
      attention: color
    - id: needs-me
      title: Needs me
      filter:
        assignee: me
        states: [doing, review]
        attention: needs-me
    - id: all-work
      title: All
      layout: list
      filter:
        assignee: all
  columns: [key, title, status, assignee, action]
```

Replace `doing` and `review` with stages from your own `policy.yml`.

| View field | Default | Meaning |
| --- | --- | --- |
| `id` | Required | Unique lowercase dash-separated ID, at most 64 characters. Cannot be `all`, `me` or a policy state name. |
| `title` | Required | Nonempty, single-line tab label. |
| `layout` | `list` | `list` or `board`. Board columns follow `policy.states`. |
| `attention` | `color` | Display style: `color` uses icons and colour; `icons` omits attention colour; `off` hides attention but keeps linked PRs. |
| `filter.assignee` | `all` for lists, `me` for boards | `me`, `unassigned` or `all`. |
| `filter.origins` | All configured origins | Source names: Jira project keys or GitHub repository names. Case-insensitive. |
| `filter.states` | All stages | Exact state names from `policy.yml`, not provider status labels. |
| `filter.attention` | No attention filter | `needs-me`, `waiting`, `ready` or `unknown`. Matches checked tickets only. |

All filters must match. Views narrow the existing provider scope; they cannot
add projects, repositories or provider writes. A board defaults to your tickets,
not everyone's. `only_mine` is separate and does not narrow tracker browsing.

With custom views, `default_view` and `--view` must name one of their IDs. An
inherited `default_view` still applies, so override it when replacing its tab.
To include an unfiltered All tab, use a custom ID such as `all-work`.

An action column, attention filter or board layout enables bounded background
checks. Attention views are samples until all their active tickets are checked;
an empty result does not clear unchecked work. See
[tracker reads and sampling](usage.md#reads-sampling-and-plain-output).

### Merge rules

- Omitted scalar settings and preview fields inherit. Explicit
  `preview.comments: false` overrides `true`.
- `columns` replaces the inherited list.
- Views merge by ID across layers, keeping their order. A matching ID replaces
  that view's entire definition, including filters. A new ID appends a view.
- `views: []` is invalid. It does not reset inherited views or restore built-ins.

For example, a private workspace override can hide comments without repeating
the shared views:

```yaml
tracker:
  preview:
    comments: false
```

## Inbox

These settings apply to the Inbox tab in `wf tracker`.

| Setting | Default | Meaning |
| --- | --- | --- |
| `poll` | `30s` | Poll interval, at least `30s`. |
| `review_requests` | `false` | Include PRs awaiting your review. |
| `repositories` | Unset | Map code-host connection names to repository names within provider scope. Watch CI even on PRs you did not author. |
| `signals` | All | List the Inbox alert types to show in either scope. `[]` hides activity rows. |
| `default_scope` | `needs_attention` | Starting Inbox scope. Choose `needs_attention` or `all`. |
| `attention_signals` | Alerts that need action or evidence | Override the alert types shown under Needs attention. All signals still includes the other configured alerts. |
| `refresh_every` | `5` | Polls between rereads of known tickets. |
| `full_every` | `15` | Polls between board reads that find newly assigned tickets. |
| `snooze` | `1h` | How long `z` hides a row, unless a new blocker appears. |
| `agent` | Unset | Your private command for `a`. Cannot be set in repo config. |
| `prompts` | Built-in prompts below | Prompt text by row kind; keys merge individually across layers. |

Durations use Go notation, such as `30s`, `2m` or `1h30m`, not `1d`.
Poll counts and snooze durations must be positive when set; zero inherits the
previous value, so it does not disable polling or snooze.
When full and known-ticket refresh are both due, the full board read covers both.
Polling runs while interactive Tracker is open. Plain output reads once.

The supported signals are `checks_failed`, `checks_passed`, `ready`,
`out_of_policy`, `needs_work`, `changes_requested`, `review_requested`,
`comments`, `cannot_check` and `info`. Connection read errors are shown
even when the signal list is restricted.

```yaml
tracker:
  default_view: inbox
inbox:
  review_requests: false
  repositories:
    example-code-host: [example-api]
  default_scope: needs_attention
  signals: [checks_failed, checks_passed, ready, out_of_policy, comments]
  attention_signals: [checks_failed, ready, out_of_policy, comments]
  snooze: 1h
```

### Agent launcher

Only the person can choose a command that runs on their computer. Set `agent`
in `~/.config/workfile/config.yml` or the private workspace file:

```yaml
inbox:
  agent: rex tab -- claude {prompt}
```

This example requires Rex and Claude to be installed. Use your own launcher.
It must open its own tab or window: Workfile does not give it the inbox terminal.
Leaving `agent` unset hides `a`. Press `a` to preview the filled prompt, then Enter
to launch. The row stays in Inbox until you resolve it. The agent can change
work with its own access.

Workfile splits the command into arguments before filling placeholders. Quotes
and backslash escapes group arguments; no shell runs, and shell operators,
variable expansion and `$(...)` do not execute. Each substituted value stays
inside its original argument. Missing required placeholder values show a message
instead of launching the command.

The supported placeholders are `{key}`, `{pr}`, `{url}`, `{stage}`, `{to}`,
`{who}`, `{count}`, `{fix}` and `{prompt}`. `{prompt}` includes the filled prompt followed by Workfile context, such as the
title, links, workflow stage and next action when available.

### Prompts

These are the built-in prompts. Override any subset under `inbox.prompts` in
any config layer:

```yaml
inbox:
  prompts:
    default: "Explain what is blocking {key} and suggest the next step"
    changes_requested: "Address {who}'s review on {pr} for {key}"
    comments: "Answer the {count} new comments on {pr} for {key}"
    review_requested: "Review {pr} for {who}"
    checks_failed: "Fix the failing checks on {pr} for {key}"
    out_of_policy: "Bring {key} back into policy: {fix}"
    ready: "Move {key} to {to}"
```

Unknown prompt kinds or placeholder names are errors. Not every row supplies
every placeholder; choose prompts that fit the row kind.

## Identity

Workfile normally asks each provider who owns your token. To override that,
put `me:` in your personal global config:

```yaml
me:
  jira: example-alice-account-id
  github: example-alice
```

Keys can be connection names from `providers.yml` or provider kinds. A connection
name wins over a provider-kind fallback. Use the provider's account ID or login,
not the friendly name from `people.yml`. `--user bob` instead reads Bob's accounts
from `people.yml`.

These identities establish ticket assignment and PR ownership or requested
reviews. A review request to a team does not imply a request to each member.

## Only your tickets

Add `name: example-web` to `policy.yml`, then create
`~/.config/workfile/example-web/config.yml`:

```yaml
only_mine: true
```

Status scope reads then filter to tickets assigned to you, reducing provider
work. Named-ticket lookups still work within scope. Tracker reads everyone's
tickets and uses its own view filters. `--me` is different: it also includes
work you authored or must review, not just assigned tickets.

## Credentials

Keep secrets out of repo YAML. Reference them in `providers.yml`, such as
`token: ${EXAMPLE_GITHUB_TOKEN}`, and store the value in your environment or
private credentials file:

```text
EXAMPLE_JIRA_TOKEN=replace-with-your-token
EXAMPLE_GITHUB_TOKEN=replace-with-your-token
```

An environment value wins over the file. The file accepts blank lines, `#`
comments, leading `export` and quoted values. It does not expand variables or
run commands. On macOS and Linux, it must not be accessible to other users:

```sh
chmod 600 ~/.config/workfile/credentials
```

On Windows, restrict access through the file's security settings.
See provider guides for [Jira](providers/jira.md) and [GitHub](providers/github.md)
token permissions.

## Local state

| Data | Default path |
| --- | --- |
| Status snapshots for `--since` | `~/.local/state/workfile/<workspace>/snapshots/` |
| Inbox cursors, pending events, done/snooze state and agent-launch notes | `~/.local/state/workfile/<workspace>/inbox.json` |

For state paths, `<workspace>` is the policy name, or a hash of the `.workfile/`
path when unnamed. Named clones share the same local state location. Tracker
previews and policy evidence are cached only for its running session. A
noninteractive inbox read does not update saved state.

## Environment variables

| Variable | Effect |
| --- | --- |
| `XDG_CONFIG_HOME` | Replaces `~/.config` for Workfile's private files. |
| `XDG_STATE_HOME` | Replaces `~/.local/state` for snapshots and inbox state. |
| `WORKFILE_CONFIG_FILE` | Full path to the personal global config only; does not relocate private workspace settings. |
| `WORKFILE_CREDENTIALS_FILE` | Full path to the private credentials file. |
| `NO_COLOR` | Disables colours and selects noninteractive reports. Supported terminal hyperlinks remain on. |
| `TERM=dumb` | Disables colours, hyperlinks and interactive reports. |
| `WORKFILE_BACKGROUND=off` | Disables terminal background queries and panel tint. |
| `WORKFILE_GRAPHICS=off` | Disables terminal images. |
| `WORKFILE_GRAPHICS=on` | Forces graphics for a compatible terminal that does not report support. |
| `WORKFILE_ICONS=nerd` | Selects Nerd Font icons in setup. |
| `COLUMNS` | Overrides initial display width for printed output; minimum 20, maximum 140. Interactive screens use terminal resize messages. |

Environment variables do not change provider scope or policy requirements.
