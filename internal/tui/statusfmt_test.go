package tui

import (
	"strings"
	"testing"
	"time"
)

func TestFormatElapsed(t *testing.T) {
	cases := map[time.Duration]string{
		5 * time.Second:    "5s",
		65 * time.Second:   "1m05s",
		3725 * time.Second: "1h02m",
	}
	for d, want := range cases {
		if got := formatElapsed(d); got != want {
			t.Fatalf("formatElapsed(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestTokPerSec(t *testing.T) {
	if got := tokPerSec(100, 2*time.Second); got != 50 {
		t.Fatalf("tokPerSec(100,2s) = %d, want 50", got)
	}
	if got := tokPerSec(100, 500*time.Millisecond); got != 0 {
		t.Fatalf("sub-second tokPerSec should be 0, got %d", got)
	}
}

func TestHumanTokens(t *testing.T) {
	if humanTokens(999) != "999" {
		t.Fatalf("999 -> %q", humanTokens(999))
	}
	if humanTokens(1234) != "1.2k" {
		t.Fatalf("1234 -> %q", humanTokens(1234))
	}
}

func TestContextBar(t *testing.T) {
	if got := contextBar(32000, 128000); got != "ctx 25% 32.0k/128.0k" {
		t.Fatalf("contextBar = %q", got)
	}
	if got := contextBar(10, 0); got != "" {
		t.Fatalf("size<=0 should be empty, got %q", got)
	}
}

func TestStatusLineShowsContextBar(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o", ctxSize: 128000}, testCfg())
	m.lastIn = 32000
	if got := m.statusLine(); !strings.Contains(got, "ctx 25%") {
		t.Fatalf("status line missing context bar:\n%s", got)
	}
}

func TestActivityLineShowsElapsedAndThroughput(t *testing.T) {
	m := newModel(&fakeControl{model: "gpt-4o"}, testCfg())
	m.state = stateRunning
	m.turnStart = time.Now().Add(-90 * time.Second)
	m.cumOut = 300
	got := m.activityLine()
	if !strings.Contains(got, "1m") || !strings.Contains(got, "tok/s") {
		t.Fatalf("activity line missing elapsed/throughput:\n%s", got)
	}
}
