package github

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyklodev/skipack/internal/config"
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
	dst := filepath.Join(t.TempDir(), "skill")
	if _, err := c.Fetch(config.Skill{Source: "acme/repo", Skill: "missing"}, dst); err == nil {
		t.Fatal("missing skill accepted")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("destination created for missing skill")
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
	dst := filepath.Join(t.TempDir(), "skill")
	if _, err := c.Fetch(config.Skill{Source: "acme/repo", Skill: "x"}, dst); err == nil {
		t.Fatal("timed out request succeeded")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("destination created after timeout")
	}
}
