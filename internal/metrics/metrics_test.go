package metrics

import (
	"testing"
	"time"

	"github.com/leelapavan/sfdc-agentops/internal/audit"
)

func TestDORA(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	h := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Hour) }
	entries := []audit.Entry{
		{Event: "analyze", PR: 1, Lane: "agentic", Time: h(0)},
		{Event: "analyze", PR: 2, Lane: "human-in-the-loop", Time: h(1), Blocked: true},
		{Event: "deploy", PR: 1, Branch: "main", Result: "success", Time: h(48)},
		{Event: "deploy", PR: 2, Branch: "main", Result: "failure", Time: h(72)},
		{Event: "deploy", PR: 2, Branch: "main", Result: "success", Time: h(76)},
		{Event: "deploy", PR: 3, Branch: "uat", Result: "success", Time: h(80)}, // not prod
	}
	r := Compute(entries, t0, t0.AddDate(0, 0, 14))
	if r.ProdDeploys != 3 {
		t.Errorf("prod deploys = %d, want 3", r.ProdDeploys)
	}
	if r.ChangeFailureRate < 0.33 || r.ChangeFailureRate > 0.34 {
		t.Errorf("CFR = %.2f, want 0.33", r.ChangeFailureRate)
	}
	if r.MeanTimeToRestore != 4*time.Hour {
		t.Errorf("MTTR = %s, want 4h", r.MeanTimeToRestore)
	}
	if r.LeadSamples != 2 || r.MedianLeadTime != 75*time.Hour {
		t.Errorf("lead = %s over %d, want 75h over 2", r.MedianLeadTime, r.LeadSamples)
	}
	if r.LaneShare["agentic"] != 0.5 || r.BlockedShare != 0.5 {
		t.Errorf("lane share %+v blocked %.2f", r.LaneShare, r.BlockedShare)
	}
}
