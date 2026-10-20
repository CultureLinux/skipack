package cli

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyklodev/skipack/internal/config"
	gh "github.com/cyklodev/skipack/internal/github"
	"github.com/cyklodev/skipack/internal/install"
	projectstate "github.com/cyklodev/skipack/internal/state"
)

type selfUpdateFunc func() error

func (f selfUpdateFunc) Update() error { return f() }

func TestHelpShowsCommandsAndExamples(t *testing.T) {
	var out, errs bytes.Buffer
	a := App{Out: &out, Err: &errs}
	if code := a.Run([]string{"help"}); code != 0 {
		t.Fatalf("code=%d error=%q", code, errs.String())
	}
	for _, text := range []string{"Usage:", "Commands:", "delete <group>", "version", "Examples:", "skipack config set https://github.com/"} {
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

func TestVersionPrintsEmbeddedVersion(t *testing.T) {
	var out, errs bytes.Buffer
	a := App{Version: "v1.2.3", Out: &out, Err: &errs}
	if code := a.Run([]string{"version"}); code != 0 {
		t.Fatalf("code=%d error=%q", code, errs.String())
	}
	if got := strings.TrimSpace(out.String()); got != "v1.2.3" {
		t.Fatalf("version=%q", got)
	}
}

func TestSelfUpdateRunsUpdater(t *testing.T) {
	var out, errs bytes.Buffer
	updated := false
	a := App{Out: &out, Err: &errs, SelfUpdater: selfUpdateFunc(func() error {
		updated = true
		return nil
	})}
	if code := a.Run([]string{"self-update"}); code != 0 {
		t.Fatalf("code=%d error=%q", code, errs.String())
	}
	if !updated {
		t.Fatal("self updater was not run")
	}
	if !strings.Contains(out.String(), "updated skipack") {
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

func TestAddAllInstallsSkillsFromEveryConfiguredGroup(t *testing.T) {
	d := t.TempDir()
	cfg := filepath.Join(d, "skills.yaml")
	if err := os.WriteFile(cfg, []byte("version: 1\ngroups:\n  reflex:\n    - source: acme/repo\n      skill: one\n  planning:\n    - source: acme/repo\n      skill: two\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store := config.Store{Path: filepath.Join(d, "config.json")}
	if err := store.Set(cfg); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	a := App{Store: store, Root: d, Out: &out, Err: &errs, Fetcher: cliFetcher{}}
	if code := a.Run([]string{"add", "all"}); code != 0 {
		t.Fatalf("code=%d error=%q", code, errs.String())
	}
	for _, name := range []string{"one", "two"} {
		if _, err := os.Stat(filepath.Join(d, ".agents", "skills", name, "SKILL.md")); err != nil {
			t.Fatalf("skill %q was not installed: %v", name, err)
		}
	}
}

func TestUpdateReportsEveryInaccessibleSkill(t *testing.T) {
	d := t.TempDir()
	for _, name := range []string{"one", "two"} {
		dir := filepath.Join(d, ".agents", "skills", name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("old"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := projectstate.Save(d, projectstate.File{Skills: []projectstate.Skill{
		{Name: "one", Source: "acme/repo", Path: "skills/one"},
		{Name: "two", Source: "acme/repo", Path: "skills/two"},
	}}); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	a := App{Root: d, Out: &out, Err: &errs, Fetcher: inaccessibleFetcher{}}
	if code := a.Run([]string{"update"}); code == 0 {
		t.Fatal("update succeeded despite inaccessible skills")
	}
	for _, name := range []string{"one", "two"} {
		if !strings.Contains(errs.String(), name) {
			t.Fatalf("error does not report %q: %q", name, errs.String())
		}
	}
}

type cliFetcher struct{}

func (cliFetcher) Fetch(s config.Skill, dst string) (install.Origin, error) {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return install.Origin{}, err
	}
	if err := os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte(s.Skill), 0644); err != nil {
		return install.Origin{}, err
	}
	return install.Origin{Source: s.Source, Path: "skills/" + s.Skill}, nil
}

type inaccessibleFetcher struct{}

func (inaccessibleFetcher) Fetch(s config.Skill, _ string) (install.Origin, error) {
	return install.Origin{}, fmt.Errorf("repository unavailable for %s", s.Skill)
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

func TestDeleteAllDoesNotRequireConfiguration(t *testing.T) {
	d := t.TempDir()
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
	a := App{Store: config.Store{Path: filepath.Join(d, "missing-config.json")}, Root: d, Out: &out, Err: &errs}
	if code := a.Run([]string{"delete", "--all"}); code != 0 {
		t.Fatalf("code=%d error=%q", code, errs.String())
	}
	if !strings.Contains(out.String(), "deleted one") {
		t.Fatalf("output=%q", out.String())
	}
}

func TestUpdateRefreshesRemoteConfigAndRepositoryCache(t *testing.T) {
	version := "old"
	configDownloads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/branches"):
			fmt.Fprint(w, `[{"name":"main"}]`)
		case strings.HasSuffix(r.URL.Path, "/tags"):
			fmt.Fprint(w, `[]`)
		case strings.Contains(r.URL.Path, "/contents/skipack.yaml"):
			configDownloads++
			content := base64.StdEncoding.EncodeToString([]byte("version: 1\ngroups: {}\n"))
			fmt.Fprintf(w, `{"encoding":"base64","content":%q}`, content)
		case r.URL.Path == "/repos/acme/skills":
			fmt.Fprint(w, `{"default_branch":"main"}`)
		case strings.Contains(r.URL.Path, "/repos/acme/skills/zipball/"):
			w.Write(testArchive(t, map[string]string{"repo-sha/skills/one/SKILL.md": version}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := gh.NewClient()
	client.APIBase = server.URL
	client.HTTP = server.Client()
	client.CacheDir = filepath.Join(t.TempDir(), "cache")
	configURL := "https://github.com/acme/config/blob/main/skipack.yaml"
	if _, err := client.ReadConfigURL(configURL); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Fetch(config.Skill{Source: "acme/skills", Skill: "one"}, filepath.Join(t.TempDir(), "cached")); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	skillDir := filepath.Join(root, ".agents", "skills", "one")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := projectstate.Save(root, projectstate.File{Skills: []projectstate.Skill{{Name: "one", Source: "acme/skills", Path: "skills/one"}}}); err != nil {
		t.Fatal(err)
	}
	store := config.Store{Path: filepath.Join(t.TempDir(), "config.json")}
	if err := store.Set(configURL); err != nil {
		t.Fatal(err)
	}
	version = "new"
	var out, errs bytes.Buffer
	a := App{Store: store, Root: root, Out: &out, Err: &errs, GitHub: client}
	if code := a.Run([]string{"update"}); code != 0 {
		t.Fatalf("code=%d error=%q", code, errs.String())
	}
	b, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "new" {
		t.Fatalf("updated content = %q", b)
	}
	if configDownloads != 2 {
		t.Fatalf("configuration downloads = %d, want initial plus refresh", configDownloads)
	}
}

func testArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
