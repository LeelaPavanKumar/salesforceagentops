// Package metadata turns changed file paths from an SFDX source-format repo
// into typed Salesforce metadata components.
package metadata

import (
	"bufio"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strings"
)

// Status of a file in the diff.
type Status string

const (
	Added    Status = "A"
	Modified Status = "M"
	Deleted  Status = "D"
)

// Change is one changed file.
type Change struct {
	Status Status `json:"status" yaml:"status"`
	Path   string `json:"path" yaml:"path"`
	Lines  int    `json:"lines,omitempty" yaml:"lines,omitempty"` // added+removed lines, if known
}

// Component is a Salesforce metadata component touched by the change.
type Component struct {
	Type   string   `json:"type"`
	Name   string   `json:"name"`
	Status Status   `json:"status"`
	Paths  []string `json:"paths"`
}

func (c Component) Key() string { return c.Type + ":" + c.Name }

// suffix -> metadata type for files whose name identifies the type.
var suffixTypes = []struct{ suffix, typ string }{
	{".cls-meta.xml", "ApexClass"},
	{".cls", "ApexClass"},
	{".trigger-meta.xml", "ApexTrigger"},
	{".trigger", "ApexTrigger"},
	{".flow-meta.xml", "Flow"},
	{".object-meta.xml", "CustomObject"},
	{".field-meta.xml", "CustomField"},
	{".validationRule-meta.xml", "ValidationRule"},
	{".recordType-meta.xml", "RecordType"},
	{".listView-meta.xml", "ListView"},
	{".profile-meta.xml", "Profile"},
	{".permissionset-meta.xml", "PermissionSet"},
	{".permissionsetgroup-meta.xml", "PermissionSetGroup"},
	{".layout-meta.xml", "Layout"},
	{".report-meta.xml", "Report"},
	{".dashboard-meta.xml", "Dashboard"},
	{".email-meta.xml", "EmailTemplate"},
	{".email", "EmailTemplate"},
	{".resource-meta.xml", "StaticResource"},
	{".labels-meta.xml", "CustomLabels"},
	{".sharingRules-meta.xml", "SharingRules"},
	{".connectedApp-meta.xml", "ConnectedApp"},
	{".namedCredential-meta.xml", "NamedCredential"},
	{".externalCredential-meta.xml", "ExternalCredential"},
	{".remoteSite-meta.xml", "RemoteSiteSetting"},
	{".settings-meta.xml", "Settings"},
	{".md-meta.xml", "CustomMetadata"},
	{".standardValueSet-meta.xml", "StandardValueSet"},
	{".globalValueSet-meta.xml", "GlobalValueSet"},
	{".flexipage-meta.xml", "FlexiPage"},
	{".tab-meta.xml", "CustomTab"},
	{".app-meta.xml", "CustomApplication"},
	{".quickAction-meta.xml", "QuickAction"},
}

// Classify maps one file path to a component type and name.
// ok is false for files that are not Salesforce metadata (README, CI, etc).
func Classify(p string) (typ, name string, ok bool) {
	p = strings.ReplaceAll(p, "\\", "/")
	base := path.Base(p)

	if strings.HasPrefix(base, "destructiveChanges") && strings.HasSuffix(base, ".xml") {
		return "DestructiveChanges", base, true
	}

	segs := strings.Split(p, "/")
	// Bundles: lwc/<name>/..., aura/<name>/..., staticresources/<name>/...
	for i, s := range segs {
		if i+2 >= len(segs) { // need <kind>/<bundle>/<file>
			break
		}
		switch s {
		case "lwc":
			return "LightningComponentBundle", segs[i+1], true
		case "aura":
			return "AuraDefinitionBundle", segs[i+1], true
		}
	}

	if !inPackageDir(segs) {
		return "", "", false
	}

	for _, st := range suffixTypes {
		if strings.HasSuffix(base, st.suffix) {
			n := strings.TrimSuffix(base, st.suffix)
			switch st.typ {
			case "CustomField", "ValidationRule", "RecordType", "ListView":
				// objects/<Object>/<kind>/<Name>.xxx-meta.xml -> Object.Name
				if obj := objectOf(segs); obj != "" {
					n = obj + "." + n
				}
			case "Report", "Dashboard", "EmailTemplate":
				// <kind>/<Folder>/<Name>
				if len(segs) >= 2 {
					n = segs[len(segs)-2] + "/" + n
				}
			}
			return st.typ, n, true
		}
	}
	return "", "", false
}

func inPackageDir(segs []string) bool {
	for _, s := range segs {
		if s == "force-app" || s == "main" || s == "default" || s == "src" {
			return true
		}
	}
	return false
}

func objectOf(segs []string) string {
	for i, s := range segs {
		if s == "objects" && i+1 < len(segs) {
			return segs[i+1]
		}
	}
	return ""
}

// Components groups changes into unique components. A component is Deleted
// only if every file of it was deleted.
func Components(changes []Change) []Component {
	byKey := map[string]*Component{}
	var order []string
	for _, ch := range changes {
		typ, name, ok := Classify(ch.Path)
		if !ok {
			continue
		}
		k := typ + ":" + name
		c, seen := byKey[k]
		if !seen {
			c = &Component{Type: typ, Name: name, Status: ch.Status}
			byKey[k] = c
			order = append(order, k)
		}
		c.Paths = append(c.Paths, ch.Path)
		if ch.Status != Deleted && c.Status == Deleted {
			c.Status = ch.Status
		}
		if ch.Status == Added && c.Status == Modified {
			c.Status = Added
		}
	}
	sort.Strings(order)
	out := make([]Component, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out
}

// GitChanges runs `git diff --numstat` and `--name-status` between base and head.
func GitChanges(repo, base, head string) ([]Change, error) {
	rng := base + "..." + head
	ns, err := exec.Command("git", "-C", repo, "diff", "--name-status", "--no-renames", rng).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-status %s: %w", rng, err)
	}
	num, err := exec.Command("git", "-C", repo, "diff", "--numstat", "--no-renames", rng).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --numstat %s: %w", rng, err)
	}
	lines := map[string]int{}
	sc := bufio.NewScanner(strings.NewReader(string(num)))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) != 3 {
			continue
		}
		var a, d int
		fmt.Sscan(f[0], &a)
		fmt.Sscan(f[1], &d)
		lines[f[2]] = a + d
	}
	var out []Change
	sc = bufio.NewScanner(strings.NewReader(string(ns)))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) < 2 {
			continue
		}
		st := Status(f[0][:1])
		if st != Added && st != Deleted {
			st = Modified
		}
		out = append(out, Change{Status: st, Path: f[1], Lines: lines[f[1]]})
	}
	return out, nil
}
