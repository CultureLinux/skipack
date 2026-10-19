package state

import (
	"os"
	"path/filepath"
	"testing"
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
