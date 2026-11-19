package github

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CultureLinux/skipack/internal/config"
)

func archive(entries map[string]string) []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for n, v := range entries {
		f, _ := w.Create(n)
		_, _ = f.Write([]byte(v))
	}
	_ = w.Close()
	return b.Bytes()
}

func TestClientUsesLongestAPIResolvedRef(t *testing.T) {
	z := archive(map[string]string{"repo-sha/skills/x/SKILL.md": "ok"})
	var requested string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.Path
		if strings.HasSuffix(r.URL.Path, "/branches") {
			fmt.Fprint(w, `[{"name":"feature"},{"name":"feature/windows"}]`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/tags") {
			fmt.Fprint(w, `[]`)
			return
		}
		w.Write(z)
	}))
	defer s.Close()
	c := NewClient()
	c.APIBase = s.URL
	c.HTTP = s.Client()
	c.CacheDir = t.TempDir()
	dst := t.TempDir() + "/skill"
	o, err := c.Fetch(config.Skill{Source: "https://github.com/acme/repo/tree/feature/windows/skills/x"}, dst)
	if err != nil {
		t.Fatal(err)
	}
	if o.Path != "skills/x" || !strings.Contains(requested, "zipball") {
		t.Fatalf("origin=%#v request=%q", o, requested)
	}
}

func TestClientRejectsAmbiguousShortSkill(t *testing.T) {
	z := archive(map[string]string{"repo-sha/skills/x/SKILL.md": "one", "repo-sha/x/SKILL.md": "two"})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme/repo" {
			fmt.Fprint(w, `{"default_branch":"main"}`)
			return
		}
		w.Write(z)
	}))
	defer s.Close()
	c := NewClient()
	c.APIBase = s.URL
	c.HTTP = s.Client()
	c.CacheDir = t.TempDir()
	_, err := c.Fetch(config.Skill{Source: "acme/repo", Skill: "x"}, t.TempDir()+"/skill")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error=%v", err)
	}
}

func TestClientReadsConfigWithSlashInRefThroughAPI(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/branches") {
			fmt.Fprint(w, `[{"name":"feature"},{"name":"feature/windows"}]`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/tags") {
			fmt.Fprint(w, `[]`)
			return
		}
		if strings.Contains(r.URL.Path, "/contents/config/skills.yaml") {
			fmt.Fprint(w, `{"encoding":"base64","content":"dmVyc2lvbjogMQo="}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer s.Close()
	c := NewClient()
	c.APIBase = s.URL
	c.HTTP = s.Client()
	c.CacheDir = t.TempDir()
	b, err := c.ReadConfigURL("https://github.com/acme/repo/blob/feature/windows/config/skills.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "version: 1\n" {
		t.Fatalf("got %q", b)
	}
}

func TestClientFetchesExactTrackedPathWithoutRediscovery(t *testing.T) {
	z := archive(map[string]string{
		"repo-sha/skills/x/SKILL.md": "other",
		"repo-sha/x/SKILL.md":        "tracked",
	})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme/repo" {
			fmt.Fprint(w, `{"default_branch":"main"}`)
			return
		}
		w.Write(z)
	}))
	defer s.Close()
	c := NewClient()
	c.APIBase = s.URL
	c.HTTP = s.Client()
	c.CacheDir = t.TempDir()
	dst := filepath.Join(t.TempDir(), "skill")
	origin, err := c.Fetch(config.Skill{Source: "acme/repo", Skill: "x", Path: "x"}, dst)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if origin.Path != "x" || string(b) != "tracked" {
		t.Fatalf("origin=%#v content=%q", origin, b)
	}
}

func TestClientInvalidURLCreatesNoDestination(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "skill")
	if _, err := NewClient().Fetch(config.Skill{Source: "http://github.com/acme/repo/tree/main/skills/x"}, dst); err == nil {
		t.Fatal("insecure URL accepted")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("destination created for invalid URL")
	}
}

func TestClientMissingShortSkillCreatesNoDestination(t *testing.T) {
	z := archive(map[string]string{"repo-sha/README.md": "none"})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme/repo" {
			fmt.Fprint(w, `{"default_branch":"main"}`)
			return
		}
		w.Write(z)
	}))
	defer s.Close()
	c := NewClient()
	c.APIBase = s.URL
	c.HTTP = s.Client()
	c.CacheDir = t.TempDir()
	dst := filepath.Join(t.TempDir(), "skill")
	if _, err := c.Fetch(config.Skill{Source: "acme/repo", Skill: "missing"}, dst); err == nil {
		t.Fatal("missing skill accepted")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("destination created for missing skill")
	}
}

func TestClientAuditValidatesSkillFileWithoutInstalling(t *testing.T) {
	z := archive(map[string]string{"repo-sha/skills/x/SKILL.md": "---\nname: x\n---\ncontent"})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme/repo" {
			fmt.Fprint(w, `{"default_branch":"main"}`)
			return
		}
		w.Write(z)
	}))
	defer s.Close()
	c := NewClient()
	c.APIBase, c.HTTP, c.CacheDir = s.URL, s.Client(), t.TempDir()
	origin, err := c.Audit(config.Skill{Source: "acme/repo", Skill: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if origin.Path != "skills/x" {
		t.Fatalf("origin=%#v", origin)
	}
}

func TestDefaultClientRejectsRedirectToUnexpectedHost(t *testing.T) {
	c := NewClient()
	req, err := http.NewRequest(http.MethodGet, "https://example.com/archive.zip", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.HTTP.CheckRedirect(req, nil); err == nil {
		t.Fatal("redirect to unexpected host accepted")
	}
}

func TestClientTimeoutLeavesNoDestination(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, `{"default_branch":"main"}`)
	}))
	defer s.Close()
	c := NewClient()
	c.APIBase = s.URL
	c.HTTP = &http.Client{Timeout: 10 * time.Millisecond}
	c.CacheDir = t.TempDir()
	dst := filepath.Join(t.TempDir(), "skill")
	if _, err := c.Fetch(config.Skill{Source: "acme/repo", Skill: "x"}, dst); err == nil {
		t.Fatal("timed out request succeeded")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("destination created after timeout")
	}
}

func TestClientReusesDiskCacheAcrossInstances(t *testing.T) {
	z := archive(map[string]string{
		"repo-sha/skills/one/SKILL.md": "one",
		"repo-sha/skills/two/SKILL.md": "two",
	})
	requests := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/repos/acme/repo" {
			fmt.Fprint(w, `{"default_branch":"main"}`)
			return
		}
		w.Write(z)
	}))
	cacheDir := t.TempDir()
	c := NewClient()
	c.APIBase = s.URL
	c.HTTP = s.Client()
	c.CacheDir = cacheDir
	if _, err := c.Fetch(config.Skill{Source: "acme/repo", Skill: "one"}, filepath.Join(t.TempDir(), "one")); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("initial requests = %d", requests)
	}
	s.Close()

	cached := NewClient()
	cached.APIBase = s.URL
	cached.HTTP = s.Client()
	cached.CacheDir = cacheDir
	if _, err := cached.Fetch(config.Skill{Source: "acme/repo", Skill: "two"}, filepath.Join(t.TempDir(), "two")); err != nil {
		t.Fatalf("cached fetch used network: %v", err)
	}
	if requests != 2 {
		t.Fatalf("cache caused %d total requests", requests)
	}
}

func TestClientRefreshesEachCachedResourceOnce(t *testing.T) {
	version := "old"
	requests := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/repos/acme/repo" {
			fmt.Fprint(w, `{"default_branch":"main"}`)
			return
		}
		w.Write(archive(map[string]string{
			"repo-sha/skills/one/SKILL.md": version,
			"repo-sha/skills/two/SKILL.md": version,
		}))
	}))
	defer s.Close()
	c := NewClient()
	c.APIBase = s.URL
	c.HTTP = s.Client()
	c.CacheDir = t.TempDir()
	first := filepath.Join(t.TempDir(), "first")
	if _, err := c.Fetch(config.Skill{Source: "acme/repo", Skill: "one"}, first); err != nil {
		t.Fatal(err)
	}
	version = "new"
	c.Refresh = true
	refreshed := filepath.Join(t.TempDir(), "refreshed")
	if _, err := c.Fetch(config.Skill{Source: "acme/repo", Skill: "one"}, refreshed); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(t.TempDir(), "second")
	if _, err := c.Fetch(config.Skill{Source: "acme/repo", Skill: "two"}, second); err != nil {
		t.Fatal(err)
	}
	if requests != 4 {
		t.Fatalf("requests = %d, want 4 (two initial, two refresh)", requests)
	}
	b, err := os.ReadFile(filepath.Join(refreshed, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "new" {
		t.Fatalf("refreshed content = %q", b)
	}
}

func TestTokenIsSentToTheAPIHostOnly(t *testing.T) {
	var got atomic.Value
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.Header.Get("Authorization"))
		fmt.Fprint(w, `{"default_branch":"main"}`)
	}))
	defer s.Close()
	c := NewClient()
	c.APIBase, c.HTTP, c.CacheDir, c.Token = s.URL, s.Client(), t.TempDir(), "secret"
	if _, err := c.defaultBranch("acme", "repo"); err != nil {
		t.Fatal(err)
	}
	if got.Load() != "Bearer secret" {
		t.Fatalf("Authorization = %q", got)
	}
	other, _ := url.Parse("https://codeload.github.com/x")
	if c.tokenAllowed(other) {
		t.Fatal("token allowed for another host")
	}
}

func TestStaleCacheIsRefetchedButUsedWhenOffline(t *testing.T) {
	var body atomic.Value
	body.Store("one")
	var fail atomic.Bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "boom", 500)
			return
		}
		fmt.Fprint(w, body.Load())
	}))
	defer s.Close()
	c := NewClient()
	c.HTTP, c.CacheDir = s.Client(), t.TempDir()
	if b, err := c.get(s.URL + "/x"); err != nil || string(b) != "one" {
		t.Fatalf("first=%q %v", b, err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(c.cachePath(s.URL+"/x"), old, old); err != nil {
		t.Fatal(err)
	}
	body.Store("two")
	if b, _ := c.get(s.URL + "/x"); string(b) != "two" {
		t.Fatalf("stale cache reused: %q", b)
	}
	if err := os.Chtimes(c.cachePath(s.URL+"/x"), old, old); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	if b, err := c.get(s.URL + "/x"); err != nil || string(b) != "two" {
		t.Fatalf("offline fallback=%q %v", b, err)
	}
}

func TestPruneCacheDropsOldFilesThenOldestOverTheLimit(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, size int, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, size), 0600); err != nil {
			t.Fatal(err)
		}
		mod := time.Now().Add(-age)
		os.Chtimes(p, mod, mod)
		return p
	}
	expired := write("a.cache", 10, 40*24*time.Hour)
	oldest := write("b.cache", 100, 3*time.Hour)
	newest := write("c.cache", 100, time.Hour)
	other := write("keep.txt", 1000, 99*24*time.Hour)
	c := &Client{CacheDir: dir}
	if err := c.PruneCache(30*24*time.Hour, 150); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]bool{expired: false, oldest: false, newest: true, other: true} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Fatalf("%s exists=%v want %v", filepath.Base(p), err == nil, want)
		}
	}
}

func TestArchiveCommitReadsTheZipballFolderName(t *testing.T) {
	if got := archiveCommit(archive(map[string]string{"acme-repo-0a1b2c3d/skills/x/SKILL.md": "x"})); got != "0a1b2c3d" {
		t.Fatalf("commit=%q", got)
	}
	for _, name := range []string{"repo/skills/x/SKILL.md", "acme-repo-nothex1/x", "acme-repo-abc/x"} {
		if got := archiveCommit(archive(map[string]string{name: "x"})); got != "" {
			t.Fatalf("%s -> %q", name, got)
		}
	}
}

func TestFetchUsesPinnedRefAndRecordsCommit(t *testing.T) {
	z := archive(map[string]string{"acme-repo-abcdef1/skills/x/SKILL.md": "ok"})
	var mu sync.Mutex
	var paths []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Write(z)
	}))
	defer s.Close()
	c := NewClient()
	c.APIBase, c.HTTP, c.CacheDir = s.URL, s.Client(), t.TempDir()
	o, err := c.Fetch(config.Skill{Source: "acme/repo", Skill: "x", Ref: "v1.0.0"}, t.TempDir()+"/out")
	if err != nil {
		t.Fatal(err)
	}
	if o.Commit != "abcdef1" {
		t.Fatalf("commit=%q", o.Commit)
	}
	for _, p := range paths {
		if strings.HasSuffix(p, "/repos/acme/repo") {
			t.Fatalf("default branch was queried despite ref: %v", paths)
		}
	}
	if !strings.HasSuffix(paths[len(paths)-1], "/zipball/v1.0.0") {
		t.Fatalf("paths=%v", paths)
	}
}

func TestRateLimitErrorExplainsTheTokenFix(t *testing.T) {
	reset := time.Now().Add(10 * time.Minute).Unix()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/limited" {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprint(reset))
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer s.Close()
	c := NewClient()
	c.HTTP, c.CacheDir = s.Client(), t.TempDir()
	_, err := c.get(s.URL + "/limited")
	if err == nil || !strings.Contains(err.Error(), "rate limit") || !strings.Contains(err.Error(), "GITHUB_TOKEN") || !strings.Contains(err.Error(), "resets at") {
		t.Fatalf("err=%v", err)
	}
	_, err = c.get(s.URL + "/other")
	if err == nil || strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("plain 403 mislabelled as rate limit: %v", err)
	}
}
