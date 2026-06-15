package main

import (
	"os"
	"testing"
)

func readyCard(t *testing.T, title string) string {
	t.Helper()
	cid, err := createCard(title, "ticket", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := moveCard(cid, "planning", "ready"); err != nil {
		t.Fatal(err)
	}
	return cid
}

func selectedID(m map[string]any) string {
	if c, ok := m["card"].(map[string]any); ok && c != nil {
		return c["id"].(string)
	}
	return ""
}

func TestNextCardOrderAndPaused(t *testing.T) {
	newTestBoard(t)
	a := readyCard(t, "Alpha")
	b := readyCard(t, "Bravo")

	// ready order is [a, b] (move appends). a wins.
	if got := selectedID(selectNextCard()); got != a {
		t.Fatalf("first pick = %q, want %q", got, a)
	}

	// pause a → b wins.
	if err := togglePause("ready", a, ""); err != nil {
		t.Fatal(err)
	}
	if got := selectedID(selectNextCard()); got != b {
		t.Fatalf("after pausing %q, pick = %q, want %q", a, got, b)
	}
}

func TestNextCardDependsOn(t *testing.T) {
	newTestBoard(t)
	dep := readyCard(t, "Dependency")
	// blocked depends on dep, which is not done yet.
	blocked, _ := createCard("Blocked", "ticket", "")
	saveBodyKeepDeps(t, "planning", blocked, []string{dep})
	moveCard(blocked, "planning", "ready")

	// dep is first in order anyway, but verify blocked is not pickable on its own:
	togglePause("ready", dep, "") // pause dep so only blocked remains
	if got := selectedID(selectNextCard()); got != "" {
		t.Fatalf("blocked card should not be eligible (dep not done), got %q", got)
	}

	// satisfy the dependency: move dep to done.
	togglePause("ready", dep, "")
	moveCard(dep, "ready", "done")
	if got := selectedID(selectNextCard()); got != blocked {
		t.Fatalf("after dep done, pick = %q, want %q", got, blocked)
	}
}

func TestNextCardEpicNextTicket(t *testing.T) {
	newTestBoard(t)
	eid, _ := createCard("Epic", "epic", "")
	moveCard(eid, "planning", "ready")
	addEpicTicket(eid, "First", "")
	addEpicTicket(eid, "Second", "")

	res := selectNextCard()
	if selectedID(res) != eid {
		t.Fatalf("epic not selected: %#v", res)
	}
	if res["next_ticket"] != "001-first" {
		t.Errorf("next_ticket = %v, want 001-first", res["next_ticket"])
	}

	// mark first ticket done → next_ticket advances to second.
	if err := setResult("ready", eid, "001-first", "done", "ok", nil, nil); err != nil {
		t.Fatal(err)
	}
	res = selectNextCard()
	if res["next_ticket"] != "002-second" {
		t.Errorf("after first done, next_ticket = %v, want 002-second", res["next_ticket"])
	}
}

func TestGetCardEnriches(t *testing.T) {
	newTestBoard(t)
	cid := readyCard(t, "Detail")
	saveBodyKeepDeps(t, "ready", cid, []string{"x", "y"})
	card := getCardDetail("ready", cid)
	if card["lane"] != "ready" {
		t.Errorf("lane = %v", card["lane"])
	}
	deps := card["depends_on"].([]string)
	if len(deps) != 2 || deps[0] != "x" {
		t.Errorf("depends_on = %#v", card["depends_on"])
	}
	if card["kind"] != "artifact" {
		t.Errorf("kind = %v", card["kind"])
	}
}

// saveBodyKeepDeps rewrites a card's task.md preserving frontmatter but setting depends_on.
func saveBodyKeepDeps(t *testing.T, lane, cid string, deps []string) {
	t.Helper()
	p := mustJoin(nodeDir(lane, cid, ""), "task.md")
	meta, content := parseFM(readText(p))
	meta["depends_on"] = deps
	if err := os.WriteFile(p, []byte(serializeFM(meta)+"\n"+content+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
