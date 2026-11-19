package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	projectstate "github.com/CultureLinux/skipack/internal/state"
)

// DefaultAgent is used when a project has not selected any agent yet.
const DefaultAgent = "codex"

// agentDirs maps each supported agent to the hidden directory whose "skills"
// subfolder it reads. Only that subfolder is ever managed by skipack.
var agentDirs = map[string]string{"codex": ".agents", "claude": ".claude"}

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
func unionAgents(a, b []string) []string {
	out := append([]string{}, a...)
	for _, x := range b {
		if !contains(out, x) {
			out = append(out, x)
		}
	}
	return out
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
	st, unlock, err := m.begin()
	defer unlock()
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
	backup := m.newBackup(".skipack-agent-*")
	rollback := func() {
		for _, c := range created {
			os.RemoveAll(c)
		}
		backup.rollback()
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
	for _, r := range removals {
		if err = backup.move(r.agent, r.name, r.path); err != nil {
			rollback()
			return result, err
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
	_ = backup.discard()
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
