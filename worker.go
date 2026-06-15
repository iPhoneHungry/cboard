package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Worker-support: the deterministic parts of the worker contract, in code rather than
// prose. next_card applies the selection protocol (order, skip paused, satisfied
// depends_on, epic next-ticket logic); get_card/set_result read and record a card.
// The agent-only parts (per-card sub-agent isolation, the loop, repo-vs-artifact
// judgement, the never-auto-done gate) stay in the kanban-worker skill.

func metaList(meta map[string]any, key string) []string {
	if v, ok := meta[key].([]string); ok && v != nil {
		return v
	}
	return []string{}
}

// doneIDs is the set of top-level card ids currently in the done lane (folders are truth).
func doneIDs() map[string]bool {
	out := map[string]bool{}
	for _, cid := range subdirs(mustJoin("kanban", "done")) {
		out[cid] = true
	}
	return out
}

func depsSatisfied(meta map[string]any, done map[string]bool) bool {
	for _, d := range metaList(meta, "depends_on") {
		if !done[d] {
			return false
		}
	}
	return true
}

func nodeStatus(relDir string) string {
	if r, ok := readJSON(mustJoin(relDir, "result.json")).(map[string]any); ok {
		if s, ok := r["status"].(string); ok {
			return s
		}
	}
	return "todo"
}

// nextEpicTicket returns the ticket id the worker should execute next for an epic, or ""
// if the epic is not currently workable (all tickets done, or the next one is paused /
// dependency-blocked). Sequential epics consider only the first non-done ticket; parallel
// epics return the first eligible non-done ticket.
func nextEpicTicket(epicRel string, done map[string]bool) string {
	epic, _ := readJSON(mustJoin(epicRel, "epic.json")).(map[string]any)
	var order []string
	parallel := false
	if epic != nil {
		order = toStringSlice(epic["order"])
		parallel = truthy(epic["parallel"])
	}
	tdir := filepath.Join(epicRel, "tickets")
	var present []string
	if isDir(mustJoin(tdir)) {
		present = subdirs(mustJoin(tdir))
	}
	for _, tid := range reconcile(order, present) {
		trel := filepath.Join(tdir, tid)
		if nodeStatus(trel) == "done" {
			continue
		}
		meta, _ := parseFM(readText(mustJoin(trel, "task.md")))
		ok := !isPaused(meta) && depsSatisfied(meta, done)
		if parallel {
			if ok {
				return tid
			}
			continue
		}
		// sequential: the first non-done ticket is "the next"; the epic is workable
		// only if that ticket itself is eligible.
		if ok {
			return tid
		}
		return ""
	}
	return ""
}

// selectNextCard walks ready in order and returns the first eligible card (with its full
// detail), plus next_ticket for an epic. {"card": null} when nothing is eligible.
func selectNextCard() map[string]any {
	done := doneIDs()
	const ready = "ready"
	order := toStringSlice(readJSON(mustJoin("kanban", ready, "order.json")))
	var present []string
	if isDir(mustJoin("kanban", ready)) {
		present = subdirs(mustJoin("kanban", ready))
	}
	for _, cid := range reconcile(order, present) {
		rel := filepath.Join("kanban", ready, cid)
		meta, _ := parseFM(readText(mustJoin(rel, "task.md")))
		if isPaused(meta) || !depsSatisfied(meta, done) {
			continue
		}
		ctype := strings.ToLower(metaStr(meta, "type"))
		if ctype == "epic" {
			nt := nextEpicTicket(rel, done)
			if nt == "" {
				continue
			}
			return map[string]any{"card": getCardDetail(ready, cid), "lane": ready, "next_ticket": nt}
		}
		return map[string]any{"card": getCardDetail(ready, cid), "lane": ready}
	}
	return map[string]any{"card": nil}
}

// getCardDetail is readCard enriched with everything a worker needs to act without reading
// the board's files itself: depends_on/context/kind, the inlined content of referenced
// global context files, and (if the card has one) the project's goal + shared docs. Epic
// tickets are enriched the same way. This keeps the worker location-independent — one tool
// call carries the whole brief, even against a remote board.
func getCardDetail(lane, id string) map[string]any {
	card := readCard(lane, id)
	rel := filepath.Join("kanban", lane, id)
	enrichNode(rel, card)
	card["lane"] = lane
	if pid, _ := card["project"].(string); pid != "" && isDir(mustJoin("projects", pid)) {
		card["project_context"] = readProject(pid, nil)
	}
	if ts, ok := card["tickets"].([]map[string]any); ok {
		for _, t := range ts {
			enrichNode(filepath.Join(rel, "tickets", fmt.Sprint(t["id"])), t)
		}
	}
	return card
}

func enrichNode(rel string, node map[string]any) {
	meta, _ := parseFM(readText(mustJoin(rel, "task.md")))
	node["depends_on"] = metaList(meta, "depends_on")
	node["context"] = metaList(meta, "context")
	node["kind"] = metaStr(meta, "kind")
	node["context_files"] = resolveContext(metaList(meta, "context"))
}

// resolveContext inlines the content of files referenced in a card's `context:` list
// (paths under the board's context/ folder).
func resolveContext(paths []string) []map[string]any {
	out := []map[string]any{}
	for _, p := range paths {
		rel := filepath.Join("context", p)
		abs, err := safeJoin(rel)
		if err != nil || !isFile(abs) {
			continue
		}
		out = append(out, map[string]any{"path": filepath.ToSlash(rel), "content": readText(abs)})
	}
	return out
}

func setResult(lane, id, ticket, status, summary string, notes, files []string) error {
	d := nodeDir(lane, id, ticket)
	if !isDir(mustJoin(d)) {
		return fmt.Errorf("card not found")
	}
	if notes == nil {
		notes = []string{}
	}
	if files == nil {
		files = []string{}
	}
	return writeJSON(mustJoin(d, "result.json"), map[string]any{
		"status": status, "summary": summary, "notes": notes, "files_changed": files,
	})
}

func strList(args map[string]any, key string) []string {
	out := []string{}
	if arr, ok := args[key].([]any); ok {
		for _, e := range arr {
			out = append(out, fmt.Sprint(e))
		}
	}
	return out
}

// register the worker tools onto the shared MCP registry.
func init() {
	mcpTools = append(mcpTools,
		mcpTool{
			Name:        "next_card",
			Description: "Return the next card to work from the ready lane, applying the selection protocol (ready order, skip paused, depends_on must all be in done, epic next-ticket logic). Returns {card: null} when nothing is eligible. For an epic, also returns next_ticket — the ticket id to execute.",
			InputSchema: obj(map[string]any{}),
			handler:     func(args map[string]any) (any, error) { return selectNextCard(), nil },
		},
		mcpTool{
			Name:        "get_card",
			Description: "Full detail for one card (auto-finds its lane): body, status, artifacts, reviews, depends_on, context, kind, and — for an epic — its tickets and their detail.",
			InputSchema: obj(map[string]any{
				"id": strProp("card id (required)"),
			}, "id"),
			handler: func(args map[string]any) (any, error) {
				id := str(args, "id")
				lane := findLane(id)
				if lane == "" {
					return nil, fmt.Errorf("card not found: %s", id)
				}
				return getCardDetail(lane, id), nil
			},
		},
		mcpTool{
			Name:        "set_result",
			Description: "Write a card's result.json (status one of done|blocked|needs_review, plus summary, notes, files_changed). For an epic sub-ticket, pass ticket. Does not move the card — use move_card for that.",
			InputSchema: obj(map[string]any{
				"id":            strProp("card id (required)"),
				"ticket":        strProp("epic sub-ticket id"),
				"status":        strProp("done | blocked | needs_review (required)"),
				"summary":       strProp("short outcome summary"),
				"notes":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "notes (e.g. why blocked)"},
				"files_changed": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "paths written (repo-relative or under artifacts/)"},
			}, "id", "status"),
			handler: func(args map[string]any) (any, error) {
				id, status := str(args, "id"), str(args, "status")
				if id == "" || status == "" {
					return nil, fmt.Errorf("id and status are required")
				}
				lane := findLane(id)
				if lane == "" {
					return nil, fmt.Errorf("card not found: %s", id)
				}
				if err := setResult(lane, id, str(args, "ticket"), status, str(args, "summary"),
					strList(args, "notes"), strList(args, "files_changed")); err != nil {
					return nil, err
				}
				return map[string]any{"ok": true, "id": id, "status": status}, nil
			},
		},
	)
}
