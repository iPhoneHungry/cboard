package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// version is reported in MCP serverInfo and `cboard version`.
const version = "0.5.0"

const usage = `cboard — local filesystem kanban (dashboard + MCP + worker CLI), one binary.

Usage:
  cboard [serve] [board] [--port N] [--host H]  run the dashboard + MCP (default localhost:8787)
  cboard ticket  "Title" [--project P] [--epic E] [--body T]
  cboard epic    "Title" [--project P] [--body T]
  cboard project "Name"  [--body T]
  cboard move <id> <lane>                  move a top-level card to another lane
  cboard list                              list cards by lane (JSON)
  cboard log <action> <id> [--ticket T] [--summary S]
  cboard doctor [--apply]                  check (and optionally repair) the board
  cboard config get | set <path>           show / set the active board
  cboard version                           print the version

Run with no arguments to just serve. The board defaults to ~/.cboard/board (created on
first run and remembered); override it for one run with a path or --root, or change the
default for good with 'cboard config set <path>'. The dashboard also exposes MCP at /mcp,
so agents and the browser share one process. On startup the board is self-healed: cards
added to a lane by hand are appended to its order.json.`

func main() {
	// No args → serve (the common case): make the happy path a single word.
	if len(os.Args) < 2 {
		if err := cmdServe(nil); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "serve":
		err = cmdServe(args)
	case "version", "--version", "-v":
		fmt.Println("cboard", version)
	case "ticket", "epic", "project", "move", "list":
		err = cmdAuthor(cmd, args)
	case "log":
		err = cmdLog(args)
	case "doctor":
		err = cmdDoctor(args)
	case "config":
		err = cmdConfig(args)
	case "mcp":
		err = fmt.Errorf("the mcp subcommand is not wired up yet")
	case "-h", "--help", "help":
		fmt.Println(usage)
	default:
		err = fmt.Errorf("unknown command: %s", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// ── flag parsing: pull --key value (and --bool) out of args, return positionals ──

func parseFlags(args []string, strKeys map[string]*string, boolKeys map[string]*bool) ([]string, error) {
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			key := strings.TrimPrefix(a, "--")
			val := ""
			if eq := strings.Index(key, "="); eq >= 0 {
				key, val = key[:eq], key[eq+1:]
				if p, ok := strKeys[key]; ok {
					*p = val
					continue
				}
			}
			if p, ok := boolKeys[key]; ok {
				*p = true
				continue
			}
			if p, ok := strKeys[key]; ok {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("flag --%s needs a value", key)
				}
				i++
				*p = args[i]
				continue
			}
			return nil, fmt.Errorf("unknown flag: --%s", key)
		}
		pos = append(pos, a)
	}
	return pos, nil
}

// resolveBoard sets the package-level root from explicit arg, then --root, then active config.
// mustExist=true errors if the folder is missing (ops); false is for serve/init that may seed.
func resolveBoard(explicit, rootFlag string, mustExist bool) error {
	chosen := explicit
	if chosen == "" {
		chosen = rootFlag
	}
	if chosen == "" {
		chosen = configGetBoard()
	}
	if chosen == "" {
		chosen = defaultBoardDir()
	}
	abs, err := filepath.Abs(expandUser(chosen))
	if err != nil {
		return err
	}
	if mustExist && !isDir(abs) {
		return fmt.Errorf("no board found at %s — run `cboard serve` (creates one) or `cboard init <dir>`", abs)
	}
	root = abs
	return nil
}

// seedBoard copies the embedded empty board into dir if it does not already exist.
func seedBoard(dir string) error {
	if isDir(dir) {
		fmt.Printf("Using existing board at %s\n", dir)
		return nil
	}
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("%s exists but is not a directory", dir)
	}
	err := fs.WalkDir(seedFS, "seed", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("seed", p)
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := seedFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		return err
	}
	fmt.Printf("Created a fresh, empty board at %s\n", dir)
	return nil
}

func cmdServe(args []string) error {
	host := "127.0.0.1"
	port := "8787"
	var rootFlag string
	pos, err := parseFlags(args,
		map[string]*string{"root": &rootFlag, "host": &host, "port": &port}, nil)
	if err != nil {
		return err
	}
	// Board resolution: explicit arg → --root → active config → ~/.cboard/board.
	explicit := ""
	if len(pos) > 0 {
		explicit = pos[0]
	}
	chosen := explicit
	if chosen == "" {
		chosen = rootFlag
	}
	hadConfig := configGetBoard() != ""
	if chosen == "" {
		chosen = configGetBoard()
	}
	if chosen == "" {
		chosen = defaultBoardDir()
	}
	abs, err := filepath.Abs(expandUser(chosen))
	if err != nil {
		return err
	}
	root = abs
	if err := seedBoard(root); err != nil {
		return err
	}
	// Zero-config: if nothing was registered yet, make this board the active one so the
	// CLI and MCP tools find it from anywhere without a path.
	if !hadConfig {
		if saved, err := configSetBoard(root); err == nil {
			fmt.Printf("Active board set to %s\n", saved)
		}
	}
	// Self-heal: fold any hand-added cards into their order.json, report what's out of place.
	startupReconcile().print()
	p, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("bad --port: %s", port)
	}
	return serve(host, p)
}

// defaultBoardDir is where a brand-new user's board lives: ~/.cboard/board (tidy, hidden,
// home-dir — works from any directory). Override per run with a path/--root, or persistently
// with `cboard config set`.
func defaultBoardDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		cwd, _ := os.Getwd()
		return filepath.Join(cwd, "cboard")
	}
	return filepath.Join(home, ".cboard", "board")
}

func cmdAuthor(cmd string, args []string) error {
	var rootFlag, project, epic, body string
	pos, err := parseFlags(args, map[string]*string{
		"root": &rootFlag, "project": &project, "epic": &epic, "body": &body,
	}, nil)
	if err != nil {
		return err
	}
	if err := resolveBoard("", rootFlag, true); err != nil {
		return err
	}
	switch cmd {
	case "ticket":
		if len(pos) < 1 {
			return fmt.Errorf("ticket needs a title")
		}
		if epic != "" {
			lane, tid, err := addEpicTicket(epic, pos[0], body)
			if err != nil {
				return err
			}
			return printJSON(map[string]any{"ok": true, "epic": epic, "lane": lane, "ticket": tid})
		}
		cid, err := createCard(pos[0], "ticket", project)
		if err != nil {
			return err
		}
		if strings.TrimSpace(body) != "" {
			saveBody("planning", cid, "", "", body)
		}
		return printJSON(map[string]any{"ok": true, "lane": "planning", "id": cid})
	case "epic":
		if len(pos) < 1 {
			return fmt.Errorf("epic needs a title")
		}
		cid, err := createCard(pos[0], "epic", project)
		if err != nil {
			return err
		}
		if strings.TrimSpace(body) != "" {
			saveBody("planning", cid, "", "", body)
		}
		return printJSON(map[string]any{"ok": true, "lane": "planning", "id": cid})
	case "project":
		if len(pos) < 1 {
			return fmt.Errorf("project needs a name")
		}
		pid, err := createProject(pos[0])
		if err != nil {
			return err
		}
		if strings.TrimSpace(body) != "" {
			saveProject(pid, body)
		}
		return printJSON(map[string]any{"ok": true, "id": pid})
	case "move":
		if len(pos) < 2 {
			return fmt.Errorf("move needs <id> <lane>")
		}
		id, to := pos[0], pos[1]
		lane := findLane(id)
		if lane == "" {
			return fmt.Errorf("card not found: %s", id)
		}
		if !contains(laneIDs(), to) {
			return fmt.Errorf("unknown lane: %s (valid: %s)", to, strings.Join(laneIDs(), ", "))
		}
		if err := moveCard(id, lane, to); err != nil {
			return err
		}
		return printJSON(map[string]any{"ok": true, "id": id, "from": lane, "to": to})
	case "list":
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
		return printJSON(out)
	}
	return nil
}

func cmdLog(args []string) error {
	var rootFlag, ticket, summaryText string
	pos, err := parseFlags(args, map[string]*string{
		"root": &rootFlag, "ticket": &ticket, "summary": &summaryText,
	}, nil)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return fmt.Errorf("log needs <action> <id>")
	}
	if err := resolveBoard("", rootFlag, true); err != nil {
		return err
	}
	task := logAction(pos[0], pos[1], ticket, summaryText)
	fmt.Printf("logged: %s %s\n", pos[0], task)
	return nil
}

func cmdDoctor(args []string) error {
	var rootFlag string
	var apply bool
	_, err := parseFlags(args, map[string]*string{"root": &rootFlag}, map[string]*bool{"apply": &apply})
	if err != nil {
		return err
	}
	if err := resolveBoard("", rootFlag, true); err != nil {
		return err
	}
	issues := runDoctor(apply)
	if len(issues) == 0 {
		fmt.Println("✓ Board is healthy — no issues found.")
		return nil
	}
	for _, it := range issues {
		mark := "[would fix]"
		if it.Fixed {
			mark = "[fixed]"
		} else if apply {
			mark = "[FAILED]"
		}
		fmt.Printf("  %s %s: %s\n", mark, it.Type, it.Detail)
	}
	fixed := 0
	for _, it := range issues {
		if it.Fixed {
			fixed++
		}
	}
	if apply {
		fmt.Printf("\n%d issue(s); %d fixed.\n", len(issues), fixed)
	} else {
		fmt.Printf("\n%d issue(s) found. Re-run with --apply to fix them.\n", len(issues))
	}
	return nil
}

func cmdConfig(args []string) error {
	if len(args) == 0 || args[0] == "get" {
		fmt.Println(configGetBoard())
		return nil
	}
	if args[0] == "set" && len(args) > 1 {
		abs, err := configSetBoard(args[1])
		if err != nil {
			return err
		}
		fmt.Println(abs)
		return nil
	}
	return fmt.Errorf("usage: cboard config [get | set <path>]")
}

// ── authoring helpers (port of board_cli.py) ──

func findLane(cardID string) string {
	if cardID == "" {
		return ""
	}
	kanban := mustJoin("kanban")
	if !isDir(kanban) {
		return ""
	}
	for _, lane := range readDirNames(kanban) {
		if isDir(mustJoin("kanban", lane, cardID)) {
			return lane
		}
	}
	return ""
}

func nextEpicTicketNum(epicRel string, order []string) string {
	n := 0
	seen := append([]string{}, order...)
	tdir := mustJoin(epicRel, "tickets")
	if isDir(tdir) {
		seen = append(seen, readDirNames(tdir)...)
	}
	for _, t := range seen {
		if m := numPrefix.FindStringSubmatch(t); m != nil {
			if v, _ := strconv.Atoi(m[1]); v > n {
				n = v
			}
		}
	}
	return fmt.Sprintf("%03d", n+1)
}

func addEpicTicket(epicID, title, body string) (string, string, error) {
	lane := findLane(epicID)
	if lane == "" {
		return "", "", fmt.Errorf("epic not found: %s", epicID)
	}
	epicRel := filepath.Join("kanban", lane, epicID)
	ej := mustJoin(epicRel, "epic.json")
	epic, ok := readJSON(ej).(map[string]any)
	if !ok {
		return "", "", fmt.Errorf("%s is not an epic (no epic.json)", epicID)
	}
	order := toStringSlice(epic["order"])
	tid := nextEpicTicketNum(epicRel, order) + "-" + slugify(title)
	d := mustJoin(epicRel, "tickets", tid)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", "", err
	}
	meta := map[string]any{"title": title, "type": "ticket", "priority": 2, "paused": false,
		"depends_on": []string{}, "context": []string{}, "repo": "", "branch": "", "kind": "artifact"}
	content := serializeFM(meta) + "\n"
	if strings.TrimSpace(body) != "" {
		content += strings.TrimSpace(body) + "\n"
	}
	if err := os.WriteFile(filepath.Join(d, "task.md"), []byte(content), 0o644); err != nil {
		return "", "", err
	}
	epic["order"] = append(order, tid)
	if err := writeJSON(ej, epic); err != nil {
		return "", "", err
	}
	return lane, tid, nil
}

func printJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
