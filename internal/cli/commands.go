package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CultureLinux/skipack/internal/config"
	"github.com/CultureLinux/skipack/internal/install"
	"github.com/CultureLinux/skipack/internal/selfupdate"
)

func sortedGroups(cfg config.Config) []string {
	names := make([]string, 0, len(cfg.Groups))
	for name := range cfg.Groups {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// selectSkills resolves group names to skills. "--all" always means every group;
// a bare "all" does too, unless the configuration defines a group named "all".
func selectSkills(cfg config.Config, names []string) ([]config.Skill, error) {
	_, hasAllGroup := cfg.Groups["all"]
	if len(names) == 1 && (names[0] == "--all" || (names[0] == "all" && !hasAllGroup)) {
		var skills []config.Skill
		for _, name := range sortedGroups(cfg) {
			skills = append(skills, cfg.Groups[name]...)
		}
		return skills, nil
	}
	var skills []config.Skill
	for _, group := range names {
		groupSkills, ok := cfg.Groups[group]
		if !ok {
			return nil, fmt.Errorf("unknown group %q", group)
		}
		skills = append(skills, groupSkills...)
	}
	return skills, nil
}

func (a App) selfUpdate(args []string) int {
	if len(args) == 4 && args[1] == "--replace" {
		if err := selfupdate.FinishReplace(args[2], args[3]); err != nil {
			return a.fail("replace executable: %v", err)
		}
		return 0
	}
	if len(args) != 1 {
		return a.usage()
	}
	updater := a.selfUpdater()
	if err := updater.Update(); err != nil {
		return a.fail("self-update: %v", err)
	}
	if d, ok := updater.(interface{ Deferred() bool }); ok && d.Deferred() {
		fmt.Fprintf(a.Out, "update downloaded and verified; it is applied when this command exits (on failure, see %s)\n", selfupdate.LogPath())
		return 0
	}
	fmt.Fprintln(a.Out, "updated skipack")
	return 0
}

func (a App) version(args []string) int {
	if len(args) != 1 {
		return a.usage()
	}
	version := a.Version
	if version == "" {
		version = "dev"
	}
	fmt.Fprintln(a.Out, version)
	return 0
}

func (a App) helpCommand(args []string) int {
	if len(args) != 1 {
		return a.usage()
	}
	a.help(a.Out)
	return 0
}

func (a App) configCommand(args []string) int {
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
}

func (a App) agentCommand(args []string) int {
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
}

func (a App) list(args []string) int {
	if len(args) != 1 {
		return a.usage()
	}
	cfg, err := a.load()
	if err != nil {
		return a.fail("%v", err)
	}
	for _, n := range sortedGroups(cfg) {
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
}

func (a App) audit(args []string) int {
	if len(args) != 1 {
		return a.usage()
	}
	cfg, err := a.load()
	if err != nil {
		return a.fail("%v", err)
	}
	failed := false
	for _, group := range sortedGroups(cfg) {
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
}

func (a App) add(args []string) int {
	if len(args) < 2 {
		return a.usage()
	}
	cfg, err := a.load()
	if err != nil {
		return a.fail("%v", err)
	}
	skills, err := selectSkills(cfg, args[1:])
	if err != nil {
		return a.fail("%v", err)
	}
	result, err := a.manager().Add(skills)
	for _, n := range result.Installed {
		fmt.Fprintf(a.Out, "installed %s\n", n)
	}
	for _, n := range result.Skipped {
		fmt.Fprintf(a.Out, "skipped %s (already exists)\n", n)
	}
	if err != nil {
		return a.fail("%v", err)
	}
	return 0
}

func (a App) delete(args []string) int {
	if len(args) < 2 {
		return a.usage()
	}
	var result install.Result
	var err error
	deleteEverything := len(args) == 2 && args[1] == "--all"
	if len(args) == 2 && args[1] == "all" {
		// Without a readable configuration, or without a group named "all",
		// a bare "all" means every tracked skill.
		cfg, loadErr := a.load()
		_, hasAllGroup := cfg.Groups["all"]
		deleteEverything = loadErr != nil || !hasAllGroup
	}
	if deleteEverything {
		result, err = a.manager().DeleteAll()
	} else {
		var cfg config.Config
		cfg, err = a.load()
		if err == nil {
			var skills []config.Skill
			skills, err = selectSkills(cfg, args[1:])
			if err == nil {
				result, err = a.manager().Delete(skills)
			}
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
}

func shortCommit(c string) string {
	if c == "" {
		return "unknown"
	}
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

func (a App) update(args []string) int {
	dryRun := len(args) == 2 && args[1] == "--dry-run"
	if len(args) != 1 && !dryRun {
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
	result, err := (install.Manager{Root: a.Root, Fetcher: fetcher, DryRun: dryRun}).Update()
	if err != nil {
		return a.fail("%v", err)
	}
	if len(result.Unavailable) > 0 {
		for _, unavailable := range result.Unavailable {
			fmt.Fprintf(a.Err, "skipack: inaccessible %s: %v\n", unavailable.Name, unavailable.Err)
		}
		return 1
	}
	verb := "updated"
	if dryRun {
		verb = "would update"
	}
	for _, c := range result.Changes {
		fmt.Fprintf(a.Out, "%s %s (%s -> %s)\n", verb, c.Name, shortCommit(c.OldCommit), shortCommit(c.NewCommit))
	}
	switch {
	case dryRun && len(result.Changes) == 0:
		fmt.Fprintln(a.Out, "everything is up to date")
	case !dryRun:
		fmt.Fprintln(a.Out, "updated tracked skills")
	}
	return 0
}
