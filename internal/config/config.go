// Package config loads agentops.yaml.
package config

import (
	"errors"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	// Lowest API version allowed on changed metadata (warning below it).
	APIVersionFloor float64 `yaml:"apiVersionFloor"`
	// API versions at or below this are retired by Salesforce (blocking).
	APIVersionRetired float64 `yaml:"apiVersionRetired"`
	// Path globs (path.Match against the repo-relative path, "**" allowed as a
	// prefix) whose changes are always human-in-the-loop.
	SOXScoped []string `yaml:"soxScoped"`
	// Escalate one lane when a PR touches more components / lines than this.
	MaxComponents int `yaml:"maxComponents"`
	MaxLines      int `yaml:"maxLines"`
	// Optional file listing components that already exist in the org but not in
	// the repo (one "Type:Name" per line), e.g. managed-package fields.
	OrgInventory string `yaml:"orgInventory"`
	Agent        Agent  `yaml:"agent"`
}

type Agent struct {
	Model         string `yaml:"model"`
	MaxToolCalls  int    `yaml:"maxToolCalls"`
	MaxTokens     int64  `yaml:"maxTokens"`
	TimeoutSecond int    `yaml:"timeoutSeconds"`
}

func Default() Config {
	return Config{
		APIVersionFloor:   58.0,
		APIVersionRetired: 30.0,
		MaxComponents:     25,
		MaxLines:          400,
		Agent: Agent{
			Model:         "claude-sonnet-5",
			MaxToolCalls:  12,
			MaxTokens:     4096,
			TimeoutSecond: 60,
		},
	}
}

// Load reads path; a missing file returns defaults.
func Load(p string) (Config, error) {
	c := Default()
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return c, nil
}

// IsSOXScoped reports whether a repo-relative path matches a soxScoped glob.
func (c Config) IsSOXScoped(p string) bool {
	for _, g := range c.SOXScoped {
		if MatchGlob(g, p) {
			return true
		}
	}
	return false
}

// MatchGlob matches a repo-relative path against a glob where "*" matches
// within one path segment and "**" matches any number of segments.
func MatchGlob(glob, p string) bool {
	var re strings.Builder
	re.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; c {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				i++
				if i+1 < len(glob) && glob[i+1] == '/' {
					i++
					re.WriteString("(?:.*/)?") // "**/" = zero or more dirs
				} else {
					re.WriteString(".*")
				}
			} else {
				re.WriteString("[^/]*")
			}
		case '?':
			re.WriteString("[^/]")
		default:
			re.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	re.WriteString("$")
	ok, _ := regexp.MatchString(re.String(), p)
	return ok
}
