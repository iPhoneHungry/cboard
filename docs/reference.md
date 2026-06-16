# cboard reference

Deeper detail than the [README](../README.md): the on-disk layout, how cards flow through the
lanes, ordering, archiving, networking, and the full CLI.

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

By default the board lives at `~/.cboard/board` (created on first run and remembered). Want it
somewhere you can see it? Point at a folder for one run — `cboard serve ~/my-board` — or make
it your default for good with `cboard config set ~/my-board`.

## Lanes & flow

Cards flow `planning → ready → in_progress → blocked → review → done`. **Test & Review is a
human gate**: a worker leaves finished cards there; you approve them to Done or send them back
with a comment. Run `cboard doctor` if a board ever gets out of sync.

**Ordering is priority.** Drag a card up within a lane to reorder it — the worker takes the
**top of Ready** first, so that's how you say "do this next." Drag across lanes to move it.

## Archive vs. delete

Archiving a card takes it off the board but keeps its folder under `archive/` — handy for
clearing out Done without losing anything. Deleting removes it and its files from disk for
good. Both work on one card, or in bulk: hit **☑ Select** at the top of any lane to archive or
delete a batch of Done cards at once.

## Networking

The dashboard is unauthenticated and binds to `127.0.0.1`. To reach it from your phone on a
trusted network (e.g. Tailscale), add `--host 0.0.0.0`.

## CLI

`serve` · `ticket` · `epic` · `project` · `move` · `list` · `log` · `doctor` · `config` — run
`cboard` with no args to just serve, or `cboard <cmd> -h` for one command's flags. Most
commands take `--root <board>`; without it they use your active board.

On startup the board self-heals: cards you (or anything) drop into a lane by hand get folded
into its `order.json`, and a loose `.md` is adopted into a proper card — `cboard doctor`
handles deeper repairs.
