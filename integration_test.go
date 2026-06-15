package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// End-to-end test that drives the real HTTP API and MCP server the way a client (a human
// dashboard or a "fake LLM") would, exercising the whole lifecycle: create, move, comment,
// approve to done, archive (files kept), delete (files gone), board context.

type client struct {
	t   *testing.T
	srv *httptest.Server
	id  int
}

func newClient(t *testing.T) *client {
	newTestBoard(t) // sets root to a seeded temp board
	srv := httptest.NewServer(newMux())
	t.Cleanup(srv.Close)
	return &client{t: t, srv: srv}
}

// api POSTs JSON to /api/* and returns the decoded response.
func (c *client) api(path string, body map[string]any) map[string]any {
	c.t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(c.srv.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		c.t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if err, ok := out["error"]; ok {
		c.t.Fatalf("POST %s returned error: %v", path, err)
	}
	return out
}

func (c *client) board() map[string]any {
	c.t.Helper()
	resp, err := http.Get(c.srv.URL + "/api/board")
	if err != nil {
		c.t.Fatalf("GET /api/board: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return out
}

// lane returns the card ids currently in a lane (in order).
func (c *client) lane(name string) []string {
	cards := c.board()["cards"].(map[string]any)[name].([]any)
	var ids []string
	for _, x := range cards {
		ids = append(ids, x.(map[string]any)["id"].(string))
	}
	return ids
}

// mcp calls a tool through the MCP endpoint like a real client, returning the parsed result.
func (c *client) mcp(tool string, args map[string]any) map[string]any {
	c.t.Helper()
	c.id++
	req := map[string]any{"jsonrpc": "2.0", "id": c.id, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args}}
	b, _ := json.Marshal(req)
	r, _ := http.NewRequest("POST", c.srv.URL+"/mcp", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		c.t.Fatalf("mcp %s: %v", tool, err)
	}
	defer resp.Body.Close()
	var env map[string]any
	json.NewDecoder(resp.Body).Decode(&env)
	res := env["result"].(map[string]any)
	if res["isError"] == true {
		c.t.Fatalf("mcp %s isError: %v", tool, res["content"])
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	var out map[string]any
	json.Unmarshal([]byte(text), &out)
	return out
}

func TestLifecycleOverHTTPAndMCP(t *testing.T) {
	c := newClient(t)

	// 1. create a ticket (dashboard) and an epic with a sub-ticket (MCP, like an agent)
	tk := c.api("/api/create", map[string]any{"type": "ticket", "title": "Fix login", "body": "steps"})["id"].(string)
	epic := c.mcp("create_epic", map[string]any{"title": "Onboarding"})["id"].(string)
	c.mcp("create_ticket", map[string]any{"title": "Welcome screen", "epic": epic})
	if got := c.lane("planning"); len(got) != 2 {
		t.Fatalf("planning should have ticket+epic, got %v", got)
	}

	// 2. move the ticket planning → ready → in_progress (movement)
	c.api("/api/move", map[string]any{"id": tk, "from": "planning", "to": "ready"})
	c.api("/api/move", map[string]any{"id": tk, "from": "ready", "to": "in_progress"})
	if got := c.lane("in_progress"); len(got) != 1 || got[0] != tk {
		t.Fatalf("ticket not in in_progress: %v", got)
	}

	// 3. worker records a result + moves to review (the report shows on the card)
	c.mcp("set_result", map[string]any{"id": tk, "status": "needs_review", "summary": "fixed the redirect", "files_changed": []any{"login.js"}})
	c.api("/api/move", map[string]any{"id": tk, "from": "in_progress", "to": "review"})
	card := c.mcp("get_card", map[string]any{"id": tk})
	if card["result"].(map[string]any)["summary"] != "fixed the redirect" {
		t.Errorf("result.summary not surfaced: %#v", card["result"])
	}

	// 4. review: send back to ready with a comment (a review round), then approve to done
	c.api("/api/review", map[string]any{"lane": "review", "id": tk, "comment": "tighten copy", "to": "ready"})
	if got := c.lane("ready"); len(got) != 1 || got[0] != tk {
		t.Fatalf("review send-back failed: %v", got)
	}
	c.api("/api/move", map[string]any{"id": tk, "from": "ready", "to": "review"})
	c.api("/api/review", map[string]any{"lane": "review", "id": tk, "comment": "ship it", "to": "done"})
	if got := c.lane("done"); len(got) != 1 || got[0] != tk {
		t.Fatalf("approve-to-done failed: %v", got)
	}
	// the card carries both review rounds
	if rounds := c.mcp("get_card", map[string]any{"id": tk})["reviews"].([]any); len(rounds) != 2 {
		t.Errorf("expected 2 review rounds, got %d", len(rounds))
	}

	// 5. archive the done ticket — leaves the board, files kept on disk
	c.api("/api/archive", map[string]any{"lane": "done", "id": tk})
	if got := c.lane("done"); len(got) != 0 {
		t.Errorf("archived card still in done: %v", got)
	}
	if len(readDirNames(mustJoin("archive"))) != 1 {
		t.Error("archived card not kept under archive/")
	}

	// 6. board context round-trips and reaches the agent via get_context
	c.api("/api/context/save", map[string]any{"body": "repos live in ~/code"})
	if c.mcp("get_context", map[string]any{})["context"] != "repos live in ~/code" {
		t.Error("board context not served to agent")
	}

	// 7. delete the epic — gone from disk entirely
	c.api("/api/delete", map[string]any{"lane": "planning", "id": epic})
	if isDir(mustJoin("kanban", "planning", epic)) {
		t.Error("deleted epic still on disk")
	}
	if got := c.lane("planning"); len(got) != 0 {
		t.Errorf("planning should be empty after delete: %v", got)
	}
}

func TestReorderViaAPI(t *testing.T) {
	c := newClient(t)
	var ids []string
	for _, name := range []string{"A", "B", "C"} {
		ids = append(ids, c.api("/api/create", map[string]any{"type": "ticket", "title": name})["id"].(string))
	}
	// reorder planning to creation order A, B, C
	c.api("/api/reorder", map[string]any{"lane": "planning", "ids": []string{ids[0], ids[1], ids[2]}})
	if got := c.lane("planning"); len(got) != 3 || got[0] != ids[0] || got[2] != ids[2] {
		t.Errorf("reorder via API: got %v, want %v", got, ids)
	}
}

func TestMCPHandshakeOverHTTP(t *testing.T) {
	c := newClient(t)
	// initialize like a real client
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-06-18"}})
	r, _ := http.NewRequest("POST", c.srv.URL+"/mcp", bytes.NewReader(body))
	r.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer resp.Body.Close()
	var env map[string]any
	json.NewDecoder(resp.Body).Decode(&env)
	if env["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "cboard" {
		t.Fatalf("initialize wrong: %#v", env)
	}
	// GET /mcp is 405 (no SSE stream)
	g, err := http.Get(c.srv.URL + "/mcp")
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	g.Body.Close()
	if g.StatusCode != 405 {
		t.Errorf("GET /mcp = %d, want 405", g.StatusCode)
	}
}

func TestDashboardServesPage(t *testing.T) {
	c := newClient(t)
	resp, err := http.Get(c.srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(b, []byte("<title>cboard</title>")) {
		t.Error("dashboard page not served")
	}
}
