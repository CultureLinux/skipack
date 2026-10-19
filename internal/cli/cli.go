package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cyklodev/skipack/internal/config"
	gh "github.com/cyklodev/skipack/internal/github"
	"github.com/cyklodev/skipack/internal/install"
)

type App struct {
	Store    config.Store
	Root     string
	Out, Err io.Writer
	GitHub   *gh.Client
	Fetcher  install.Fetcher
}

func (a App) fail(format string, args ...any) int {
	fmt.Fprintf(a.Err, "skipack: "+format+"\n", args...)
	return 1
}
func (a App) client() *gh.Client {
	if a.GitHub != nil {
		return a.GitHub
	}
	return gh.NewClient()
}
func (a App) load() (config.Config, error) {
	source, err := a.Store.Get()
	if err != nil {
		return config.Config{}, err
	}
	var b []byte
	if strings.HasPrefix(source, "https://") {
		b, err = a.client().ReadConfigURL(source)
	} else {
		b, err = os.ReadFile(source)
	}
	if err != nil {
		return config.Config{}, fmt.Errorf("load configuration: %w", err)
	}
	return config.Parse(b)
}
func (a App) manager() install.Manager {
	f := a.Fetcher
	if f == nil {
		f = a.client()
	}
	return install.Manager{Root: a.Root, Fetcher: f}
}
func (a App) Run(args []string) int {
	if a.Out == nil {
		a.Out = os.Stdout
	}
	if a.Err == nil {
		a.Err = os.Stderr
	}
	if len(args) == 0 {
		a.help(a.Out)
		return 0
	}
	switch args[0] {
	case "help", "-h", "--help":
		if len(args) != 1 {
			return a.usage()
		}
		a.help(a.Out)
		return 0
	case "config":
		if len(args) == 3 && args[1] == "set" {
			source := args[2]
			if !strings.HasPrefix(source, "https://") {
				abs, err := filepath.Abs(source)
				if err != nil {
					return a.fail("%v", err)
				}
				b, err := os.ReadFile(abs)
				if err != nil {
					return a.fail("read configuration: %v", err)
				}
				if _, err = config.Parse(b); err != nil {
					return a.fail("%v", err)
				}
				source = abs
			} else if !strings.HasPrefix(source, "https://github.com/") {
				return a.fail("configuration URL must use HTTPS on github.com")
			}
			if err := a.Store.Set(source); err != nil {
				return a.fail("%v", err)
			}
			fmt.Fprintln(a.Out, source)
			return 0
		}
		if len(args) == 2 && args[1] == "show" {
			source, err := a.Store.Get()
			if err != nil {
				return a.fail("%v", err)
			}
			fmt.Fprintln(a.Out, source)
			return 0
		}
		return a.usage()
	case "list":
		if len(args) != 1 {
			return a.usage()
		}
		cfg, err := a.load()
		if err != nil {
			return a.fail("%v", err)
		}
		names := make([]string, 0, len(cfg.Groups))
		for n := range cfg.Groups {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(a.Out, "%s:\n", n)
			for _, s := range cfg.Groups[n] {
				label := s.Skill
				if label == "" {
					label = s.Source
				}
				fmt.Fprintf(a.Out, "  - %s (%s)\n", label, s.Source)
			}
		}
		return 0
	case "add":
		if len(args) != 2 {
			return a.usage()
		}
		cfg, err := a.load()
		if err != nil {
			return a.fail("%v", err)
		}
		skills, ok := cfg.Groups[args[1]]
		if !ok {
			return a.fail("unknown group %q", args[1])
		}
		result, err := a.manager().Add(skills)
		if err != nil {
			return a.fail("%v", err)
		}
		for _, n := range result.Installed {
			fmt.Fprintf(a.Out, "installed %s\n", n)
		}
		for _, n := range result.Skipped {
			fmt.Fprintf(a.Out, "skipped %s (already exists)\n", n)
		}
		return 0
	case "delete":
		if len(args) != 2 {
			return a.usage()
		}
		cfg, err := a.load()
		if err != nil {
			return a.fail("%v", err)
		}
		skills, ok := cfg.Groups[args[1]]
		if !ok {
			return a.fail("unknown group %q", args[1])
		}
		result, err := a.manager().Delete(skills)
		if err != nil {
			return a.fail("%v", err)
		}
		for _, n := range result.Deleted {
			fmt.Fprintf(a.Out, "deleted %s\n", n)
		}
		for _, n := range result.Skipped {
			fmt.Fprintf(a.Out, "skipped %s (not tracked by skipack)\n", n)
		}
		return 0
	case "update":
		if len(args) != 1 {
			return a.usage()
		}
		if err := a.manager().Update(); err != nil {
			return a.fail("%v", err)
		}
		fmt.Fprintln(a.Out, "updated tracked skills")
		return 0
	default:
		return a.usage()
	}
}
func (a App) usage() int {
	a.help(a.Err)
	return 2
}

func (a App) help(w io.Writer) {
	fmt.Fprint(w, `skipack — install and manage packs of agent skills

Usage:
  skipack <command> [arguments]

Commands:
  config set <source>  Set a local YAML file or public GitHub file as config
  config show          Show the active configuration source
  list                 List configured groups and skills
  add <group>          Install every skill in a group
  delete <group>       Delete tracked skills from a group
  update               Update all skills tracked in the current project
  help                 Show this help

Examples:
  skipack config set ./skipack.yaml
  skipack config set https://github.com/owner/repo/blob/main/skipack.yaml
  skipack list
  skipack add reflex
  skipack delete reflex

Skills are installed in .agents/skills relative to the current directory.
`)
}
