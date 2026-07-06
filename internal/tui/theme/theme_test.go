package theme

import "testing"

func TestGetSelectsAndDefaults(t *testing.T) {
	if Get("light").Name != "light" {
		t.Fatalf("Get(light).Name = %q", Get("light").Name)
	}
	if Get("").Name != "lathe-dark" {
		t.Fatalf("Get(\"\") should default to dark, got %q", Get("").Name)
	}
	if Get("nope").Name != "lathe-dark" {
		t.Fatalf("Get(unknown) should default to dark, got %q", Get("nope").Name)
	}
}

func TestGlamourStyles(t *testing.T) {
	if Dark().GlamourStyle != "dark" {
		t.Fatalf("dark glamour: %q", Dark().GlamourStyle)
	}
	if Light().GlamourStyle != "light" {
		t.Fatalf("light glamour: %q", Light().GlamourStyle)
	}
}
