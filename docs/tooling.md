# Mapping to Gearset, Copado and AutoRABIT

The pipeline uses the plain `sf` CLI so it runs anywhere. If a team already uses a commercial tool, `agentops` still slots in as the PR gate and audit layer.

| Capability | This repo | Gearset | Copado |
| --- | --- | --- | --- |
| Delta manifest | `sfdx-git-delta` | Built into pipeline promotions | User story metadata selection |
| Check-only + quick deploy | `sf project deploy validate` / `quick` | "Validate" then "Deploy validated package" | Validation, then deploy |
| Pre-deploy backup | `sf project retrieve` snapshot artifact | Deployment rollback | Backup / rollback snapshots |
| Approvals | GitHub Environments + Slack | Pipeline approvals | Quality gates, approvals |
| PR checks | `pr-gate` + `agentops analyze` | Gearset CI + code review (PMD) | Quality gates |

To use `agentops` with Gearset: keep Gearset for deploys, and add `pr-gate.yml` as a required status check on the Gearset pipeline branches. The lane label and the audit log work the same way.
