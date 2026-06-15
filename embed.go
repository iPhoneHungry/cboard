package main

import "embed"

// pageHTML is the dashboard, embedded so the binary is fully self-contained.
//
//go:embed web/index.html
var pageHTML []byte

// seedFS is the empty starter board copied into a new board folder on first run.
//
//go:embed seed
var seedFS embed.FS
