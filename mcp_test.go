package main

import (
	"encoding/json"
	"testing"
)

// These exercise the hand-rolled JSON-RPC dispatch directly, so the MCP protocol layer is
// guarded in CI rather than only by manual curl calls.

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMCPInitialize(t *testing.T) {
	res, rerr := dispatchMCP("initialize", rawJSON(t, map[string]any{"protocolVersion": "2025-06-18"}))
	if rerr != nil {
		t.Fatalf("rpc error: %v", rerr)
	}
	m := res.(map[string]any)
	if m["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v", m["protocolVersion"])
	}
	if m["serverInfo"].(map[string]any)["name"] != "cboard" {
		t.Errorf("serverInfo = %#v", m["serverInfo"])
	}
	if _, ok := m["capabilities"].(map[string]any)["tools"]; !ok {
		t.Error("missing tools capability")
	}
}

func TestMCPToolsListCoversRegistry(t *testing.T) {
	res, rerr := dispatchMCP("tools/list", nil)
	if rerr != nil {
		t.Fatal(rerr)
	}
	got := map[string]bool{}
	for _, tl := range res.(map[string]any)["tools"].([]map[string]any) {
		got[tl["name"].(string)] = true
	}
	for _, want := range []string{"board_snapshot", "create_ticket", "move_card", "next_card",
		"get_card", "set_result", "read_file", "list_projects", "get_project"} {
		if !got[want] {
			t.Errorf("tools/list missing %q", want)
		}
	}
	if len(got) != len(mcpTools) {
		t.Errorf("tools/list returned %d tools, registry has %d", len(got), len(mcpTools))
	}
}

func TestMCPToolsCallCreatesCard(t *testing.T) {
	newTestBoard(t)
	params := rawJSON(t, map[string]any{
		"name":      "create_ticket",
		"arguments": map[string]any{"title": "Via MCP"},
	})
	res, rerr := dispatchMCP("tools/call", params)
	if rerr != nil {
		t.Fatal(rerr)
	}
	m := res.(map[string]any)
	if m["isError"] == true {
		t.Fatalf("tool reported error: %#v", m)
	}
	// content[0].text is the JSON-encoded tool result
	var out map[string]any
	text := m["content"].([]map[string]any)[0]["text"].(string)
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("content not JSON: %v", err)
	}
	if out["id"] != "001-via-mcp" {
		t.Errorf("created id = %v", out["id"])
	}
	// and it's really on the board
	if !isDir(mustJoin("kanban", "planning", "001-via-mcp")) {
		t.Error("card folder not created")
	}
}

func TestMCPToolErrorIsReportedNotThrown(t *testing.T) {
	newTestBoard(t)
	params := rawJSON(t, map[string]any{
		"name":      "move_card",
		"arguments": map[string]any{"id": "does-not-exist", "to": "ready"},
	})
	res, rerr := dispatchMCP("tools/call", params)
	if rerr != nil {
		t.Fatalf("a failing tool should not be a JSON-RPC error, got %v", rerr)
	}
	if res.(map[string]any)["isError"] != true {
		t.Errorf("expected isError:true, got %#v", res)
	}
}

func TestMCPUnknownMethod(t *testing.T) {
	_, rerr := dispatchMCP("no/such/method", nil)
	if rerr == nil || rerr.Code != -32601 {
		t.Errorf("expected method-not-found (-32601), got %#v", rerr)
	}
}
