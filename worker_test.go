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

func TestReadBoardFile(t *testing.T) {
	newTestBoard(t)
	os.MkdirAll(mustJoin("projects", "p", "docs"), 0o755)
	os.WriteFile(mustJoin("projects", "p", "docs", "spec.md"), []byte("# Spec\nhi"), 0o644)
	os.WriteFile(mustJoin("projects", "p", "docs", "blob.bin"), []byte{0x00, 0x01, 0x02, 0xff}, 0o644)

	r, err := readBoardFile("projects/p/docs/spec.md")
	if err != nil {
		t.Fatal(err)
	}
	m := r.(map[string]any)
	if m["encoding"] != "utf-8" || m["content"] != "# Spec\nhi" {
		t.Errorf("text read = %#v", m)
	}
	r, _ = readBoardFile("projects/p/docs/blob.bin")
	m = r.(map[string]any)
	if m["encoding"] != "base64" {
		t.Errorf("binary should be base64, got %#v", m)
	}
	// path traversal is refused
	if _, err := readBoardFile("../../etc/passwd"); err == nil {
		t.Error("expected traversal to be refused")
	}
	if _, err := readBoardFile("nope.md"); err == nil {
		t.Error("expected missing file error")
	}
}

func TestDocsAreReferencesNotInlined(t *testing.T) {
	newTestBoard(t)
	pid, _ := createProject("Launch")
	os.MkdirAll(mustJoin("projects", pid, "docs"), 0o755)
	os.WriteFile(mustJoin("projects", pid, "docs", "spec.md"), []byte("# Spec\nbig text"), 0o644)
	cid := readyCard(t, "Member")
	setCardProject("ready", cid, "", pid)

	card := getCardDetail("ready", cid)
	pc := card["project_context"].(map[string]any)
	docs := pc["docs"].([]map[string]any)
	if len(docs) != 1 {
		t.Fatalf("expected 1 project doc, got %d", len(docs))
	}
	if _, hasContent := docs[0]["content"]; hasContent {
		t.Error("project doc should be a reference (no inlined content)")
	}
	if docs[0]["path"] != "projects/"+pid+"/docs/spec.md" {
		t.Errorf("doc path = %v", docs[0]["path"])
	}
	// the card body itself stays inline
	if _, ok := card["content"]; !ok {
		t.Error("card content should still be present")
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
