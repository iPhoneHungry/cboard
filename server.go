package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func sendJSON(w http.ResponseWriter, code int, v any) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(b)
}

func errJSON(w http.ResponseWriter, code int, err error) {
	sendJSON(w, code, map[string]any{"error": err.Error()})
}

// guard recovers panics from the trusted-path mustJoin/type assertions and turns
// them into a 500 rather than killing the server.
func guard(w http.ResponseWriter) {
	if r := recover(); r != nil {
		errJSON(w, 500, fmt.Errorf("%v", r))
	}
}

func handleGET(w http.ResponseWriter, r *http.Request) {
	defer guard(w)
	p := r.URL.Path
	switch {
	case p == "/" || p == "/index.html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(pageHTML)
	case p == "/api/board":
		sendJSON(w, 200, boardSnapshot())
	case p == "/api/summary":
		sendJSON(w, 200, summary())
	case p == "/api/projects":
		sendJSON(w, 200, listProjects())
	case p == "/api/context":
		sendJSON(w, 200, map[string]any{"body": readBoardContext()})
	case strings.HasPrefix(p, "/files/"):
		rel := path.Clean(strings.TrimPrefix(p, "/files/"))
		abs, err := safeJoin(filepath.FromSlash(rel))
		if err != nil {
			errJSON(w, 403, fmt.Errorf("forbidden"))
			return
		}
		if !isFile(abs) {
			errJSON(w, 404, fmt.Errorf("not found"))
			return
		}
		ctype := mime.TypeByExtension(filepath.Ext(abs))
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			errJSON(w, 500, err)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Write(data)
	default:
		errJSON(w, 404, fmt.Errorf("not found"))
	}
}

func handlePOST(w http.ResponseWriter, r *http.Request) {
	defer guard(w)
	var d map[string]any
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		errJSON(w, 400, fmt.Errorf("bad json"))
		return
	}
	s := func(k string) string { v, _ := d[k].(string); return v }

	var err error
	switch r.URL.Path {
	case "/api/move":
		err = moveCard(s("id"), s("from"), s("to"))
	case "/api/reorder":
		err = reorderLane(s("lane"), toStringSlice(d["ids"]))
	case "/api/pause":
		err = togglePause(s("lane"), s("id"), s("ticketId"))
	case "/api/save":
		if s("docfile") != "" {
			err = saveDoc(s("lane"), s("id"), s("docfile"), s("body"))
		} else {
			err = saveBody(s("lane"), s("id"), s("ticketId"), s("title"), s("body"))
		}
	case "/api/adddoc":
		var name string
		name, err = addDoc(s("lane"), s("id"), orDefault(s("name"), "doc.md"))
		if err == nil {
			sendJSON(w, 200, map[string]any{"ok": true, "name": name})
			return
		}
	case "/api/setproject":
		err = setCardProject(s("lane"), s("id"), s("ticketId"), s("project"))
	case "/api/project/create":
		var id string
		id, err = createProject(s("name"))
		if err == nil {
			sendJSON(w, 200, map[string]any{"ok": true, "id": id})
			return
		}
	case "/api/project/save":
		err = saveProject(s("id"), s("body"))
	case "/api/project/adddoc":
		var name string
		name, err = addProjectDoc(s("id"), orDefault(s("name"), "doc.md"))
		if err == nil {
			sendJSON(w, 200, map[string]any{"ok": true, "name": name})
			return
		}
	case "/api/project/savedoc":
		err = saveProjectDoc(s("id"), s("name"), s("body"))
	case "/api/project/delete":
		err = deleteProject(s("id"))
	case "/api/project/archive":
		err = archiveProject(s("id"))
	case "/api/context/save":
		err = saveBoardContext(s("body"))
	case "/api/project/done":
		done := true
		if v, ok := d["done"].(bool); ok {
			done = v
		}
		err = setProjectDone(s("id"), done)
	case "/api/upload":
		var raw []byte
		raw, err = base64.StdEncoding.DecodeString(s("data"))
		if err == nil {
			var fn string
			fn, err = addAsset(s("lane"), s("id"), s("ticketId"), orDefault(s("name"), "file"), raw)
			if err == nil {
				sendJSON(w, 200, map[string]any{"ok": true, "file": fn})
				return
			}
		}
	case "/api/create":
		if epic := s("epic"); epic != "" { // add a sub-ticket to an epic
			var lane, tid string
			lane, tid, err = addEpicTicket(epic, s("title"), s("body"))
			if err == nil {
				sendJSON(w, 200, map[string]any{"ok": true, "epic": epic, "lane": lane, "ticket": tid})
				return
			}
			break
		}
		var id string
		id, err = createCard(s("title"), orDefault(s("type"), "ticket"), s("project"))
		if err == nil {
			sendJSON(w, 200, map[string]any{"ok": true, "id": id})
			return
		}
	case "/api/delete":
		err = deleteCard(s("lane"), s("id"), s("ticketId"))
	case "/api/archive":
		err = archiveCard(s("lane"), s("id"), s("ticketId"))
	case "/api/review":
		var n int
		n, err = addReview(s("lane"), s("id"), s("ticketId"), s("comment"), s("to"))
		if err == nil {
			sendJSON(w, 200, map[string]any{"ok": true, "round": n})
			return
		}
	case "/api/bulkmove":
		moved := bulkMove(toStringSlice(d["ids"]), s("from"), s("to"))
		sendJSON(w, 200, map[string]any{"ok": true, "moved": moved})
		return
	case "/api/bulkarchive":
		out := bulkArchive(toStringSlice(d["ids"]), s("lane"))
		sendJSON(w, 200, map[string]any{"ok": true, "archived": out})
		return
	case "/api/bulkdelete":
		out := bulkDelete(toStringSlice(d["ids"]), s("lane"))
		sendJSON(w, 200, map[string]any{"ok": true, "deleted": out})
		return
	default:
		errJSON(w, 404, fmt.Errorf("unknown action"))
		return
	}
	if err != nil {
		errJSON(w, 500, err)
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// newMux builds the HTTP router (dashboard + API + MCP). Extracted so tests can mount it
// with httptest without binding a port.
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	// MCP endpoint for agents (more specific than "/", so it wins).
	mux.HandleFunc("/mcp", handleMCP)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleGET(w, r)
		case http.MethodPost:
			handlePOST(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	})
	return mux
}

func serve(host string, port int) error {
	mux := newMux()
	addr := fmt.Sprintf("%s:%d", host, port)
	fmt.Printf("cboard serving %s\n", root)
	fmt.Printf("  dashboard:  http://localhost:%d/\n", port)
	fmt.Printf("  mcp:        http://localhost:%d/mcp   (claude mcp add --transport http cboard http://localhost:%d/mcp)\n", port, port)
	if host == "0.0.0.0" {
		fmt.Printf("  network:    http://<this-machine-ip>:%d/  (no auth — trusted networks only)\n", port)
	}
	return http.ListenAndServe(addr, mux)
}
