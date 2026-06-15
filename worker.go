package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
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
			res := map[string]any{"card": getCardDetail(ready, cid), "lane": ready, "next_ticket": nt}
			if d, err := ticketDetail(ready, cid, nt); err == nil {
				res["next_ticket_detail"] = d // the ticket to work, full — no extra call needed
			}
			return res
		}
		return map[string]any{"card": getCardDetail(ready, cid), "lane": ready}
	}
	return map[string]any{"card": nil}
}

// getCardDetail is readCard plus what a worker needs to act: depends_on/context/kind, the
// card's project context, and references (not content) for docs, context files, artifacts,
// and assets. The card's own body (`content`) stays inline — it's needed every time — while
// everything else is a reference the caller pulls on demand with read_file. That keeps
// responses lean and works against a remote board.
func getCardDetail(lane, id string) map[string]any {
	card := readCard(lane, id)
	rel := filepath.Join("kanban", lane, id)
	enrichNode(rel, card)
	card["lane"] = lane
	stripDocContent(card["docs"])
	if pid, _ := card["project"].(string); pid != "" && isDir(mustJoin("projects", pid)) {
		pc := readProject(pid, nil)
		stripDocContent(pc["docs"])
		card["project_context"] = pc
	}
	if ts, ok := card["tickets"].([]map[string]any); ok {
		card["tickets"] = leanTickets(lane, id, ts)
	}
	return card
}

// leanTickets projects an epic's full ticket nodes down to overview summaries: id, title,
// status, paused, and the one-line result summary. Working ticket #5, you see #1–4 done
// (with what they produced) and #6–10 pending — enough trajectory without inlining every
// sibling's body. Pull a specific sibling's full info with get_card(id, ticket=<tid>).
func leanTickets(epicLane, epicID string, full []map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, t := range full {
		tid := fmt.Sprint(t["id"])
		out = append(out, map[string]any{
			"id": tid, "title": t["title"], "type": "ticket",
			"status": t["status"], "paused": t["paused"],
			"summary": resultSummary(filepath.Join("kanban", epicLane, epicID, "tickets", tid)),
		})
	}
	return out
}

func resultSummary(relDir string) string {
	if r, ok := readResult(relDir)["summary"].(string); ok {
		return r
	}
	return ""
}

func readResult(relDir string) map[string]any {
	if r, ok := readJSON(mustJoin(relDir, "result.json")).(map[string]any); ok {
		return r
	}
	return map[string]any{}
}

// ticketDetail is the full brief for one epic sub-ticket (for pulling a sibling's info):
// body, status, result, depends_on, reviews, log, and reference lists for artifacts/assets.
func ticketDetail(epicLane, epicID, tid string) (map[string]any, error) {
	rel := filepath.Join("kanban", epicLane, epicID, "tickets", tid)
	if !isDir(mustJoin(rel)) {
		return nil, fmt.Errorf("ticket not found: %s/%s", epicID, tid)
	}
	node := readNode(rel)
	enrichNode(rel, node)
	node["result"] = readResult(rel)
	return node, nil
}

func enrichNode(rel string, node map[string]any) {
	meta, _ := parseFM(readText(mustJoin(rel, "task.md")))
	node["depends_on"] = metaList(meta, "depends_on")
	node["context"] = metaList(meta, "context")
	node["kind"] = metaStr(meta, "kind")
	node["context_files"] = resolveContext(metaList(meta, "context"))
}

// stripDocContent turns inlined docs (the dashboard needs the content; MCP callers don't)
// into lean references — the caller pulls the body with read_file when it wants it.
func stripDocContent(docs any) {
	if arr, ok := docs.([]map[string]any); ok {
		for _, d := range arr {
			delete(d, "content")
		}
	}
}

// resolveContext returns references (path + ext + size, no content) for files named in a
// card's `context:` list, under the board's context/ folder. Pull them with read_file.
func resolveContext(paths []string) []map[string]any {
	out := []map[string]any{}
	for _, p := range paths {
		rel := filepath.Join("context", p)
		abs, err := safeJoin(rel)
		if err != nil || !isFile(abs) {
			continue
		}
		ref := map[string]any{
			"path": filepath.ToSlash(rel),
			"ext":  strings.ToLower(strings.TrimPrefix(filepath.Ext(p), ".")),
		}
		if fi, err := os.Stat(abs); err == nil {
			ref["size"] = fi.Size()
		}
		out = append(out, ref)
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
			Description: "Full detail for one card (auto-finds its lane): body (inline), status, depends_on, context/artifacts/assets as references. For an epic, its tickets come back as overview summaries (id, title, status, one-line result). To pull a specific epic sub-ticket's full detail (e.g. an already-done sibling's body + result), pass its ticket id.",
			InputSchema: obj(map[string]any{
				"id":     strProp("card id (required)"),
				"ticket": strProp("an epic sub-ticket id — returns that ticket's full detail instead of the epic"),
			}, "id"),
			handler: func(args map[string]any) (any, error) {
				id := str(args, "id")
				lane := findLane(id)
				if lane == "" {
					return nil, fmt.Errorf("card not found: %s", id)
				}
				if tid := str(args, "ticket"); tid != "" {
					return ticketDetail(lane, id, tid)
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
		mcpTool{
			Name:        "get_context",
			Description: "Read the board's global standing context — notes that apply to every card (repo locations, test tooling like 'when I say test, use X', conventions). Load this first, before per-card context.",
			InputSchema: obj(map[string]any{}),
			handler: func(args map[string]any) (any, error) {
				return map[string]any{"context": readBoardContext()}, nil
			},
		},
		mcpTool{
			Name:        "read_file",
			Description: "Read a board file by its board-relative path — the `path` on any doc, artifact, asset, or context reference. Returns {path, encoding, content}: utf-8 text, or base64 for binary files. This is how you pull docs/context on demand.",
			InputSchema: obj(map[string]any{
				"path": strProp("board-relative path (required), e.g. projects/<id>/docs/spec.md"),
			}, "path"),
			handler: func(args map[string]any) (any, error) {
				return readBoardFile(str(args, "path"))
			},
		},
		mcpTool{
			Name:        "list_projects",
			Description: "List all projects: id, goal, done flag, member cards, and doc references (read a doc with read_file).",
			InputSchema: obj(map[string]any{}),
			handler: func(args map[string]any) (any, error) {
				ps := listProjects()
				for _, p := range ps {
					stripDocContent(p["docs"])
				}
				return ps, nil
			},
		},
		mcpTool{
			Name:        "get_project",
			Description: "Get one project: goal, member cards, and doc references. Pull a doc's body with read_file using its path.",
			InputSchema: obj(map[string]any{
				"id": strProp("project id (required)"),
			}, "id"),
			handler: func(args map[string]any) (any, error) {
				id := str(args, "id")
				if id == "" {
					return nil, fmt.Errorf("id is required")
				}
				if !isDir(mustJoin("projects", id)) {
					return nil, fmt.Errorf("project not found: %s", id)
				}
				p := readProject(id, nil)
				stripDocContent(p["docs"])
				return p, nil
			},
		},
	)
}

// readBoardFile reads a file under the board root (path is board-relative), returning text
// or base64-for-binary. Refuses paths that escape the board.
func readBoardFile(p string) (any, error) {
	if p == "" {
		return nil, fmt.Errorf("path is required")
	}
	abs, err := safeJoin(filepath.FromSlash(p))
	if err != nil {
		return nil, fmt.Errorf("forbidden path")
	}
	if !isFile(abs) {
		return nil, fmt.Errorf("not found: %s", p)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	if utf8.Valid(b) && bytes.IndexByte(b, 0) == -1 {
		return map[string]any{"path": p, "encoding": "utf-8", "content": string(b)}, nil
	}
	return map[string]any{"path": p, "encoding": "base64", "content": base64.StdEncoding.EncodeToString(b)}, nil
}
