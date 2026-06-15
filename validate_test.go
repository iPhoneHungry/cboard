package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStartupAdoptsLooseMarkdown(t *testing.T) {
	newTestBoard(t)
	// someone drops a bare markdown file into the ready lane
	os.WriteFile(mustJoin("kanban", "ready", "quick-idea.md"), []byte("do the thing"), 0o644)

	rep := startupReconcile()
	if len(rep.Adopted) != 1 {
		t.Fatalf("expected 1 adoption, got %#v", rep)
	}
	// it became a real card folder with a task.md…
	if !isDir(mustJoin("kanban", "ready", "001-quick-idea")) {
		t.Fatal("loose md was not turned into a card folder")
	}
	meta, body := parseFM(readText(mustJoin("kanban", "ready", "001-quick-idea", "task.md")))
	if meta["title"] != "quick idea" || body != "do the thing" {
		t.Errorf("adopted card = %#v / %q", meta, body)
	}
	// …the loose file is gone…
	if isFile(mustJoin("kanban", "ready", "quick-idea.md")) {
		t.Error("loose md should have been consumed")
	}
	// …and it's at the end of the order.
	order := toStringSlice(readJSON(mustJoin("kanban", "ready", "order.json")))
	if len(order) != 1 || order[0] != "001-quick-idea" {
		t.Errorf("order.json = %#v", order)
	}
}

func TestStartupAppendsUntrackedFolderAtEnd(t *testing.T) {
	newTestBoard(t)
	a := readyCard(t, "Existing") // goes through the normal path, tracked in order
	// hand-create a card folder NOT listed in order.json
	d := mustJoin("kanban", "ready", "099-handmade")
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "task.md"), []byte("---\ntitle: Handmade\n---\n"), 0o644)

	rep := startupReconcile()
	order := toStringSlice(readJSON(mustJoin("kanban", "ready", "order.json")))
	// existing stays first, the hand-made one is appended at the end
	if len(order) != 2 || order[0] != a || order[1] != "099-handmade" {
		t.Fatalf("order = %#v (want [%s 099-handmade])", order, a)
	}
	if len(rep.Appended) != 1 || rep.Appended[0] != "ready/099-handmade" {
		t.Errorf("report.Appended = %#v", rep.Appended)
	}
}

func TestStartupLeavesStrayNonCardInPlace(t *testing.T) {
	newTestBoard(t)
	os.WriteFile(mustJoin("kanban", "ready", "screenshot.png"), []byte{0x89, 0x50}, 0o644)
	rep := startupReconcile()
	if len(rep.Unplaced) != 1 {
		t.Fatalf("expected 1 unplaced, got %#v", rep)
	}
	// it must NOT be deleted or turned into a card
	if !isFile(mustJoin("kanban", "ready", "screenshot.png")) {
		t.Error("stray non-card file should be left in place, not removed")
	}
}

func TestStartupCleanBoardIsNoop(t *testing.T) {
	newTestBoard(t)
	readyCard(t, "One")
	before := readText(mustJoin("kanban", "ready", "order.json"))
	rep := startupReconcile()
	if rep.any() {
		t.Errorf("clean board should produce no changes, got %#v", rep)
	}
	if readText(mustJoin("kanban", "ready", "order.json")) != before {
		t.Error("order.json changed on a clean board")
	}
}
