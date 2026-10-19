package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyklodev/skipack/internal/config"
	projectstate "github.com/cyklodev/skipack/internal/state"
)

func TestHelpShowsCommandsAndExamples(t *testing.T) {
	var out, errs bytes.Buffer
	a := App{Out: &out, Err: &errs}
	if code := a.Run([]string{"help"}); code != 0 {
		t.Fatalf("code=%d error=%q", code, errs.String())
	}
	for _, text := range []string{"Usage:", "Commands:", "delete <group>", "Examples:", "skipack config set https://github.com/"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("help does not contain %q:\n%s", text, out.String())
		}
	}
}

func TestNoArgumentsShowsHelpSuccessfully(t *testing.T) {
	var out, errs bytes.Buffer
	if code := (App{Out: &out, Err: &errs}).Run(nil); code != 0 {
		t.Fatalf("code=%d error=%q", code, errs.String())
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("output=%q", out.String())
	}
}

func TestConfigSetStoresAbsoluteLocalPathAndShowPrintsIt(t *testing.T) {
	d := t.TempDir()
	cfg := filepath.Join(d, "skills.yaml")
	os.WriteFile(cfg, []byte("version: 1\ngroups: {}\n"), 0644)
	var out, errs bytes.Buffer
	a := App{Store: config.Store{Path: filepath.Join(d, "user", "config.json")}, Root: d, Out: &out, Err: &errs}
	if code := a.Run([]string{"config", "set", cfg}); code != 0 {
		t.Fatalf("code=%d err=%s", code, errs.String())
	}
	out.Reset()
	if code := a.Run([]string{"config", "show"}); code != 0 {
		t.Fatalf("code=%d", code)
	}
	abs, _ := filepath.Abs(cfg)
	if strings.TrimSpace(out.String()) != abs {
		t.Fatalf("show=%q want=%q", out.String(), abs)
	}
}

func TestListPrintsUTF8GroupsAndSkills(t *testing.T) {
	d := t.TempDir()
	cfg := filepath.Join(d, "skills.yaml")
	os.WriteFile(cfg, []byte("version: 1\ngroups:\n  qualité:\n    - source: acme/repo\n      skill: contrôle\n"), 0644)
	store := config.Store{Path: filepath.Join(d, "config.json")}
	store.Set(cfg)
	var out, errs bytes.Buffer
	a := App{Store: store, Root: d, Out: &out, Err: &errs}
	if code := a.Run([]string{"list"}); code != 0 {
		t.Fatalf("code=%d err=%s", code, errs.String())
	}
	if !strings.Contains(out.String(), "qualité") || !strings.Contains(out.String(), "contrôle") {
		t.Fatalf("output=%q", out.String())
	}
}

func TestAddUnknownGroupReturnsFailure(t *testing.T) {
	d := t.TempDir()
	cfg := filepath.Join(d, "skills.yaml")
	os.WriteFile(cfg, []byte("version: 1\ngroups: {}\n"), 0644)
	store := config.Store{Path: filepath.Join(d, "config.json")}
	store.Set(cfg)
	var out, errs bytes.Buffer
	a := App{Store: store, Root: d, Out: &out, Err: &errs}
	if code := a.Run([]string{"add", "missing"}); code == 0 {
		t.Fatal("unknown group succeeded")
	}
	if !strings.Contains(errs.String(), "unknown group") {
		t.Fatalf("error=%q", errs.String())
	}
}

func TestDeleteGroupRemovesTrackedSkill(t *testing.T) {
	d := t.TempDir()
	cfg := filepath.Join(d, "skills.yaml")
	if err := os.WriteFile(cfg, []byte("version: 1\ngroups:\n  reflex:\n    - source: acme/repo\n      skill: one\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store := config.Store{Path: filepath.Join(d, "config.json")}
	if err := store.Set(cfg); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(d, ".agents", "skills", "one")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("one"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := projectstate.Save(d, projectstate.File{Skills: []projectstate.Skill{{Name: "one", Source: "acme/repo", Path: "skills/one"}}}); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	a := App{Store: store, Root: d, Out: &out, Err: &errs}
	if code := a.Run([]string{"delete", "reflex"}); code != 0 {
		t.Fatalf("code=%d error=%q", code, errs.String())
	}
	if !strings.Contains(out.String(), "deleted one") {
		t.Fatalf("output=%q", out.String())
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Fatal("skill directory still exists")
	}
}
