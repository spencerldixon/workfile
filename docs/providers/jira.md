# Jira provider

New to Workfile? Start with [Getting started](../getting-started.md).

Each entry in `providers.yml` is one connection. Its name is the start of every
expression that uses it, and it is the name you give to `wf test`.

The `provider` setting says which provider to use. If you leave it out, the
connection's name is used. Keep your real secrets in your environment or your
personal credentials file. Workfile refuses secrets written straight into
`auth`. A token can only read what its account is allowed to see.

```yaml
jira:
  site: example-team.atlassian.net
  scope: project = APP AND (statusCategory != Done OR resolved >= -14d)
  statuses:
    todo: To Do
    doing: In Progress
    review: [In Review, Review]
    done: Done
  auth:
    email: ${EXAMPLE_JIRA_EMAIL}
    token: ${EXAMPLE_JIRA_TOKEN}
```

## Jira Cloud

Make a Jira API token with scopes in your
[Atlassian account](https://id.atlassian.com/manage-profile/security/api-tokens),
and use the email address of that account. Workfile needs permission to read Jira
work (`read:jira-work`) and to read who you are (`read:jira-user`). The second is
used by `wf test` and by `--me`. If your organisation uses finer-grained scopes,
grant the matching permissions to search, to read issue change history, and to
read the current user. These guides help:
[Atlassian's token guide](https://support.atlassian.com/atlassian-account/docs/manage-api-tokens-for-your-atlassian-account/)
and [REST authentication](https://developer.atlassian.com/cloud/jira/platform/basic-auth-for-rest-apis/).

| Setting | What it means |
| --- | --- |
| `site` | Required. Your Jira Cloud address, such as `example-team.atlassian.net`. |
| `scope` | Required. A JQL search that picks the tickets to check. Used by health, status, and when you name tickets. |
| `statuses` | Required. Maps each stage in your policy to one or more Jira status names. Case does not matter. |
| `auth.email` | The name of the credential that holds your account email. |
| `auth.token` | The name of the credential that holds your Jira API token. |

Every Jira status must map to one stage. A status that is not mapped is
reported as an error. If some work should not be checked, leave it out of your
search:

```yaml
scope: >-
  project = APP
  AND status not in (Backlog, "On hold")
  AND (statusCategory != Done OR resolved >= -14d)
```

Workfile adds `ORDER BY updated DESC` unless your search already has an order.
It looks up your site's cloud ID, then talks to Atlassian's gateway at
`api.atlassian.com/ex/jira/<cloudId>`. It uses Jira v3 search, and reads label
history in bulk when a `by` rule needs to know who added a label. The cloud ID
lookup needs no login. Your token is only sent to the gateway. Ticket links
point to your Jira site.

### Ticket attributes

If you renamed your Jira connection, use that name instead of `jira`.

| Attribute | What it holds |
| --- | --- |
| `jira.summary` | The ticket title. |
| `jira.description` | The description as plain text, with formatting removed. |
| `jira.type` | The issue type, such as `Story` or `Bug`. |
| `jira.project` | The project key, such as `APP`. |
| `jira.assignee` | The assignee's display name. Empty if nobody is assigned. |
| `jira.labels` | The current labels. Works with `by`. |
| `jira.headings` | Headings in the description, plus short paragraphs written fully in bold. |

Description length counts characters. Mentions and emoji count as their text
when Jira gives it. A bold-only paragraph of up to 80 characters counts as a
heading, because teams often use those as section titles.

For label rules with `by`, Workfile reads the label history to find who added
each current label most recently. Labels added when the ticket was created might
have no recorded person. Those labels can never satisfy a `by` rule. Put Jira
account IDs in `people.yml`, not names or emails. A person's Jira profile address
usually has their account ID after `/people/`.

```yaml
gates:
  sign_off:
    - if: jira.labels contains accepted
      by: [alice]
```

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
