# cboard

A local, filesystem-backed kanban board in a **single, dependency-free binary**. One
`go build` produces a self-contained executable for Linux, macOS, or Windows — no Python,
no Node, no runtime to install. The board is just folders and JSON on disk, so it's easy to
read, diff, and back up.

This is a Go rewrite of an earlier Python version: the dashboard HTML/CSS/JS is carried over
unchanged, and the on-disk format is identical, so existing boards keep working.

## Install

```sh
go build -o cboard .          # current OS
# cross-compile, e.g. for a Mac from Linux:
GOOS=darwin GOARCH=arm64 go build -o cboard-mac .
```

## Quick start

```sh
cboard init ~/my-board        # seed an empty board and make it the active one
cboard serve                  # open the dashboard at http://localhost:8787/
```

`init` records the board in `~/.cboard.json`, so `serve` and the authoring commands find it
without a path. Pass `--root <dir>` (or a positional folder to `serve`) to target another board.

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
