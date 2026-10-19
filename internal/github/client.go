package github

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/cyklodev/skipack/internal/config"
	"github.com/cyklodev/skipack/internal/install"
)

const maxArchiveBytes int64 = 100 << 20

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type Client struct {
	HTTP    *http.Client
	APIBase string
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
	return &Client{HTTP: h, APIBase: "https://api.github.com"}
}

func (c *Client) get(raw string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "skipack/1")
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
		return nil, errors.New("download exceeds 100 MiB limit")
	}
	return b, nil
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
		return install.Origin{Source: spec.Source, Path: path}, nil
	}
	if !repoPattern.MatchString(spec.Source) || spec.Skill == "" {
		return install.Origin{}, errors.New("short source must be owner/repo with a skill name")
	}
	p := strings.Split(spec.Source, "/")
	ref, err := c.defaultBranch(p[0], p[1])
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
		return install.Origin{Source: spec.Source, Path: spec.Path}, nil
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
	return install.Origin{Source: spec.Source, Path: valid[0]}, nil
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
