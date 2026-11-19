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

func TestParseRefRules(t *testing.T) {
	ok := "version: 1\ngroups:\n  g:\n    - source: a/b\n      skill: s\n      ref: v1.2.0\n"
	c, err := Parse([]byte(ok))
	if err != nil || c.Groups["g"][0].Ref != "v1.2.0" {
		t.Fatalf("cfg=%#v err=%v", c, err)
	}
	for name, doc := range map[string]string{
		"url source": "version: 1\ngroups:\n  g:\n    - source: https://github.com/a/b/tree/main/s\n      ref: v1\n",
		"bad chars":  "version: 1\ngroups:\n  g:\n    - source: a/b\n      skill: s\n      ref: \"v1 --x\"\n",
		"dash first": "version: 1\ngroups:\n  g:\n    - source: a/b\n      skill: s\n      ref: -rf\n",
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestParseRejectsBadDocuments(t *testing.T) {
	for name, doc := range map[string]string{
		"unknown field": "version: 1\ngroups:\n  g:\n    - source: a/b\n      color: red\n",
		"no groups":     "version: 1\n",
		"no source":     "version: 1\ngroups:\n  g:\n    - skill: x\n",
		"empty group":   "version: 1\ngroups:\n  \"\":\n    - source: a/b\n",
		"invalid utf8":  "version: 1\ngroups:\n  \xff:\n",
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestParseAcceptsBOM(t *testing.T) {
	if _, err := Parse([]byte("\xef\xbb\xbfversion: 1\ngroups:\n  g:\n    - source: a/b\n      skill: s\n")); err != nil {
		t.Fatal(err)
	}
}

func TestStoreErrors(t *testing.T) {
	dir := t.TempDir()
	s := Store{Path: filepath.Join(dir, "config.json")}
	if _, err := s.Get(); err == nil {
		t.Fatal("missing settings accepted")
	}
	if err := s.Set(""); err == nil {
		t.Fatal("empty source accepted")
	}
	os.WriteFile(s.Path, []byte("not json"), 0600)
	if _, err := s.Get(); err == nil {
		t.Fatal("invalid json accepted")
	}
	os.WriteFile(s.Path, []byte(`{"source":""}`), 0600)
	if _, err := s.Get(); err == nil {
		t.Fatal("empty stored source accepted")
	}
}
