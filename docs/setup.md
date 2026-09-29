# Free setup (about 1 hour)

1. **Orgs.** Sign up for three free Developer Edition orgs at developer.salesforce.com/signup: one each for QA, UAT and production. In the production one, enable Dev Hub (Setup → Dev Hub) if you want scratch orgs for feature work.
2. **JWT auth.** For each org:
   - `openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -keyout server.key -out server.crt -subj "/CN=agentops"`
   - Create an External Client App / Connected App with OAuth, *Use digital signatures* (upload `server.crt`), scopes `api refresh_token`, and pre-authorize the integration user's profile or permission set.
   - Add `SF_CLIENT_ID`, `SF_JWT_KEY` (contents of `server.key`), `SF_USERNAME` as environment secrets and `SF_INSTANCE_URL` (`https://login.salesforce.com`) as an environment variable.
3. **Repo.** Push this repo to a public GitHub repo (Actions minutes are free for public repos). Create the branches: `git push origin main:int main:qa main:uat`.
4. **Environments and protection.** Follow [branching.md](branching.md). Replace `@your-org/...` in `.github/CODEOWNERS` with your GitHub username or teams.
5. **Claude.** Add `ANTHROPIC_API_KEY` as a repository secret. Without it, everything still runs; the PR comment just has no Claude review.
6. **Slack (optional).** Create a free workspace, add an Incoming Webhook app, and store the URL as `SLACK_WEBHOOK_URL`.
7. **First deploy.** Deploy the sample `force-app` into each org once: `sf project deploy start --source-dir force-app --target-org <alias>`.

Then try the demo in the README.
