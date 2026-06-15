package main

import (
	"os"
	"path/filepath"
	"testing"
)

// newTestBoard seeds an empty board in a temp dir and points root at it.
func newTestBoard(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	board := filepath.Join(dir, "board")
	if err := seedBoard(board); err != nil {
		t.Fatalf("seed: %v", err)
	}
	root = board
	return board
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Fix the Login Bug": "fix-the-login-bug",
		"  Spaces   here  ": "spaces-here",
		"Weird!@#chars$$":    "weirdchars",
		"a---b":              "a-b",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFrontmatterRoundTrip(t *testing.T) {
	meta := map[string]any{
		"title": "Hello", "type": "ticket", "priority": 2, "paused": false,
		"depends_on": []string{"a", "b"}, "repo": "",
	}
	text := serializeFM(meta) + "\nbody text\n"
	got, body := parseFM(text)
	if got["title"] != "Hello" || got["type"] != "ticket" {
		t.Errorf("scalar lost: %#v", got)
	}
	if body != "body text" {
		t.Errorf("body = %q", body)
	}
	dep, ok := got["depends_on"].([]string)
	if !ok || len(dep) != 2 || dep[0] != "a" {
		t.Errorf("list lost: %#v", got["depends_on"])
	}
	if isPaused(got) {
		t.Error("paused should be false")
	}
}

func TestCreateMoveLifecycle(t *testing.T) {
	newTestBoard(t)

	cid, err := createCard("First Task", "ticket", "")
	if err != nil {
		t.Fatal(err)
	}
	if cid != "001-first-task" {
		t.Errorf("cid = %q, want 001-first-task", cid)
	}
	// lands in planning
	snap := boardSnapshot()
	planning := snap["cards"].(map[string]any)["planning"].([]map[string]any)
	if len(planning) != 1 || planning[0]["id"] != cid {
		t.Fatalf("planning = %#v", planning)
	}

	// numbering increments across the board
	cid2, _ := createCard("Second", "ticket", "")
	if cid2 != "002-second" {
		t.Errorf("cid2 = %q, want 002-second", cid2)
	}

	// move keeps order.json consistent with the folder
	if err := moveCard(cid, "planning", "ready"); err != nil {
		t.Fatal(err)
	}
	if isDir(mustJoin("kanban", "planning", cid)) {
		t.Error("folder still in planning")
	}
	if !isDir(mustJoin("kanban", "ready", cid)) {
		t.Error("folder not in ready")
	}
	order := toStringSlice(readJSON(mustJoin("kanban", "ready", "order.json")))
	if len(order) != 1 || order[0] != cid {
		t.Errorf("ready order.json = %#v", order)
	}
}

func TestSaveBodyUpdatesTitleWhenGiven(t *testing.T) {
	newTestBoard(t)
	cid, _ := createCard("Old Title", "ticket", "")
	// body-only save keeps the title
	if err := saveBody("planning", cid, "", "", "just a body"); err != nil {
		t.Fatal(err)
	}
	if got := readNode(filepath.Join("kanban", "planning", cid))["title"]; got != "Old Title" {
		t.Errorf("title changed on body-only save: %v", got)
	}
	// a non-empty title updates it
	if err := saveBody("planning", cid, "", "New Title", "body again"); err != nil {
		t.Fatal(err)
	}
	n := readNode(filepath.Join("kanban", "planning", cid))
	if n["title"] != "New Title" || n["content"] != "body again" {
		t.Errorf("title/body after edit = %v / %v", n["title"], n["content"])
	}
}

func TestReorderLane(t *testing.T) {
	newTestBoard(t)
	a, b, cc := readyCard(t, "A"), readyCard(t, "B"), readyCard(t, "C")
	// reorder ready to put C on top
	if err := reorderLane("ready", []string{cc, a, b}); err != nil {
		t.Fatal(err)
	}
	if got := toStringSlice(readJSON(mustJoin("kanban", "ready", "order.json"))); !equalSlice(got, []string{cc, a, b}) {
		t.Fatalf("order = %v, want [%s %s %s]", got, cc, a, b)
	}
	// validation: unknown ids dropped, present-but-omitted appended (still 3 cards)
	reorderLane("ready", []string{cc, "ghost"})
	got := toStringSlice(readJSON(mustJoin("kanban", "ready", "order.json")))
	if len(got) != 3 || got[0] != cc || contains(got, "ghost") {
		t.Errorf("validation failed: %v", got)
	}
}

func TestEpicWithTicket(t *testing.T) {
	newTestBoard(t)
	eid, _ := createCard("My Epic", "epic", "")
	lane, tid, err := addEpicTicket(eid, "Sub task", "")
	if err != nil {
		t.Fatal(err)
	}
	if lane != "planning" || tid != "001-sub-task" {
		t.Errorf("lane=%q tid=%q", lane, tid)
	}
	card := readCard("planning", eid)
	if card["type"] != "epic" {
		t.Fatalf("type = %v", card["type"])
	}
	prog := card["progress"].(map[string]any)
	if prog["total"] != 1 {
		t.Errorf("progress = %#v", prog)
	}
}

func TestDailyUpsertDoesNotClobber(t *testing.T) {
	newTestBoard(t)
	createCard("Task A", "ticket", "")
	createCard("Task B", "ticket", "")
	logAction("completed", "001-task-a", "", "did a")
	logAction("completed", "002-task-b", "", "did b")
	logAction("blocked", "002-task-b", "", "now blocked") // moves B from completed to blocked

	today := "" // find the one daily file
	dailyDir := mustJoin("logs", "daily")
	for _, n := range readDirNames(dailyDir) {
		today = filepath.Join(dailyDir, n)
	}
	data, _ := os.ReadFile(today)
	got := string(data)
	// A stays completed; B is deduped out of Completed and lands in Blocked.
	want := "## Completed\n- 001-task-a — did a\n\n## Blocked\n- 002-task-b — now blocked\n\n## Summary\n- 1 completed, 1 blocked\n"
	if got != want {
		t.Errorf("daily upsert wrong:\n got: %q\nwant: %q", got, want)
	}
}

func TestDoctorReconcilesOrder(t *testing.T) {
	newTestBoard(t)
	cid, _ := createCard("Orphan", "ticket", "")
	// corrupt: remove the card from order.json but leave the folder (untracked)
	writeJSON(mustJoin("kanban", "planning", "order.json"), []string{})
	issues := runDoctor(false)
	if len(issues) == 0 {
		t.Fatal("doctor found no issues on a desynced board")
	}
	runDoctor(true)
	order := toStringSlice(readJSON(mustJoin("kanban", "planning", "order.json")))
	if !contains(order, cid) {
		t.Errorf("doctor did not re-add %q: %#v", cid, order)
	}
}
