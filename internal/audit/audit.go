// Package audit is an append-only, hash-chained JSONL log of pipeline
// decisions. Editing or deleting any line breaks the chain, which
// `agentops audit verify` detects. It is designed to be usable as SOX
// change-management evidence: who changed what, who approved it, and what
// was deployed.
package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const Genesis = "0000000000000000000000000000000000000000000000000000000000000000"

type Entry struct {
	Seq       int       `json:"seq"`
	Time      time.Time `json:"time"`
	Event     string    `json:"event"` // analyze | approve | deploy | rollback
	Repo      string    `json:"repo,omitempty"`
	PR        int       `json:"pr,omitempty"`
	Commit    string    `json:"commit,omitempty"`
	Author    string    `json:"author,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	TargetOrg string    `json:"target_org,omitempty"`
	Lane      string    `json:"lane,omitempty"`
	Blocked   bool      `json:"blocked,omitempty"`
	Findings  []string  `json:"findings,omitempty"` // RULE:severity:component
	// Approvals are kept separate on purpose: code review vs business sign-off.
	CodeApprovers     []string `json:"code_approvers,omitempty"`
	BusinessApprovers []string `json:"business_approvers,omitempty"`
	AgentModel        string   `json:"agent_model,omitempty"`
	AgentTokens       int64    `json:"agent_tokens,omitempty"`
	AgentToolCalls    int      `json:"agent_tool_calls,omitempty"`
	DeployID          string   `json:"deploy_id,omitempty"`
	Result            string   `json:"result,omitempty"` // success | failure | pending
	RunURL            string   `json:"run_url,omitempty"`
	PrevHash          string   `json:"prev_hash"`
	Hash              string   `json:"hash"`
}

// digest hashes the entry with Hash cleared. json.Marshal of a struct has a
// fixed field order, so this is canonical.
func digest(e Entry) string {
	e.Hash = ""
	b, _ := json.Marshal(e)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ReadAll loads every entry in the file (missing file = empty log).
func ReadAll(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return out, fmt.Errorf("line %d: %w", line, err)
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// Append chains e onto the log at path and writes it. It returns the stored entry.
func Append(path string, e Entry) (Entry, error) {
	all, err := ReadAll(path)
	if err != nil {
		return e, err
	}
	e.PrevHash = Genesis
	e.Seq = 1
	if n := len(all); n > 0 {
		e.PrevHash = all[n-1].Hash
		e.Seq = all[n-1].Seq + 1
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	e.Time = e.Time.UTC().Truncate(time.Second)
	e.Hash = digest(e)
	b, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return e, err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return e, err
}

// Verify checks every hash and link. It returns the number of valid entries
// and the first problem found.
func Verify(path string) (int, error) {
	all, err := ReadAll(path)
	if err != nil {
		return 0, err
	}
	prev := Genesis
	for i, e := range all {
		if e.PrevHash != prev {
			return i, fmt.Errorf("entry seq %d: prev_hash does not match the previous entry (a line was removed or reordered)", e.Seq)
		}
		if got := digest(e); got != e.Hash {
			return i, fmt.Errorf("entry seq %d: content does not match its hash (the line was edited)", e.Seq)
		}
		if i > 0 && e.Seq != all[i-1].Seq+1 {
			return i, fmt.Errorf("entry seq %d: sequence gap after %d", e.Seq, all[i-1].Seq)
		}
		prev = e.Hash
	}
	return len(all), nil
}

// ExportCSV writes an auditor-friendly change report for entries in [from, to).
func ExportCSV(w io.Writer, entries []Entry, from, to time.Time) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"seq", "time_utc", "event", "pr", "commit", "author", "branch", "target_org",
		"lane", "code_approvers", "business_approvers", "deploy_id", "result", "run_url", "hash"})
	for _, e := range entries {
		if (!from.IsZero() && e.Time.Before(from)) || (!to.IsZero() && !e.Time.Before(to)) {
			continue
		}
		_ = cw.Write([]string{strconv.Itoa(e.Seq), e.Time.Format(time.RFC3339), e.Event, strconv.Itoa(e.PR),
			e.Commit, e.Author, e.Branch, e.TargetOrg, e.Lane,
			strings.Join(e.CodeApprovers, ";"), strings.Join(e.BusinessApprovers, ";"),
			e.DeployID, e.Result, e.RunURL, e.Hash})
	}
	cw.Flush()
	return cw.Error()
}
