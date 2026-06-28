// Package settings loads lathe's settings.json (project + user). Currently
// holds only the hooks configuration; other settings may be added later.
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Settings is the parsed settings.json. The Hooks map is keyed by hook event
// name (PreToolUse, PostToolUse, UserPromptSubmit, Stop).
type Settings struct {
	Hooks map[string][]Matcher `json:"hooks"`
}

// Matcher is one matcher entry under an event: a tool-name pattern and the
// hooks to run when it matches.
type Matcher struct {
	Matcher string    `json:"matcher"`
	Hooks   []Command `json:"hooks"`
}

// Command is one hook command. Type is "command" (the only supported type).
type Command struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"` // seconds; 0 = default 10
}

// Load reads ~/.lathe/settings.json and <cwd>/.lathe/settings.json, merging
// them. A project event list overrides a user event list of the same key
// (whole-list replace). Missing files are silent; a parse error returns an
// error (caller should warn and proceed with no hooks).
func Load(cwd string) (*Settings, error) {
	s := &Settings{Hooks: map[string][]Matcher{}}
	add := func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // missing file is silent
		}
		var doc Settings
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("%s: parse: %w", path, err)
		}
		for event, matchers := range doc.Hooks {
			s.Hooks[event] = matchers // project (loaded later) overrides user
		}
		return nil
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if err := add(filepath.Join(home, ".lathe", "settings.json")); err != nil {
			return nil, err
		}
	}
	if cwd != "" {
		if err := add(filepath.Join(cwd, ".lathe", "settings.json")); err != nil {
			return nil, err
		}
	}
	return s, nil
}
