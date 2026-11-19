package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Skill struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Path   string `json:"path"`
	Ref    string `json:"ref,omitempty"`
	// Commit is the commit the installed copy came from, when known.
	Commit string `json:"commit,omitempty"`
	// Agents lists where skipack installed this skill; empty means "codex" (legacy).
	Agents []string `json:"agents,omitempty"`
}
type File struct {
	Version int `json:"version"`
	// Agents lists the configured agents; empty means "codex" (legacy).
	Agents []string `json:"agents,omitempty"`
	Skills []Skill  `json:"skills"`
}

func path(root string) string { return filepath.Join(root, ".agents", "skipack-state.json") }
func Load(root string) (File, error) {
	b, err := os.ReadFile(path(root))
	if err != nil {
		if os.IsNotExist(err) {
			return File{Version: 1, Skills: []Skill{}}, nil
		}
		return File{}, err
	}
	var f File
	if err = json.Unmarshal(b, &f); err != nil {
		return f, err
	}
	if f.Version != 1 {
		return f, errors.New("unsupported state version")
	}
	return f, nil
}
func Save(root string, f File) error {
	f.Version = 1
	if f.Skills == nil {
		f.Skills = []Skill{}
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Join(root, ".agents")
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "skipack-state-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0644); err == nil {
		_, err = tmp.Write(b)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path(root))
}

const staleLockAge = 10 * time.Minute

// Lock prevents two skipack processes from changing the same project at once.
// A lock file older than ten minutes is treated as left behind by a crash.
func Lock(root string) (func(), error) {
	dir := filepath.Join(root, ".agents")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	lock := filepath.Join(dir, "skipack.lock")
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return func() { _ = os.Remove(lock) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		info, statErr := os.Stat(lock)
		if statErr == nil && time.Since(info.ModTime()) > staleLockAge {
			_ = os.Remove(lock)
			continue
		}
		return nil, fmt.Errorf("another skipack process is running in this project (remove %s if it is stale)", lock)
	}
	return nil, errors.New("cannot acquire the project lock")
}
