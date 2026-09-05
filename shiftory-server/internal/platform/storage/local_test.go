package storage

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestLocalStoreRoundTripAndRejectsTraversal(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	if err := store.Put(context.Background(), "imports/test.txt", strings.NewReader("shiftory")); err != nil {
		t.Fatalf("put: %v", err)
	}
	reader, err := store.Open(context.Background(), "imports/test.txt")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer reader.Close()
	content, _ := io.ReadAll(reader)
	if string(content) != "shiftory" {
		t.Fatalf("unexpected content %q", content)
	}
	if err := store.Put(context.Background(), "../outside.txt", strings.NewReader("bad")); err == nil {
		t.Fatal("expected traversal key to be rejected")
	}
}
