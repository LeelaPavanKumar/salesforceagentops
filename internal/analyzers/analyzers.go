// Package analyzers holds fast, deterministic checks that run before the
// Claude agent. They never call a model and never touch an org.
package analyzers

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/leelapavan/sfdc-agentops/internal/config"
	"github.com/leelapavan/sfdc-agentops/internal/metadata"
)

type Severity string

const (
	Low    Severity = "low"
	Medium Severity = "medium"
	High   Severity = "high" // blocks the merge in every lane
)

type Finding struct {
	RuleID    string   `json:"rule_id"`
	Severity  Severity `json:"severity"`
	Component string   `json:"component"`
	Path      string   `json:"path,omitempty"`
	Message   string   `json:"message"`
	Fix       string   `json:"fix,omitempty"`
}

// Context is everything a rule may look at.
type Context struct {
	Root       string
	Changes    []metadata.Change
	Components []metadata.Component
	Config     config.Config
	Index      *RepoIndex
}

// Rule is one check.
type Rule struct {
	ID    string
	Title string
	Run   func(*Context) []Finding
}

// Rules is the registry, in report order.
var Rules = []Rule{
	{"CONF001", "Unresolved merge conflict markers", ruleConflictMarkers},
	{"DEP001", "Reference to a component that does not exist", ruleMissingDependency},
	{"DEP002", "Reference to a component deleted in this change", ruleDeletedDependency},
	{"PERM001", "Profile changed", ruleProfileChanged},
	{"PERM002", "Duplicate entries in profile or permission set", ruleDuplicatePermEntries},
	{"DEL001", "Destructive change", ruleDestructive},
	{"API001", "API version below floor or retired", ruleAPIVersion},
	{"APEX001", "Apex without a test class", ruleApexWithoutTest},
	{"APEX002", "Hardcoded Salesforce record ID", ruleHardcodedID},
	{"FLOW001", "Flow DML without fault path", ruleFlowFaultPath},
}

// Run executes every rule and returns findings sorted by severity then rule.
func Run(ctx *Context) []Finding {
	if ctx.Index == nil {
		ctx.Index = BuildIndex(ctx.Root, ctx.Config.OrgInventory)
	}
	var all []Finding
	for _, r := range Rules {
		all = append(all, r.Run(ctx)...)
	}
	rank := map[Severity]int{High: 0, Medium: 1, Low: 2}
	sort.SliceStable(all, func(i, j int) bool {
		if rank[all[i].Severity] != rank[all[j].Severity] {
			return rank[all[i].Severity] < rank[all[j].Severity]
		}
		return all[i].RuleID < all[j].RuleID
	})
	return all
}

// Blocking reports whether any finding blocks the merge.
func Blocking(fs []Finding) bool {
	for _, f := range fs {
		if f.Severity == High {
			return true
		}
	}
	return false
}

// RepoIndex is what exists in the repo (plus the optional org inventory).
type RepoIndex struct {
	Exists    map[string]bool   // "Type:Name"
	TestFiles map[string]string // path -> lowercase content of @isTest classes
}

func BuildIndex(root, inventory string) *RepoIndex {
	idx := &RepoIndex{Exists: map[string]bool{}, TestFiles: map[string]string{}}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "node_modules" || n == ".sfdx" || n == ".sf" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if typ, name, ok := metadata.Classify(rel); ok {
			idx.Exists[typ+":"+name] = true
		}
		if strings.HasSuffix(rel, ".cls") {
			if b, err := os.ReadFile(p); err == nil {
				low := strings.ToLower(string(b))
				if strings.Contains(low, "@istest") {
					idx.TestFiles[rel] = low
				}
			}
		}
		return nil
	})
	if inventory != "" {
		ip := inventory
		if !filepath.IsAbs(ip) {
			ip = filepath.Join(root, ip)
		}
		if f, err := os.Open(ip); err == nil {
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				if l := strings.TrimSpace(sc.Text()); l != "" && !strings.HasPrefix(l, "#") {
					idx.Exists[l] = true
				}
			}
			f.Close()
		}
	}
	return idx
}

// read returns a changed file's content, or "" if it was deleted/unreadable.
func (c *Context) read(rel string) string {
	b, err := os.ReadFile(filepath.Join(c.Root, rel))
	if err != nil {
		return ""
	}
	return string(b)
}

func (c *Context) liveChanges() []metadata.Change {
	var out []metadata.Change
	for _, ch := range c.Changes {
		if ch.Status != metadata.Deleted {
			out = append(out, ch)
		}
	}
	return out
}

func (c *Context) deletedKeys() map[string]bool {
	m := map[string]bool{}
	for _, comp := range c.Components {
		if comp.Status == metadata.Deleted {
			m[comp.Key()] = true
		}
	}
	return m
}
