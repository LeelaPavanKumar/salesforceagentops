# Security model: blast radius first

## The agent

| Control | How it is enforced |
| --- | --- |
| Read-only tools | The Go loop offers exactly four tools (`get_findings`, `list_changed_components`, `read_source_file`, `query_dependencies`). There is no write, shell, merge or deploy tool. |
| Unknown tool calls refused | Any other tool name is refused, returned to the model as an error, and logged in the result. |
| File jail | `read_source_file` only reads under `force-app/`, rejects `..` and absolute paths, and resolves symlinks to make sure they stay inside `force-app/`. Files are capped at 64 KB. |
| Fixed SOQL | `query_dependencies` accepts only a validated API name. The query text is fixed, so the model cannot run arbitrary SOQL. |
| Budgets | Max tool calls (default 12), max tokens per turn, and a wall-clock deadline (default 60 s). |
| Prompt injection | The system prompt tells the model that file contents are untrusted data, and file contents are wrapped in `<file>` tags. More importantly, nothing the model says can change the lane or unblock a merge. |
| Advisory only | If the agent fails or times out, the lane and findings are unchanged and the PR comment says the review is unavailable. |

Tests in `internal/agent/agent_test.go` cover the allowlist refusal, the budget and the file jail, using a fake Claude API server.

## Salesforce credentials

- **Validate and deploy** use a dedicated integration user per org, authenticated with JWT (no passwords stored). Its permission set grants Modify Metadata through the API only. It cannot log in through the UI.
- **Agent dependency queries** (optional) use a separate read-only user with *View Setup and Configuration* and *API Enabled*, nothing else.
- Secrets live in GitHub Environments, so a job only gets an org's secrets when it runs in that environment, on the allowed branch, after any required approval.
- The JWT key is written to `$RUNNER_TEMP` with `umask 077` and deleted right after login.

## Workflow hardening

- Untrusted event fields (PR title, dispatch reason) are passed through `env:`, never interpolated into shell (checked by `actionlint`).
- Workflows default to `contents: read`; jobs raise permissions only where needed (auto-merge, audit push).
- PRs from forks do not get the audit job or secrets.
- `audit-append` runs are serialized (`concurrency: audit-log`) so the hash chain never forks.

## Separation of duties (SOX)

- Code review: CODEOWNERS, enforced by branch protection.
- Business sign-off: GitHub Environment reviewers from a different team, with *prevent self-review*.
- Both approver lists are written into each audit entry, and `agentops audit export` produces the evidence CSV.
