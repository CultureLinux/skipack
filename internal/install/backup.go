package install

import (
	"os"
	"path/filepath"
)

type movedPath struct{ source, backup string }

// backupMover parks folders in a temporary backup root so an operation can be
// undone, and so recover can put them back after a crash. The root is created
// on the first move, next to the project state.
type backupMover struct {
	stateDir string
	pattern  string
	root     string
	moved    []movedPath
}

func (m Manager) newBackup(pattern string) *backupMover {
	return &backupMover{stateDir: filepath.Join(m.Root, agentDirs[DefaultAgent]), pattern: pattern}
}

func (b *backupMover) move(agent, name, source string) error {
	if b.root == "" {
		if err := os.MkdirAll(b.stateDir, 0755); err != nil {
			return err
		}
		root, err := os.MkdirTemp(b.stateDir, b.pattern)
		if err != nil {
			return err
		}
		b.root = root
	}
	backup := filepath.Join(b.root, agent, name)
	if err := os.MkdirAll(filepath.Dir(backup), 0755); err != nil {
		return err
	}
	if err := os.Rename(source, backup); err != nil {
		return err
	}
	b.moved = append(b.moved, movedPath{source: source, backup: backup})
	return nil
}

// rollback puts every parked folder back and removes the backup root.
func (b *backupMover) rollback() {
	for i := len(b.moved) - 1; i >= 0; i-- {
		_ = os.Rename(b.moved[i].backup, b.moved[i].source)
	}
	b.moved = nil
	_ = b.discard()
}

// discard removes the backup root once the operation is committed.
func (b *backupMover) discard() error {
	if b.root == "" {
		return nil
	}
	err := os.RemoveAll(b.root)
	b.root = ""
	return err
}
