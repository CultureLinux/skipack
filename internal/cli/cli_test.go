package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyklodev/skipack/internal/config"
)

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
