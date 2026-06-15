package main

import (
	"os"
	"path/filepath"
	"testing"
)

// archivedDirs returns the entries under the board's archive/ folder.
func archivedDirs(t *testing.T) []string {
	t.Helper()
	d := mustJoin("archive")
	if !isDir(d) {
		return nil
	}
	return readDirNames(d)
}

func TestDeleteActuallyRemovesFromDisk(t *testing.T) {
	newTestBoard(t)
	cid := readyCard(t, "Doomed")
	if err := deleteCard("ready", cid, ""); err != nil {
		t.Fatal(err)
	}
	// folder is gone, not moved anywhere
	if isDir(mustJoin("kanban", "ready", cid)) {
		t.Error("card folder still on disk after delete")
	}
	if len(archivedDirs(t)) != 0 {
		t.Error("delete should not archive — nothing should land in archive/")
	}
	// and it's out of the lane order
	order := toStringSlice(readJSON(mustJoin("kanban", "ready", "order.json")))
	if contains(order, cid) {
		t.Errorf("deleted card still in order.json: %#v", order)
	}
}

func TestArchiveHidesButKeepsFiles(t *testing.T) {
	newTestBoard(t)
	cid := readyCard(t, "Keep My Files")
	// give it an artifact so we can prove the files survive
	os.MkdirAll(mustJoin("kanban", "ready", cid, "artifacts"), 0o755)
	os.WriteFile(mustJoin("kanban", "ready", cid, "artifacts", "out.txt"), []byte("precious"), 0o644)

	if err := archiveCard("ready", cid, ""); err != nil {
		t.Fatal(err)
	}
	// gone from the lane (board won't list it)…
	if isDir(mustJoin("kanban", "ready", cid)) {
		t.Error("archived card should leave the lane")
	}
	if contains(toStringSlice(readJSON(mustJoin("kanban", "ready", "order.json"))), cid) {
		t.Error("archived card still in order.json")
	}
	// …but its folder (and artifact) survive under archive/
	arch := archivedDirs(t)
	if len(arch) != 1 {
		t.Fatalf("expected 1 archived folder, got %#v", arch)
	}
	body, err := os.ReadFile(filepath.Join(mustJoin("archive"), arch[0], "artifacts", "out.txt"))
	if err != nil || string(body) != "precious" {
		t.Errorf("archived artifact not preserved: %v / %q", err, body)
	}
	// the board snapshot no longer shows it
	for _, c := range boardSnapshot()["cards"].(map[string]any)["ready"].([]map[string]any) {
		if c["id"] == cid {
			t.Error("archived card still appears in board snapshot")
		}
	}
}

func TestBulkArchiveAndDelete(t *testing.T) {
	newTestBoard(t)
	a, b, c := readyCard(t, "A"), readyCard(t, "B"), readyCard(t, "C")

	if got := bulkArchive([]string{a, b}, "ready"); len(got) != 2 {
		t.Fatalf("bulkArchive moved %#v", got)
	}
	if got := bulkDelete([]string{c}, "ready"); len(got) != 1 {
		t.Fatalf("bulkDelete removed %#v", got)
	}
	order := toStringSlice(readJSON(mustJoin("kanban", "ready", "order.json")))
	if len(order) != 0 {
		t.Errorf("ready should be empty, order=%#v", order)
	}
	if len(archivedDirs(t)) != 2 {
		t.Errorf("expected 2 archived (a,b), deleted c kept none; got %d", len(archivedDirs(t)))
	}
}

func TestArchiveEpicSubticketUpdatesEpicOrder(t *testing.T) {
	newTestBoard(t)
	eid, _ := createCard("Epic", "epic", "")
	addEpicTicket(eid, "Keep", "")
	addEpicTicket(eid, "Toss", "")
	if err := deleteCard("planning", eid, "002-toss"); err != nil {
		t.Fatal(err)
	}
	e, _ := readJSON(mustJoin("kanban", "planning", eid, "epic.json")).(map[string]any)
	order := toStringSlice(e["order"])
	if contains(order, "002-toss") || !contains(order, "001-keep") {
		t.Errorf("epic order after delete = %#v", order)
	}
	if isDir(mustJoin("kanban", "planning", eid, "tickets", "002-toss")) {
		t.Error("deleted sub-ticket still on disk")
	}
}
