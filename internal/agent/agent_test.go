package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/leelapavan/sfdc-agentops/internal/config"
)

// fakeClaude replays scripted responses and records each request body.
type fakeClaude struct {
	mu        sync.Mutex
	responses []string
	requests  []map[string]any
}

func (f *fakeClaude) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, _ := io.ReadAll(r.Body)
	var req map[string]any
	_ = json.Unmarshal(b, &req)
	f.requests = append(f.requests, req)
	i := len(f.requests) - 1
	if i >= len(f.responses) {
		i = len(f.responses) - 1
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, f.responses[i])
}

func toolUse(id, name, input string) string {
	return `{"id":"msg_` + id + `","type":"message","role":"assistant","model":"test","content":[{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":` + input + `}],"stop_reason":"tool_use","usage":{"input_tokens":100,"output_tokens":10}}`
}

const final = `{"id":"msg_end","type":"message","role":"assistant","model":"test","content":[{"type":"text","text":"### Summary\nLooks fine."}],"stop_reason":"end_turn","usage":{"input_tokens":200,"output_tokens":50}}`

func run(t *testing.T, f *fakeClaude, ws *Workspace, budget int) *Result {
	t.Helper()
	srv := httptest.NewServer(f)
	defer srv.Close()
	cfg := config.Default().Agent
	cfg.MaxToolCalls = budget
	res, err := Run(context.Background(), cfg, ws, "test-key", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func TestLoopRunsToolsAndReturnsReview(t *testing.T) {
	f := &fakeClaude{responses: []string{toolUse("t1", "get_findings", `{}`), final}}
	res := run(t, f, &Workspace{Root: t.TempDir()}, 12)
	if !strings.Contains(res.Markdown, "Looks fine") {
		t.Fatalf("markdown = %q", res.Markdown)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Refused {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}
	if res.InputTokens != 300 || res.OutputTokens != 60 {
		t.Fatalf("usage = %d/%d", res.InputTokens, res.OutputTokens)
	}
	// Only allowlisted tools are offered to the model.
	tools := f.requests[0]["tools"].([]any)
	if len(tools) != 4 {
		t.Fatalf("offered %d tools, want 4", len(tools))
	}
}

func TestUnknownToolIsRefused(t *testing.T) {
	f := &fakeClaude{responses: []string{toolUse("t1", "deploy_to_production", `{}`), final}}
	res := run(t, f, &Workspace{Root: t.TempDir()}, 12)
	if len(res.ToolCalls) != 1 || !res.ToolCalls[0].Refused {
		t.Fatalf("expected refused call, got %+v", res.ToolCalls)
	}
}

func TestBudgetIsEnforced(t *testing.T) {
	f := &fakeClaude{responses: []string{
		toolUse("t1", "get_findings", `{}`),
		toolUse("t2", "get_findings", `{}`),
		toolUse("t3", "get_findings", `{}`),
		final,
	}}
	res := run(t, f, &Workspace{Root: t.TempDir()}, 2)
	if len(res.ToolCalls) != 3 || !res.ToolCalls[2].Refused || res.ToolCalls[1].Refused {
		t.Fatalf("expected third call refused, got %+v", res.ToolCalls)
	}
}

func TestReadSourceFileIsJailed(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "force-app"), 0o755)
	os.WriteFile(filepath.Join(root, "force-app", "A.cls"), []byte("class A {}"), 0o644)
	os.WriteFile(filepath.Join(root, "secrets.env"), []byte("KEY=1"), 0o644)
	os.Symlink(filepath.Join(root, "secrets.env"), filepath.Join(root, "force-app", "link.cls"))
	ws := &Workspace{Root: root}

	if out, err := readSourceFile(context.Background(), ws, json.RawMessage(`{"path":"force-app/A.cls"}`)); err != nil || !strings.Contains(out, "class A") {
		t.Fatalf("allowed read failed: %q %v", out, err)
	}
	for _, p := range []string{"secrets.env", "../etc/passwd", "force-app/../secrets.env", "/etc/passwd"} {
		if _, err := readSourceFile(context.Background(), ws, json.RawMessage(`{"path":"`+p+`"}`)); err == nil {
			t.Errorf("read of %q should be refused", p)
		}
	}
	// A symlink inside force-app that points outside it is refused.
	if _, err := readSourceFile(context.Background(), ws, json.RawMessage(`{"path":"force-app/link.cls"}`)); err == nil {
		t.Error("symlink escaping force-app should be refused")
	}
}
