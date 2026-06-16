package main

import (
	"os"
	"strings"
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

func TestEpicTicketsLeanWithPullableSiblings(t *testing.T) {
	newTestBoard(t)
	eid, _ := createCard("Epic", "epic", "")
	moveCard(eid, "planning", "ready")
	addEpicTicket(eid, "First", "the first ticket body")
	addEpicTicket(eid, "Second", "the second body")
	// finish ticket 1 with a result summary — like a done sibling.
	setResult("ready", eid, "001-first", "done", "built the first thing", nil, []string{"artifacts/a.html"})

	card := getCardDetail("ready", eid)
	tickets := card["tickets"].([]map[string]any)
	if len(tickets) != 2 {
		t.Fatalf("want 2 ticket summaries, got %d", len(tickets))
	}
	// overview shows status + one-line result, but NOT the body.
	if tickets[0]["status"] != "done" || tickets[0]["summary"] != "built the first thing" {
		t.Errorf("ticket 1 summary = %#v", tickets[0])
	}
	if _, hasContent := tickets[0]["content"]; hasContent {
		t.Error("epic ticket overview should not inline the body")
	}
	if tickets[1]["status"] != "todo" {
		t.Errorf("ticket 2 status = %v, want todo (pending)", tickets[1]["status"])
	}

	// pull a specific sibling's full info on demand.
	det, err := ticketDetail("ready", eid, "001-first")
	if err != nil {
		t.Fatal(err)
	}
	if det["content"] != "the first ticket body" {
		t.Errorf("pulled ticket body = %v", det["content"])
	}
	res := det["result"].(map[string]any)
	if res["summary"] != "built the first thing" {
		t.Errorf("pulled result = %#v", res)
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

// TestNestedWorkContextLayering checks the hand-off for the deepest case — a ticket inside an
// epic inside a project, with a global board context — and asserts every layer reaches the work
// unit: global → everyone (get_context), project → its epic (project_context), epic docs → its
// tickets (the epic detail rides along in next_card), and the ticket itself in full. This is the
// inheritance contract; the shallower cases (ticket-in-epic, lone ticket+project) are subsets.
func TestNestedWorkContextLayering(t *testing.T) {
	newTestBoard(t)

	// Global board context — the broadest layer, shared by everything.
	if err := saveBoardContext("repos live in ~/code; test with go test"); err != nil {
		t.Fatal(err)
	}

	// A project with a goal + a shared doc — should reach its epics.
	pid, err := createProject("Billing")
	if err != nil {
		t.Fatal(err)
	}
	saveProject(pid, "# Billing\n\n**Goal:** rewrite billing")
	if _, err := addProjectDoc(pid, "stripe.md"); err != nil {
		t.Fatal(err)
	}

	// An epic in that project, with its own brief + a shared doc — should reach all its tickets.
	eid, err := createCard("Checkout", "epic", pid)
	if err != nil {
		t.Fatal(err)
	}
	saveBody("planning", eid, "", "", "the checkout epic brief")
	if err := saveDoc("planning", eid, "design.md", "# Design\nthe epic design doc"); err != nil {
		t.Fatal(err)
	}
	addEpicTicket(eid, "Schema", "create the schema")
	moveCard(eid, "planning", "ready")

	res := selectNextCard()
	card, _ := res["card"].(map[string]any)
	if card == nil || card["id"] != eid {
		t.Fatalf("expected epic %q selected, got %#v", eid, res["card"])
	}

	// 1) global → everyone: the board context is available (it's what get_context returns).
	if got := readBoardContext(); got != "repos live in ~/code; test with go test" {
		t.Errorf("global context = %q", got)
	}

	// 2) project → epic: the epic detail carries its project's goal + doc references.
	pc, _ := card["project_context"].(map[string]any)
	if pc == nil {
		t.Fatal("project did not reach the epic: project_context missing")
	}
	if g, _ := pc["goal"].(string); !strings.Contains(g, "rewrite billing") {
		t.Errorf("project goal not handed to the epic: %q", g)
	}
	if !hasDocNamed(asDocs(pc["docs"]), "stripe.md") {
		t.Errorf("project doc reference not handed to the epic: %#v", pc["docs"])
	}

	// 3) epic → its items: the epic's own docs/links travel with the work unit.
	if !hasDocNamed(asDocs(card["docs"]), "design.md") {
		t.Errorf("epic doc reference missing from the hand-off: %#v", card["docs"])
	}

	// 4) the unit of work itself — the ticket to do now, body included.
	if res["next_ticket"] != "001-schema" {
		t.Errorf("next_ticket = %v, want 001-schema", res["next_ticket"])
	}
	nt, _ := res["next_ticket_detail"].(map[string]any)
	if nt == nil || nt["content"] != "create the schema" {
		t.Errorf("next_ticket_detail body = %#v", nt)
	}

	// docs are handed as references (path), never inlined wholesale.
	for _, d := range append(asDocs(pc["docs"]), asDocs(card["docs"])...) {
		if _, inlined := d["content"]; inlined {
			t.Errorf("doc %v should be a reference, not inlined", d["name"])
		}
	}
}

// TestBoardContextCarriesDocsAndAssets checks the global level is a full context peer: its
// shared docs and file assets are returned alongside the standing note, and as references (no
// inlined doc bodies) the way projects/cards are.
func TestBoardContextCarriesDocsAndAssets(t *testing.T) {
	newTestBoard(t)
	if err := saveBoardContext("global note"); err != nil {
		t.Fatal(err)
	}
	if _, err := addBoardDoc("conventions"); err != nil { // gets a .md extension
		t.Fatal(err)
	}
	if _, err := addBoardAsset("logo.png", []byte{0x89, 'P', 'N', 'G'}); err != nil {
		t.Fatal(err)
	}

	bc := boardContext()
	if bc["body"] != "global note" {
		t.Errorf("body = %v", bc["body"])
	}
	docs := asDocs(bc["docs"])
	if !hasDocNamed(docs, "conventions.md") {
		t.Fatalf("board doc missing: %#v", docs)
	}
	assets := asDocs(bc["assets"])
	if !hasDocNamed(assets, "logo.png") {
		t.Fatalf("board asset missing: %#v", assets)
	}
	if assets[0]["path"] != "context/assets/logo.png" {
		t.Errorf("asset path = %v", assets[0]["path"])
	}

	// get_context (the agent's view) returns docs/assets as references — no inlined doc bodies.
	res, err := callContextTool()
	if err != nil {
		t.Fatal(err)
	}
	if res["context"] != "global note" {
		t.Errorf("get_context context = %v", res["context"])
	}
	for _, d := range asDocs(res["docs"]) {
		if _, inlined := d["content"]; inlined {
			t.Errorf("get_context doc %v should be a reference, not inlined", d["name"])
		}
	}
	if !hasDocNamed(asDocs(res["docs"]), "conventions.md") || !hasDocNamed(asDocs(res["assets"]), "logo.png") {
		t.Errorf("get_context missing docs/assets: %#v", res)
	}
}

// callContextTool invokes the get_context MCP tool handler the way an agent would.
func callContextTool() (map[string]any, error) {
	for _, tl := range mcpTools {
		if tl.Name == "get_context" {
			r, err := tl.handler(map[string]any{})
			if err != nil {
				return nil, err
			}
			return r.(map[string]any), nil
		}
	}
	t := map[string]any{}
	return t, nil
}

func asDocs(v any) []map[string]any {
	d, _ := v.([]map[string]any)
	return d
}

func hasDocNamed(docs []map[string]any, name string) bool {
	for _, d := range docs {
		if d["name"] == name {
			return true
		}
	}
	return false
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
