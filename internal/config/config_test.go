package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePreservesUTF8AndValidatesVersion(t *testing.T) {
	cfg, err := Parse([]byte("version: 1\ngroups:\n  outils:\n    - source: propriétaire/dépôt\n      skill: qualité\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Groups["outils"][0].Skill; got != "qualité" {
		t.Fatalf("skill = %q", got)
	}
	if _, err := Parse([]byte("version: 2\ngroups: {}\n")); err == nil {
		t.Fatal("version 2 accepted")
	}
}

func TestStoreWritesSourceAsUTF8WithoutBOM(t *testing.T) {
	p := filepath.Join(t.TempDir(), "skipack", "config.json")
	s := Store{Path: p}
	if err := s.Set("C:/données/skills.yaml"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) >= 3 && b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		t.Fatal("unexpected UTF-8 BOM")
	}
	got, err := s.Get()
	if err != nil {
		t.Fatal(err)
	}
	if got != "C:/données/skills.yaml" {
		t.Fatalf("source = %q", got)
	}
}
