package github

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/CultureLinux/skipack/internal/config"
	"github.com/CultureLinux/skipack/internal/install"
)

// maxArchiveBytes bounds a whole repository archive; the skill itself is limited
// separately (maxSkillBytes) once extracted.
const maxArchiveBytes int64 = 200 << 20

// cacheTTL is how long a cached response is reused before GitHub is asked again.
const cacheTTL = 24 * time.Hour

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type Client struct {
	HTTP     *http.Client
	APIBase  string
	CacheDir string
	Refresh  bool
	// Token is sent only to the host of APIBase (GITHUB_TOKEN or GH_TOKEN).
	Token     string
	refreshed map[string]bool
}

func NewClient() *Client {
	h := &http.Client{Timeout: 30 * time.Second}
	h.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		switch strings.ToLower(req.URL.Hostname()) {
		case "github.com", "api.github.com", "codeload.github.com":
			return nil
		default:
			return errors.New("GitHub redirected to an unexpected host")
		}
	}
	cacheDir := ""
	if userDir, err := os.UserConfigDir(); err == nil {
		cacheDir = filepath.Join(userDir, "skipack", "cache")
	}
	return &Client{HTTP: h, APIBase: "https://api.github.com", CacheDir: cacheDir, Token: tokenFromEnv(), refreshed: map[string]bool{}}
}

func (c *Client) get(raw string) ([]byte, error) {
	cachePath := c.cachePath(raw)
	var stale []byte
	if cachePath != "" {
		alreadyRefreshed := c.Refresh && c.refreshed[cachePath]
		if !c.Refresh || alreadyRefreshed {
			b, err := os.ReadFile(cachePath)
			if err == nil && len(b) > 0 {
				if info, statErr := os.Stat(cachePath); statErr == nil && time.Since(info.ModTime()) > cacheTTL && !alreadyRefreshed {
					stale = b
				} else {
					return b, nil
				}
			} else if err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("read GitHub cache: %w", err)
			}
		}
	}
	b, err := c.getNetwork(raw)
	if err != nil {
		if stale != nil {
			return stale, nil
		}
		return nil, err
	}
	if cachePath != "" {
		if err := writeCacheFile(cachePath, b); err != nil {
			return nil, fmt.Errorf("write GitHub cache: %w", err)
		}
		if c.Refresh {
			if c.refreshed == nil {
				c.refreshed = map[string]bool{}
			}
			c.refreshed[cachePath] = true
		}
	}
	return b, nil
}

func (c *Client) getNetwork(raw string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "skipack/1")
	if c.Token != "" && c.tokenAllowed(req.URL) {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxArchiveBytes {
		return nil, fmt.Errorf("repository archive exceeds the %d MiB download limit", maxArchiveBytes>>20)
	}
	return b, nil
}

func (c *Client) cachePath(raw string) string {
	if c.CacheDir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return filepath.Join(c.CacheDir, hex.EncodeToString(sum[:])+".cache")
}

func writeCacheFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".skipack-cache-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
func (c *Client) refs(owner, repo string) ([]string, error) {
	var out []string
	for _, kind := range []string{"branches", "tags"} {
		for page := 1; page <= 10; page++ {
			b, err := c.get(fmt.Sprintf("%s/repos/%s/%s/%s?per_page=100&page=%d", c.APIBase, owner, repo, kind, page))
			if err != nil {
				return nil, err
			}
			var rows []struct {
				Name string `json:"name"`
			}
			if err = json.Unmarshal(b, &rows); err != nil {
				return nil, err
			}
			for _, r := range rows {
				out = append(out, r.Name)
			}
			if len(rows) < 100 {
				break
			}
		}
	}
	return out, nil
}
func (c *Client) archive(owner, repo, ref string) ([]byte, error) {
	return c.get(fmt.Sprintf("%s/repos/%s/%s/zipball/%s", c.APIBase, owner, repo, url.PathEscape(ref)))
}
func (c *Client) defaultBranch(owner, repo string) (string, error) {
	b, err := c.get(fmt.Sprintf("%s/repos/%s/%s", c.APIBase, owner, repo))
	if err != nil {
		return "", err
	}
	var v struct {
		Default string `json:"default_branch"`
	}
	if err = json.Unmarshal(b, &v); err != nil {
		return "", err
	}
	if v.Default == "" {
		return "", errors.New("repository has no default branch")
	}
	return v.Default, nil
}

func (c *Client) Fetch(spec config.Skill, dst string) (install.Origin, error) {
	if strings.HasPrefix(spec.Source, "https://") {
		s, err := ParseTreeURL(spec.Source)
		if err != nil {
			return install.Origin{}, err
		}
		refs, err := c.refs(s.Owner, s.Repo)
		if err != nil {
			return install.Origin{}, err
		}
		ref, path, err := ChooseRef(s.Remainder, refs)
		if err != nil {
			return install.Origin{}, err
		}
		data, err := c.archive(s.Owner, s.Repo, ref)
		if err != nil {
			return install.Origin{}, err
		}
		if err = ExtractSkillZip(data, path, dst); err != nil {
			return install.Origin{}, err
		}
		return install.Origin{Source: spec.Source, Commit: archiveCommit(data), Path: path}, nil
	}
	if !repoPattern.MatchString(spec.Source) || spec.Skill == "" {
		return install.Origin{}, errors.New("short source must be owner/repo with a skill name")
	}
	p := strings.Split(spec.Source, "/")
	ref, err := c.resolveRef(p[0], p[1], spec)
	if err != nil {
		return install.Origin{}, err
	}
	data, err := c.archive(p[0], p[1], ref)
	if err != nil {
		return install.Origin{}, err
	}
	if spec.Path != "" {
		if err = ExtractSkillZip(data, spec.Path, dst); err != nil {
			return install.Origin{}, err
		}
		return install.Origin{Source: spec.Source, Commit: archiveCommit(data), Path: spec.Path}, nil
	}
	candidates := []string{"skills/" + spec.Skill, ".agents/skills/" + spec.Skill, ".claude/skills/" + spec.Skill, spec.Skill}
	valid := validCandidates(data, candidates)
	if len(valid) == 0 {
		return install.Origin{}, fmt.Errorf("skill %q not found in supported locations", spec.Skill)
	}
	if len(valid) > 1 {
		return install.Origin{}, fmt.Errorf("skill %q is ambiguous: %s", spec.Skill, strings.Join(valid, ", "))
	}
	if err = ExtractSkillZip(data, valid[0], dst); err != nil {
		return install.Origin{}, err
	}
	return install.Origin{Source: spec.Source, Commit: archiveCommit(data), Path: valid[0]}, nil
}

// Audit verifies that a configured skill still resolves to a non-empty UTF-8
// SKILL.md without writing anything to the project.
func (c *Client) Audit(spec config.Skill) (install.Origin, error) {
	var owner, repo, ref, path string
	if strings.HasPrefix(spec.Source, "https://") {
		s, err := ParseTreeURL(spec.Source)
		if err != nil {
			return install.Origin{}, err
		}
		refs, err := c.refs(s.Owner, s.Repo)
		if err != nil {
			return install.Origin{}, err
		}
		ref, path, err = ChooseRef(s.Remainder, refs)
		if err != nil {
			return install.Origin{}, err
		}
		owner, repo = s.Owner, s.Repo
	} else {
		if !repoPattern.MatchString(spec.Source) || spec.Skill == "" {
			return install.Origin{}, errors.New("short source must be owner/repo with a skill name")
		}
		parts := strings.Split(spec.Source, "/")
		var err error
		owner, repo = parts[0], parts[1]
		ref, err = c.resolveRef(owner, repo, spec)
		if err != nil {
			return install.Origin{}, err
		}
		path = spec.Path
	}
	data, err := c.archive(owner, repo, ref)
	if err != nil {
		return install.Origin{}, err
	}
	if path == "" {
		candidates := []string{"skills/" + spec.Skill, ".agents/skills/" + spec.Skill, ".claude/skills/" + spec.Skill, spec.Skill}
		valid := validCandidates(data, candidates)
		if len(valid) == 0 {
			return install.Origin{}, fmt.Errorf("skill %q not found in supported locations", spec.Skill)
		}
		if len(valid) > 1 {
			return install.Origin{}, fmt.Errorf("skill %q is ambiguous: %s", spec.Skill, strings.Join(valid, ", "))
		}
		path = valid[0]
	}
	if err := validateSkillFile(data, path); err != nil {
		return install.Origin{}, err
	}
	return install.Origin{Source: spec.Source, Commit: archiveCommit(data), Path: path}, nil
}

func validateSkillFile(data []byte, path string) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	want := strings.Trim(path, "/") + "/SKILL.md"
	for _, f := range r.File {
		parts := strings.Split(filepathSlash(f.Name), "/")
		if len(parts) < 2 || strings.Join(parts[1:], "/") != want || f.FileInfo().IsDir() {
			continue
		}
		file, err := f.Open()
		if err != nil {
			return err
		}
		b, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if len(b) == 0 {
			return errors.New("SKILL.md is empty")
		}
		if !utf8.Valid(b) {
			return errors.New("SKILL.md is not valid UTF-8")
		}
		return nil
	}
	return errors.New("missing SKILL.md")
}
func validCandidates(data []byte, candidates []string) []string {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, f := range r.File {
		parts := strings.Split(filepathSlash(f.Name), "/")
		if len(parts) < 2 {
			continue
		}
		rest := strings.Join(parts[1:], "/")
		for _, c := range candidates {
			if rest == c+"/SKILL.md" && !f.FileInfo().IsDir() {
				seen[c] = true
			}
		}
	}
	var out []string
	for _, c := range candidates {
		if seen[c] {
			out = append(out, c)
		}
	}
	return out
}
func filepathSlash(s string) string { return strings.ReplaceAll(s, "\\", "/") }

func (c *Client) ReadConfigURL(raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("configuration URL must use HTTPS on github.com")
	}
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(p) < 5 || p[2] != "blob" {
		return nil, errors.New("expected https://github.com/<owner>/<repo>/blob/<ref>/<path>")
	}
	remainder := strings.Join(p[3:], "/")
	refs, err := c.refs(p[0], p[1])
	if err != nil {
		return nil, err
	}
	ref, file, err := ChooseRef(remainder, refs)
	if err != nil {
		return nil, err
	}
	b, err := c.get(fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", c.APIBase, p[0], p[1], strings.Join(strings.Split(file, "/"), "/"), url.QueryEscape(ref)))
	if err != nil {
		return nil, err
	}
	var v struct{ Encoding, Content string }
	if err = json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if v.Encoding != "base64" {
		return nil, errors.New("GitHub returned unsupported file encoding")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(v.Content, "\n", ""))
	if err != nil {
		return nil, err
	}
	if len(decoded) > 1<<20 {
		return nil, errors.New("configuration exceeds 1 MiB limit")
	}
	return decoded, nil
}

func tokenFromEnv() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("GH_TOKEN")
}

// tokenAllowed keeps the token away from any host but the API one. Go also drops
// the Authorization header when a request is redirected to another host.
func (c *Client) tokenAllowed(u *url.URL) bool {
	api, err := url.Parse(c.APIBase)
	return err == nil && strings.EqualFold(u.Host, api.Host)
}

// PruneCache removes cache files older than maxAge, then the oldest ones until
// the cache fits in maxBytes.
func (c *Client) PruneCache(maxAge time.Duration, maxBytes int64) error {
	if c.CacheDir == "" {
		return nil
	}
	entries, err := os.ReadDir(c.CacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".cache") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(c.CacheDir, e.Name())
		if time.Since(info.ModTime()) > maxAge {
			_ = os.Remove(p)
			continue
		}
		files = append(files, file{p, info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if total <= maxBytes {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
	return nil
}

// resolveRef returns the pinned ref of a short source, or the default branch.
func (c *Client) resolveRef(owner, repo string, spec config.Skill) (string, error) {
	if spec.Ref != "" {
		return spec.Ref, nil
	}
	return c.defaultBranch(owner, repo)
}

// archiveCommit reads the commit from the top folder GitHub puts in a zipball
// ("owner-repo-<sha>/"). It returns "" when the name has no hexadecimal suffix.
func archiveCommit(data []byte) string {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(r.File) == 0 {
		return ""
	}
	top := strings.SplitN(filepathSlash(r.File[0].Name), "/", 2)[0]
	i := strings.LastIndex(top, "-")
	if i < 0 {
		return ""
	}
	sha := top[i+1:]
	if len(sha) < 7 || len(sha) > 40 {
		return ""
	}
	for _, c := range sha {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return ""
		}
	}
	return sha
}
