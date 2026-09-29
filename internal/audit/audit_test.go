package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChainDetectsTampering(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.jsonl")
	for i, ev := range []string{"analyze", "approve", "deploy"} {
		e, err := Append(p, Entry{Event: ev, PR: 42, Lane: "human-in-the-loop"})
		if err != nil {
			t.Fatal(err)
		}
		if e.Seq != i+1 {
			t.Fatalf("seq = %d, want %d", e.Seq, i+1)
		}
	}
	if n, err := Verify(p); err != nil || n != 3 {
		t.Fatalf("Verify = %d, %v; want 3, nil", n, err)
	}

	b, _ := os.ReadFile(p)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")

	// Edit a field without fixing the hash.
	edited := strings.Replace(lines[1], `"lane":"human-in-the-loop"`, `"lane":"agentic"`, 1)
	os.WriteFile(p, []byte(strings.Join([]string{lines[0], edited, lines[2]}, "\n")+"\n"), 0o644)
	if _, err := Verify(p); err == nil || !strings.Contains(err.Error(), "edited") {
		t.Fatalf("expected edit to be detected, got %v", err)
	}

	// Remove a line.
	os.WriteFile(p, []byte(lines[0]+"\n"+lines[2]+"\n"), 0o644)
	if _, err := Verify(p); err == nil || !strings.Contains(err.Error(), "removed") {
		t.Fatalf("expected removal to be detected, got %v", err)
	}
}
