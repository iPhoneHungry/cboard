package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Minimal MCP server over Streamable HTTP, mounted at /mcp on the dashboard process.
//
// It speaks JSON-RPC 2.0 and answers each POST with a single application/json response
// (the spec permits this for servers that don't push server-initiated messages). A
// tools-only board doesn't need SSE, so GET returns 405 — which the spec explicitly
// allows for servers that don't offer a notification stream. No external dependency.

const mcpProtocolVersion = "2025-06-18"

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"` // absent → notification
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	handler     func(args map[string]any) (any, error)
}

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func strProp(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

// mcpTools is the ordered tool registry. Each handler wraps the same board functions
// the CLI and HTTP API call, so the three front doors never drift.
var mcpTools = []mcpTool{
	{
		Name:        "board_snapshot",
		Description: "Return the full board: lanes and every card (with status, artifacts, epic progress).",
		InputSchema: obj(map[string]any{}),
		handler:     func(args map[string]any) (any, error) { return boardSnapshot(), nil },
	},
	{
		Name:        "list_cards",
		Description: "List cards by lane (compact: id, title, type). Good for a quick overview.",
		InputSchema: obj(map[string]any{}),
		handler: func(args map[string]any) (any, error) {
			snap := boardSnapshot()
			out := map[string]any{}
			for _, lane := range snap["lanes"].([]map[string]any) {
				lid := fmt.Sprint(lane["id"])
				cards := []map[string]any{}
				for _, c := range snap["cards"].(map[string]any)[lid].([]map[string]any) {
					cards = append(cards, map[string]any{"id": c["id"], "title": c["title"], "type": c["type"]})
				}
				out[lid] = cards
			}
			return out, nil
		},
	},
	{
		Name:        "create_ticket",
		Description: "Create a ticket (lands in the planning lane). Optionally attach to a project, add it as a sub-ticket of an epic, or give it a markdown body.",
		InputSchema: obj(map[string]any{
			"title":   strProp("ticket title (required)"),
			"project": strProp("project id to assign"),
			"epic":    strProp("epic id to add this under as a sub-ticket"),
			"body":    strProp("markdown body"),
		}, "title"),
		handler: func(args map[string]any) (any, error) {
			title := str(args, "title")
			if title == "" {
				return nil, fmt.Errorf("title is required")
			}
			if epic := str(args, "epic"); epic != "" {
				lane, tid, err := addEpicTicket(epic, title, str(args, "body"))
				if err != nil {
					return nil, err
				}
				return map[string]any{"epic": epic, "lane": lane, "ticket": tid}, nil
			}
			cid, err := createCard(title, "ticket", str(args, "project"))
			if err != nil {
				return nil, err
			}
			if b := str(args, "body"); b != "" {
				saveBody("planning", cid, "", "", b)
			}
			return map[string]any{"lane": "planning", "id": cid}, nil
		},
	},
	{
		Name:        "create_epic",
		Description: "Create an epic (lands in the planning lane). An epic holds sub-tickets.",
		InputSchema: obj(map[string]any{
			"title":   strProp("epic title (required)"),
			"project": strProp("project id to assign"),
			"body":    strProp("markdown body"),
		}, "title"),
		handler: func(args map[string]any) (any, error) {
			title := str(args, "title")
			if title == "" {
				return nil, fmt.Errorf("title is required")
			}
			cid, err := createCard(title, "epic", str(args, "project"))
			if err != nil {
				return nil, err
			}
			if b := str(args, "body"); b != "" {
				saveBody("planning", cid, "", "", b)
			}
			return map[string]any{"lane": "planning", "id": cid}, nil
		},
	},
	{
		Name:        "create_project",
		Description: "Create a project (groups epics and tickets under one goal + shared docs).",
		InputSchema: obj(map[string]any{
			"name": strProp("project name (required)"),
			"body": strProp("markdown goal/body"),
		}, "name"),
		handler: func(args map[string]any) (any, error) {
			name := str(args, "name")
			if name == "" {
				return nil, fmt.Errorf("name is required")
			}
			pid, err := createProject(name)
			if err != nil {
				return nil, err
			}
			if b := str(args, "body"); b != "" {
				saveProject(pid, b)
			}
			return map[string]any{"id": pid}, nil
		},
	},
	{
		Name:        "move_card",
		Description: "Move a top-level card to another lane. The source lane is found automatically. Lanes: planning, ready, in_progress, blocked, review, done.",
		InputSchema: obj(map[string]any{
			"id": strProp("card id (required)"),
			"to": strProp("destination lane id (required)"),
		}, "id", "to"),
		handler: func(args map[string]any) (any, error) {
			id, to := str(args, "id"), str(args, "to")
			lane := findLane(id)
			if lane == "" {
				return nil, fmt.Errorf("card not found: %s", id)
			}
			if !contains(laneIDs(), to) {
				return nil, fmt.Errorf("unknown lane: %s", to)
			}
			if err := moveCard(id, lane, to); err != nil {
				return nil, err
			}
			return map[string]any{"id": id, "from": lane, "to": to}, nil
		},
	},
	{
		Name:        "add_review",
		Description: "Record a review round on a top-level card (comment + system snapshot) and optionally move it (e.g. to 'done' to approve, or back to 'ready'). Source lane is found automatically.",
		InputSchema: obj(map[string]any{
			"id":      strProp("card id (required)"),
			"comment": strProp("review comment"),
			"to":      strProp("lane to move the card to after the review (e.g. done, ready)"),
		}, "id"),
		handler: func(args map[string]any) (any, error) {
			id := str(args, "id")
			lane := findLane(id)
			if lane == "" {
				return nil, fmt.Errorf("card not found: %s", id)
			}
			n, err := addReview(lane, id, "", str(args, "comment"), str(args, "to"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"id": id, "round": n}, nil
		},
	},
	{
		Name:        "log_progress",
		Description: "Append a worker log line and upsert the daily summary. action is one of picked|completed|blocked|review|note (only completed/blocked touch the daily summary).",
		InputSchema: obj(map[string]any{
			"action":  strProp("picked | completed | blocked | review | note (required)"),
			"id":      strProp("card id (required)"),
			"ticket":  strProp("epic sub-ticket id"),
			"summary": strProp("short summary for the log line / daily bullet"),
		}, "action", "id"),
		handler: func(args map[string]any) (any, error) {
			action, id := str(args, "action"), str(args, "id")
			if action == "" || id == "" {
				return nil, fmt.Errorf("action and id are required")
			}
			task := logAction(action, id, str(args, "ticket"), str(args, "summary"))
			return map[string]any{"logged": action, "task": task}, nil
		},
	},
	{
		Name:        "doctor",
		Description: "Check the board for inconsistencies (order.json drift, stranded cards, corrupt JSON). Pass apply=true to repair them.",
		InputSchema: obj(map[string]any{
			"apply": map[string]any{"type": "boolean", "description": "write the fixes (default: report only)"},
		}),
		handler: func(args map[string]any) (any, error) {
			apply, _ := args["apply"].(bool)
			issues := runDoctor(apply)
			if issues == nil {
				issues = []issue{}
			}
			return map[string]any{"issues": issues, "applied": apply}, nil
		},
	},
}

func str(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func handleMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// No server-initiated stream; the spec allows 405 here.
		http.Error(w, "this MCP server does not offer an SSE stream; use POST", http.StatusMethodNotAllowed)
	case http.MethodDelete:
		// Stateless: nothing to tear down.
		w.WriteHeader(http.StatusOK)
	case http.MethodPost:
		handleMCPPost(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleMCPPost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeRPCError(w, nil, -32700, "could not read body")
		return
	}
	var req rpcReq
	if json.Unmarshal(body, &req) != nil {
		writeRPCError(w, nil, -32700, "parse error")
		return
	}

	// Notifications (no id) get an empty 202 and no JSON-RPC response.
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	result, rerr := dispatchMCP(req.Method, req.Params)

	if isNotification {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if rerr != nil {
		writeRPCError(w, req.ID, rerr.Code, rerr.Message)
		return
	}
	resp := map[string]any{"jsonrpc": "2.0", "id": rawOrNull(req.ID), "result": result}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func dispatchMCP(method string, params json.RawMessage) (any, *rpcErr) {
	switch method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(params, &p)
		ver := p.ProtocolVersion
		if ver == "" {
			ver = mcpProtocolVersion
		}
		return map[string]any{
			"protocolVersion": ver,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "cboard", "version": version},
		}, nil
	case "notifications/initialized":
		return nil, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		tools := []map[string]any{}
		for _, t := range mcpTools {
			tools = append(tools, map[string]any{
				"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema,
			})
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		return callTool(params)
	default:
		return nil, &rpcErr{Code: -32601, Message: "method not found: " + method}
	}
}

func callTool(params json.RawMessage) (any, *rpcErr) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if json.Unmarshal(params, &p) != nil {
		return nil, &rpcErr{Code: -32602, Message: "invalid params"}
	}
	for _, t := range mcpTools {
		if t.Name == p.Name {
			res, err := runToolGuarded(t, p.Arguments)
			if err != nil {
				// Tool errors are reported as a successful result with isError:true,
				// so the model sees the message rather than a transport-level failure.
				return map[string]any{
					"content": []map[string]any{{"type": "text", "text": "error: " + err.Error()}},
					"isError": true,
				}, nil
			}
			text, _ := json.MarshalIndent(res, "", "  ")
			return map[string]any{
				"content": []map[string]any{{"type": "text", "text": string(text)}},
			}, nil
		}
	}
	return nil, &rpcErr{Code: -32602, Message: "unknown tool: " + p.Name}
}

// runToolGuarded recovers panics from the trusted-path helpers so one bad call
// can't take down the whole server process.
func runToolGuarded(t mcpTool, args map[string]any) (res any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	if args == nil {
		args = map[string]any{}
	}
	return t.handler(args)
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": rawOrNull(id), "error": rpcErr{Code: code, Message: msg},
	})
}

func rawOrNull(id json.RawMessage) any {
	if len(id) == 0 {
		return nil
	}
	return id
}
