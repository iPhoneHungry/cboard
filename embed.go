package main

import "embed"

// pageHTML is the dashboard, embedded so the binary is fully self-contained.
//
//go:embed web/index.html
var pageHTML []byte

// markedJS and purifyJS are the markdown renderer and HTML sanitizer, vendored and embedded
// so the dashboard renders (and sanitizes) without reaching out to a third-party CDN — the
// binary stays self-contained and offline-capable, and there's no CDN supply-chain surface.
//
//go:embed web/marked.min.js
var markedJS []byte

//go:embed web/purify.min.js
var purifyJS []byte

// seedFS is the empty starter board copied into a new board folder on first run.
//
//go:embed seed
var seedFS embed.FS

// workerContract is AGENTS.md, embedded and served over MCP as the `kanban-worker` prompt so a
// connected agent gets the worker contract from the binary itself — no separate skill or plugin
// to install, and a single source of truth that ships (and versions) with each release.
//
//go:embed AGENTS.md
var workerContract string
