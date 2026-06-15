# cboard

**A kanban board that lives in a folder.**

No database, no login, no cloud — just files on your machine. One small binary gives you
three ways into the *same* board: a dashboard in your browser, an MCP endpoint your AI agents
can drive, and a CLI for scripts. Made for the world where you and a coding agent share a
to-do list.

[![release](https://img.shields.io/github/v/release/iPhoneHungry/cboard)](https://github.com/iPhoneHungry/cboard/releases/latest)

## 30 seconds to a board

```sh
go install github.com/iPhoneHungry/cboard@latest    # or grab a binary ↓
cboard
```

No Go? Download the binary for your OS from the
[latest release](https://github.com/iPhoneHungry/cboard/releases/latest), make it executable,
and run `cboard`.

That's the whole setup. `cboard` with no arguments opens the dashboard at
**http://localhost:8787** and, if you don't have a board yet, creates one at `~/.cboard/board`
and remembers it. Open the page and start dragging cards.

Want the board somewhere you can see it? Point at a folder for one run — `cboard serve
~/my-board` — or make it your default for good with `cboard config set ~/my-board`.

## Three doors, one board

| | |
|---|---|
| 🖥  **Dashboard** | A clean browser UI — drag cards between lanes, write tickets, drop in screenshots. |
| 🤖  **MCP** | The same board as typed tools for Claude, Codex, or any MCP client — so an agent can add, track, and *work* cards. |
| ⌨️  **CLI** | `cboard ticket "…"`, `move`, `doctor` — for scripts and muscle memory. |

And because the board is just folders and JSON, you can `git` it, `grep` it, back it up, or
read it with your eyeballs. Nothing is hidden in a database.

## Why Project → Epic → Ticket

Three levels, read top-down as **why → what → do**:

- 🎯 **Project** — the *why*. The goal and the docs everything under it should know about.
- 🗂  **Epic** — the *what*. A feature too big for one sitting: a brief, shared docs, and an
  ordered list of tickets that tracks its own progress.
- 📋 **Ticket** — the *do*. One thing you (or an agent) can pick up and finish in a single
  focused pass. The atom of the board.

You don't have to use all three — a lone ticket is perfectly happy on its own. Reach for an
epic when one card isn't enough, and a project when several epics share a goal. The payoff:
each piece of work carries *exactly* the context it needs, nothing more.

## Hand it to an agent

The interface is MCP, so cboard isn't Claude-only — any MCP-capable tool drives the same board.

```sh
cboard serve                                                   # dashboard + MCP, one process
claude mcp add --transport http cboard http://localhost:8787/mcp
```

For **Codex / Cursor / others**, point your tool's MCP config at `…/mcp` and let it read
[`AGENTS.md`](AGENTS.md) — the tool-agnostic guide to the tools and how to behave as a worker.

**Want a worker?** Use the bundled [`kanban-worker` skill](skills/kanban-worker/SKILL.md): it
takes Ready cards in order, runs each in its own clean context, and parks finished work in
**Test & Review** for you to approve — it never marks things Done on its own, and never
invents or reorders tasks. Or write your own loop against the tools; the deterministic bits
(what to pick, logging, ordering) live in the binary so they can't drift.

## Where your stuff lives

```
my-board/
  lanes.json                       # the lanes and their colors
  kanban/<lane>/order.json         # card order within a lane
  kanban/<lane>/<id>/task.md       # a card: frontmatter + markdown
            …/result.json          #   its outcome (done/blocked/…)
            …/reviews.json          #   review-round history
            …/artifacts/  …/assets/ #   what it produced; attachments
  kanban/<lane>/<epic>/tickets/    # an epic's sub-tickets
  projects/<id>/                   # a project's goal + shared docs
  logs/                            # daily summaries + the agent feed
  archive/                         # archived cards — hidden from the board, kept on disk
```

Cards flow `planning → ready → in_progress → blocked → review → done`. **Test & Review is a
human gate**: a worker leaves finished cards there; you approve them to Done or send them back
with a comment. Run `cboard doctor` if a board ever gets out of sync.

**Archive vs. delete.** Archiving a card takes it off the board but keeps its folder under
`archive/` — handy for clearing out Done without losing anything. Deleting removes it and its
files from disk for good. Both work on one card, or in bulk: hit **☑ Select** at the top of
any lane to archive or delete a batch of Done cards at once.

The dashboard is unauthenticated and binds to `127.0.0.1`. To reach it from your phone on a
trusted network (e.g. Tailscale), add `--host 0.0.0.0`.

## CLI, briefly

`serve` · `ticket` · `epic` · `project` · `move` · `list` · `log` · `doctor` · `config` —
run `cboard` with no args to just serve, or `cboard <cmd> -h`. Most commands take
`--root <board>`; without it they use your active board. On startup the board self-heals:
cards you (or anything) drop into a lane by hand get folded into its `order.json`, and a
loose `.md` is adopted into a proper card — `cboard doctor` handles deeper repairs.

## Hacking on it

```sh
go test ./...     # model, worker, and MCP-protocol tests (stdlib only, no deps)
go vet ./...
go build -o cboard .
```

The dashboard lives in `web/index.html` and the empty starter board in `seed/`; both are
embedded into the binary. MIT licensed.
