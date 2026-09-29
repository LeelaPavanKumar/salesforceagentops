package lanes

import (
	"testing"

	"github.com/leelapavan/sfdc-agentops/internal/analyzers"
	"github.com/leelapavan/sfdc-agentops/internal/config"
	"github.com/leelapavan/sfdc-agentops/internal/metadata"
)

func TestRiskiestComponentWins(t *testing.T) {
	d := Classify(Input{
		Components: []metadata.Component{
			{Type: "Report", Name: "R", Status: metadata.Modified},
			{Type: "Profile", Name: "Admin", Status: metadata.Modified},
			{Type: "ApexClass", Name: "A", Status: metadata.Modified},
		},
		Config: config.Default(),
	})
	if d.Lane != HITL {
		t.Fatalf("lane = %s, want human-in-the-loop", d.Lane)
	}
}

func TestDeleteIsAlwaysHITL(t *testing.T) {
	d := Classify(Input{
		Components: []metadata.Component{{Type: "Report", Name: "R", Status: metadata.Deleted}},
		Config:     config.Default(),
	})
	if d.Lane != HITL {
		t.Fatalf("lane = %s, want human-in-the-loop", d.Lane)
	}
}

func TestHighFindingBlocksButDoesNotChangeLane(t *testing.T) {
	d := Classify(Input{
		Components: []metadata.Component{{Type: "Report", Name: "R", Status: metadata.Modified}},
		Findings:   []analyzers.Finding{{RuleID: "CONF001", Severity: analyzers.High}},
		Config:     config.Default(),
	})
	if d.Lane != Agentic || !d.Blocked {
		t.Fatalf("got lane=%s blocked=%v, want agentic + blocked", d.Lane, d.Blocked)
	}
}
