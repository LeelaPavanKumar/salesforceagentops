// Package metrics computes DORA and agent metrics from the audit log.
package metrics

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/leelapavan/sfdc-agentops/internal/audit"
)

type Report struct {
	From, To              time.Time
	ProdDeploys           int
	DeploysPerWeek        float64
	MedianLeadTime        time.Duration // first analyze of a PR -> its prod deploy
	LeadSamples           int
	RestoreSamples        int
	ChangeFailureRate     float64       // failed or rolled-back prod deploys / prod deploys
	MeanTimeToRestore     time.Duration // failure -> next successful prod deploy
	PRsAnalyzed           int
	LaneShare             map[string]float64
	BlockedShare          float64
	AvgAgentTokensPerPR   float64
	AgentRefusedToolCalls int
}

func isProd(e audit.Entry) bool {
	return e.TargetOrg == "production" || e.Branch == "main"
}

func Compute(entries []audit.Entry, from, to time.Time) Report {
	r := Report{From: from, To: to, LaneShare: map[string]float64{}}
	in := func(t time.Time) bool { return !t.Before(from) && t.Before(to) }

	firstAnalyze := map[int]time.Time{}
	lastLane := map[int]string{}
	blocked := map[int]bool{}
	var tokens int64
	var leads []time.Duration
	var failures int
	var restoreSum time.Duration
	var restores int
	var openFailure *time.Time

	sorted := append([]audit.Entry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	for _, e := range sorted {
		switch e.Event {
		case "analyze":
			if t, ok := firstAnalyze[e.PR]; !ok || e.Time.Before(t) {
				firstAnalyze[e.PR] = e.Time
			}
			if in(e.Time) {
				lastLane[e.PR] = e.Lane
				blocked[e.PR] = blocked[e.PR] || e.Blocked
				tokens += e.AgentTokens
			}
		case "deploy", "rollback":
			if !isProd(e) || !in(e.Time) {
				continue
			}
			if e.Event == "rollback" || e.Result == "failure" {
				if e.Event == "deploy" {
					r.ProdDeploys++
				}
				failures++
				if openFailure == nil {
					t := e.Time
					openFailure = &t
				}
				continue
			}
			if e.Result != "success" {
				continue
			}
			r.ProdDeploys++
			if t, ok := firstAnalyze[e.PR]; ok && e.PR != 0 {
				leads = append(leads, e.Time.Sub(t))
			}
			if openFailure != nil {
				restoreSum += e.Time.Sub(*openFailure)
				restores++
				openFailure = nil
			}
		}
	}

	weeks := to.Sub(from).Hours() / (24 * 7)
	if weeks > 0 {
		r.DeploysPerWeek = float64(r.ProdDeploys) / weeks
	}
	if len(leads) > 0 {
		sort.Slice(leads, func(i, j int) bool { return leads[i] < leads[j] })
		r.MedianLeadTime = leads[len(leads)/2]
		r.LeadSamples = len(leads)
	}
	if r.ProdDeploys > 0 {
		r.ChangeFailureRate = float64(failures) / float64(r.ProdDeploys)
	}
	if restores > 0 {
		r.MeanTimeToRestore = restoreSum / time.Duration(restores)
		r.RestoreSamples = restores
	}
	r.PRsAnalyzed = len(lastLane)
	if r.PRsAnalyzed > 0 {
		nb := 0
		for pr, l := range lastLane {
			r.LaneShare[l] += 1 / float64(r.PRsAnalyzed)
			if blocked[pr] {
				nb++
			}
		}
		r.BlockedShare = float64(nb) / float64(r.PRsAnalyzed)
		r.AvgAgentTokensPerPR = float64(tokens) / float64(r.PRsAnalyzed)
	}
	return r
}

func (r Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Pipeline metrics %s to %s\n\n", r.From.Format("2006-01-02"), r.To.Format("2006-01-02"))
	b.WriteString("| Metric | Value |\n| --- | --- |\n")
	fmt.Fprintf(&b, "| Production deploys | %d (%.1f / week) |\n", r.ProdDeploys, r.DeploysPerWeek)
	fmt.Fprintf(&b, "| Median lead time (first review to prod) | %s |\n", fmtDur(r.MedianLeadTime, r.LeadSamples))
	fmt.Fprintf(&b, "| Change failure rate | %.0f%% |\n", r.ChangeFailureRate*100)
	fmt.Fprintf(&b, "| Mean time to restore | %s |\n", fmtDur(r.MeanTimeToRestore, r.RestoreSamples))
	fmt.Fprintf(&b, "| PRs analyzed | %d |\n", r.PRsAnalyzed)
	for _, l := range []string{"agentic", "ai-assisted", "human-in-the-loop"} {
		fmt.Fprintf(&b, "| Lane share: %s | %.0f%% |\n", l, r.LaneShare[l]*100)
	}
	fmt.Fprintf(&b, "| PRs blocked by findings | %.0f%% |\n", r.BlockedShare*100)
	fmt.Fprintf(&b, "| Avg agent tokens per PR | %.0f |\n", r.AvgAgentTokensPerPR)
	return b.String()
}

func fmtDur(d time.Duration, samples int) string {
	if samples == 0 {
		return "n/a"
	}
	if d >= 24*time.Hour {
		return fmt.Sprintf("%.1f days", d.Hours()/24)
	}
	return fmt.Sprintf("%.1f hours", d.Hours())
}
