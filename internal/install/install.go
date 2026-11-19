package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CultureLinux/skipack/internal/config"
	projectstate "github.com/CultureLinux/skipack/internal/state"
)

type Origin struct{ Source, Path, Commit string }
type Fetcher interface {
	Fetch(config.Skill, string) (Origin, error)
}
type Manager struct {
	Root string
	// DryRun makes Update report changes without touching any file or the state.
	DryRun  bool
	Fetcher Fetcher
}
type Result struct {
	Installed, Deleted, Skipped []string
	Copied, Removed, Left       []string
}
type Unavailable struct {
	Name string
	Err  error
}

// Change describes a tracked skill whose content differs from upstream.
type Change struct{ Name, OldCommit, NewCommit string }
type UpdateResult struct {
	Unavailable []Unavailable
	// Changes lists skills whose files changed (or would change with DryRun).
	Changes []Change
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func skillName(s config.Skill) (string, error) {
	n := s.Skill
	if n == "" {
		p := strings.Split(strings.TrimRight(s.Source, "/"), "/")
		if len(p) > 0 {
			n = p[len(p)-1]
		}
	}
	if n == "" || n == "." || n == ".." || strings.ContainsAny(n, "/\\") {
		return "", errors.New("invalid skill name")
	}
	return n, nil
}

func trackedIndex(st projectstate.File, name string) int {
	for i, s := range st.Skills {
		if s.Name == name {
			return i
		}
	}
	return -1
}

func (m Manager) Add(specs []config.Skill) (Result, error) {
	var result Result
	st, unlock, err := m.begin()
	defer unlock()
	if err != nil {
		return result, err
	}
	agents := Agents(st)
	for _, a := range agents {
		if err = os.MkdirAll(m.skillsDir(a), 0755); err != nil {
			return result, err
		}
	}
	for _, spec := range specs {
		name, err := skillName(spec)
		if err != nil {
			return result, err
		}
		var missing []string
		for _, a := range agents {
			if _, err = os.Stat(filepath.Join(m.skillsDir(a), name)); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				return result, err
			}
			missing = append(missing, a)
		}
		if len(missing) == 0 {
			result.Skipped = append(result.Skipped, name)
			continue
		}
		idx := trackedIndex(st, name)
		// A copy already managed by skipack is reused, so nothing is downloaded again.
		src := ""
		if idx >= 0 {
			for _, a := range owned(st.Skills[idx]) {
				if d := filepath.Join(m.skillsDir(a), name); isDir(d) {
					src = d
					break
				}
			}
		}
		var created, installed []string
		cleanup := func() {
			for _, d := range created {
				os.RemoveAll(d)
			}
		}
		origin := Origin{}
		rest := missing
		if src == "" {
			first := missing[0]
			rest = missing[1:]
			base := m.skillsDir(first)
			tmp, err := os.MkdirTemp(base, ".skipack-*")
			if err != nil {
				return result, err
			}
			var fetchErr error
			origin, fetchErr = m.Fetcher.Fetch(spec, tmp)
			if fetchErr != nil {
				os.RemoveAll(tmp)
				return result, fmt.Errorf("install %s: %w", name, fetchErr)
			}
			if _, err = os.Stat(filepath.Join(tmp, "SKILL.md")); err != nil {
				os.RemoveAll(tmp)
				return result, fmt.Errorf("install %s: missing SKILL.md", name)
			}
			dst := filepath.Join(base, name)
			if err = os.Rename(tmp, dst); err != nil {
				os.RemoveAll(tmp)
				return result, err
			}
			created = append(created, dst)
			installed = append(installed, first)
			src = dst
		}
		for _, a := range rest {
			dst := filepath.Join(m.skillsDir(a), name)
			stage, err := stageCopy(src, m.skillsDir(a), ".skipack-copy-*")
			if err == nil {
				err = os.Rename(stage, dst)
				if err != nil {
					os.RemoveAll(stage)
				}
			}
			if err != nil {
				cleanup()
				return result, fmt.Errorf("copy %s to %s: %w", name, a, err)
			}
			created = append(created, dst)
			installed = append(installed, a)
		}
		if idx >= 0 {
			sk := &st.Skills[idx]
			sk.Agents = unionAgents(owned(*sk), installed)
			if origin.Source != "" {
				sk.Source, sk.Path, sk.Commit = origin.Source, origin.Path, origin.Commit
			}
		} else {
			st.Skills = append(st.Skills, projectstate.Skill{Name: name, Source: origin.Source, Path: origin.Path, Ref: spec.Ref, Commit: origin.Commit, Agents: installed})
		}
		if err = projectstate.Save(m.Root, st); err != nil {
			cleanup()
			return result, err
		}
		result.Installed = append(result.Installed, name)
	}
	return result, nil
}

func (m Manager) Delete(specs []config.Skill) (Result, error) {
	var result Result
	st, unlock, err := m.begin()
	defer unlock()
	if err != nil {
		return result, err
	}
	tracked := make(map[string]projectstate.Skill, len(st.Skills))
	for _, skill := range st.Skills {
		tracked[skill.Name] = skill
	}
	requested := make(map[string]bool, len(specs))
	for _, spec := range specs {
		name, err := skillName(spec)
		if err != nil {
			return result, err
		}
		if requested[name] {
			continue
		}
		requested[name] = true
		if _, ok := tracked[name]; !ok {
			result.Skipped = append(result.Skipped, name)
		}
	}

	backup := m.newBackup(".skipack-delete-*")
	remaining := make([]projectstate.Skill, 0, len(st.Skills))
	for _, skill := range st.Skills {
		if !requested[skill.Name] {
			remaining = append(remaining, skill)
			continue
		}
		for _, agent := range owned(skill) {
			if _, ok := agentDirs[agent]; !ok {
				continue
			}
			dst := filepath.Join(m.skillsDir(agent), skill.Name)
			if _, err := os.Stat(dst); err == nil {
				if err = backup.move(agent, skill.Name, dst); err != nil {
					backup.rollback()
					return result, err
				}
			} else if !os.IsNotExist(err) {
				backup.rollback()
				return result, err
			}
		}
		result.Deleted = append(result.Deleted, skill.Name)
	}
	st.Skills = remaining
	if err = projectstate.Save(m.Root, st); err != nil {
		backup.rollback()
		return result, err
	}
	if err = backup.discard(); err != nil {
		return result, err
	}
	return result, nil
}

func (m Manager) DeleteAll() (Result, error) {
	st, err := LoadState(m.Root)
	if err != nil {
		return Result{}, err
	}
	specs := make([]config.Skill, 0, len(st.Skills))
	for _, skill := range st.Skills {
		specs = append(specs, config.Skill{Source: skill.Source, Skill: skill.Name, Path: skill.Path})
	}
	return m.Delete(specs)
}

func (m Manager) Update() (UpdateResult, error) {
	var result UpdateResult
	st, unlock, err := m.begin()
	defer unlock()
	if err != nil {
		return result, err
	}
	type prepared struct {
		dst    string
		dir    string
		index  int
		origin Origin
	}
	var all []prepared
	defer func() {
		for _, p := range all {
			if p.dir != "" {
				os.RemoveAll(p.dir)
			}
		}
	}()
	meta := map[int]Origin{}
	for index, tracked := range st.Skills {
		own := owned(tracked)
		for _, a := range own {
			if _, ok := agentDirs[a]; !ok {
				return result, fmt.Errorf("tracked skill %s has unknown agent %q", tracked.Name, a)
			}
		}
		fetchBase := m.skillsDir(own[0])
		if err = os.MkdirAll(fetchBase, 0755); err != nil {
			return result, err
		}
		tmp, err := os.MkdirTemp(fetchBase, ".skipack-update-*")
		if err != nil {
			return result, err
		}
		spec := config.Skill{Source: tracked.Source, Skill: tracked.Name, Ref: tracked.Ref, Path: tracked.Path}
		origin, err := m.Fetcher.Fetch(spec, tmp)
		if err != nil {
			os.RemoveAll(tmp)
			result.Unavailable = append(result.Unavailable, Unavailable{Name: tracked.Name, Err: err})
			continue
		}
		if _, err = os.Stat(filepath.Join(tmp, "SKILL.md")); err != nil {
			os.RemoveAll(tmp)
			result.Unavailable = append(result.Unavailable, Unavailable{Name: tracked.Name, Err: errors.New("missing SKILL.md")})
			continue
		}
		changed := false
		for _, a := range own {
			dst := filepath.Join(m.skillsDir(a), tracked.Name)
			if _, err = os.Stat(dst); err != nil {
				os.RemoveAll(tmp)
				return result, fmt.Errorf("tracked skill %s is missing", tracked.Name)
			}
			equal, err := directoriesEqual(tmp, dst)
			if err != nil {
				os.RemoveAll(tmp)
				return result, fmt.Errorf("compare update %s: %w", tracked.Name, err)
			}
			if equal {
				continue
			}
			stage, err := stageCopy(tmp, m.skillsDir(a), ".skipack-update-*")
			if err != nil {
				os.RemoveAll(tmp)
				return result, fmt.Errorf("prepare update %s: %w", tracked.Name, err)
			}
			all = append(all, prepared{dst: dst, dir: stage, index: index, origin: origin})
			changed = true
		}
		os.RemoveAll(tmp)
		if changed {
			result.Changes = append(result.Changes, Change{Name: tracked.Name, OldCommit: tracked.Commit, NewCommit: origin.Commit})
		} else if origin.Commit != tracked.Commit || origin.Source != tracked.Source || origin.Path != tracked.Path {
			// Same files, but the recorded origin was missing or moved (for
			// example a state written before commits were tracked).
			meta[index] = origin
		}
	}
	if len(result.Unavailable) > 0 || m.DryRun {
		return result, nil
	}
	if len(all) == 0 && len(meta) == 0 {
		return result, nil
	}
	type swapped struct{ dst, bak string }
	var done []swapped
	rollback := func() {
		for i := len(done) - 1; i >= 0; i-- {
			os.RemoveAll(done[i].dst)
			_ = os.Rename(done[i].bak, done[i].dst)
		}
	}
	for i, p := range all {
		bak := p.dst + ".skipack-backup"
		if err := os.Rename(p.dst, bak); err != nil {
			rollback()
			return result, err
		}
		done = append(done, swapped{p.dst, bak})
		if err := os.Rename(p.dir, p.dst); err != nil {
			rollback()
			return result, err
		}
		all[i].dir = ""
		st.Skills[p.index].Source = p.origin.Source
		st.Skills[p.index].Path = p.origin.Path
		st.Skills[p.index].Commit = p.origin.Commit
	}
	for index, origin := range meta {
		st.Skills[index].Source = origin.Source
		st.Skills[index].Path = origin.Path
		st.Skills[index].Commit = origin.Commit
	}
	if err := projectstate.Save(m.Root, st); err != nil {
		rollback()
		return result, err
	}
	for _, s := range done {
		_ = os.RemoveAll(s.bak)
	}
	return result, nil
}
