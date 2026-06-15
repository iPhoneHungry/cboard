package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Active-board config, stored at ~/.cboard.json so all subcommands share one default
// board: `cboard init` records the board it sets up; serve/work/plan fall back to it.

func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".cboard.json")
}

func configLoad() map[string]any {
	b, err := os.ReadFile(configPath())
	if err != nil {
		return map[string]any{}
	}
	var v map[string]any
	if json.Unmarshal(b, &v) != nil {
		return map[string]any{}
	}
	return v
}

func configGetBoard() string {
	if v, ok := configLoad()["board"].(string); ok {
		return v
	}
	return ""
}

func configSetBoard(path string) (string, error) {
	abs, err := filepath.Abs(expandUser(path))
	if err != nil {
		return "", err
	}
	data := configLoad()
	data["board"] = abs
	b, _ := json.MarshalIndent(data, "", "  ")
	if err := os.WriteFile(configPath(), b, 0o644); err != nil {
		return "", err
	}
	return abs, nil
}

func expandUser(p string) string {
	if p == "~" || len(p) >= 2 && p[:2] == "~/" {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
