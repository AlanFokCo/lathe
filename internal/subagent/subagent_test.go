package subagent

import (
	"testing"
	"time"
)

func TestTrackerStartCompleteList(t *testing.T) {
	tr := NewTracker()
	if got := tr.List(); got != nil {
		t.Fatalf("empty tracker should list nil, got %+v", got)
	}
	id := tr.Start("sweep configs")
	if id == "" {
		t.Fatal("Start returned empty id")
	}
	got := tr.List()
	if len(got) != 1 || got[0].Status != "running" || got[0].Description != "sweep configs" {
		t.Fatalf("initial list = %+v", got)
	}
	time.Sleep(time.Millisecond) // ensure Duration > 0
	tr.Complete(id, "completed", 42)
	got = tr.List()
	if got[0].Status != "completed" || got[0].OutputBytes != 42 {
		t.Fatalf("post-complete: %+v", got[0])
	}
	if got[0].Duration <= 0 {
		t.Fatalf("Duration not set: %+v", got[0])
	}
}

func TestTrackerCompleteUnknownIDIsNoop(t *testing.T) {
	tr := NewTracker()
	tr.Complete("bogus", "completed", 1) // must not panic
	if got := tr.List(); got != nil {
		t.Fatalf("unknown complete should not add entries: %+v", got)
	}
}

func TestTrackerListReturnsCopy(t *testing.T) {
	tr := NewTracker()
	tr.Start("a")
	snap := tr.List()
	snap[0].Description = "MUTATED"
	if tr.List()[0].Description != "a" {
		t.Fatal("Tracker.List should return a copy, mutation leaked")
	}
}
