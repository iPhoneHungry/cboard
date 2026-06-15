package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Deterministic logger for the worker. Records each action in three places:
//   - logs/agent/YYYY-MM-DD.log   one line appended (rotating agent feed)
//   - <card>/task.log             one line appended (the card's own log, if found)
//   - logs/daily/YYYY-MM-DD.md    UPSERTED — merged into Completed/Blocked, Summary recomputed
//
// The daily file is never overwritten wholesale; it is parsed, merged, and rewritten,
// so concurrent worker runs across a day accumulate instead of clobbering each other.

var dailySections = []string{"Completed", "Blocked"}

func cardDir(cid, ticket string) string {
	for _, lid := range laneIDs() {
		d := mustJoin("kanban", lid, cid)
		if isDir(d) {
			if ticket != "" {
				return filepath.Join(d, "tickets", ticket)
			}
			return d
		}
	}
	return ""
}

// parseDaily returns {"Completed": [...], "Blocked": [...]} bullets from an existing daily file.
func parseDaily(path string) map[string][]string {
	out := map[string][]string{}
	for _, s := range dailySections {
		out[s] = []string{}
	}
	data := readText(path)
	if data == "" {
		return out
	}
	current := ""
	for _, raw := range strings.Split(data, "\n") {
		line := strings.TrimRight(raw, "\n")
		h := strings.TrimSpace(line)
		if strings.HasPrefix(h, "## ") {
			current = strings.TrimSpace(h[3:])
			continue
		}
		if _, ok := out[current]; ok && strings.HasPrefix(strings.TrimSpace(line), "- ") {
			out[current] = append(out[current], strings.TrimSpace(line))
		}
	}
	return out
}

// taskKey extracts "<task>" from "- <task> — summary".
func taskKey(bullet string) string {
	return strings.TrimSpace(strings.SplitN(strings.TrimPrefix(bullet, "- "), " — ", 2)[0])
}

func upsertDaily(today, task, summaryText, section string) error {
	path := mustJoin("logs", "daily", today+".md")
	os.MkdirAll(filepath.Dir(path), 0o755)
	sections := parseDaily(path)
	bullet := "- " + task
	if summaryText != "" {
		bullet += " — " + summaryText
	}
	for _, s := range dailySections {
		kept := []string{}
		for _, b := range sections[s] {
			if taskKey(b) != task {
				kept = append(kept, b)
			}
		}
		sections[s] = kept
	}
	sections[section] = append(sections[section], bullet)
	done, blocked := len(sections["Completed"]), len(sections["Blocked"])
	var lines []string
	for _, s := range dailySections {
		lines = append(lines, "## "+s)
		lines = append(lines, sections[s]...)
		lines = append(lines, "")
	}
	lines = append(lines, "## Summary", fmt.Sprintf("- %d completed, %d blocked", done, blocked), "")
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

func logAction(action, cid, ticket, summaryText string) string {
	now := time.Now()
	today := now.Format("2006-01-02")
	task := cid
	if ticket != "" {
		task = cid + "/" + ticket
	}
	line := fmt.Sprintf("[%s] %s %s", now.Format("15:04"), action, task)
	if summaryText != "" {
		line += " — " + summaryText
	}
	appendLine(mustJoin("logs", "agent", today+".log"), line)
	if cdir := cardDir(cid, ticket); cdir != "" {
		appendLine(filepath.Join(cdir, "task.log"), line)
	}
	switch action {
	case "completed":
		upsertDaily(today, task, summaryText, "Completed")
	case "blocked":
		upsertDaily(today, task, summaryText, "Blocked")
	}
	return task
}
