package local

import (
	"context"
	"testing"
)

func TestStorePutGetExists(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	if err := store.Put(ctx, "daily/us/2026/test.json", []byte("ok"), "application/json"); err != nil {
		t.Fatal(err)
	}
	exists, err := store.Exists(ctx, "daily/us/2026/test.json")
	if err != nil || !exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	raw, err := store.Get(ctx, "daily/us/2026/test.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "ok" {
		t.Fatalf("unexpected payload %q", string(raw))
	}
}
