# cboard

**A private, local kanban board for your coding agents. Download it, run it, and get out of the terminal.**

cboard is a kanban board that lives in a folder on your machine. One small binary gives you a
board in your browser *and* an endpoint your coding agent plugs into — so your agent keeps
processing tickets while you plan, drag, and review in a clean UI
instead of squinting at terminal scrollback.

It's for people who want to break work down *past* Jira and "the sprint" — into real tickets,
epics, and projects — without juggling a stack of SaaS tools to get there.

And it's all local: no database, no account, no cloud, nothing leaving your machine. A task
system you can actually run at work without making the security team lose their minds.

[![ci](https://github.com/iPhoneHungry/cboard/actions/workflows/ci.yml/badge.svg)](https://github.com/iPhoneHungry/cboard/actions/workflows/ci.yml) [latest release](https://github.com/iPhoneHungry/cboard/releases/latest)

## Three steps

### 1. Download & run

Grab the binary for your OS from the **[latest release](https://github.com/iPhoneHungry/cboard/releases/latest)** — no Go, no build.

**macOS** — Apple Silicon (swap in `cboard-darwin-amd64` on Intel):

```sh
curl -L -o cboard https://github.com/iPhoneHungry/cboard/releases/latest/download/cboard-darwin-arm64
chmod +x cboard && ./cboard
```

Downloaded it through the browser instead? Clear Gatekeeper once: `xattr -d com.apple.quarantine cboard`.

**Windows** — PowerShell:

```powershell
curl.exe -L -o cboard.exe https://github.com/iPhoneHungry/cboard/releases/latest/download/cboard-windows-amd64.exe
.\cboard.exe
```

*Linux: grab `cboard-linux-amd64` / `-arm64`, `chmod +x`, run. Prefer Go anywhere? `go install github.com/iPhoneHungry/cboard@latest && cboard`.*

`cboard` with no arguments opens the dashboard at **http://localhost:8787** and creates a board
at `~/.cboard/board` (remembered for next time). Open the page and start stacking tickets for
your worker.

### 2. Add work

Hit **➕ New card** in the dashboard to write tickets — group them into epics and projects when
one card outgrows a single commit (see [Project → Epic → Ticket](#project--epic--ticket) below).
Drag a card up within a lane to set its priority, and into **Ready** to queue it: the top of
Ready is what the worker picks next.

### 3. Connect an agent

cboard talks to agents over **MCP** — one endpoint, no plugin, no skill to install. Point any
MCP client at the board (same command on every OS):

```sh
claude mcp add --transport http cboard http://localhost:8787/mcp
```

Using Codex, Cursor, or something else? Add `http://localhost:8787/mcp` as an HTTP MCP server
in its config. Either way your agent now has the board's tools — `next_card`, `set_result`,
`move_card`, and the rest.

Then tell it to work. Paste this to your agent:

> You are my cboard worker. Use the cboard MCP tools to drain the **Ready** lane: call
> `next_card`, take cards top-down **one at a time**, work each in its own clean context, record
> the outcome with `set_result`, then `move_card` the finished card to **Test & Review** — never
> to Done. Don't reorder, invent, or skip cards. Repeat until `next_card` returns nothing, then
> stop and summarize.

That's the whole loop: it picks the top of Ready, runs each card in isolation, and parks the
result in **Test & Review** for you to approve — it never marks anything Done on its own. The
deterministic bits — what to pick, ordering, logging — live in the binary's tools, so the
worker can't drift no matter which agent runs it. The full worker contract is
[`AGENTS.md`](AGENTS.md).

## Project → Epic → Ticket

Three levels, sized to how an agent commits work:

- **Ticket** — the atom. One focused change, about a branch with a single commit.
  *Example: "Add a `--port` flag to `cboard serve`."*
- **Epic** — a feature too big for one commit: an ordered, **stacked** set of tickets built and
  tested together, like a feature branch. *Example: "User auth" → tickets for the schema, the
  login endpoint, session middleware, and the tests — in order.*
- **Project** — the standing context a group of epics shares: *why* you're doing it, where the
  repo is, which files the agent may read, links, conventions. *Example: "Billing rewrite" —
  the goal, the repo path, the Stripe API docs link, the design doc.*

You don't have to use all three — a lone ticket is perfectly happy on its own. Reach for an
epic when one card isn't enough, and a project when several epics share a goal. The payoff:
each piece of work carries *exactly* the context it needs, nothing more.

## Three doors, one board

| | |
|---|---|
| **Dashboard** | A clean browser UI — drag cards between lanes, write tickets, drop in screenshots. |
| **MCP** | The same board as typed tools for Claude, Codex, or any MCP client — so an agent can add, track, and *work* cards. |
| **CLI** | `cboard ticket "…"`, `move`, `doctor` — for scripts and muscle memory. |

And because the board is just folders and JSON, you can `git` it, `grep` it, back it up, or
read it with your eyeballs. Nothing is hidden in a database.

## Going deeper

The on-disk layout, how cards flow through the lanes, ordering, archiving, networking, and the
full CLI live in **[docs/reference.md](docs/reference.md)**.

## Hacking on it

```sh
go test ./...     # model, worker, and MCP-protocol tests (stdlib only, no deps)
go vet ./...
go build -o cboard .
```

The dashboard lives in `web/index.html` and the empty starter board in `seed/`; both are
embedded into the binary. MIT licensed.
