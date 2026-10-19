package github

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type TreeSource struct{ Owner, Repo, Remainder string }

func ParseTreeURL(raw string) (TreeSource, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return TreeSource{}, err
	}
	if u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return TreeSource{}, errors.New("skill URL must be an HTTPS github.com tree URL")
	}
	p := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if len(p) < 5 || p[2] != "tree" {
		return TreeSource{}, errors.New("expected https://github.com/<owner>/<repo>/tree/<ref>/<path>")
	}
	parts := make([]string, 0, len(p)-3)
	for _, x := range p[3:] {
		y, e := url.PathUnescape(x)
		if e != nil || y == "" || y == "." || y == ".." || strings.ContainsAny(y, "\\/") {
			return TreeSource{}, errors.New("invalid URL path")
		}
		parts = append(parts, y)
	}
	return TreeSource{Owner: p[0], Repo: strings.TrimSuffix(p[1], ".git"), Remainder: strings.Join(parts, "/")}, nil
}

func ChooseRef(remainder string, refs []string) (string, string, error) {
	best := ""
	for _, r := range refs {
		if remainder == r || strings.HasPrefix(remainder, r+"/") {
			if len(r) > len(best) {
				best = r
			}
		}
	}
	if best == "" {
		return "", "", errors.New("no branch or tag matches the tree URL")
	}
	path := strings.TrimPrefix(remainder, best+"/")
	if path == "" || path == remainder {
		return "", "", fmt.Errorf("tree URL must point to a folder below ref %q", best)
	}
	return best, path, nil
}
