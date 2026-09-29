// Package lanes decides who is allowed to act on a change.
//
// The lane is decided by rules only. The Claude agent may explain a lane but
// can never lower it: a model should not be able to reduce its own oversight.
package lanes

import (
	"fmt"

	"github.com/leelapavan/sfdc-agentops/internal/analyzers"
	"github.com/leelapavan/sfdc-agentops/internal/config"
	"github.com/leelapavan/sfdc-agentops/internal/metadata"
)

type Lane int

const (
	Agentic    Lane = iota // agent may auto-fix and auto-merge after checks
	AIAssisted             // engineer approves, agent reviews
	HITL                   // human decides; engineer + business sign-off
)

func (l Lane) String() string {
	switch l {
	case Agentic:
		return "agentic"
	case AIAssisted:
		return "ai-assisted"
	default:
		return "human-in-the-loop"
	}
}

func (l Lane) MarshalText() ([]byte, error) { return []byte(l.String()), nil }

func Parse(s string) (Lane, error) {
	switch s {
	case "agentic":
		return Agentic, nil
	case "ai-assisted":
		return AIAssisted, nil
	case "human-in-the-loop", "hitl":
		return HITL, nil
	}
	return HITL, fmt.Errorf("unknown lane %q", s)
}

// Base lane per metadata type. Types not listed default to AIAssisted.
var typeLane = map[string]Lane{
	"Report": Agentic, "Dashboard": Agentic, "ListView": Agentic,
	"EmailTemplate": Agentic, "StaticResource": Agentic, "CustomLabels": Agentic,
	"Layout": Agentic, "FlexiPage": Agentic, "CustomTab": Agentic,

	"ApexClass": AIAssisted, "ApexTrigger": AIAssisted,
	"LightningComponentBundle": AIAssisted, "AuraDefinitionBundle": AIAssisted,
	"Flow": AIAssisted, "CustomField": AIAssisted, "CustomObject": AIAssisted,
	"ValidationRule": AIAssisted, "RecordType": AIAssisted, "CustomMetadata": AIAssisted,
	"QuickAction": AIAssisted, "CustomApplication": AIAssisted,
	"GlobalValueSet": AIAssisted, "StandardValueSet": AIAssisted,

	"Profile": HITL, "PermissionSet": HITL, "PermissionSetGroup": HITL,
	"SharingRules": HITL, "DestructiveChanges": HITL, "ConnectedApp": HITL,
	"NamedCredential": HITL, "ExternalCredential": HITL, "RemoteSiteSetting": HITL,
	"Settings": HITL,
}

// Input to the classifier.
type Input struct {
	Changes    []metadata.Change
	Components []metadata.Component
	Findings   []analyzers.Finding
	Config     config.Config
	Hotfix     bool // PR targets main directly
}

// Decision is the lane plus the reasons, most important first.
type Decision struct {
	Lane     Lane     `json:"lane"`
	Reasons  []string `json:"reasons"`
	Blocked  bool     `json:"blocked"`
	Escalate bool     `json:"escalated"`
}

func Classify(in Input) Decision {
	d := Decision{Lane: Agentic}
	driver := ""
	for _, c := range in.Components {
		l, ok := typeLane[c.Type]
		if !ok {
			l = AIAssisted
		}
		if c.Status == metadata.Deleted {
			l = HITL
		}
		if driver == "" || l > d.Lane {
			d.Lane = l
			driver = c.Key()
			if c.Status == metadata.Deleted {
				driver += " (deleted)"
			}
		}
	}
	if len(in.Components) == 0 {
		d.Reasons = append(d.Reasons, "no Salesforce metadata changed")
	} else {
		d.Reasons = append(d.Reasons, fmt.Sprintf("riskiest component: %s -> %s", driver, d.Lane))
	}

	for _, ch := range in.Changes {
		if in.Config.IsSOXScoped(ch.Path) && d.Lane < HITL {
			d.Lane = HITL
			d.Reasons = append(d.Reasons, "SOX-scoped path changed: "+ch.Path)
			break
		}
	}

	lines := 0
	for _, ch := range in.Changes {
		lines += ch.Lines
	}
	if (in.Config.MaxComponents > 0 && len(in.Components) > in.Config.MaxComponents) ||
		(in.Config.MaxLines > 0 && lines > in.Config.MaxLines) {
		if d.Lane < HITL {
			d.Lane++
			d.Escalate = true
		}
		d.Reasons = append(d.Reasons, fmt.Sprintf("large change (%d components, %d lines) -> escalated one lane", len(in.Components), lines))
	}

	if in.Hotfix && d.Lane < HITL {
		d.Lane = HITL
		d.Reasons = append(d.Reasons, "hotfix to main is always human-in-the-loop")
	}

	if analyzers.Blocking(in.Findings) {
		d.Blocked = true
		d.Reasons = append(d.Reasons, "high-severity findings block the merge until fixed")
	}
	return d
}
