package install

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/CultureLinux/skipack/internal/config"
	projectstate "github.com/CultureLinux/skipack/internal/state"
)

// DefaultAgent is used when a project has not selected any agent yet.
const DefaultAgent = "codex"

// agentDirs maps each supported agent to the hidden directory whose "skills"
// subfolder it reads. Only that subfolder is ever managed by skipack.
var agentDirs = map[string]string{"codex": ".agents", "claude": ".claude"}

type Origin struct{ Source, Path string }
type Fetcher interface {
	Fetch(config.Skill, string) (Origin, error)
}
type Manager struct {
	Root    string
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
type UpdateResult struct {
	Unavailable []Unavailable
}

func LoadState(root string) (projectstate.File, error) { return projectstate.Load(root) }

// ValidAgents checks agent names and removes duplicates, keeping the order.
func ValidAgents(names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, errors.New("at least one agent is required (codex, claude)")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if _, ok := agentDirs[n]; !ok {
			return nil, fmt.Errorf("unknown agent %q (expected codex or claude)", n)
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
}

// Agents returns the agents configured for the project.
func Agents(st projectstate.File) []string {
	if len(st.Agents) == 0 {
		return []string{DefaultAgent}
	}
	return st.Agents
}

func owned(s projectstate.Skill) []string {
	if len(s.Agents) == 0 {
		return []string{DefaultAgent}
	}
	return s.Agents
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (m Manager) skillsDir(agent string) string {
	return filepath.Join(m.Root, agentDirs[agent], "skills")
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

// copyTree copies regular files and directories only, so a local copy can never
// introduce symlinks or special entries.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported local entry %q", rel)
		}
		mode := os.FileMode(0644)
		if info.Mode()&0111 != 0 {
			mode = 0755
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		if closeErr := out.Close(); copyErr == nil {
			copyErr = closeErr
		}
		return copyErr
	})
}

// stageCopy copies src into a fresh temporary folder inside parent and returns it.
func stageCopy(src, parent, pattern string) (string, error) {
	if err := os.MkdirAll(parent, 0755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", err
	}
	if err = copyTree(src, tmp); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}

func (m Manager) Add(specs []config.Skill) (Result, error) {
	var result Result
	st, err := projectstate.Load(m.Root)
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
				sk.Source, sk.Path = origin.Source, origin.Path
			}
		} else {
			st.Skills = append(st.Skills, projectstate.Skill{Name: name, Source: origin.Source, Path: origin.Path, Agents: installed})
		}
		if err = projectstate.Save(m.Root, st); err != nil {
			cleanup()
			return result, err
		}
		result.Installed = append(result.Installed, name)
	}
	return result, nil
}

func unionAgents(a, b []string) []string {
	out := append([]string{}, a...)
	for _, x := range b {
		if !contains(out, x) {
			out = append(out, x)
		}
	}
	return out
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
		for _, agent := range owned(skill) {
			if _, ok := agentDirs[agent]; !ok {
				continue
			}
			dst := filepath.Join(m.skillsDir(agent), skill.Name)
			if _, err := os.Stat(dst); err == nil {
				if backupRoot == "" {
					stateDir := filepath.Join(m.Root, agentDirs[DefaultAgent])
					if err = os.MkdirAll(stateDir, 0755); err != nil {
						return result, err
					}
					backupRoot, err = os.MkdirTemp(stateDir, ".skipack-delete-*")
					if err != nil {
						return result, err
					}
				}
				backup := filepath.Join(backupRoot, agent, skill.Name)
				if err = os.MkdirAll(filepath.Dir(backup), 0755); err == nil {
					err = os.Rename(dst, backup)
				}
				if err != nil {
					rollback()
					return result, err
				}
				moved = append(moved, movedSkill{source: dst, backup: backup})
			} else if !os.IsNotExist(err) {
				rollback()
				return result, err
			}
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

// SetAgents changes the agents of the project. Skills tracked by skipack are
// copied from an existing local copy (no download) into the folders of added
// agents and removed from the folders of dropped agents. Untracked folders are
// never moved or deleted. Everything is checked before any change is made.
func (m Manager) SetAgents(requested []string) (Result, error) {
	var result Result
	agents, err := ValidAgents(requested)
	if err != nil {
		return result, err
	}
	st, err := projectstate.Load(m.Root)
	if err != nil {
		return result, err
	}
	previous := Agents(st)

	type copyOp struct{ name, agent, src, dst string }
	type removeOp struct{ name, agent, path string }
	var copies []copyOp
	var removals []removeOp
	newOwned := make([][]string, len(st.Skills))
	for i, sk := range st.Skills {
		own := owned(sk)
		src := ""
		for _, a := range own {
			if _, ok := agentDirs[a]; !ok {
				continue
			}
			if d := filepath.Join(m.skillsDir(a), sk.Name); isDir(d) {
				src = d
				break
			}
		}
		for _, a := range agents {
			dst := filepath.Join(m.skillsDir(a), sk.Name)
			exists := isDir(dst)
			if contains(own, a) && exists {
				continue
			}
			if exists {
				return result, fmt.Errorf("cannot copy %s to %s: %s already exists and is not tracked by skipack", sk.Name, a, dst)
			}
			if src == "" {
				return result, fmt.Errorf("tracked skill %s has no local copy; run 'skipack add' with the group first", sk.Name)
			}
			copies = append(copies, copyOp{sk.Name, a, src, dst})
		}
		for _, a := range own {
			if _, ok := agentDirs[a]; ok && !contains(agents, a) {
				if p := filepath.Join(m.skillsDir(a), sk.Name); isDir(p) {
					removals = append(removals, removeOp{sk.Name, a, p})
				}
			}
		}
		newOwned[i] = append([]string{}, agents...)
	}

	// Stage every copy first: a failure here leaves the project untouched.
	type staged struct {
		op    copyOp
		stage string
	}
	var stages []staged
	discard := func() {
		for _, s := range stages {
			os.RemoveAll(s.stage)
		}
	}
	for _, op := range copies {
		stage, err := stageCopy(op.src, m.skillsDir(op.agent), ".skipack-copy-*")
		if err != nil {
			discard()
			return result, fmt.Errorf("copy %s to %s: %w", op.name, op.agent, err)
		}
		stages = append(stages, staged{op, stage})
	}

	var created []string
	backupRoot := ""
	type movedSkill struct{ source, backup string }
	var moved []movedSkill
	rollback := func() {
		for _, c := range created {
			os.RemoveAll(c)
		}
		for i := len(moved) - 1; i >= 0; i-- {
			_ = os.Rename(moved[i].backup, moved[i].source)
		}
		if backupRoot != "" {
			_ = os.RemoveAll(backupRoot)
		}
		discard()
	}
	for i, s := range stages {
		if err := os.Rename(s.stage, s.op.dst); err != nil {
			stages = stages[i:]
			rollback()
			return result, err
		}
		created = append(created, s.op.dst)
	}
	stages = nil
	if len(removals) > 0 {
		stateDir := filepath.Join(m.Root, agentDirs[DefaultAgent])
		if err = os.MkdirAll(stateDir, 0755); err == nil {
			backupRoot, err = os.MkdirTemp(stateDir, ".skipack-agent-*")
		}
		if err != nil {
			rollback()
			return result, err
		}
		for _, r := range removals {
			backup := filepath.Join(backupRoot, r.agent, r.name)
			if err = os.MkdirAll(filepath.Dir(backup), 0755); err == nil {
				err = os.Rename(r.path, backup)
			}
			if err != nil {
				rollback()
				return result, err
			}
			moved = append(moved, movedSkill{r.path, backup})
		}
	}
	for i := range st.Skills {
		st.Skills[i].Agents = newOwned[i]
	}
	st.Agents = agents
	if err = projectstate.Save(m.Root, st); err != nil {
		rollback()
		return result, err
	}
	if backupRoot != "" {
		_ = os.RemoveAll(backupRoot)
	}
	for _, c := range copies {
		result.Copied = append(result.Copied, c.agent+"/"+c.name)
	}
	for _, r := range removals {
		result.Removed = append(result.Removed, r.agent+"/"+r.name)
	}
	for _, a := range previous {
		if contains(agents, a) {
			continue
		}
		entries, err := os.ReadDir(m.skillsDir(a))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				result.Left = append(result.Left, a+"/"+e.Name())
			}
		}
	}
	return result, nil
}

func (m Manager) Update() (UpdateResult, error) {
	var result UpdateResult
	st, err := projectstate.Load(m.Root)
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
		}
		os.RemoveAll(tmp)
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
