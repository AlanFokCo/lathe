package tui

import (
	"fmt"
	"strconv"
	"time"
)

// formatElapsed renders a compact elapsed duration: 45s, 2m05s, 1h03m. M6c.
func formatElapsed(d time.Duration) string {
	s := int(d.Seconds())
	if s < 0 {
		s = 0
	}
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	default:
		return fmt.Sprintf("%dh%02dm", s/3600, (s%3600)/60)
	}
}

// tokPerSec returns output tokens/sec, or 0 when under a second (avoids noise). M6c.
func tokPerSec(tokens int, d time.Duration) int {
	sec := d.Seconds()
	if sec < 1 || tokens <= 0 {
		return 0
	}
	return int(float64(tokens) / sec)
}

// humanTokens abbreviates a token count: 999 -> "999", 1234 -> "1.2k". M6c.
func humanTokens(n int) string {
	if n < 1000 {
		return strconv.Itoa(n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

// contextBar renders context-window usage from the last request's input tokens.
// Empty when the size is unknown (<=0). M6c.
func contextBar(used, size int) string {
	if size <= 0 {
		return ""
	}
	pct := used * 100 / size
	if pct > 100 {
		pct = 100
	}
	return fmt.Sprintf("ctx %d%% %s/%s", pct, humanTokens(used), humanTokens(size))
}
