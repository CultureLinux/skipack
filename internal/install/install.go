package install

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyklodev/skipack/internal/config"
	projectstate "github.com/cyklodev/skipack/internal/state"
)

type Origin struct{ Source, Path string }
type Fetcher interface {
	Fetch(config.Skill, string) (Origin, error)
}
type Manager struct {
	Root    string
	Fetcher Fetcher
}
type Result struct{ Installed, Deleted, Skipped []string }
type Unavailable struct {
	Name string
	Err  error
}
type UpdateResult struct {
	Unavailable []Unavailable
}

func LoadState(root string) (projectstate.File, error) { return projectstate.Load(root) }
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

func (m Manager) Add(specs []config.Skill) (Result, error) {
	var result Result
	st, err := projectstate.Load(m.Root)
	if err != nil {
		return result, err
	}
	base := filepath.Join(m.Root, ".agents", "skills")
	if err = os.MkdirAll(base, 0755); err != nil {
		return result, err
	}
	for _, spec := range specs {
		name, err := skillName(spec)
		if err != nil {
			return result, err
		}
		dst := filepath.Join(base, name)
		if _, err = os.Stat(dst); err == nil {
			result.Skipped = append(result.Skipped, name)
			continue
		} else if !os.IsNotExist(err) {
			return result, err
		}
		tmp, err := os.MkdirTemp(base, ".skipack-*")
		if err != nil {
			return result, err
		}
		origin, fetchErr := m.Fetcher.Fetch(spec, tmp)
		if fetchErr != nil {
			os.RemoveAll(tmp)
			return result, fmt.Errorf("install %s: %w", name, fetchErr)
		}
		if _, err = os.Stat(filepath.Join(tmp, "SKILL.md")); err != nil {
			os.RemoveAll(tmp)
			return result, fmt.Errorf("install %s: missing SKILL.md", name)
		}
		if err = os.Rename(tmp, dst); err != nil {
			os.RemoveAll(tmp)
			return result, err
		}
		st.Skills = append(st.Skills, projectstate.Skill{Name: name, Source: origin.Source, Path: origin.Path})
		if err = projectstate.Save(m.Root, st); err != nil {
			_ = os.RemoveAll(dst)
			return result, err
		}
		result.Installed = append(result.Installed, name)
	}
	return result, nil
}

func (m Manager) Delete(specs []config.Skill) (Result, error) {
	var result Result
	st, err := projectstate.Load(m.Root)
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

	base := filepath.Join(m.Root, ".agents", "skills")
	backupRoot := ""
	type movedSkill struct{ source, backup string }
	var moved []movedSkill
	rollback := func() {
		for i := len(moved) - 1; i >= 0; i-- {
			_ = os.Rename(moved[i].backup, moved[i].source)
		}
		if backupRoot != "" {
			_ = os.RemoveAll(backupRoot)
		}
	}

	remaining := make([]projectstate.Skill, 0, len(st.Skills))
	for _, skill := range st.Skills {
		if !requested[skill.Name] {
			remaining = append(remaining, skill)
			continue
		}
		dst := filepath.Join(base, skill.Name)
		if _, err := os.Stat(dst); err == nil {
			if backupRoot == "" {
				backupRoot, err = os.MkdirTemp(base, ".skipack-delete-*")
				if err != nil {
					return result, err
				}
			}
			backup := filepath.Join(backupRoot, skill.Name)
			if err = os.Rename(dst, backup); err != nil {
				rollback()
				return result, err
			}
			moved = append(moved, movedSkill{source: dst, backup: backup})
		} else if !os.IsNotExist(err) {
			rollback()
			return result, err
		}
		result.Deleted = append(result.Deleted, skill.Name)
	}
	st.Skills = remaining
	if err = projectstate.Save(m.Root, st); err != nil {
		rollback()
		return result, err
	}
	if backupRoot != "" {
		if err = os.RemoveAll(backupRoot); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (m Manager) DeleteAll() (Result, error) {
	st, err := projectstate.Load(m.Root)
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
	st, err := projectstate.Load(m.Root)
	if err != nil {
		return result, err
	}
	base := filepath.Join(m.Root, ".agents", "skills")
	type prepared struct {
		s     projectstate.Skill
		dir   string
		index int
	}
	all := make([]prepared, 0, len(st.Skills))
	defer func() {
		for _, p := range all {
			os.RemoveAll(p.dir)
		}
	}()
	for index, tracked := range st.Skills {
		tmp, err := os.MkdirTemp(base, ".skipack-update-*")
		if err != nil {
			return result, err
		}
		spec := config.Skill{Source: tracked.Source, Skill: tracked.Name, Path: tracked.Path}
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
		dst := filepath.Join(base, tracked.Name)
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
			os.RemoveAll(tmp)
			continue
		}
		tracked.Source = origin.Source
		tracked.Path = origin.Path
		all = append(all, prepared{s: tracked, dir: tmp, index: index})
	}
	if len(result.Unavailable) > 0 {
		return result, nil
	}
	if len(all) == 0 {
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
		dst := filepath.Join(base, p.s.Name)
		bak := dst + ".skipack-backup"
		if err := os.Rename(dst, bak); err != nil {
			rollback()
			return result, err
		}
		done = append(done, swapped{dst, bak})
		if err := os.Rename(p.dir, dst); err != nil {
			rollback()
			return result, err
		}
		all[i].dir = ""
		st.Skills[p.index] = p.s
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

type fileSnapshot struct {
	directory bool
	digest    [sha256.Size]byte
}

func directoriesEqual(left, right string) (bool, error) {
	leftFiles, err := snapshotDirectory(left)
	if err != nil {
		return false, err
	}
	rightFiles, err := snapshotDirectory(right)
	if err != nil {
		return false, err
	}
	if len(leftFiles) != len(rightFiles) {
		return false, nil
	}
	for path, leftFile := range leftFiles {
		rightFile, ok := rightFiles[path]
		if !ok || leftFile != rightFile {
			return false, nil
		}
	}
	return true, nil
}

func snapshotDirectory(root string) (map[string]fileSnapshot, error) {
	files := map[string]fileSnapshot{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(relative)
		if entry.IsDir() {
			files[key] = fileSnapshot{directory: true}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported local entry %q", relative)
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		var digest [sha256.Size]byte
		copy(digest[:], hash.Sum(nil))
		files[key] = fileSnapshot{digest: digest}
		return nil
	})
	return files, err
}
