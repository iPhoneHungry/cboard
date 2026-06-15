# cboard

A local, filesystem-backed kanban board in a **single, dependency-free binary**. One
`go build` produces a self-contained executable for Linux, macOS, or Windows — no Python,
no Node, no runtime to install. The board is just folders and JSON on disk, so it's easy to
read, diff, and back up.

It opens **three doors onto the same board**, all sharing one core:

- **Dashboard** — a browser UI for humans (`http://localhost:8787/`)
- **MCP** — typed tools for Claude and any MCP client (`http://localhost:8787/mcp`)
- **CLI** — subcommands for scripts and cron

This is a Go rewrite of an earlier Python version: the dashboard HTML/CSS/JS is carried over
unchanged, and the on-disk format is identical, so existing boards keep working.

## Install

```sh
# devs / Go users:
go install github.com/iPhoneHungry/cboard@latest

# everyone else: grab the binary for your OS from the Releases page, then:
chmod +x cboard-* && mv cboard-* /usr/local/bin/cboard
```

…or build from a clone: `go build -o cboard .`

## Quick start

```sh
cboard            # just run it — serves the dashboard + MCP, auto-creating a board
```

With no board configured yet, the first run creates one at `~/.cboard/board` and marks it
active, so you never have to think about where the folders are. Every door (dashboard, MCP,
CLI) then finds that board from any directory. Want it elsewhere? `cboard init ~/my-board`
once, or pass `--root <dir>` / a folder argument to `serve`.

## Connecting Claude (MCP)

The dashboard process also serves MCP, so one running `cboard` covers both the browser and
agents. Point Claude Code at it once:

```sh
claude mcp add --transport http cboard http://localhost:8787/mcp
```

…or drop the included [`.mcp.json`](.mcp.json) into a project so Claude offers to connect
automatically. Tools exposed:

- **Author / track:** `board_snapshot`, `list_cards`, `create_ticket`, `create_epic`,
  `create_project`, `move_card`, `add_review`, `doctor`
- **Worker support:** `next_card` (deterministic selection — ready order, skip paused,
  `depends_on`, epic next-ticket), `get_card` (full brief with inlined context), `set_result`,
  `log_progress`

## The worker: bring your own, or use ours

The board doesn't care who moves the cards — you can drive the MCP tools however you like. If
you want a disciplined task runner, use the bundled **`kanban-worker` skill**
([`skills/kanban-worker/SKILL.md`](skills/kanban-worker/SKILL.md)): it picks Ready cards in
strict order, runs **each card in its own fresh sub-agent** (context isolation), records a
result, and parks finished work in **Test & Review** — never auto-completing to Done, never
inventing or reordering tasks.

The split is deliberate: the **deterministic mechanics** (selection, ordering, logging,
`order.json` consistency) live in the binary as MCP tools, so they can't be gotten wrong; the
**judgment and isolation** (which sub-agent runs what, repo-vs-artifact, the review gate) live
in the skill, because only an agent can do those. Any MCP client can write its own worker loop
against the same tools.

## Commands

| Command | What it does |
|---|---|
| `cboard init [dir]` | Seed a board folder and set it active |
| `cboard serve [dir] [--port N] [--host H]` | Run the dashboard (default `localhost:8787`) |
| `cboard ticket "Title" [--project P] [--epic E] [--body T]` | Create a ticket (lands in `planning`) |
| `cboard epic "Title" [--project P] [--body T]` | Create an epic |
| `cboard project "Name" [--body T]` | Create a project (groups epics/tickets) |
| `cboard move <id> <lane>` | Move a top-level card to another lane |
| `cboard list` | List cards by lane (JSON) |
| `cboard log <action> <id> [--ticket T] [--summary S]` | Append worker logs + upsert the daily summary |
| `cboard doctor [--apply]` | Check (and optionally repair) the board |
| `cboard config get \| set <path>` | Show / set the active board |

The dashboard is **unauthenticated** and binds to `127.0.0.1` by default. To reach it from a
phone on a trusted network (e.g. Tailscale), pass `--host 0.0.0.0`.

## How the board is stored

```
my-board/
  lanes.json                     # lane ids, names, colors
  kanban/<lane>/order.json       # ordering within a lane
  kanban/<lane>/<id>/task.md     # frontmatter + markdown body
  kanban/<lane>/<id>/result.json # status (todo/done/blocked/…)
  kanban/<lane>/<id>/reviews.json# review rounds
  kanban/<lane>/<epic>/epic.json # sub-ticket order + parallel flag
  kanban/<lane>/<epic>/tickets/  # epic sub-tickets
  projects/<id>/                 # project goal + shared docs
  logs/agent/<date>.log          # rotating worker feed
  logs/daily/<date>.md           # merged daily summary
  trash/                         # soft-deletes (never hard-deleted)
```

Lanes: `planning → ready → in_progress → blocked → review → done`. `review` ("Test & Review")
is a human approval gate before `done`. Every move keeps `order.json` consistent with the
folders on disk; `cboard doctor` reconciles them if anything drifts.

## Develop

```sh
go test ./...     # unit tests
go vet ./...
go build -o cboard .
```

The dashboard page lives in `web/index.html` and is embedded into the binary (`embed.go`),
along with the empty starter board in `seed/`.
