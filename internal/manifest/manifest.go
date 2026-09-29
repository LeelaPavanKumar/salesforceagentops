// Package manifest reads and writes Salesforce package.xml files.
package manifest

import (
	"encoding/xml"
	"os"
	"sort"
)

type Type struct {
	Members []string `xml:"members"`
	Name    string   `xml:"name"`
}

type Package struct {
	XMLName xml.Name `xml:"Package"`
	Xmlns   string   `xml:"xmlns,attr"`
	Types   []Type   `xml:"types"`
	Version string   `xml:"version,omitempty"`
}

func Read(path string) (Package, error) {
	var p Package
	b, err := os.ReadFile(path)
	if err != nil {
		return p, err
	}
	err = xml.Unmarshal(b, &p)
	return p, err
}

func Write(path string, p Package) error {
	p.Xmlns = "http://soap.sforce.com/2006/04/metadata"
	b, err := xml.MarshalIndent(p, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), append(b, '\n')...), 0o644)
}

// Subtract returns members in a that are not in b (wildcards in b match all).
func Subtract(a, b Package) Package {
	have := map[string]map[string]bool{}
	for _, t := range b.Types {
		if have[t.Name] == nil {
			have[t.Name] = map[string]bool{}
		}
		for _, m := range t.Members {
			have[t.Name][m] = true
		}
	}
	out := Package{Version: a.Version}
	for _, t := range a.Types {
		var keep []string
		for _, m := range t.Members {
			if m == "*" || have[t.Name]["*"] || have[t.Name][m] {
				continue
			}
			keep = append(keep, m)
		}
		if len(keep) > 0 {
			sort.Strings(keep)
			out.Types = append(out.Types, Type{Members: keep, Name: t.Name})
		}
	}
	sort.Slice(out.Types, func(i, j int) bool { return out.Types[i].Name < out.Types[j].Name })
	return out
}
