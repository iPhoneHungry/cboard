package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// root is the absolute path to the board folder (the kanban data lives directly under it).
// It is set once per process by the CLI dispatch before any board operation runs.
var root string

var lanesFallback = []map[string]any{
	{"id": "planning", "name": "Planning", "color": "#888888"},
	{"id": "ready", "name": "Ready", "color": "#3b82f6"},
	{"id": "in_progress", "name": "In Progress", "color": "#f59e0b"},
	{"id": "blocked", "name": "Blocked", "color": "#ef4444"},
	{"id": "review", "name": "Test & Review", "color": "#a855f7"},
	{"id": "done", "name": "Done", "color": "#22c55e"},
}

// ─── path safety ────────────────────────────────────────────────────────────

// safeJoin joins parts under root and refuses anything that escapes it.
func safeJoin(parts ...string) (string, error) {
	p := filepath.Clean(filepath.Join(append([]string{root}, parts...)...))
	if p != root && !strings.HasPrefix(p, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes root")
	}
	return p, nil
}

// mustJoin is safeJoin for internal callers that pass trusted, fixed segments.
func mustJoin(parts ...string) string {
	p, err := safeJoin(parts...)
	if err != nil {
		panic(err)
	}
	return p
}

// ─── frontmatter ──────────────────────────────────────────────────────────────

var fmRe = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---\r?\n?(.*)$`)
var fmKVRe = regexp.MustCompile(`^(\w+):\s*(.*)$`)

// parseFM splits a task.md into a frontmatter map and the trimmed body.
// List values written as [a, b, c] come back as []string; everything else is a string.
func parseFM(text string) (map[string]any, string) {
	m := fmRe.FindStringSubmatch(text)
	if m == nil {
		return map[string]any{}, strings.TrimSpace(text)
	}
	meta := map[string]any{}
	for _, line := range strings.Split(m[1], "\n") {
		kv := fmKVRe.FindStringSubmatch(line)
		if kv == nil {
			continue
		}
		k := kv[1]
		v := strings.Trim(strings.TrimSpace(kv[2]), `'"`)
		if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
			var list []string
			for _, s := range strings.Split(v[1:len(v)-1], ",") {
				s = strings.Trim(strings.TrimSpace(s), `'"`)
				if s != "" {
					list = append(list, s)
				}
			}
			meta[k] = list
		} else {
			meta[k] = v
		}
	}
	return meta, strings.TrimSpace(m[2])
}

var fmOrder = []string{"title", "type", "priority", "paused", "depends_on", "context", "repo", "branch", "kind", "project"}

func serializeFM(meta map[string]any) string {
	seen := map[string]bool{}
	var keys []string
	for _, k := range fmOrder {
		if _, ok := meta[k]; ok {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	var rest []string
	for k := range meta {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)

	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range keys {
		switch v := meta[k].(type) {
		case []string:
			b.WriteString(fmt.Sprintf("%s: [%s]\n", k, strings.Join(v, ", ")))
		case bool:
			b.WriteString(fmt.Sprintf("%s: %t\n", k, v))
		default:
			b.WriteString(fmt.Sprintf("%s: %v\n", k, v))
		}
	}
	b.WriteString("---\n")
	return b.String()
}

func isPaused(meta map[string]any) bool {
	return strings.ToLower(fmt.Sprint(meta["paused"])) == "true"
}

func metaStr(meta map[string]any, key string) string {
	if v, ok := meta[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ─── low-level IO ───────────────────────────────────────────────────────────

func readText(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func readJSON(path string) any {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var v any
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	return v
}

func writeJSON(path string, data any) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

var imgRe = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp|svg|bmp|avif)$`)
var htmlRe = regexp.MustCompile(`(?i)\.html?$`)

// ─── board reading ────────────────────────────────────────────────────────────

// listFiles lists files under a dir (relative to root) recursively, as board-relative paths.
func listFiles(relDir string) []map[string]any {
	out := []map[string]any{}
	absDir := mustJoin(relDir)
	if !isDir(absDir) {
		return out
	}
	type entry struct {
		abs, name string
	}
	var entries []entry
	filepath.WalkDir(absDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		entries = append(entries, entry{p, d.Name()})
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].abs < entries[j].abs })
	for _, e := range entries {
		rel, _ := filepath.Rel(root, e.abs)
		nameRel, _ := filepath.Rel(absDir, e.abs)
		var size int64
		if fi, err := os.Stat(e.abs); err == nil {
			size = fi.Size()
		}
		out = append(out, map[string]any{
			"name":  filepath.ToSlash(nameRel),
			"path":  filepath.ToSlash(rel),
			"ext":   strings.ToLower(strings.TrimPrefix(filepath.Ext(e.name), ".")),
			"size":  size,
			"image": imgRe.MatchString(e.name),
			"html":  htmlRe.MatchString(e.name),
		})
	}
	return out
}

func readNode(relDir string) map[string]any {
	meta, content := parseFM(readText(mustJoin(relDir, "task.md")))
	title := metaStr(meta, "title")
	if title == "" {
		title = strings.ReplaceAll(filepath.Base(relDir), "-", " ")
	}
	result, _ := readJSON(mustJoin(relDir, "result.json")).(map[string]any)
	if result == nil {
		result = map[string]any{}
	}
	status := "todo"
	if s, ok := result["status"].(string); ok {
		status = s
	}
	reviews := readJSON(mustJoin(relDir, "reviews.json"))
	if reviews == nil {
		reviews = []any{}
	}
	return map[string]any{
		"id":        filepath.Base(relDir),
		"title":     title,
		"content":   content,
		"priority":  metaStr(meta, "priority"),
		"paused":    isPaused(meta),
		"repo":      metaStr(meta, "repo"),
		"branch":    metaStr(meta, "branch"),
		"project":   metaStr(meta, "project"),
		"status":    status,
		"result":    result,
		"artifacts": listFiles(filepath.Join(relDir, "artifacts")),
		"assets":    listFiles(filepath.Join(relDir, "assets")),
		"log":       readText(mustJoin(relDir, "task.log")),
		"reviews":   reviews,
	}
}

func readCard(laneID, cid string) map[string]any {
	rel := filepath.Join("kanban", laneID, cid)
	node := readNode(rel)
	meta, _ := parseFM(readText(mustJoin(rel, "task.md")))
	ctype := strings.ToLower(metaStr(meta, "type"))
	if ctype == "" {
		ctype = "ticket"
	}
	node["type"] = ctype
	if ctype == "epic" {
		epic, _ := readJSON(mustJoin(rel, "epic.json")).(map[string]any)
		var order []string
		if epic != nil {
			order = toStringSlice(epic["order"])
		}
		tdir := filepath.Join(rel, "tickets")
		var present []string
		if isDir(mustJoin(tdir)) {
			present = subdirs(mustJoin(tdir))
		}
		ids := reconcile(order, present)
		tickets := []map[string]any{}
		done := 0
		for _, t := range ids {
			tn := readNode(filepath.Join(tdir, t))
			if tn["status"] == "done" {
				done++
			}
			tickets = append(tickets, tn)
		}
		node["tickets"] = tickets
		node["parallel"] = epic != nil && truthy(epic["parallel"])
		node["progress"] = map[string]any{"done": done, "total": len(tickets)}
		node["docs"] = readDocs(rel)
	}
	return node
}

// readDocs returns epic/project shared docs, inlining the content of small text docs.
func readDocs(rel string) []map[string]any {
	out := []map[string]any{}
	if !isDir(mustJoin(rel, "docs")) {
		return out
	}
	for _, f := range listFiles(filepath.Join(rel, "docs")) {
		name := strings.ToLower(f["name"].(string))
		if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".markdown") || strings.HasSuffix(name, ".txt") {
			f["content"] = readText(mustJoin(f["path"].(string)))
		}
		out = append(out, f)
	}
	return out
}

func summary() map[string]any {
	today := time.Now().Format("2006-01-02")
	daily := []string{}
	if dd := mustJoin("logs", "daily"); isDir(dd) {
		for _, e := range readDirNames(dd) {
			if strings.HasSuffix(e, ".md") {
				daily = append(daily, strings.TrimSuffix(e, ".md"))
			}
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(daily)))
	agentDays := []string{}
	if ad := mustJoin("logs", "agent"); isDir(ad) {
		for _, e := range readDirNames(ad) {
			if strings.HasSuffix(e, ".log") {
				agentDays = append(agentDays, strings.TrimSuffix(e, ".log"))
			}
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(agentDays)))
	tail := ""
	if len(agentDays) > 0 {
		agent := readText(mustJoin("logs", "agent", agentDays[0]+".log"))
		var lines []string
		for _, l := range strings.Split(agent, "\n") {
			if strings.TrimSpace(l) != "" && !strings.HasPrefix(l, "#") {
				lines = append(lines, l)
			}
		}
		if len(lines) > 300 {
			lines = lines[len(lines)-300:]
		}
		tail = strings.Join(lines, "\n")
	}
	return map[string]any{"today": today, "dailyFiles": daily, "agentDays": agentDays, "agentTail": tail}
}

func laneList() []map[string]any {
	if v, ok := readJSON(mustJoin("lanes.json")).([]any); ok {
		out := []map[string]any{}
		for _, l := range v {
			if m, ok := l.(map[string]any); ok {
				out = append(out, m)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return lanesFallback
}

func laneIDs() []string {
	var ids []string
	for _, l := range laneList() {
		ids = append(ids, fmt.Sprint(l["id"]))
	}
	return ids
}

func boardSnapshot() map[string]any {
	lanes := laneList()
	cards := map[string]any{}
	for _, lane := range lanes {
		lid := fmt.Sprint(lane["id"])
		laneDir := filepath.Join("kanban", lid)
		order := toStringSlice(readJSON(mustJoin(laneDir, "order.json")))
		var present []string
		if isDir(mustJoin(laneDir)) {
			present = subdirs(mustJoin(laneDir))
		}
		ids := reconcile(order, present)
		list := []map[string]any{}
		for _, c := range ids {
			list = append(list, readCard(lid, c))
		}
		cards[lid] = list
	}
	return map[string]any{"lanes": lanes, "cards": cards}
}

// ─── mutations ──────────────────────────────────────────────────────────────

func updateOrder(laneID string, fn func([]string) []string) error {
	p := mustJoin("kanban", laneID, "order.json")
	order := toStringSlice(readJSON(p))
	return writeJSON(p, fn(order))
}

func moveCard(cid, from, to string) error {
	if from == to {
		return nil
	}
	src := mustJoin("kanban", from, cid)
	dst := mustJoin("kanban", to, cid)
	if !isDir(src) {
		return fmt.Errorf("not found: %s", cid)
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	updateOrder(from, func(o []string) []string { return without(o, cid) })
	updateOrder(to, func(o []string) []string {
		if contains(o, cid) {
			return o
		}
		return append(o, cid)
	})
	return nil
}

func nodeDir(lane, cid, ticket string) string {
	rel := filepath.Join("kanban", lane, cid)
	if ticket != "" {
		rel = filepath.Join(rel, "tickets", ticket)
	}
	return rel
}

func boardCounts() map[string]any {
	out := map[string]any{}
	for _, lid := range laneIDs() {
		out[lid] = len(toStringSlice(readJSON(mustJoin("kanban", lid, "order.json"))))
	}
	return out
}

func addReview(lane, cid, ticket, comment, to string) (int, error) {
	d := nodeDir(lane, cid, ticket)
	p := mustJoin(d, "reviews.json")
	rounds := toAnySlice(readJSON(p))
	node := readNode(d)
	names := func(key string) []string {
		var out []string
		for _, a := range node[key].([]map[string]any) {
			out = append(out, a["name"].(string))
		}
		if out == nil {
			out = []string{}
		}
		return out
	}
	toLane := to
	if toLane == "" {
		toLane = lane
	}
	rounds = append(rounds, map[string]any{
		"round":   len(rounds) + 1,
		"at":      time.Now().Format("2006-01-02T15:04:05"),
		"from":    lane,
		"to":      toLane,
		"comment": strings.TrimSpace(comment),
		"system": map[string]any{
			"board":     boardCounts(),
			"artifacts": names("artifacts"),
			"files":     names("assets"),
			"status":    node["status"],
		},
	})
	if err := writeJSON(p, rounds); err != nil {
		return 0, err
	}
	line := fmt.Sprintf("[%s] review round %d → sent to %s", time.Now().Format("15:04"), len(rounds), toLane)
	if c := strings.TrimSpace(comment); c != "" {
		if len(c) > 80 {
			c = c[:80]
		}
		line += ": " + c
	}
	appendLine(mustJoin(d, "task.log"), line)
	if ticket == "" && to != "" && to != lane {
		if err := moveCard(cid, lane, to); err != nil {
			return 0, err
		}
	}
	return len(rounds), nil
}

func bulkMove(ids []string, from, to string) []string {
	moved := []string{}
	for _, cid := range ids {
		if moveCard(cid, from, to) == nil {
			moved = append(moved, cid)
		}
	}
	return moved
}

func archiveDir() string {
	d := mustJoin("archive")
	os.MkdirAll(d, 0o755)
	return d
}

func stamp() string { return time.Now().Format("20060102-150405") }

// cardPath resolves a card or epic sub-ticket folder and a label for archive naming.
func cardPath(lane, cid, ticket string) (src, label string, err error) {
	if ticket != "" {
		src, label = mustJoin("kanban", lane, cid, "tickets", ticket), "ticket-"+ticket
	} else {
		src, label = mustJoin("kanban", lane, cid), lane+"-"+cid
	}
	if !isDir(src) {
		id := cid
		if ticket != "" {
			id = ticket
		}
		return "", "", fmt.Errorf("not found: %s", id)
	}
	return src, label, nil
}

// detach removes a card/ticket id from its parent ordering (lane order.json, or the epic's
// epic.json for a sub-ticket) after its folder has been moved or removed.
func detach(lane, cid, ticket string) error {
	if ticket != "" {
		ej := mustJoin("kanban", lane, cid, "epic.json")
		e, _ := readJSON(ej).(map[string]any)
		if e == nil {
			e = map[string]any{"order": []string{}, "parallel": false}
		}
		e["order"] = without(toStringSlice(e["order"]), ticket)
		return writeJSON(ej, e)
	}
	return updateOrder(lane, func(o []string) []string { return without(o, cid) })
}

// archiveCard moves a card/epic/sub-ticket into archive/ — it disappears from the board but
// its files are kept (under archive/<timestamp>-<label>/). Reversible: move the folder back.
func archiveCard(lane, cid, ticket string) error {
	src, label, err := cardPath(lane, cid, ticket)
	if err != nil {
		return err
	}
	if err := os.Rename(src, filepath.Join(archiveDir(), stamp()+"-"+label)); err != nil {
		return err
	}
	return detach(lane, cid, ticket)
}

// deleteCard permanently removes a card/epic/sub-ticket and all its files from disk.
func deleteCard(lane, cid, ticket string) error {
	src, _, err := cardPath(lane, cid, ticket)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(src); err != nil {
		return err
	}
	return detach(lane, cid, ticket)
}

func bulkArchive(ids []string, lane string) []string {
	out := []string{}
	for _, cid := range ids {
		if archiveCard(lane, cid, "") == nil {
			out = append(out, cid)
		}
	}
	return out
}

func bulkDelete(ids []string, lane string) []string {
	out := []string{}
	for _, cid := range ids {
		if deleteCard(lane, cid, "") == nil {
			out = append(out, cid)
		}
	}
	return out
}

func togglePause(lane, cid, ticket string) error {
	p := mustJoin(nodeDir(lane, cid, ticket), "task.md")
	text := readText(p)
	if text == "" {
		text = "---\n---\n"
	}
	meta, content := parseFM(text)
	meta["paused"] = !isPaused(meta)
	return os.WriteFile(p, []byte(serializeFM(meta)+"\n"+content+"\n"), 0o644)
}

// saveBody rewrites a card's markdown body, preserving frontmatter. A non-empty title also
// updates the frontmatter title (so the dashboard can edit the title inline).
func saveBody(lane, cid, ticket, title, body string) error {
	p := mustJoin(nodeDir(lane, cid, ticket), "task.md")
	text := readText(p)
	if text == "" {
		text = "---\n---\n"
	}
	meta, _ := parseFM(text)
	if title != "" {
		meta["title"] = title
	}
	return os.WriteFile(p, []byte(serializeFM(meta)+"\n"+strings.TrimSpace(body)+"\n"), 0o644)
}

// Board-level standing context: notes every card shares (repo locations, test tooling,
// conventions). Stored at context/board.md, surfaced in the dashboard and via the
// get_context MCP tool, and loaded first (broadest layer) by the worker.
func readBoardContext() string {
	return readText(mustJoin("context", "board.md"))
}

func saveBoardContext(body string) error {
	d := mustJoin("context")
	os.MkdirAll(d, 0o755)
	return os.WriteFile(filepath.Join(d, "board.md"), []byte(body), 0o644)
}

func saveDoc(lane, cid, name, body string) error {
	docs := mustJoin(nodeDir(lane, cid, ""), "docs")
	os.MkdirAll(docs, 0o755)
	return os.WriteFile(filepath.Join(docs, filepath.Base(name)), []byte(body), 0o644)
}

func addDoc(lane, cid, name string) (string, error) {
	name = filepath.Base(name)
	if name == "" || name == "." {
		name = "doc.md"
	}
	if !regexp.MustCompile(`(?i)\.(md|markdown|txt)$`).MatchString(name) {
		name += ".md"
	}
	docs := mustJoin(nodeDir(lane, cid, ""), "docs")
	os.MkdirAll(docs, 0o755)
	p := filepath.Join(docs, name)
	if !isFile(p) {
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if err := os.WriteFile(p, []byte("# "+base+"\n\n"), 0o644); err != nil {
			return "", err
		}
	}
	return name, nil
}

func addAsset(lane, cid, ticket, name string, raw []byte) (string, error) {
	d := nodeDir(lane, cid, ticket)
	assets := mustJoin(d, "assets")
	os.MkdirAll(assets, 0o755)
	name = filepath.Base(name)
	if name == "" || name == "." {
		name = "file"
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	final, n := name, 0
	for isFile(filepath.Join(assets, final)) {
		n++
		final = fmt.Sprintf("%s-%d%s", base, n, ext)
	}
	if err := os.WriteFile(filepath.Join(assets, final), raw, 0o644); err != nil {
		return "", err
	}
	p := mustJoin(d, "task.md")
	text := readText(p)
	if text == "" {
		text = "---\n---\n"
	}
	meta, content := parseFM(text)
	var ref string
	if imgRe.MatchString(final) {
		ref = fmt.Sprintf("![%s](assets/%s)", base, final)
	} else {
		ref = fmt.Sprintf("[%s](assets/%s)", final, final)
	}
	body := ref
	if content != "" {
		body = content + "\n\n" + ref
	}
	if err := os.WriteFile(p, []byte(serializeFM(meta)+"\n"+body+"\n"), 0o644); err != nil {
		return "", err
	}
	return final, nil
}

var slugStrip = regexp.MustCompile(`[^a-z0-9\s-]`)
var slugSpace = regexp.MustCompile(`\s+`)
var slugDash = regexp.MustCompile(`-+`)

func slugify(s string) string {
	s = slugStrip.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "")
	s = slugDash.ReplaceAllString(slugSpace.ReplaceAllString(s, "-"), "-")
	if s == "" {
		return fmt.Sprintf("card-%d", time.Now().Unix())
	}
	return s
}

var numPrefix = regexp.MustCompile(`^(\d+)`)

func nextNum() string {
	mx := 0
	kanban := mustJoin("kanban")
	for _, lane := range readDirNames(kanban) {
		ld := filepath.Join(kanban, lane)
		if !isDir(ld) {
			continue
		}
		for _, d := range readDirNames(ld) {
			if m := numPrefix.FindStringSubmatch(d); m != nil {
				if n, _ := strconv.Atoi(m[1]); n > mx {
					mx = n
				}
			}
		}
	}
	return fmt.Sprintf("%03d", mx+1)
}

func createCard(title, ctype, project string) (string, error) {
	cid := nextNum() + "-" + slugify(title)
	d := mustJoin("kanban", "planning", cid)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	kind := "artifact"
	if ctype == "epic" {
		kind = ""
	}
	meta := map[string]any{
		"title": title, "type": ctype, "priority": 2, "paused": false,
		"depends_on": []string{}, "context": []string{}, "repo": "", "branch": "",
		"kind": kind, "project": project,
	}
	if err := os.WriteFile(filepath.Join(d, "task.md"), []byte(serializeFM(meta)+"\n"), 0o644); err != nil {
		return "", err
	}
	if ctype == "epic" {
		writeJSON(filepath.Join(d, "epic.json"), map[string]any{"order": []string{}, "parallel": false})
		os.MkdirAll(filepath.Join(d, "tickets"), 0o755)
	}
	updateOrder("planning", func(o []string) []string { return append([]string{cid}, o...) })
	return cid, nil
}

// ─── projects ─────────────────────────────────────────────────────────────────

func listProjects() []map[string]any {
	pdir := mustJoin("projects")
	out := []map[string]any{}
	if !isDir(pdir) {
		return out
	}
	snap := boardSnapshot()
	names := readDirNames(pdir)
	sort.Strings(names)
	for _, pid := range names {
		if isDir(filepath.Join(pdir, pid)) {
			out = append(out, readProject(pid, snap))
		}
	}
	return out
}

func readProject(pid string, snap map[string]any) map[string]any {
	rel := filepath.Join("projects", pid)
	goal := readText(mustJoin(rel, "project.md"))
	docs := readDocs(rel)
	done := false
	if pj, ok := readJSON(mustJoin(rel, "project.json")).(map[string]any); ok {
		done = truthy(pj["done"])
	}
	if snap == nil {
		snap = boardSnapshot()
	}
	members := []map[string]any{}
	for lane, cs := range snap["cards"].(map[string]any) {
		for _, c := range cs.([]map[string]any) {
			if c["project"] == pid {
				m := map[string]any{"id": c["id"], "title": c["title"], "type": c["type"],
					"lane": lane, "status": c["status"]}
				if p, ok := c["progress"]; ok {
					m["progress"] = p
				}
				members = append(members, m)
			}
		}
	}
	return map[string]any{"id": pid, "goal": goal, "docs": docs, "members": members, "done": done}
}

func createProject(name string) (string, error) {
	pid := slugify(name)
	d := mustJoin("projects", pid)
	if err := os.MkdirAll(filepath.Join(d, "docs"), 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(d, "project.md")
	if !isFile(p) {
		os.WriteFile(p, []byte("# "+name+"\n\n**Goal:**\n\n**Milestone / target:**\n"), 0o644)
	}
	return pid, nil
}

func saveProject(pid, body string) error {
	d := mustJoin("projects", pid)
	os.MkdirAll(d, 0o755)
	return os.WriteFile(filepath.Join(d, "project.md"), []byte(body), 0o644)
}

func addProjectDoc(pid, name string) (string, error) {
	name = filepath.Base(name)
	if name == "" || name == "." {
		name = "doc.md"
	}
	if !regexp.MustCompile(`(?i)\.(md|markdown|txt)$`).MatchString(name) {
		name += ".md"
	}
	d := mustJoin("projects", pid, "docs")
	os.MkdirAll(d, 0o755)
	p := filepath.Join(d, name)
	if !isFile(p) {
		base := strings.TrimSuffix(name, filepath.Ext(name))
		os.WriteFile(p, []byte("# "+base+"\n\n"), 0o644)
	}
	return name, nil
}

func saveProjectDoc(pid, name, body string) error {
	d := mustJoin("projects", pid, "docs")
	os.MkdirAll(d, 0o755)
	return os.WriteFile(filepath.Join(d, filepath.Base(name)), []byte(body), 0o644)
}

func setCardProject(lane, cid, ticket, project string) error {
	p := mustJoin(nodeDir(lane, cid, ticket), "task.md")
	text := readText(p)
	if text == "" {
		text = "---\n---\n"
	}
	meta, content := parseFM(text)
	meta["project"] = project
	return os.WriteFile(p, []byte(serializeFM(meta)+"\n"+content+"\n"), 0o644)
}

func setProjectDone(pid string, done bool) error {
	d := mustJoin("projects", pid)
	if !isDir(d) {
		return fmt.Errorf("not found: %s", pid)
	}
	return writeJSON(filepath.Join(d, "project.json"), map[string]any{"done": done})
}

// clearProjectTags removes the project tag from every member card (so they aren't left
// pointing at a project that's gone).
func clearProjectTags(pid string) {
	snap := boardSnapshot()
	for lane, cs := range snap["cards"].(map[string]any) {
		for _, c := range cs.([]map[string]any) {
			if c["project"] == pid {
				setCardProject(lane, c["id"].(string), "", "")
			}
		}
	}
}

// archiveProject moves a project into archive/ (kept on disk); deleteProject removes it.
// Both clear the project tag from member cards; neither touches the member cards otherwise.
func archiveProject(pid string) error {
	src := mustJoin("projects", pid)
	if !isDir(src) {
		return fmt.Errorf("not found: %s", pid)
	}
	clearProjectTags(pid)
	return os.Rename(src, filepath.Join(archiveDir(), stamp()+"-project-"+pid))
}

func deleteProject(pid string) error {
	src := mustJoin("projects", pid)
	if !isDir(src) {
		return fmt.Errorf("not found: %s", pid)
	}
	clearProjectTags(pid)
	return os.RemoveAll(src)
}

// ─── small helpers ──────────────────────────────────────────────────────────

func readDirNames(d string) []string {
	entries, err := os.ReadDir(d)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func subdirs(d string) []string {
	var out []string
	for _, n := range readDirNames(d) {
		if isDir(filepath.Join(d, n)) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// reconcile returns order entries that still exist, then any new folders (sorted).
func reconcile(order, folders []string) []string {
	fset := map[string]bool{}
	for _, f := range folders {
		fset[f] = true
	}
	oset := map[string]bool{}
	out := []string{}
	for _, c := range order {
		if fset[c] {
			out = append(out, c)
			oset[c] = true
		}
	}
	var extra []string
	for _, f := range folders {
		if !oset[f] {
			extra = append(extra, f)
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}

func toStringSlice(v any) []string {
	out := []string{}
	if arr, ok := v.([]any); ok {
		for _, e := range arr {
			out = append(out, fmt.Sprint(e))
		}
	} else if arr, ok := v.([]string); ok {
		out = append(out, arr...)
	}
	return out
}

func toAnySlice(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	return []any{}
}

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

func without(s []string, x string) []string {
	out := []string{}
	for _, v := range s {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.ToLower(t) == "true"
	}
	return false
}

func appendLine(path, line string) {
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(line + "\n")
}
