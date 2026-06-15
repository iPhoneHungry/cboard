package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Startup self-heal: a light, non-destructive pass run when the dashboard starts, so a
// board someone edited by hand (dropped a ticket folder or a stray .md into a lane) comes
// up consistent instead of with invisible cards. It:
//   - ensures each lane has a folder + order.json,
//   - adopts loose markdown files in a lane into proper card folders (appended to the order),
//   - appends any untracked card folders / epic sub-tickets to the END of their order,
//   - reports anything it can't safely place (and leaves it untouched).
// It never deletes data or rewrites corrupt JSON — that's `cboard doctor`'s job.

type reconcileReport struct {
	Created  []string // lane dir / order.json created
	Adopted  []string // loose .md turned into a card
	Appended []string // untracked folder appended to an order
	Unplaced []string // couldn't place — left as-is
}

func (r reconcileReport) any() bool {
	return len(r.Created)+len(r.Adopted)+len(r.Appended)+len(r.Unplaced) > 0
}

func (r reconcileReport) print() {
	if !r.any() {
		return
	}
	fmt.Println("Startup reconcile:")
	for _, x := range r.Created {
		fmt.Printf("  created   %s\n", x)
	}
	for _, x := range r.Adopted {
		fmt.Printf("  adopted   %s\n", x)
	}
	for _, x := range r.Appended {
		fmt.Printf("  ordered   %s\n", x)
	}
	for _, x := range r.Unplaced {
		fmt.Printf("  ⚠ left    %s\n", x)
	}
}

var looseMdRe = regexp.MustCompile(`(?i)\.(md|markdown)$`)

func startupReconcile() reconcileReport {
	rep := reconcileReport{}
	for _, lid := range laneIDs() {
		laneDir := mustJoin("kanban", lid)
		orderPath := filepath.Join(laneDir, "order.json")
		if !isDir(laneDir) {
			os.MkdirAll(laneDir, 0o755)
			writeJSON(orderPath, []string{})
			rep.Created = append(rep.Created, lid+"/ (lane dir + order.json)")
		}

		// Adopt loose files dropped directly in the lane (not inside a card folder).
		for _, name := range readDirNames(laneDir) {
			p := filepath.Join(laneDir, name)
			if isDir(p) || name == "order.json" || strings.HasPrefix(name, ".") {
				continue
			}
			if !looseMdRe.MatchString(name) {
				rep.Unplaced = append(rep.Unplaced, fmt.Sprintf("%s/%s (not a card — stray file, left in place)", lid, name))
				continue
			}
			if id, err := adoptLooseCard(lid, name); err == nil {
				rep.Adopted = append(rep.Adopted, fmt.Sprintf("%s/%s → %s", lid, name, id))
			} else {
				rep.Unplaced = append(rep.Unplaced, fmt.Sprintf("%s/%s (couldn't adopt: %v)", lid, name, err))
			}
		}

		// Reconcile the lane order with the folders on disk (untracked → appended at end).
		order := toStringSlice(readJSON(orderPath))
		folders := subdirs(laneDir)
		if reconciled := reconcile(order, folders); !equalSlice(reconciled, order) || !isFile(orderPath) {
			for _, f := range folders {
				if !contains(order, f) {
					rep.Appended = append(rep.Appended, lid+"/"+f)
				}
			}
			writeJSON(orderPath, reconciled)
		}

		// Reconcile each epic's sub-ticket order the same way.
		for _, cid := range folders {
			ep := filepath.Join(laneDir, cid, "epic.json")
			if !isFile(ep) {
				continue
			}
			e, _ := readJSON(ep).(map[string]any)
			var eorder []string
			parallel := false
			if e != nil {
				eorder = toStringSlice(e["order"])
				parallel = truthy(e["parallel"])
			}
			tickets := subdirs(filepath.Join(laneDir, cid, "tickets"))
			if rec := reconcile(eorder, tickets); !equalSlice(rec, eorder) {
				for _, t := range tickets {
					if !contains(eorder, t) {
						rep.Appended = append(rep.Appended, lid+"/"+cid+"/tickets/"+t)
					}
				}
				writeJSON(ep, map[string]any{"order": rec, "parallel": parallel})
			}
		}
	}
	return rep
}

// adoptLooseCard turns a stray markdown file in a lane into a proper card folder
// (<num>-<slug>/task.md), preserving any frontmatter it already had, and appends it to the
// lane's order. The original file's content moves into the card; the loose file is removed.
func adoptLooseCard(lane, filename string) (string, error) {
	src := mustJoin("kanban", lane, filename)
	meta, body := parseFM(readText(src))
	title := metaStr(meta, "title")
	if title == "" {
		base := strings.TrimSuffix(filename, filepath.Ext(filename))
		title = strings.NewReplacer("-", " ", "_", " ").Replace(base)
	}
	id := nextNum() + "-" + slugify(title)
	d := mustJoin("kanban", lane, id)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	full := map[string]any{"title": title, "type": "ticket", "priority": 2, "paused": false,
		"depends_on": []string{}, "context": []string{}, "repo": "", "branch": "",
		"kind": "artifact", "project": ""}
	for k, v := range meta {
		full[k] = v
	}
	full["title"] = title
	content := serializeFM(full) + "\n"
	if strings.TrimSpace(body) != "" {
		content += strings.TrimSpace(body) + "\n"
	}
	if err := os.WriteFile(filepath.Join(d, "task.md"), []byte(content), 0o644); err != nil {
		return "", err
	}
	os.Remove(src)
	updateOrder(lane, func(o []string) []string {
		if contains(o, id) {
			return o
		}
		return append(o, id)
	})
	return id, nil
}
