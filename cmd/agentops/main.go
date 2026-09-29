// Command agentops is the CLI for the governed Salesforce AgentOps pipeline.
//
//	agentops analyze   review a change: analyzers, lane, optional Claude review
//	agentops eval      run the seeded eval cases and score the pipeline
//	agentops audit     append | verify | export the hash-chained audit log
//	agentops metrics   DORA + agent metrics from the audit log
//	agentops notify    send a Slack business sign-off request
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/leelapavan/sfdc-agentops/internal/audit"
	"github.com/leelapavan/sfdc-agentops/internal/config"
	"github.com/leelapavan/sfdc-agentops/internal/evals"
	"github.com/leelapavan/sfdc-agentops/internal/manifest"
	"github.com/leelapavan/sfdc-agentops/internal/metadata"
	"github.com/leelapavan/sfdc-agentops/internal/metrics"
	"github.com/leelapavan/sfdc-agentops/internal/notify"
	"github.com/leelapavan/sfdc-agentops/internal/pipeline"
)

const usage = `usage: agentops <command> [flags]

commands:
  analyze   review a change (analyzers, lane, optional Claude review)
  eval      run seeded eval cases
  audit     append | verify | export
  metrics   DORA and agent metrics from the audit log
  notify    Slack business sign-off request
  rollback-plan  destructive manifest for components a deploy added

run "agentops <command> -h" for flags.`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(64)
	}
	var err error
	code := 0
	switch os.Args[1] {
	case "analyze":
		code, err = cmdAnalyze(os.Args[2:])
	case "eval":
		code, err = cmdEval(os.Args[2:])
	case "audit":
		code, err = cmdAudit(os.Args[2:])
	case "metrics":
		err = cmdMetrics(os.Args[2:])
	case "notify":
		err = cmdNotify(os.Args[2:])
	case "rollback-plan":
		err = cmdRollbackPlan(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Println(usage)
	default:
		fmt.Fprintln(os.Stderr, usage)
		code = 64
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentops:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func cmdAnalyze(args []string) (int, error) {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	repo := fs.String("repo", ".", "repo root")
	base := fs.String("base", "origin/int", "base ref for git diff")
	head := fs.String("head", "HEAD", "head ref for git diff")
	changesFile := fs.String("changes", "", "JSON file of changes instead of git diff")
	cfgPath := fs.String("config", "agentops.yaml", "config file (relative to repo)")
	hotfix := fs.Bool("hotfix", false, "PR targets main directly")
	useAgent := fs.Bool("agent", false, "run the Claude review (needs ANTHROPIC_API_KEY)")
	sfOrg := fs.String("sf-org", os.Getenv("SF_TARGET_ORG"), "sf CLI alias for read-only dependency queries")
	outMD := fs.String("out-md", "", "write PR comment markdown here")
	outJSON := fs.String("out-json", "", "write full result JSON here")
	failOnBlock := fs.Bool("fail-on-block", false, "exit 2 when high-severity findings block the merge")
	auditLog := fs.String("audit-log", "", "append an analyze entry to this audit log")
	pr := fs.Int("pr", 0, "PR number (audit)")
	author := fs.String("author", "", "PR author (audit)")
	branch := fs.String("branch", "", "target branch (audit)")
	commit := fs.String("commit", "", "head commit SHA (audit)")
	runURL := fs.String("run-url", "", "CI run URL (audit)")
	_ = fs.Parse(args)

	cfg, err := config.Load(joinPath(*repo, *cfgPath))
	if err != nil {
		return 1, fmt.Errorf("config: %w", err)
	}
	var changes []metadata.Change
	if *changesFile != "" {
		b, err := os.ReadFile(*changesFile)
		if err != nil {
			return 1, err
		}
		if err := json.Unmarshal(b, &changes); err != nil {
			return 1, fmt.Errorf("changes: %w", err)
		}
	} else {
		changes, err = metadata.GitChanges(*repo, *base, *head)
		if err != nil {
			return 1, err
		}
	}

	r, err := pipeline.Run(context.Background(), pipeline.Options{
		Root: *repo, Changes: changes, Config: cfg, Hotfix: *hotfix,
		UseAgent: *useAgent, SFTargetOrg: *sfOrg,
	})
	if err != nil {
		return 1, err
	}

	md := r.Markdown()
	if *outMD != "" {
		if err := os.WriteFile(*outMD, []byte(md), 0o644); err != nil {
			return 1, err
		}
	} else {
		fmt.Println(md)
	}
	if *outJSON != "" {
		b, _ := json.MarshalIndent(r, "", "  ")
		if err := os.WriteFile(*outJSON, b, 0o644); err != nil {
			return 1, err
		}
	}
	ghOutput(map[string]string{
		"lane": r.Decision.Lane.String(), "blocked": fmt.Sprint(r.Decision.Blocked),
		"risk": fmt.Sprint(r.Risk), "components": fmt.Sprint(len(r.Components)),
	})

	if *auditLog != "" {
		e := audit.Entry{
			Event: "analyze", Repo: os.Getenv("GITHUB_REPOSITORY"), PR: *pr, Commit: *commit, Author: *author,
			Branch: *branch, Lane: r.Decision.Lane.String(), Blocked: r.Decision.Blocked, RunURL: *runURL,
			Result: "success",
		}
		for _, f := range r.Findings {
			e.Findings = append(e.Findings, f.RuleID+":"+string(f.Severity)+":"+f.Component)
		}
		if r.Agent != nil {
			e.AgentModel = r.Agent.Model
			e.AgentTokens = r.Agent.InputTokens + r.Agent.OutputTokens
			e.AgentToolCalls = len(r.Agent.ToolCalls)
		}
		if _, err := audit.Append(*auditLog, e); err != nil {
			return 1, fmt.Errorf("audit: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "lane=%s blocked=%v risk=%d findings=%d\n", r.Decision.Lane, r.Decision.Blocked, r.Risk, len(r.Findings))
	if *failOnBlock && r.Decision.Blocked {
		return 2, nil
	}
	return 0, nil
}

func cmdEval(args []string) (int, error) {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	dir := fs.String("dir", "evals/cases", "cases directory")
	useAgent := fs.Bool("agent", false, "also run the Claude review and report tokens/latency")
	outMD := fs.String("out-md", "", "write results markdown here")
	_ = fs.Parse(args)

	cases, err := evals.Load(*dir)
	if err != nil {
		return 1, err
	}
	s := evals.Run(context.Background(), cases, config.Default(), *useAgent, "")
	md := s.Markdown(*useAgent)
	fmt.Println(md)
	if *outMD != "" {
		_ = os.WriteFile(*outMD, []byte(md), 0o644)
	}
	if s.Passed != len(s.Cases) {
		return 1, fmt.Errorf("%d of %d eval cases failed", len(s.Cases)-s.Passed, len(s.Cases))
	}
	return 0, nil
}

func cmdAudit(args []string) (int, error) {
	if len(args) == 0 {
		return 64, fmt.Errorf("audit needs a subcommand: append | verify | export")
	}
	fs := flag.NewFlagSet("audit "+args[0], flag.ExitOnError)
	logPath := fs.String("log", "audit/audit.jsonl", "audit log path")
	switch args[0] {
	case "append":
		event := fs.String("event", "deploy", "analyze | approve | deploy | rollback")
		pr := fs.Int("pr", 0, "PR number")
		commit := fs.String("commit", "", "commit SHA")
		author := fs.String("author", "", "author")
		branch := fs.String("branch", "", "branch")
		org := fs.String("target-org", "", "target org name (e.g. uat, production)")
		lane := fs.String("lane", "", "lane")
		deployID := fs.String("deploy-id", "", "Salesforce deploy ID")
		result := fs.String("result", "success", "success | failure | pending")
		codeAppr := fs.String("code-approvers", "", "comma-separated")
		bizAppr := fs.String("business-approvers", "", "comma-separated")
		runURL := fs.String("run-url", "", "CI run URL")
		resultJSON := fs.String("result-json", "", "fill lane/findings/agent fields from an analyze --out-json file")
		_ = fs.Parse(args[1:])
		entry := audit.Entry{
			Event: *event, Repo: os.Getenv("GITHUB_REPOSITORY"), PR: *pr, Commit: *commit, Author: *author,
			Branch: *branch, TargetOrg: *org, Lane: *lane, DeployID: *deployID, Result: *result,
			CodeApprovers: split(*codeAppr), BusinessApprovers: split(*bizAppr), RunURL: *runURL,
		}
		if *resultJSON != "" {
			if err := fillFromResult(&entry, *resultJSON); err != nil {
				return 1, err
			}
		}
		e, err := audit.Append(*logPath, entry)
		if err != nil {
			return 1, err
		}
		fmt.Printf("appended seq %d %s\n", e.Seq, e.Hash[:12])
	case "verify":
		_ = fs.Parse(args[1:])
		n, err := audit.Verify(*logPath)
		if err != nil {
			return 1, fmt.Errorf("audit log INVALID after %d good entries: %w", n, err)
		}
		fmt.Printf("audit log OK: %d entries, chain intact\n", n)
	case "export":
		from := fs.String("from", "", "YYYY-MM-DD inclusive")
		to := fs.String("to", "", "YYYY-MM-DD exclusive")
		_ = fs.Parse(args[1:])
		var f, t time.Time
		var err error
		if *from != "" {
			if f, err = time.Parse("2006-01-02", *from); err != nil {
				return 1, err
			}
		}
		if *to != "" {
			if t, err = time.Parse("2006-01-02", *to); err != nil {
				return 1, err
			}
		}
		if _, err := audit.Verify(*logPath); err != nil {
			return 1, fmt.Errorf("refusing to export an invalid log: %w", err)
		}
		entries, err := audit.ReadAll(*logPath)
		if err != nil {
			return 1, err
		}
		return 0, audit.ExportCSV(os.Stdout, entries, f, t)
	default:
		return 64, fmt.Errorf("unknown audit subcommand %q", args[0])
	}
	return 0, nil
}

func cmdMetrics(args []string) error {
	fs := flag.NewFlagSet("metrics", flag.ExitOnError)
	logPath := fs.String("log", "audit/audit.jsonl", "audit log path")
	days := fs.Int("days", 30, "window in days ending now")
	_ = fs.Parse(args)
	entries, err := audit.ReadAll(*logPath)
	if err != nil {
		return err
	}
	to := time.Now().UTC()
	fmt.Println(metrics.Compute(entries, to.AddDate(0, 0, -*days), to).Markdown())
	return nil
}

func cmdNotify(args []string) error {
	fs := flag.NewFlagSet("notify", flag.ExitOnError)
	webhook := fs.String("webhook", os.Getenv("SLACK_WEBHOOK_URL"), "Slack incoming webhook URL")
	title := fs.String("title", "", "e.g. \"PR #42 -> production\"")
	lane := fs.String("lane", "", "lane")
	risk := fs.Int("risk", 0, "risk score")
	summary := fs.String("summary", "", "one-line summary")
	approveURL := fs.String("approve-url", "", "GitHub run URL where approval waits")
	prURL := fs.String("pr-url", "", "PR URL")
	_ = fs.Parse(args)
	return notify.Slack(context.Background(), *webhook, notify.Approval{
		Title: *title, Lane: *lane, Risk: *risk, Summary: *summary, ApproveURL: *approveURL, PRURL: *prURL,
	})
}

// fillFromResult copies the analyze result into an audit entry.
func fillFromResult(e *audit.Entry, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var r pipeline.Result
	if err := json.Unmarshal(b, &r); err != nil {
		return fmt.Errorf("result json: %w", err)
	}
	e.Lane = r.Decision.Lane.String()
	e.Blocked = r.Decision.Blocked
	for _, f := range r.Findings {
		e.Findings = append(e.Findings, f.RuleID+":"+string(f.Severity)+":"+f.Component)
	}
	if r.Agent != nil {
		e.AgentModel = r.Agent.Model
		e.AgentTokens = r.Agent.InputTokens + r.Agent.OutputTokens
		e.AgentToolCalls = len(r.Agent.ToolCalls)
	}
	return nil
}

func cmdRollbackPlan(args []string) error {
	fs := flag.NewFlagSet("rollback-plan", flag.ExitOnError)
	deployed := fs.String("deployed", "", "package.xml of the deploy being rolled back")
	snapshot := fs.String("snapshot", "", "package.xml of the pre-deploy snapshot")
	out := fs.String("out", "destructiveChanges.xml", "output destructive manifest")
	_ = fs.Parse(args)
	d, err := manifest.Read(*deployed)
	if err != nil {
		return err
	}
	s, err := manifest.Read(*snapshot)
	if err != nil {
		return err
	}
	added := manifest.Subtract(d, s)
	if err := manifest.Write(*out, added); err != nil {
		return err
	}
	n := 0
	for _, t := range added.Types {
		n += len(t.Members)
	}
	fmt.Printf("%d component(s) were added by the deploy and will be deleted on rollback -> %s\n", n, *out)
	return nil
}

// ghOutput writes step outputs when running in GitHub Actions.
func ghOutput(kv map[string]string) {
	p := os.Getenv("GITHUB_OUTPUT")
	if p == "" {
		return
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	for k, v := range kv {
		fmt.Fprintf(f, "%s=%s\n", k, v)
	}
}

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func joinPath(root, p string) string {
	if strings.HasPrefix(p, "/") {
		return p
	}
	return strings.TrimSuffix(root, "/") + "/" + p
}
