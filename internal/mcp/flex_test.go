package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// LLMs sometimes send numeric MCP arguments as strings. The server must
// accept both forms and coerce the value, rather than rejecting the call
// at schema validation.

func TestCreatePriorityAsString(t *testing.T) {
	session := testServer(t)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title":    "Stringly typed priority",
			"priority": "1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var tk map[string]any
	if err := json.Unmarshal([]byte(text), &tk); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if p, _ := tk["priority"].(float64); int(p) != 1 {
		t.Errorf("priority = %v, want 1", tk["priority"])
	}
}

func TestEditPriorityAsString(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	createResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title": "Edit priority target",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := createResult.Content[0].(*mcp.TextContent).Text
	var tk map[string]any
	_ = json.Unmarshal([]byte(text), &tk)
	id, _ := tk["id"].(string)
	if id == "" {
		t.Fatal("created ticket has no id")
	}

	editResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_edit",
		Arguments: map[string]any{
			"id":       id,
			"priority": "0",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if editResult.IsError {
		t.Fatalf("edit failed: %v", editResult.Content)
	}
	var after map[string]any
	_ = json.Unmarshal([]byte(editResult.Content[0].(*mcp.TextContent).Text), &after)
	if p, _ := after["priority"].(float64); int(p) != 0 {
		t.Errorf("priority after edit = %v, want 0", after["priority"])
	}
}

func TestListPriorityOffsetLimitAsStrings(t *testing.T) {
	session := testServer(t)
	ctx := context.Background()

	// Seed three tickets.
	for _, title := range []string{"one", "two", "three"} {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "ticket_create",
			Arguments: map[string]any{
				"title":    title,
				"priority": 1,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "ticket_list",
		Arguments: map[string]any{
			"status":   "backlog",
			"priority": "1",
			"offset":   "0",
			"limit":    "2",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("list failed: %v", result.Content)
	}

	var resp struct {
		Tickets []map[string]any `json:"tickets"`
		Limit   int              `json:"limit"`
	}
	_ = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &resp)
	if resp.Limit != 2 {
		t.Errorf("limit = %d, want 2", resp.Limit)
	}
	if len(resp.Tickets) != 2 {
		t.Errorf("tickets returned = %d, want 2", len(resp.Tickets))
	}
}

func TestCreatePriorityInvalidStringRejected(t *testing.T) {
	session := testServer(t)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ticket_create",
		Arguments: map[string]any{
			"title":    "Bad priority",
			"priority": "not a number",
		},
	})
	// An unparseable string should be reported back clearly. The session call
	// itself may surface this as either IsError=true or a transport error;
	// either is acceptable as long as the server doesn't crash or silently
	// succeed.
	if err == nil && !result.IsError {
		t.Fatal("expected error for unparseable priority string, got success")
	}
}
