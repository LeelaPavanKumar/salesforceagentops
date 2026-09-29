package analyzers

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/leelapavan/sfdc-agentops/internal/metadata"
)

// ---------- CONF001: merge conflict markers ----------

var conflictRe = regexp.MustCompile(`(?m)^(<{7} |={7}$|>{7} )`)

func ruleConflictMarkers(c *Context) []Finding {
	var out []Finding
	for _, ch := range c.liveChanges() {
		if conflictRe.MatchString(c.read(ch.Path)) {
			out = append(out, Finding{
				RuleID: "CONF001", Severity: High, Component: compName(ch.Path), Path: ch.Path,
				Message: "File contains unresolved git merge conflict markers; the deploy will fail with an XML/parse error.",
				Fix:     "Resolve the conflict. For profile/permission set XML, merge by entry (field, class, object) rather than by line.",
			})
		}
	}
	return out
}

// ---------- DEP001 / DEP002: references in permsets, profiles, layouts ----------

type permFile struct {
	FieldPermissions []struct {
		Field string `xml:"field"`
	} `xml:"fieldPermissions"`
	ClassAccesses []struct {
		ApexClass string `xml:"apexClass"`
	} `xml:"classAccesses"`
	ObjectPermissions []struct {
		Object string `xml:"object"`
	} `xml:"objectPermissions"`
	RecordTypeVisibilities []struct {
		RecordType string `xml:"recordType"`
	} `xml:"recordTypeVisibilities"`
}

type layoutFile struct {
	Sections []struct {
		Columns []struct {
			Items []struct {
				Field string `xml:"field"`
			} `xml:"layoutItems"`
		} `xml:"layoutColumns"`
	} `xml:"layoutSections"`
}

type ref struct{ key, label string }

func referencesIn(c *Context, ch metadata.Change) []ref {
	typ, name, _ := metadata.Classify(ch.Path)
	body := c.read(ch.Path)
	if body == "" {
		return nil
	}
	var refs []ref
	switch typ {
	case "PermissionSet", "Profile":
		var pf permFile
		if xml.Unmarshal([]byte(body), &pf) != nil {
			return nil
		}
		for _, f := range pf.FieldPermissions {
			if isCustomLocal(f.Field) {
				refs = append(refs, ref{"CustomField:" + f.Field, "field " + f.Field})
			}
		}
		for _, a := range pf.ClassAccesses {
			if !strings.Contains(a.ApexClass, "__") { // skip managed-package classes
				refs = append(refs, ref{"ApexClass:" + a.ApexClass, "Apex class " + a.ApexClass})
			}
		}
		for _, o := range pf.ObjectPermissions {
			if isCustomLocal(o.Object) {
				refs = append(refs, ref{"CustomObject:" + o.Object, "object " + o.Object})
			}
		}
		for _, r := range pf.RecordTypeVisibilities {
			if obj, _, ok := strings.Cut(r.RecordType, "."); ok && isCustomLocal(obj) {
				refs = append(refs, ref{"RecordType:" + r.RecordType, "record type " + r.RecordType})
			}
		}
	case "Layout":
		var lf layoutFile
		if xml.Unmarshal([]byte(body), &lf) != nil {
			return nil
		}
		obj, _, _ := strings.Cut(name, "-") // Account-Account Layout
		for _, s := range lf.Sections {
			for _, col := range s.Columns {
				for _, it := range col.Items {
					if isCustomLocal(it.Field) {
						refs = append(refs, ref{"CustomField:" + obj + "." + it.Field, "field " + obj + "." + it.Field})
					}
				}
			}
		}
	}
	return refs
}

// isCustomLocal: a custom (non-managed) API name such as Invoice__c or
// Account.Tier__c. Standard names and namespaced ns__X__c are skipped.
func isCustomLocal(apiName string) bool {
	last := apiName
	if i := strings.LastIndex(apiName, "."); i >= 0 {
		last = apiName[i+1:]
	}
	return strings.HasSuffix(last, "__c") && strings.Count(last, "__") == 1
}

func ruleMissingDependency(c *Context) []Finding {
	deleted := c.deletedKeys()
	var out []Finding
	for _, ch := range c.liveChanges() {
		for _, r := range referencesIn(c, ch) {
			if c.Index.Exists[r.key] || deleted[r.key] {
				continue
			}
			out = append(out, Finding{
				RuleID: "DEP001", Severity: High, Component: compName(ch.Path), Path: ch.Path,
				Message: fmt.Sprintf("References %s, which is not in the repo or the org inventory. The deploy will fail with \"no CustomField named ... found\" or similar.", r.label),
				Fix:     "Add the missing component to this PR, or remove the reference.",
			})
		}
	}
	return out
}

func ruleDeletedDependency(c *Context) []Finding {
	deleted := c.deletedKeys()
	if len(deleted) == 0 {
		return nil
	}
	var out []Finding
	for _, ch := range c.liveChanges() {
		for _, r := range referencesIn(c, ch) {
			if deleted[r.key] {
				out = append(out, Finding{
					RuleID: "DEP002", Severity: High, Component: compName(ch.Path), Path: ch.Path,
					Message: fmt.Sprintf("References %s, which this change deletes.", r.label),
					Fix:     "Remove the reference in the same PR, and deploy the destructive change after the reference removal.",
				})
			}
		}
	}
	return out
}

// ---------- PERM001 / PERM002 ----------

func ruleProfileChanged(c *Context) []Finding {
	var out []Finding
	for _, comp := range c.Components {
		if comp.Type == "Profile" && comp.Status != metadata.Deleted {
			out = append(out, Finding{
				RuleID: "PERM001", Severity: Medium, Component: comp.Key(),
				Message: "Profile metadata changed. Profile deploys overwrite the included sections in the target org and are a common source of permission drift.",
				Fix:     "Prefer a permission set. If a profile change is needed, keep it to the entries this story needs and confirm with a branch-vs-org compare.",
			})
		}
	}
	return out
}

func ruleDuplicatePermEntries(c *Context) []Finding {
	var out []Finding
	for _, ch := range c.liveChanges() {
		typ, _, _ := metadata.Classify(ch.Path)
		if typ != "Profile" && typ != "PermissionSet" {
			continue
		}
		var pf permFile
		if xml.Unmarshal([]byte(c.read(ch.Path)), &pf) != nil {
			continue
		}
		seen := map[string]int{}
		for _, f := range pf.FieldPermissions {
			seen["field "+f.Field]++
		}
		for _, a := range pf.ClassAccesses {
			seen["class "+a.ApexClass]++
		}
		for _, o := range pf.ObjectPermissions {
			seen["object "+o.Object]++
		}
		var dups []string
		for k, n := range seen {
			if n > 1 {
				dups = append(dups, k)
			}
		}
		if len(dups) > 0 {
			sort.Strings(dups)
			out = append(out, Finding{
				RuleID: "PERM002", Severity: High, Component: compName(ch.Path), Path: ch.Path,
				Message: "Duplicate entries (usually a bad merge): " + strings.Join(dups, ", ") + ". The deploy fails with \"Duplicate value found\".",
				Fix:     "Keep one entry per field/class/object, using the most restrictive value unless the story says otherwise.",
			})
		}
	}
	return out
}

// ---------- DEL001 ----------

func ruleDestructive(c *Context) []Finding {
	var out []Finding
	for _, comp := range c.Components {
		if comp.Type == "DestructiveChanges" && comp.Status != metadata.Deleted {
			out = append(out, Finding{
				RuleID: "DEL001", Severity: Medium, Component: comp.Key(),
				Message: "Destructive manifest included. Deleted metadata cannot be restored by redeploying, and field data is lost.",
				Fix:     "Confirm a data backup and business sign-off. Deploy pre-destructive vs post-destructive changes in the right order.",
			})
		} else if comp.Status == metadata.Deleted {
			out = append(out, Finding{
				RuleID: "DEL001", Severity: Medium, Component: comp.Key(),
				Message: "Component deleted from source. Removing a file does not delete it from the org; a destructiveChanges.xml is needed.",
				Fix:     "Add a destructiveChanges manifest, or keep the component if the deletion was unintended.",
			})
		}
	}
	return out
}

// ---------- API001 ----------

var apiRe = regexp.MustCompile(`<apiVersion>\s*([0-9]+(?:\.[0-9]+)?)\s*</apiVersion>`)

func ruleAPIVersion(c *Context) []Finding {
	var out []Finding
	for _, ch := range c.liveChanges() {
		if !strings.HasSuffix(ch.Path, "-meta.xml") {
			continue
		}
		m := apiRe.FindStringSubmatch(c.read(ch.Path))
		if m == nil {
			continue
		}
		v, _ := strconv.ParseFloat(m[1], 64)
		switch {
		case v <= c.Config.APIVersionRetired:
			out = append(out, Finding{
				RuleID: "API001", Severity: High, Component: compName(ch.Path), Path: ch.Path,
				Message: fmt.Sprintf("API version %s is retired (<= %.1f). Salesforce no longer accepts calls or deploys at retired versions.", m[1], c.Config.APIVersionRetired),
				Fix:     fmt.Sprintf("Raise apiVersion to at least %.1f and re-run tests; check for behavior changes between versions.", c.Config.APIVersionFloor),
			})
		case v < c.Config.APIVersionFloor:
			out = append(out, Finding{
				RuleID: "API001", Severity: Low, Component: compName(ch.Path), Path: ch.Path,
				Message: fmt.Sprintf("API version %s is below the team floor of %.1f.", m[1], c.Config.APIVersionFloor),
				Fix:     fmt.Sprintf("Raise apiVersion to %.1f or later while the file is already being changed.", c.Config.APIVersionFloor),
			})
		}
	}
	return out
}

// ---------- APEX001 ----------

var triggerRe = regexp.MustCompile(`(?i)trigger\s+(\w+)\s+on\s+(\w+)`)

func ruleApexWithoutTest(c *Context) []Finding {
	var out []Finding
	for _, ch := range c.liveChanges() {
		isClass := strings.HasSuffix(ch.Path, ".cls")
		isTrigger := strings.HasSuffix(ch.Path, ".trigger")
		if !isClass && !isTrigger {
			continue
		}
		body := c.read(ch.Path)
		low := strings.ToLower(body)
		if isClass && strings.Contains(low, "@istest") {
			continue // it is a test
		}
		_, name, _ := metadata.Classify(ch.Path)
		needles := []string{strings.ToLower(name)}
		if isTrigger {
			if m := triggerRe.FindStringSubmatch(body); m != nil {
				needles = append(needles, strings.ToLower(m[2]))
			}
		}
		if !testMentions(c.Index.TestFiles, needles) {
			out = append(out, Finding{
				RuleID: "APEX001", Severity: High, Component: compName(ch.Path), Path: ch.Path,
				Message: "No @isTest class in the repo references this code. Production deploys need 75% org-wide coverage and every trigger needs some coverage.",
				Fix:     "Add or update a test class that exercises this code, with assertions, and include it in the PR.",
			})
		}
	}
	return out
}

func testMentions(tests map[string]string, needles []string) bool {
	for _, body := range tests {
		for _, n := range needles {
			if regexp.MustCompile(`\b` + regexp.QuoteMeta(n) + `\b`).MatchString(body) {
				return true
			}
		}
	}
	return false
}

// ---------- APEX002 ----------

// 15/18-char IDs that start with a key prefix beginning with 0 or a0 and
// contain the run of zeros real record IDs have.
var idRe = regexp.MustCompile(`['">](0[0-9A-Za-z]{14}(?:[0-9A-Za-z]{3})?|a0[0-9A-Za-z]{13}(?:[0-9A-Za-z]{3})?)['"<]`)

func ruleHardcodedID(c *Context) []Finding {
	var out []Finding
	for _, ch := range c.liveChanges() {
		if !(strings.HasSuffix(ch.Path, ".cls") || strings.HasSuffix(ch.Path, ".trigger") || strings.HasSuffix(ch.Path, ".flow-meta.xml")) {
			continue
		}
		var ids []string
		for _, m := range idRe.FindAllStringSubmatch(c.read(ch.Path), -1) {
			if strings.Contains(m[1][3:], "000") {
				ids = append(ids, m[1])
			}
		}
		if len(ids) > 0 {
			out = append(out, Finding{
				RuleID: "APEX002", Severity: Medium, Component: compName(ch.Path), Path: ch.Path,
				Message: "Hardcoded record ID(s) " + strings.Join(uniq(ids), ", ") + ". IDs differ between sandboxes and production, so this breaks after promotion.",
				Fix:     "Look the record up by DeveloperName/unique field, or store it in Custom Metadata.",
			})
		}
	}
	return out
}

// ---------- FLOW001 ----------

type flowFile struct {
	Status  string    `xml:"status"`
	Creates []flowDML `xml:"recordCreates"`
	Updates []flowDML `xml:"recordUpdates"`
	Deletes []flowDML `xml:"recordDeletes"`
}

type flowDML struct {
	Name  string    `xml:"name"`
	Fault *struct{} `xml:"faultConnector"`
}

func ruleFlowFaultPath(c *Context) []Finding {
	var out []Finding
	for _, ch := range c.liveChanges() {
		if !strings.HasSuffix(ch.Path, ".flow-meta.xml") {
			continue
		}
		var f flowFile
		if xml.Unmarshal([]byte(c.read(ch.Path)), &f) != nil {
			continue
		}
		var missing []string
		for _, group := range [][]flowDML{f.Creates, f.Updates, f.Deletes} {
			for _, e := range group {
				if e.Fault == nil {
					missing = append(missing, e.Name)
				}
			}
		}
		if len(missing) > 0 {
			out = append(out, Finding{
				RuleID: "FLOW001", Severity: Medium, Component: compName(ch.Path), Path: ch.Path,
				Message: "DML elements without a fault path: " + strings.Join(missing, ", ") + ". Users get an unhandled flow error and the admin gets an email.",
				Fix:     "Add a fault connector that logs the error and shows a friendly message or rolls back.",
			})
		}
	}
	return out
}

// ---------- helpers ----------

func compName(p string) string {
	if t, n, ok := metadata.Classify(p); ok {
		return t + ":" + n
	}
	return p
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
