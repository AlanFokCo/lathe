package workspace

import (
	"context"
	"testing"
)

func TestNewWorkspaceNone(t *testing.T) {
	ws, closer, err := NewWorkspace(context.Background(), "", "/tmp")
	if err != nil || ws != nil || closer != nil {
		t.Fatalf("want nil ws/closer, got ws=%v err=%v", ws, err)
	}
	ws2, closer2, err2 := NewWorkspace(context.Background(), "none", "/tmp")
	if err2 != nil || ws2 != nil || closer2 != nil {
		t.Fatalf("want nil ws/closer for none, got ws=%v err=%v", ws2, err2)
	}
}

func TestNewWorkspaceE2BNoKey(t *testing.T) {
	t.Setenv("E2B_API_KEY", "")
	if _, _, err := NewWorkspace(context.Background(), "e2b", "/tmp"); err == nil {
		t.Fatal("want error when E2B_API_KEY is unset")
	}
}

func TestNewWorkspaceUnknownKind(t *testing.T) {
	if _, _, err := NewWorkspace(context.Background(), "bogus", "/tmp"); err == nil {
		t.Fatal("want error for unknown sandbox kind")
	}
}
