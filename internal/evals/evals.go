// Package evals runs seeded broken changes through the pipeline and scores
// lane accuracy and finding recall/precision.
//
// Each case is a directory under evals/cases/ containing case.yaml and the
// repo files the change leaves behind (the "head" checkout).
package evals

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/leelapavan/sfdc-agentops/internal/config"
	"github.com/leelapavan/sfdc-agentops/internal/metadata"
	"github.com/leelapavan/sfdc-agentops/internal/pipeline"
)

type Case struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Hotfix      bool              `yaml:"hotfix"`
	Changes     []metadata.Change `yaml:"changes"`
	Expect      struct {
		Lane    string   `yaml:"lane"`
		Blocked bool     `yaml:"blocked"`
		Rules   []string `yaml:"rules"`
	} `yaml:"expect"`
	dir string
}

type CaseResult struct {
	Name        string
	LaneWant    string
	LaneGot     string
	BlockedWant bool
	BlockedGot  bool
	Missed      []string // expected rules not found (false negatives)
	Extra       []string // found rules not expected (false positives)
	TP          int
	Tokens      int64
	Duration    time.Duration
	AgentError  string
}

func (c CaseResult) Pass() bool {
	return c.LaneWant == c.LaneGot && c.BlockedWant == c.BlockedGot && len(c.Missed) == 0 && len(c.Extra) == 0
}

type Summary struct {
	Cases        []CaseResult
	LaneAccuracy float64
	Recall       float64
	Precision    float64
	Passed       int
}

func Load(dir string) ([]Case, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Case
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name(), "case.yaml")
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		var c Case
		if err := yaml.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if c.Name == "" {
			c.Name = e.Name()
		}
		c.dir = filepath.Join(dir, e.Name())
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func Run(ctx context.Context, cases []Case, cfg config.Config, useAgent bool, apiKey string) Summary {
	var s Summary
	var tp, fp, fn, laneOK int
	for _, c := range cases {
		start := time.Now()
		ccfg := cfg
		if b, err := os.ReadFile(filepath.Join(c.dir, "agentops.yaml")); err == nil {
			_ = yaml.Unmarshal(b, &ccfg)
		}
		r, _ := pipeline.Run(ctx, pipeline.Options{
			Root: c.dir, Changes: c.Changes, Config: ccfg, Hotfix: c.Hotfix, UseAgent: useAgent, APIKey: apiKey,
		})
		got := pipeline.RuleIDs(r.Findings)
		cr := CaseResult{
			Name: c.Name, LaneWant: c.Expect.Lane, LaneGot: r.Decision.Lane.String(),
			BlockedWant: c.Expect.Blocked, BlockedGot: r.Decision.Blocked, Duration: time.Since(start),
			AgentError: r.AgentError,
		}
		if r.Agent != nil {
			cr.Tokens = r.Agent.InputTokens + r.Agent.OutputTokens
		}
		want := map[string]bool{}
		for _, id := range c.Expect.Rules {
			want[id] = true
		}
		have := map[string]bool{}
		for _, id := range got {
			have[id] = true
			if want[id] {
				cr.TP++
			} else {
				cr.Extra = append(cr.Extra, id)
			}
		}
		for _, id := range c.Expect.Rules {
			if !have[id] {
				cr.Missed = append(cr.Missed, id)
			}
		}
		tp += cr.TP
		fp += len(cr.Extra)
		fn += len(cr.Missed)
		if cr.LaneWant == cr.LaneGot {
			laneOK++
		}
		if cr.Pass() {
			s.Passed++
		}
		s.Cases = append(s.Cases, cr)
	}
	if n := len(cases); n > 0 {
		s.LaneAccuracy = float64(laneOK) / float64(n)
	}
	s.Recall, s.Precision = 1, 1
	if tp+fn > 0 {
		s.Recall = float64(tp) / float64(tp+fn)
	}
	if tp+fp > 0 {
		s.Precision = float64(tp) / float64(tp+fp)
	}
	return s
}

func (s Summary) Markdown(withAgent bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Eval results: %d/%d cases pass\n\n", s.Passed, len(s.Cases))
	fmt.Fprintf(&b, "| Lane accuracy | Finding recall | Finding precision |\n| --- | --- | --- |\n| %.0f%% | %.0f%% | %.0f%% |\n\n",
		s.LaneAccuracy*100, s.Recall*100, s.Precision*100)
	b.WriteString("| Case | Lane (want / got) | Blocked (want / got) | Missed | Extra |")
	if withAgent {
		b.WriteString(" Agent tokens | Time |")
	}
	b.WriteString(" Result |\n| --- | --- | --- | --- | --- |")
	if withAgent {
		b.WriteString(" --- | --- |")
	}
	b.WriteString(" --- |\n")
	for _, c := range s.Cases {
		res := "pass"
		if !c.Pass() {
			res = "**FAIL**"
		}
		fmt.Fprintf(&b, "| %s | %s / %s | %v / %v | %s | %s |", c.Name, c.LaneWant, c.LaneGot,
			c.BlockedWant, c.BlockedGot, join(c.Missed), join(c.Extra))
		if withAgent {
			fmt.Fprintf(&b, " %d | %.1fs |", c.Tokens, c.Duration.Seconds())
		}
		fmt.Fprintf(&b, " %s |\n", res)
	}
	return b.String()
}

func join(s []string) string {
	if len(s) == 0 {
		return "-"
	}
	return strings.Join(s, ", ")
}
