// Package notify posts approval requests to Slack through an incoming webhook.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Approval struct {
	Title      string // e.g. "PR #42 -> production"
	Lane       string
	Risk       int
	Summary    string
	ApproveURL string // GitHub Actions run page where the Environment approval waits
	PRURL      string
}

// Slack sends the approval request. The Slack message only links to the
// GitHub approval page; the approval itself happens in GitHub so that it is
// tied to an authenticated reviewer and recorded in the run.
func Slack(ctx context.Context, webhookURL string, a Approval) error {
	if webhookURL == "" {
		return fmt.Errorf("no Slack webhook URL configured")
	}
	text := fmt.Sprintf("*%s* needs business sign-off\nLane: `%s` · Risk: %d/100\n%s", a.Title, a.Lane, a.Risk, a.Summary)
	payload := map[string]any{
		"text": fmt.Sprintf("%s needs business sign-off", a.Title),
		"blocks": []any{
			map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": text}},
			map[string]any{"type": "actions", "elements": buttons(a)},
		},
	}
	body, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned %s", resp.Status)
	}
	return nil
}

func buttons(a Approval) []any {
	var out []any
	if a.ApproveURL != "" {
		out = append(out, button("Review and approve", a.ApproveURL, "primary"))
	}
	if a.PRURL != "" {
		out = append(out, button("View PR", a.PRURL, ""))
	}
	return out
}

func button(text, url, style string) map[string]any {
	b := map[string]any{"type": "button", "text": map[string]any{"type": "plain_text", "text": text}, "url": url}
	if style != "" {
		b["style"] = style
	}
	return b
}
