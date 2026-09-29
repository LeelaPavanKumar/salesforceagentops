// Package pipeline wires the analyzers, lane classifier and agent together.
package pipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/leelapavan/sfdc-agentops/internal/agent"
	"github.com/leelapavan/sfdc-agentops/internal/analyzers"
	"github.com/leelapavan/sfdc-agentops/internal/config"
	"github.com/leelapavan/sfdc-agentops/internal/lanes"
	"github.com/leelapavan/sfdc-agentops/internal/metadata"
)

type Options struct {
	Root        string
	Changes     []metadata.Change
	Config      config.Config
	Hotfix      bool
	UseAgent    bool
	APIKey      string
	SFTargetOrg string
}

type Result struct {
	Changes    []metadata.Change    `json:"changes"`
	Components []metadata.Component `json:"components"`
	Findings   []analyzers.Finding  `json:"findings"`
	Decision   lanes.Decision       `json:"decision"`
	Risk       int                  `json:"risk"`
	Agent      *agent.Result        `json:"agent,omitempty"`
	AgentError string               `json:"agent_error,omitempty"`
}

func Run(ctx context.Context, o Options) (*Result, error) {
	comps := metadata.Components(o.Changes)
	actx := &analyzers.Context{Root: o.Root, Changes: o.Changes, Components: comps, Config: o.Config}
	findings := analyzers.Run(actx)
	dec := lanes.Classify(lanes.Input{
		Changes: o.Changes, Components: comps, Findings: findings, Config: o.Config, Hotfix: o.Hotfix,
	})
	if findings == nil {
		findings = []analyzers.Finding{}
	}
	if comps == nil {
		comps = []metadata.Component{}
	}
	r := &Result{Changes: o.Changes, Components: comps, Findings: findings, Decision: dec, Risk: Risk(findings, dec)}

	if o.UseAgent && len(comps) > 0 {
		ws := &agent.Workspace{Root: o.Root, Components: comps, Findings: findings, Decision: dec, SFTargetOrg: o.SFTargetOrg}
		ar, err := agent.Run(ctx, o.Config.Agent, ws, o.APIKey)
		r.Agent = ar
		if err != nil {
			// The agent is advisory. Its failure never changes the lane or
			// unblocks anything; it is reported and the pipeline continues.
			r.AgentError = err.Error()
		}
	}
	return r, nil
}

// Risk is a 0-100 score for humans skimming a list of PRs. It does not gate
// anything; the lane and blocking findings do.
func Risk(fs []analyzers.Finding, d lanes.Decision) int {
	s := []int{0, 10, 25}[d.Lane]
	for _, f := range fs {
		switch f.Severity {
		case analyzers.High:
			s += 40
		case analyzers.Medium:
			s += 15
		case analyzers.Low:
			s += 5
		}
	}
	if s > 100 {
		s = 100
	}
	return s
}

// RuleIDs returns the sorted unique rule IDs in the findings (used by evals).
func RuleIDs(fs []analyzers.Finding) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range fs {
		if !seen[f.RuleID] {
			seen[f.RuleID] = true
			out = append(out, f.RuleID)
		}
	}
	sort.Strings(out)
	return out
}

// Markdown renders the PR comment.
func (r *Result) Markdown() string {
	var b strings.Builder
	b.WriteString("<!-- agentops-report -->\n")
	status := "Ready for review"
	if r.Decision.Blocked {
		status = "Blocked: fix high-severity findings"
	}
	fmt.Fprintf(&b, "## AgentOps release review\n\n**Lane:** `%s` · **Risk:** %d/100 · **Status:** %s\n\n",
		r.Decision.Lane, r.Risk, status)
	switch r.Decision.Lane {
	case lanes.Agentic:
		b.WriteString("> Agentic lane: auto-merges to `int` when all checks pass.\n\n")
	case lanes.AIAssisted:
		b.WriteString("> AI-assisted lane: needs a code-owner approval.\n\n")
	case lanes.HITL:
		b.WriteString("> Human-in-the-loop lane: needs a code-owner approval **and** business sign-off.\n\n")
	}
	b.WriteString("<details><summary>Why this lane</summary>\n\n")
	for _, reason := range r.Decision.Reasons {
		fmt.Fprintf(&b, "- %s\n", reason)
	}
	b.WriteString("\n</details>\n\n")

	fmt.Fprintf(&b, "### Analyzer findings (%d)\n\n", len(r.Findings))
	if len(r.Findings) == 0 {
		b.WriteString("None.\n\n")
	} else {
		b.WriteString("| Severity | Rule | Component | Issue | Fix |\n| --- | --- | --- | --- | --- |\n")
		for _, f := range r.Findings {
			fmt.Fprintf(&b, "| %s | %s | `%s` | %s | %s |\n", f.Severity, f.RuleID, f.Component, esc(f.Message), esc(f.Fix))
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "<details><summary>Changed components (%d)</summary>\n\n", len(r.Components))
	for _, c := range r.Components {
		fmt.Fprintf(&b, "- `%s` %s\n", c.Key(), c.Status)
	}
	b.WriteString("\n</details>\n\n")

	switch {
	case r.Agent != nil && r.Agent.Markdown != "":
		b.WriteString("---\n#### Claude review (advisory)\n\n")
		b.WriteString(r.Agent.Markdown)
		fmt.Fprintf(&b, "\n\n<sub>%s · %d in / %d out tokens · %d tool calls · %.1fs</sub>\n",
			r.Agent.Model, r.Agent.InputTokens, r.Agent.OutputTokens, len(r.Agent.ToolCalls), r.Agent.Duration.Seconds())
	case r.AgentError != "":
		fmt.Fprintf(&b, "---\n_Claude review unavailable: %s. Lane and findings above are unaffected._\n", esc(r.AgentError))
	}
	return b.String()
}

func esc(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ").Replace(s)
}
