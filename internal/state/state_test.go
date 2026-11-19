package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveUsesUTF8AndRoundTripsAccents(t *testing.T) {
	root := t.TempDir()
	want := File{Version: 1, Skills: []Skill{{Name: "qualité", Source: "équipe/dépôt", Path: "skills/qualité"}}}
	if err := Save(root, want); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, ".agents", "skipack-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) >= 3 && b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		t.Fatal("BOM found")
	}
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Skills[0].Name != "qualité" {
		t.Fatalf("got %#v", got)
	}
}

func TestLockIsExclusiveAndReleasable(t *testing.T) {
	root := t.TempDir()
	unlock, err := Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(root); err == nil {
		t.Fatal("second lock succeeded while the first was held")
	}
	unlock()
	unlock2, err := Lock(root)
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	unlock2()
}

func TestLockReplacesStaleLock(t *testing.T) {
	root := t.TempDir()
	unlock, err := Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, ".agents", "skipack.lock"), old, old); err != nil {
		t.Fatal(err)
	}
	unlock2, err := Lock(root)
	if err != nil {
		t.Fatalf("stale lock was not replaced: %v", err)
	}
	unlock2()
	_ = unlock
}
