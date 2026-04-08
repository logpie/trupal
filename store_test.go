package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestKVStoreKeysIncludesPersistedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "disk-only"), []byte("value"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store, err := NewKVStore(dir)
	if err != nil {
		t.Fatalf("NewKVStore() error = %v", err)
	}
	if err := store.Set("memory", []byte("value")); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	keys := store.Keys()
	sort.Strings(keys)

	want := []string{"disk-only", "memory"}
	if len(keys) != len(want) {
		t.Fatalf("Keys() len = %d, want %d (%v)", len(keys), len(want), keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("Keys()[%d] = %q, want %q (%v)", i, keys[i], want[i], keys)
		}
	}
}

func TestKVStoreDeleteRemovesPersistedFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewKVStore(dir)
	if err != nil {
		t.Fatalf("NewKVStore() error = %v", err)
	}
	if err := store.Set("answer", []byte("42")); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := store.Delete("answer"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, ok := store.Get("answer"); ok {
		t.Fatal("Get() returned deleted key")
	}
	if _, err := os.Stat(filepath.Join(dir, "answer")); !os.IsNotExist(err) {
		t.Fatalf("expected persisted key to be deleted, stat err = %v", err)
	}
}
