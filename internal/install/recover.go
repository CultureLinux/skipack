package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	projectstate "github.com/CultureLinux/skipack/internal/state"
)

// LoadState loads the project state and rejects agents skipack does not know,
// so a hand-edited state can never redirect writes outside the agent folders.
func LoadState(root string) (projectstate.File, error) {
	st, err := projectstate.Load(root)
	if err != nil {
		return st, err
	}
	check := func(agents []string) error {
		for _, a := range agents {
			if _, ok := agentDirs[a]; !ok {
				return fmt.Errorf("state lists unknown agent %q (expected codex or claude)", a)
			}
		}
		return nil
	}
	if err = check(st.Agents); err != nil {
		return st, err
	}
	for _, s := range st.Skills {
		if err = check(s.Agents); err != nil {
			return st, err
		}
	}
	return st, nil
}

// begin locks the project, loads its state and repairs what an interrupted
// run left behind. The returned unlock function is always safe to call.
func (m Manager) begin() (projectstate.File, func(), error) {
	unlock, err := projectstate.Lock(m.Root)
	if err != nil {
		return projectstate.File{}, func() {}, err
	}
	st, err := LoadState(m.Root)
	if err != nil {
		unlock()
		return st, func() {}, err
	}
	if err = m.recover(st); err != nil {
		unlock()
		return st, func() {}, fmt.Errorf("recover interrupted run: %w", err)
	}
	return st, unlock, nil
}

func tracks(st projectstate.File, agent, name string) bool {
	for _, s := range st.Skills {
		if s.Name == name && contains(owned(s), agent) {
			return true
		}
	}
	return false
}

// recover restores or removes folders left by a crashed update, delete or agent
// change. A tracked skill is never lost: its backup is put back when its folder
// is missing.
func (m Manager) recover(st projectstate.File) error {
	for agent := range agentDirs {
		dir := m.skillsDir(agent)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, e := range entries {
			name := e.Name()
			path := filepath.Join(dir, name)
			switch {
			case strings.HasSuffix(name, ".skipack-backup"):
				orig := filepath.Join(dir, strings.TrimSuffix(name, ".skipack-backup"))
				if _, err := os.Stat(orig); os.IsNotExist(err) {
					if err := os.Rename(path, orig); err != nil {
						return err
					}
				} else if err := os.RemoveAll(path); err != nil {
					return err
				}
			case strings.HasPrefix(name, ".skipack-") && e.IsDir():
				if err := os.RemoveAll(path); err != nil {
					return err
				}
			}
		}
	}
	stateDir := filepath.Join(m.Root, agentDirs[DefaultAgent])
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || !(strings.HasPrefix(e.Name(), ".skipack-delete-") || strings.HasPrefix(e.Name(), ".skipack-agent-")) {
			continue
		}
		root := filepath.Join(stateDir, e.Name())
		agents, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		for _, a := range agents {
			if _, ok := agentDirs[a.Name()]; !ok {
				continue
			}
			skills, err := os.ReadDir(filepath.Join(root, a.Name()))
			if err != nil {
				return err
			}
			for _, s := range skills {
				dst := filepath.Join(m.skillsDir(a.Name()), s.Name())
				if _, err := os.Stat(dst); os.IsNotExist(err) && tracks(st, a.Name(), s.Name()) {
					if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
						return err
					}
					if err := os.Rename(filepath.Join(root, a.Name(), s.Name()), dst); err != nil {
						return err
					}
				}
			}
		}
		if err := os.RemoveAll(root); err != nil {
			return err
		}
	}
	return nil
}
