package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Board doctor — check and repair a board after something broke mid-stream.
// Dry-run by default; apply=true writes the fixes (mirrors board_doctor.py).

type issue struct {
	Type   string `json:"type"`
	Detail string `json:"detail"`
	Fixed  bool   `json:"fixed"`
}

// rawState reports "ok"/"missing"/"corrupt" for a JSON file, plus its parsed value.
func rawState(path string) (string, any) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "missing", nil
		}
		return "corrupt", nil
	}
	var v any
	if json.Unmarshal(b, &v) != nil {
		return "corrupt", nil
	}
	return "ok", v
}

func runDoctor(apply bool) []issue {
	var issues []issue
	add := func(t, detail string) *issue {
		issues = append(issues, issue{Type: t, Detail: detail})
		return &issues[len(issues)-1]
	}
	laneIds := laneIDs()

	// ── lane order.json reconciliation ──
	for _, lid := range laneIds {
		laneDir := mustJoin("kanban", lid)
		orderPath := filepath.Join(laneDir, "order.json")
		if !isDir(laneDir) {
			it := add("missing_lane_dir", lid+"/ does not exist")
			if apply {
				os.MkdirAll(laneDir, 0o755)
				writeJSON(orderPath, []string{})
				it.Fixed = true
			}
			continue
		}
		state, raw := rawState(orderPath)
		folders := subdirs(laneDir)
		order := toStringSlice(raw)
		reconciled := reconcile(order, folders)
		var flagged []*issue
		if state == "corrupt" {
			flagged = append(flagged, add("corrupt_order", lid+"/order.json is not valid JSON"))
		} else if state == "missing" && len(folders) > 0 {
			flagged = append(flagged, add("missing_order", lid+"/order.json is missing"))
		}
		var missing, untracked []string
		for _, c := range order {
			if !contains(folders, c) {
				missing = append(missing, c)
			}
		}
		for _, f := range folders {
			if !contains(order, f) {
				untracked = append(untracked, f)
			}
		}
		if len(missing) > 0 {
			flagged = append(flagged, add("order_lists_missing_folder", fmt.Sprintf("%s: %v have no folder", lid, missing)))
		}
		if len(untracked) > 0 {
			flagged = append(flagged, add("untracked_folder", fmt.Sprintf("%s: %v not in order.json", lid, untracked)))
		}
		if len(flagged) > 0 && (!equalSlice(reconciled, order) || state != "ok") && apply {
			writeJSON(orderPath, reconciled)
			for _, it := range flagged {
				it.Fixed = true
			}
		}
	}

	// ── per-card JSON sanity + epic reconciliation ──
	for _, lid := range laneIds {
		for _, cid := range subdirs(mustJoin("kanban", lid)) {
			cdir := mustJoin("kanban", lid, cid)
			for _, jf := range []struct {
				name  string
				reset any // nil → delete; otherwise write this
				del   bool
			}{{"reviews.json", []string{}, false}, {"result.json", nil, true}} {
				p := filepath.Join(cdir, jf.name)
				if st, _ := rawState(p); st == "corrupt" {
					it := add("corrupt_card_json", lid+"/"+cid+"/"+jf.name)
					if apply {
						copyFile(p, p+".bak")
						if jf.del {
							os.Remove(p)
						} else {
							writeJSON(p, jf.reset)
						}
						it.Fixed = true
					}
				}
			}
			ep := filepath.Join(cdir, "epic.json")
			if isFile(ep) {
				st, raw := rawState(ep)
				tickets := subdirs(filepath.Join(cdir, "tickets"))
				var eorder []string
				rawMap, _ := raw.(map[string]any)
				if rawMap != nil {
					eorder = toStringSlice(rawMap["order"])
				}
				reconciled := reconcile(eorder, tickets)
				if st == "corrupt" || !equalSlice(reconciled, eorder) {
					it := add("epic_order_mismatch", lid+"/"+cid+"/epic.json out of sync with tickets/")
					if apply {
						if st == "corrupt" {
							copyFile(ep, ep+".bak")
						}
						parallel := rawMap != nil && truthy(rawMap["parallel"])
						writeJSON(ep, map[string]any{"order": reconciled, "parallel": parallel})
						it.Fixed = true
					}
				}
			}
		}
	}

	// ── cards stranded in in_progress (a worker that died mid-run) ──
	if contains(laneIds, "in_progress") {
		for _, cid := range subdirs(mustJoin("kanban", "in_progress")) {
			it := add("stranded_in_progress", cid+" left in in_progress — requeue to ready")
			if apply {
				if moveCard(cid, "in_progress", "ready") == nil {
					it.Fixed = true
				}
			}
		}
	}
	return issues
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func copyFile(src, dst string) {
	b, err := os.ReadFile(src)
	if err != nil {
		return
	}
	os.WriteFile(dst, b, 0o644)
}
