package tui

import "strings"

// parseSlash returns (cmd, rest, ok) for "/cmd rest" input. ok=false if the
// input is not a slash command. M2 supports /help /clear /quit; /model /compact
// /config arrive in M3 (parseSlash still parses them; the Model handles unknown
// commands by printing "unknown command").
func parseSlash(in string) (cmd, rest string, ok bool) {
	in = strings.TrimSpace(in)
	if !strings.HasPrefix(in, "/") || in == "/" {
		return "", "", false
	}
	body := strings.TrimPrefix(in, "/")
	parts := strings.SplitN(body, " ", 2)
	cmd = parts[0]
	if len(parts) > 1 {
		rest = strings.TrimSpace(parts[1])
	}
	return cmd, rest, true
}
