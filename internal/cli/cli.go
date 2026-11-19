package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CultureLinux/skipack/internal/config"
	gh "github.com/CultureLinux/skipack/internal/github"
	"github.com/CultureLinux/skipack/internal/install"
	"github.com/CultureLinux/skipack/internal/selfupdate"
)

type SelfUpdater interface {
	Update() error
}

type App struct {
	Version     string
	Store       config.Store
	Root        string
	Out, Err    io.Writer
	GitHub      *gh.Client
	Fetcher     install.Fetcher
	SelfUpdater SelfUpdater
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
func (a App) selfUpdater() SelfUpdater {
	if a.SelfUpdater != nil {
		return a.SelfUpdater
	}
	return selfupdate.New()
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
	case "self-update":
		if len(args) == 4 && args[1] == "--replace" {
			if err := selfupdate.Replace(args[2], args[3]); err != nil {
				return a.fail("replace executable: %v", err)
			}
			return 0
		}
		if len(args) != 1 {
			return a.usage()
		}
		if err := a.selfUpdater().Update(); err != nil {
			return a.fail("self-update: %v", err)
		}
		fmt.Fprintln(a.Out, "updated skipack")
		return 0
	case "version", "--version":
		if len(args) != 1 {
			return a.usage()
		}
		version := a.Version
		if version == "" {
			version = "dev"
		}
		fmt.Fprintln(a.Out, version)
		return 0
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
	case "agent":
		if len(args) == 2 && args[1] == "show" {
			st, err := install.LoadState(a.Root)
			if err != nil {
				return a.fail("%v", err)
			}
			fmt.Fprintln(a.Out, strings.Join(install.Agents(st), " "))
			return 0
		}
		if len(args) >= 3 && args[1] == "set" {
			result, err := a.manager().SetAgents(args[2:])
			if err != nil {
				return a.fail("%v", err)
			}
			for _, n := range result.Copied {
				fmt.Fprintf(a.Out, "copied %s\n", n)
			}
			for _, n := range result.Removed {
				fmt.Fprintf(a.Out, "removed %s\n", n)
			}
			for _, n := range result.Left {
				fmt.Fprintf(a.Out, "left in place %s (not tracked by skipack)\n", n)
			}
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
	case "audit":
		if len(args) != 1 {
			return a.usage()
		}
		cfg, err := a.load()
		if err != nil {
			return a.fail("%v", err)
		}
		names := make([]string, 0, len(cfg.Groups))
		for name := range cfg.Groups {
			names = append(names, name)
		}
		sort.Strings(names)
		failed := false
		for _, group := range names {
			for _, skill := range cfg.Groups[group] {
				name := skill.Skill
				if name == "" {
					parts := strings.Split(strings.TrimRight(skill.Source, "/"), "/")
					name = parts[len(parts)-1]
				}
				if _, err := a.client().Audit(skill); err != nil {
					fmt.Fprintf(a.Err, "invalid %s/%s: %v\n", group, name, err)
					failed = true
					continue
				}
				fmt.Fprintf(a.Out, "ok %s/%s\n", group, name)
			}
		}
		if failed {
			return 1
		}
		return 0
	case "add":
		if len(args) < 2 {
			return a.usage()
		}
		cfg, err := a.load()
		if err != nil {
			return a.fail("%v", err)
		}
		var skills []config.Skill
		if len(args) == 2 && (args[1] == "all" || args[1] == "--all") {
			names := make([]string, 0, len(cfg.Groups))
			for name := range cfg.Groups {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				skills = append(skills, cfg.Groups[name]...)
			}
		} else {
			for _, group := range args[1:] {
				groupSkills, ok := cfg.Groups[group]
				if !ok {
					return a.fail("unknown group %q", group)
				}
				skills = append(skills, groupSkills...)
			}
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
		if len(args) < 2 {
			return a.usage()
		}
		var result install.Result
		var err error
		if len(args) == 2 && (args[1] == "all" || args[1] == "--all") {
			result, err = a.manager().DeleteAll()
		} else {
			var cfg config.Config
			cfg, err = a.load()
			if err == nil {
				var skills []config.Skill
				for _, group := range args[1:] {
					groupSkills, ok := cfg.Groups[group]
					if !ok {
						return a.fail("unknown group %q", group)
					}
					skills = append(skills, groupSkills...)
				}
				result, err = a.manager().Delete(skills)
			}
		}
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
		client := a.client()
		client.Refresh = true
		if source, err := a.Store.Get(); err == nil && strings.HasPrefix(source, "https://") {
			if _, err = client.ReadConfigURL(source); err != nil {
				return a.fail("refresh configuration cache: %v", err)
			}
		}
		fetcher := a.Fetcher
		if fetcher == nil {
			fetcher = client
		}
		result, err := (install.Manager{Root: a.Root, Fetcher: fetcher}).Update()
		if err != nil {
			return a.fail("%v", err)
		}
		if len(result.Unavailable) > 0 {
			for _, unavailable := range result.Unavailable {
				fmt.Fprintf(a.Err, "skipack: inaccessible %s: %v\n", unavailable.Name, unavailable.Err)
			}
			return 1
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
  agent set <agent>... Choose codex (.agents), claude (.claude) or both; tracked skills are copied locally
  agent show           Show the agents of the current project
  list                 List configured groups and skills
  audit                Verify configured skills without installing them
  add <group>...|--all Install skills from one or more groups, or all configured skills
  delete <group>...    Delete tracked skills from one or more groups
  delete --all         Delete every skill tracked in the current project
  update               Refresh the GitHub cache, then update tracked skills
  self-update          Replace skipack with the latest GitHub release
  version              Show the installed skipack version
  help                 Show this help

Examples:
  skipack config set ./skipack.yaml
  skipack config set https://github.com/CultureLinux/skipack/blob/main/skipack.yaml
  skipack agent set codex claude
  skipack list
  skipack add reflex design-pro php docker github-actions gitlab-ci tailwind refactoring-pro planning debugging
  skipack add --all
  skipack delete reflex
  skipack delete --all

Skills are installed in <agent dir>/skills relative to the current directory:
.agents/skills for codex (default), .claude/skills for claude.
`)
}
