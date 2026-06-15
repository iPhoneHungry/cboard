# Working with cboard (for AI agents)

This is the **canonical, tool-agnostic** guide for an AI agent using cboard. Claude Code
loads the worker half of this as the `kanban-worker` skill; Codex, Cursor, Cline, and other
AGENTS.md-aware tools read this file directly. cboard itself is reached the same way
everywhere — over **MCP** — so one set of instructions works across tools.

## What cboard is

A local, filesystem-backed kanban board. Every card is a folder of files (`task.md`,
`result.json`, logs, artifacts), so the board is plain data you can read, diff, and back up.
A single binary serves a human **dashboard** and an **MCP** endpoint from one process.

## Connect

Run the board (`cboard serve` — or it's already running) and point your tool at the MCP
endpoint, default `http://localhost:8787/mcp`:

- **Claude Code:** `claude mcp add --transport http cboard http://localhost:8787/mcp`
  (or drop the repo's `.mcp.json` into your project).
- **Codex / Cursor / other MCP clients:** add an HTTP MCP server with that URL in the tool's
  MCP config.

You do **not** need a board path or filesystem access — every tool below works through MCP,
including against a remote board. Don't edit board files by hand and don't shell out to the
`cboard` CLI from inside an agent; use the tools.

## The model: Project → Epic → Ticket

Read top-down as **why → what → do**:

- **Project** — the goal + shared docs; everything under it inherits this as context.
- **Epic** — a feature too big for one sitting: a shared brief + docs and an ordered set of
  tickets, with progress tracked.
- **Ticket** — one unit of work finishable in a single focused pass. The atom.

Lanes flow `planning → ready → in_progress → blocked → review → done`. **Test & Review is a
human gate** — finished work is parked there; a person approves it to Done.

## Tools

**Read / explore**
- `board_snapshot` — full board. `list_cards` — compact by lane.
- `get_card(id [, ticket])` — one card's detail: body inline; docs/context/artifacts as
  **references** (`path`). For an epic, tickets come as an overview (id, title, status,
  one-line result); pass `ticket` to pull one sub-ticket's full detail.
- `list_projects`, `get_project(id)` — projects with their doc references.
- `read_file(path)` — pull the body of any doc/asset/context reference (utf-8, or base64 for
  binary). This is how you fetch content on demand instead of receiving it all up front.

**Author / track**
- `create_ticket(title [, project, epic, body])`, `create_epic(title [, project, body])`,
  `create_project(name [, body])` — new cards land in `planning`.
- `move_card(id, to)` — move a card between lanes (source lane found automatically).
- `add_review(id [, comment, to])` — record a review round and optionally move the card.

**Worker support**
- `next_card` — the next card to work, applying selection deterministically. Returns
  `{card: null}` when nothing is eligible.
- `set_result(id [, ticket], status, summary, notes, files_changed)` — record a card's outcome.
- `log_progress(action, id [, ticket, summary])` — append logs + upsert the daily summary.

## Acting as a worker

If asked to "run the worker" / "drain the ready lane", operate as a **deterministic
executor**, not a planner:

1. **`next_card`.** It already enforces the rules: ready order, skip paused, every
   `depends_on` must be in Done, and for an epic it returns `next_ticket` (+
   `next_ticket_detail`, the ticket to work, in full). Trust it — don't re-pick or reorder.
2. **Read the brief, pull what you need.** The card body and `reviews` are inline; docs and
   context are references — `read_file` only the ones relevant, broad → narrow (project goal
   → epic brief → ticket task). If the card returned from review, the **latest review
   comment is the work to do now**.
3. **`move_card(id, "in_progress")`**, then `log_progress("picked", id)`.
4. **Execute in isolation.** Do the work in a **fresh sub-agent / clean context** seeded with
   only this card's brief, so unrelated cards never bleed in. Honor the work target: if the
   card has a `repo`, work in it on `branch`; otherwise write the deliverable into the card's
   `artifacts/` folder. For an epic, work tickets in order, **each in its own fresh context**;
   you may deliberately pull a done sibling's detail (`get_card(epicId, ticket=…)`) when it
   informs the current one — curated context is fine, accidental carryover is not.
5. **`set_result(...)`** (add `ticket` for an epic sub-ticket), then
   `log_progress("completed"|"blocked", id, summary)`.
6. **Move to the terminal lane** — `review` when done/needs-review, `blocked` if blocked.
   An epic goes to `review` only when **all** tickets are done. **Never move a card to
   `done`** — that's the human reviewer's call.
7. **Repeat** until `next_card` returns `{card: null}`.

**Hard rules.** Take cards in the order `next_card` gives; record a result + log for every
card you touch. Never reorder the board, touch `planning`, delete cards, invent tasks, skip
arbitrarily, or move anything to `done`. You are a worker, not a manager — file non-binding
ideas via `log_progress("note", …)`, don't act on them.
