# Branching model and merge gates

```
feature/*  ──PR──▶  int  ──PR──▶  qa  ──PR──▶  uat  ──PR──▶  main (production)
                                                  ▲
hotfix/*  ────────────────────────────────PR──────┴──▶  main
                                    back-promotion PRs: main ▶ uat, qa, int
```

| Branch | Org | Gets changes when | Promote gate |
| --- | --- | --- | --- |
| `feature/*` | Developer sandbox / scratch org | Developer commits | None |
| `int` | INT sandbox | A user story is done | `pr-gate`: analyzers + lane |
| `qa` | QA sandbox | Sprint scope is ready for test | `promote`: validate + deploy (no approval) |
| `uat` | UAT sandbox | Sprint is ready for business test (about a week) | `promote`: validate, **business sign-off**, quick deploy |
| `main` | Production | Monthly release, or a hotfix | `promote`: validate with `RunLocalTests`, **business sign-off**, quick deploy |

## Branch protection (Settings → Branches)

For `int`, `qa`, `uat`, `main`:

- Require a pull request before merging, with **Require review from Code Owners** on.
- Required status checks: `pr-gate / analyze` (and `ci / test` if the PR touches the tool).
- Require branches to be up to date; require linear history.
- Do not allow bypassing the above settings.
- `int` only: allow auto-merge, and set required approvals to 0. CODEOWNERS still requires review for every path it lists, so only unowned paths (the agentic lane: reports, dashboards, list views, layouts, labels) can merge without a human.

Protect the `audit` branch so that only `github-actions[bot]` can push.

## Environments (Settings → Environments)

| Environment | Reviewers | Branches | Secrets |
| --- | --- | --- | --- |
| `business-signoff` | Business owners team; prevent self-review | any | none |
| `qa-validate`, `qa` | none | `qa` | QA org JWT secrets |
| `uat-validate` | none | `uat` | UAT org JWT secrets |
| `uat` | Business owners; prevent self-review | `uat` | UAT org JWT secrets |
| `production-validate` | none | `main` | Prod JWT secrets |
| `production` | Business owners + release manager; prevent self-review; optional wait timer | `main` | Prod JWT secrets |

Each environment needs secrets `SF_CLIENT_ID`, `SF_JWT_KEY`, `SF_USERNAME` and a variable `SF_INSTANCE_URL`.

## Why the validate/deploy split

`sf project deploy validate` runs a check-only deploy with tests against the real target org **before** anyone approves, so approvers see a green validation. After sign-off, `sf project deploy quick --job-id` deploys that exact validated job. What was approved is exactly what ships. The validated ID is valid for 10 days; if it expires, re-run the workflow.

## Hotfixes

A PR into `main` from any branch other than `uat` is a hotfix. It is always human-in-the-loop. After the production deploy succeeds, `promote` opens back-promotion PRs from `main` into `uat`, `qa` and `int` so the fix is not lost at the next release.
